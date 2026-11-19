//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// The flags of an io.
const (
	ioAppend   = 1
	ioNonblock = 2
)

// remoteNode is what the kernel keeps of an open file of a server: the
// end of its pair, its flags and position, and what its server told of
// it. Each kind of open file of a server, remoteFile, remoteTerminal
// (remote_terminal.go), remoteDir and remotePath, has one and answers its
// calls with it, each call by a method of its own; a node is no object.
type remoteNode struct {
	end  *pairEnd
	refs atomic.Int32
	// connecting are who asked the server for a connection and has no
	// answer yet, the first to ask first, with mu; gone is set once the
	// server's end closed. watchers are the subscriptions of a file whose
	// server tells its readiness.
	mu         sync.Mutex
	connecting []*connector
	gone       bool
	watchers   subscribers
	// accmode is the O_ACCMODE it was opened with, append its O_APPEND,
	// which F_SETFL sets as well as O_NONBLOCK.
	accmode  int
	append   atomic.Bool
	nonblock atomic.Bool
	// pos is the position of read, write and seek; after the position of
	// the last entry readdir gave.
	pos   int64
	after uint64
	// tells is set for a file whose server tells its readiness, READY on
	// its endpoint; ready is what the last said, and a terminal's
	// foreground group and TOSTOP.
	tells      bool
	ready      atomic.Int32
	foreground atomic.Int32
	tostop     atomic.Bool
	// locks is where the locks of flock and fcntl on the file are kept,
	// by file, what names it there (lock_table.go): none of a file its
	// server made with no ino. locked is set once it asked for a lock.
	locks  *lockTable
	file   fileKey
	locked atomic.Bool
}

// revive takes a reference to a node that still has one: false for one
// whose last went.
func (n *remoteNode) revive() bool {
	for {
		r := n.refs.Load()

		if r == 0 {
			return false
		}

		if n.refs.CompareAndSwap(r, r+1) {
			return true
		}
	}
}

func (n *remoteNode) ref() {
	n.refs.Add(1)
}

func (n *remoteNode) unref() {
	if n.refs.Add(-1) == 0 {
		if n.locked.Load() {
			n.locks.forget(n)
		}

		n.end.unref()
	}
}

// A file whose server tells its readiness wakes its subscriptions as a
// READY comes; any other is ready whenever asked.
func (n *remoteNode) subscribe(s *subscription, events int16) {
	if n.tells {
		n.mu.Lock()
		n.watchers.add(&s.in)
		n.mu.Unlock()
	}
}

func (n *remoteNode) unsubscribe(s *subscription) {
	if n.tells {
		n.mu.Lock()
		n.watchers.remove(&s.in)
		n.mu.Unlock()
	}
}

func (n *remoteNode) poll(events int16) int16 {
	if !n.tells {
		return events & (pollIn | pollOut | pollRdnorm | pollWrnorm)
	}

	return n.readiness() & (events | pollErr | pollHup)
}

// readiness is what the server said last of the file, the READY that came
// on its endpoint; a server gone is a hangup and an error.
func (n *remoteNode) readiness() int16 {
	return int16(n.ready.Load())
}

// A connector is the place of a process that asked a server to connect,
// among those of the open file in the order of the questions, as the
// answers come: w is the waiter the answer is given to, none once the
// process waits no more, and its answer goes.
type connector struct {
	w *waiter[received]
}

// deliver takes what the server sends on its endpoint of the open file:
// READY, the file's readiness now, which wakes who watches it; and the
// answer to a CONNECT, the first's of those that wait, as the answers
// come in the order of the questions.
func (n *remoteNode) deliver(pk *packet) {
	if len(pk.data) < remoteHeader {
		dropFiles(pk.files)

		return
	}

	switch binary.LittleEndian.Uint32(pk.data) {
	case rReady:
		if n.tells && len(pk.data) >= remoteHeader+8 {
			n.foreground.Store(int32(binary.LittleEndian.Uint32(pk.data[remoteHeader:])))
			n.tostop.Store(binary.LittleEndian.Uint32(pk.data[remoteHeader+4:])&readyTostop != 0)

			events := int16(binary.LittleEndian.Uint32(pk.data[4:])) & (pollIn | pollPri | pollOut | pollErr | pollHup)

			if events&pollIn != 0 {
				events |= pollRdnorm
			}

			if events&pollOut != 0 {
				events |= pollWrnorm
			}

			n.ready.Store(int32(events))
			n.mu.Lock()
			n.watchers.tell()
			n.mu.Unlock()
		}
	case rOk, rErr:
		n.mu.Lock()

		if len(n.connecting) == 0 {
			n.mu.Unlock()

			break
		}

		c := n.connecting[0]
		n.connecting[0] = nil
		n.connecting = n.connecting[1:]
		w := c.w

		if w == nil {
			n.mu.Unlock()

			break
		}

		c.w = nil
		w.wake(received{pk: *pk})
		n.mu.Unlock()

		return
	}

	dropFiles(pk.files)
}

// hungUp hears that the server's end closed: the file hangs up, and who
// waits for a connection reads the end.
func (n *remoteNode) hungUp() {
	n.ready.Store(int32(pollHup | pollErr))
	n.mu.Lock()
	n.gone = true
	waiting := n.connecting
	n.connecting = nil

	for _, c := range waiting {
		if w := c.w; w != nil {
			c.w = nil
			w.wake(received{eof: true})
		}
	}

	n.watchers.tell()
	n.mu.Unlock()
}

// connect is a connection of p's to the open file: CONNECT on its
// endpoint, a question the server answers there with the asking end of a
// call pair it made. The process takes its signals as it waits, and waits
// on. A server that is gone is EIO.
func (n *remoteNode) connect(p *Process) (*callEnd, error) {
	data := make([]byte, remoteHeader)
	p.head(data, rConnect, 0)
	pk := packet{data: data}
	w := p.receives.get(p)
	place := &connector{w: w}

	// However the wait ends, the place has no waiter after it: an answer
	// that comes then goes.
	defer func() {
		n.mu.Lock()
		place.w = nil
		n.mu.Unlock()
		p.receives.put(w)
	}()

	// The question and its place among those that wait, as one: the
	// answers come in the questions' order. A full endpoint is waited
	// for, for room.
	for {
		n.mu.Lock()

		if n.gone {
			n.mu.Unlock()

			return nil, syscall.EIO
		}

		full, err := n.end.to.put(&pk)

		if err == nil && !full {
			n.connecting = append(n.connecting, place)
		}

		n.mu.Unlock()

		if err != nil {
			return nil, syscall.EIO
		}

		if !full {
			break
		}

		n.end.to.writable(p, func() bool { return false }, time.Time{}, waitsOn)
	}

	p.await(&w.done, time.Time{}, waitsOn)
	got := w.answer

	if err := replyOf(&got.pk, got.eof); err != nil {
		return nil, err
	}

	if len(got.pk.files) == 0 {
		return nil, syscall.EIO
	}

	dropFiles(got.pk.files[1:])
	c := got.pk.files[0].asCall()

	if c == nil || c.serves {
		got.pk.files[0].unref()

		return nil, syscall.EIO
	}

	return c, nil
}

func (n *remoteNode) flags() (int, error) {
	fl := n.accmode

	if n.append.Load() {
		fl |= syscall.O_APPEND
	}

	if n.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (n *remoteNode) setFlags(fl int) error {
	n.nonblock.Store(fl&syscall.O_NONBLOCK != 0)
	n.append.Store(fl&syscall.O_APPEND != 0)

	return nil
}

// The connections a process keeps: as many as its calls go round among.
const keptConnections = 8

// A keptConnection is a connection of a process's to an open file, node
// its.
type keptConnection struct {
	node *remoteNode
	c    *callEnd
}

// connected is a connection of p's to the open file for a call: the one
// the process kept, which is the call's alone until it gives it back, or
// a new one.
func (n *remoteNode) connected(p *Process) (*callEnd, error) {
	for i := range p.kept {
		if k := &p.kept[i]; k.node == n {
			c := k.c
			copy(p.kept[i:], p.kept[i+1:])
			p.kept[keptConnections-1] = keptConnection{}

			if c.idle() {
				return c, nil
			}

			// The server let go of it.
			c.unref()

			break
		}
	}

	return n.connect(p)
}

// keep gives the connection of a call that is done back to the process,
// for its next call on the open file, when it takes one: a call left
// unanswered, or a server gone, leaves none to keep. The connection the
// process used longest ago goes for it, and another kept to the same open
// file meanwhile, a handler's.
func (n *remoteNode) keep(p *Process, c *callEnd) {
	if !c.idle() || p.disconnected {
		c.unref()

		return
	}

	last := keptConnections - 1

	for i := range p.kept {
		if p.kept[i].node == n {
			last = i

			break
		}
	}

	if old := p.kept[last].c; old != nil {
		old.unref()
	}

	copy(p.kept[1:last+1], p.kept[:last])
	p.kept[0] = keptConnection{n, c}
}

// waiting is a READ or a WRITE, which may wait at its server as long as
// the file says. A signal the process takes meanwhile sends CANCEL for
// it (takeSignals), and the server's word comes after all the same: the
// count so far, or EINTR. restart says the request was cancelled so and
// no handler the call ran asked not to restart calls: the caller makes
// the call again. A call such a handler interrupted already asks nothing
// more: EINTR. The reply brings no files.
func (n *remoteNode) waiting(p *Process, op, arg uint32, body []byte, files []Object) (pk packet, restart bool, err error) {
	if p.interrupts(true) {
		return packet{}, false, syscall.EINTR
	}

	c, err := n.connected(p)

	if err != nil {
		return packet{}, false, err
	}

	defer n.keep(p, c)

	if p.interrupts(true) {
		return packet{}, false, syscall.EINTR
	}

	data := make([]byte, remoteHeader+len(body))
	p.head(data, op, arg)
	copy(data[remoteHeader:], body)

	w := p.receives.get(p)
	defer c.leave(p, w)

	if err := c.ask(w, data, files); err != nil {
		return packet{}, false, syscall.EIO // the server is gone
	}

	outer, was := p.ask(c)
	defer p.unask(outer, was)

	p.await(&w.done, time.Time{}, waitsOn)
	pk = w.answer.pk
	err = replyOf(&pk, w.answer.eof)
	dropFiles(pk.files)
	pk.files = nil

	return pk, p.cancelled && !p.interrupts(true), err
}

// call is a call on the node: the request sent on a connection to it,
// the reply's packet. files go with the request and stay the caller's.
// Only an OPEN's reply brings a file, the open file's endpoint, and a
// CONNECT's, the listener, which the packet keeps; the files of any other
// reply, or the rest of theirs, go here.
func (n *remoteNode) call(p *Process, op, arg uint32, body []byte, files []Object) (packet, error) {
	data := make([]byte, remoteHeader+len(body))
	p.head(data, op, arg)
	copy(data[remoteHeader:], body)

	return n.asked(p, op, data, files)
}

// asked is call with the request made already, its header and its body
// in data, which the packet keeps.
func (n *remoteNode) asked(p *Process, op uint32, data []byte, files []Object) (packet, error) {
	c, err := n.connected(p)

	if err != nil {
		return packet{}, err
	}

	defer n.keep(p, c)

	// The answer is given to the waiter as the server sends it: the
	// process takes its signals as it waits, and waits on.
	w := p.receives.get(p)
	defer c.leave(p, w)

	if err := c.ask(w, data, files); err != nil {
		return packet{}, syscall.EIO // the server is gone
	}

	outer, was := p.ask(c)
	defer p.unask(outer, was)

	p.await(&w.done, time.Time{}, waitsOn)
	pk := w.answer.pk

	if err := replyOf(&pk, w.answer.eof); err != nil {
		return packet{}, err
	}

	if (op == vOpen || op == vConnect || op == rOpen) && len(pk.files) > 0 {
		dropFiles(pk.files[1:])
		pk.files = pk.files[:1]
	} else {
		dropFiles(pk.files)
		pk.files = nil
	}

	return pk, nil
}

// on is a call on the node that brings no file: the reply's packet.
func (n *remoteNode) on(p *Process, op, arg uint32, body []byte) (packet, error) {
	return n.call(p, op, arg, body, nil)
}

// The attributes of an open file of a server, what its Attrs role asks.

func (n *remoteNode) simple(p *Process, op, arg uint32, body []byte) error {
	_, err := n.on(p, op, arg, body)

	return err
}

func (n *remoteNode) truncate(p *Process, size int64) error {
	var body [8]byte
	binary.LittleEndian.PutUint64(body[:], uint64(size))

	return n.simple(p, rFtruncate, 0, body[:])
}

func (n *remoteNode) sync(p *Process, data bool) error {
	arg := uint32(0)

	if data {
		arg = 1
	}

	return n.simple(p, rFsync, arg, nil)
}

func (n *remoteNode) chmod(p *Process, mode uint32) error {
	return n.simple(p, rFchmod, mode, nil)
}

func (n *remoteNode) chown(p *Process, uid, gid int) error {
	var body [8]byte
	binary.LittleEndian.PutUint32(body[:], uint32(uid))
	binary.LittleEndian.PutUint32(body[4:], uint32(gid))

	return n.simple(p, rFchown, 0, body[:])
}

func (n *remoteNode) utimes(p *Process, ts []syscall.Timespec) error {
	when, body := whens(ts)

	return n.simple(p, rFutimes, when, body)
}

func (n *remoteNode) stat(p *Process, st *syscall.Stat_t) error {
	pk, err := n.on(p, rFstat, 0, nil)

	if err != nil {
		return err
	}

	return decodeStat(pk.data, st)
}

func (n *remoteNode) statfs(p *Process, st *syscall.Statfs_t) error {
	pk, err := n.on(p, rFstatfs, 0, nil)

	if err != nil {
		return err
	}

	return decodeStatfs(pk.data, st)
}

// The locks of an open file of a server, what its Locker role asks, are
// the kernel's (lock_table.go): a file its server made with no ino has
// none, EINVAL.

// flock is flock's lock of the open file, shared, exclusive or none: one
// another open file's is in the way of is EAGAIN with LOCK_NB, and waited
// for without.
func (n *remoteNode) flock(p *Process, op int) error {
	if n.locks == nil {
		return syscall.EINVAL
	}

	switch kind := op &^ syscall.LOCK_NB; kind {
	case syscall.LOCK_UN:
		n.locks.unlock(n, false, 0, 0)

		return nil
	case syscall.LOCK_SH, syscall.LOCK_EX:
		return n.locks.lock(p, &fileLock{owner: n, write: kind == syscall.LOCK_EX}, op&syscall.LOCK_NB == 0)
	}

	return syscall.EINVAL
}

// waits is a call its server may keep for as long as it takes, a
// terminal's drain: a signal the process takes cancels it, and it is made
// again when the handlers ask for that.
func (n *remoteNode) waits(p *Process, op, arg uint32, body []byte) error {
	for {
		_, restart, err := n.waiting(p, op, arg, body, nil)

		if err == syscall.EINTR && restart {
			continue
		}

		return err
	}
}

// recordLock is fcntl's lock on the bytes a struct flock says, from the
// start, the position or the end, before the start for a negative length,
// to the end and past for none: F_OFD_SETLK sets it in place of what the
// open file held of the bytes, EAGAIN for another open file's in the way,
// F_OFD_SETLKW waits for it, F_UNLCK lets the bytes go, and F_OFD_GETLK
// tells the lock in the way, from the start, F_UNLCK for none. A lock to
// read is set on a file open to read, one to write on a file open to
// write, as POSIX has it: EBADF else.
func (n *remoteNode) recordLock(p *Process, cmd int, lk *syscall.Flock_t) error {
	if n.locks == nil {
		return syscall.EINVAL
	}

	start, length := lk.Start, lk.Len

	switch lk.Whence {
	case 0:
	case 1:
		start += atomic.LoadInt64(&n.pos)
	case 2:
		var st syscall.Stat_t

		if err := n.stat(p, &st); err != nil {
			return err
		}

		start += st.Size
	default:
		return syscall.EINVAL
	}

	if length < 0 {
		start, length = start+length, -length
	}

	if start < 0 || lk.Type != fRdlck && lk.Type != fWrlck && lk.Type != fUnlck {
		return syscall.EINVAL
	}

	want := &fileLock{owner: n, ranged: true, write: lk.Type == fWrlck, start: uint64(start), stop: lockPast}

	if length != 0 && want.start <= lockPast-uint64(length) {
		want.stop = want.start + uint64(length)
	}

	switch {
	case cmd != fOfdGetlk && (lk.Type == fRdlck && n.accmode == syscall.O_WRONLY || lk.Type == fWrlck && n.accmode == syscall.O_RDONLY):
		return syscall.EBADF
	case cmd == fOfdGetlk:
		lk.Whence, lk.Pid = 0, -1
		way, found := n.locks.way(want)

		if !found {
			lk.Type, lk.Start, lk.Len = fUnlck, start, length

			return nil
		}

		lk.Type, lk.Start, lk.Len = fRdlck, int64(way.start), 0

		if way.write {
			lk.Type = fWrlck
		}

		if way.stop != lockPast {
			lk.Len = int64(way.stop - way.start)
		}

		return nil
	case lk.Type == fUnlck:
		n.locks.unlock(n, true, want.start, want.stop)

		return nil
	}

	return n.locks.lock(p, want, cmd == fOfdSetlkw)
}

// The reads and writes of an open file of a server, a file's or a
// terminal's.

// readAt reads into b, the process's memory, at off: the READ brings a
// memfd of b.
func (n *remoteNode) readAt(p *Process, b []byte, off int64) (int, error) {
	if off < 0 || len(b) == 0 {
		return n.readInto(p, nil, 0, 0, off)
	}

	w := window(p, b, syscall.O_WRONLY)
	count, err := n.readInto(p, w, 0, len(b), off)
	w.unref()

	return count, err
}

// readInto reads size bytes at off into the memfd w, from its byte woff
// on: the READ brings w, and the server writes the bytes there itself. A
// count past size is the server's lie: EIO.
func (n *remoteNode) readInto(p *Process, w *memfdFile, woff uint32, size int, off int64) (int, error) {
	if off < 0 {
		return 0, syscall.EINVAL
	}

	if size == 0 {
		return 0, nil
	}

	body := ioBody(off, woff, n.ioFlags())

	for {
		pk, cancelled, err := n.waiting(p, rRead, uint32(size), body[:], []Object{w})

		if err == syscall.EINTR && cancelled && p.interrupted() {
			continue
		}

		if err != nil {
			return 0, err
		}

		if len(pk.data) < 4 || int(binary.LittleEndian.Uint32(pk.data)) > size {
			return 0, syscall.EIO
		}

		return int(binary.LittleEndian.Uint32(pk.data)), nil
	}
}

// ioBody is a READ's or a WRITE's body, an io.
func ioBody(off int64, woff, flags uint32) [16]byte {
	var b [16]byte
	binary.LittleEndian.PutUint64(b[:], uint64(off))
	binary.LittleEndian.PutUint32(b[8:], woff)
	binary.LittleEndian.PutUint32(b[12:], flags)

	return b
}

// ioFlags are the flags of the file's reads and writes, O_NONBLOCK's.
func (n *remoteNode) ioFlags() uint32 {
	if n.nonblock.Load() {
		return ioNonblock
	}

	return 0
}

func (n *remoteNode) writeAt(p *Process, b []byte, off int64) (int, error) {
	count, _, err := n.put(p, b, off, false)

	return count, err
}

// put writes b, the process's memory, at off, or atEnd at the end the
// file has when the server takes the write: the WRITE brings a memfd of b.
func (n *remoteNode) put(p *Process, b []byte, off int64, atEnd bool) (int, int64, error) {
	if off < 0 || len(b) == 0 {
		return n.writeFrom(p, nil, 0, 0, off, atEnd)
	}

	w := window(p, b, syscall.O_RDONLY)
	count, at, err := n.writeFrom(p, w, 0, len(b), off, atEnd)
	w.unref()

	return count, at, err
}

// writeFrom writes size bytes of the memfd w, from its byte woff on, at
// off, or atEnd at the end the file has when the server takes the write:
// the WRITE brings w, and the server reads the bytes from there itself.
// The count and where they went; a count past size is the server's lie:
// EIO.
func (n *remoteNode) writeFrom(p *Process, w *memfdFile, woff uint32, size int, off int64, atEnd bool) (int, int64, error) {
	if off < 0 {
		return 0, 0, syscall.EINVAL
	}

	if size == 0 {
		return 0, off, nil
	}

	flags := n.ioFlags()

	if atEnd {
		flags |= ioAppend
	}

	body := ioBody(off, woff, flags)

	for {
		pk, cancelled, err := n.waiting(p, rWrite, uint32(size), body[:], []Object{w})

		if err == syscall.EINTR && cancelled && p.interrupted() {
			continue
		}

		if err != nil {
			return 0, 0, err
		}

		if len(pk.data) < 12 || int(binary.LittleEndian.Uint32(pk.data)) > size {
			return 0, 0, syscall.EIO
		}

		return int(binary.LittleEndian.Uint32(pk.data)), int64(binary.LittleEndian.Uint64(pk.data[4:])), nil
	}
}

func (n *remoteNode) read(p *Process, b []byte, mode ioMode) (int, error) {
	if p.chaosFail("read") {
		return 0, syscall.EIO
	}

	count, err := n.readAt(p, b, atomic.LoadInt64(&n.pos))
	atomic.AddInt64(&n.pos, int64(count))

	return count, err
}

// write writes at the position, or with O_APPEND at the end, which the
// server finds as it writes and says; the position is after what was
// written. Only an append's place is the server's to say: a server that
// handed the WRITE on, moved, answers with where it went on its device.
func (n *remoteNode) write(p *Process, b []byte, mode ioMode) (int, error) {
	if p.chaosFail("write") {
		return 0, syscall.EIO
	}

	off := atomic.LoadInt64(&n.pos)
	atEnd := n.append.Load()
	count, at, err := n.put(p, b, off, atEnd)

	if err == nil {
		if !atEnd {
			at = off
		}

		atomic.StoreInt64(&n.pos, at+int64(count))
	}

	return count, err
}

func (n *remoteNode) seek(p *Process, off int64, whence int) (int64, error) {
	base := int64(0)

	switch whence {
	case 0:
	case 1:
		base = atomic.LoadInt64(&n.pos)
	case 2:
		var st syscall.Stat_t

		if err := n.stat(p, &st); err != nil {
			return 0, err
		}

		base = st.Size
	default:
		return 0, syscall.EINVAL
	}

	if base+off < 0 {
		return 0, syscall.EINVAL
	}

	atomic.StoreInt64(&n.pos, base+off)

	return base + off, nil
}

// vfs is a call of the VFS's on the handle id, the node's: the root's id,
// then the body, the files with it; the reply's packet.
func (n *remoteNode) vfs(p *Process, id uint64, op, arg uint32, body []byte, files ...Object) (packet, error) {
	data := make([]byte, remoteHeader+8+len(body))
	p.head(data, op, arg)
	binary.LittleEndian.PutUint64(data[remoteHeader:], p.rootID(id))
	copy(data[remoteHeader+8:], body)

	return n.asked(p, op, data, files)
}

// readWindow reads size bytes of the server's file at from, or at its
// position, which moves, into the memfd w from its byte woff on.
func (n *remoteNode) readWindow(p *Process, w *memfdFile, woff uint32, size int, from *int64) (int, error) {
	if from != nil {
		count, err := n.readInto(p, w, woff, size, *from)
		*from += int64(count)

		return count, err
	}

	count, err := n.readInto(p, w, woff, size, atomic.LoadInt64(&n.pos))
	atomic.AddInt64(&n.pos, int64(count))

	return count, err
}

// writeWindow writes size bytes of the memfd w, from its byte woff on,
// into the server's file at to, or at its position, which moves. A file
// open to append is EBADF, as Linux has it.
func (n *remoteNode) writeWindow(p *Process, w *memfdFile, woff uint32, size int, to *int64) (int, error) {
	if n.append.Load() {
		return 0, syscall.EBADF
	}

	if to != nil {
		count, _, err := n.writeFrom(p, w, woff, size, *to, false)
		*to += int64(count)

		return count, err
	}

	count, _, err := n.writeFrom(p, w, woff, size, atomic.LoadInt64(&n.pos), false)
	atomic.AddInt64(&n.pos, int64(count))

	return count, err
}
