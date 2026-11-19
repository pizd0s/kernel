//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ioAppend        = 1
	ioNonblock      = 2
	keptConnections = 8
)

type RemoteNode struct {
	end  *PairEnd
	refs atomic.Int32

	mu         sync.Mutex
	connecting []*Connector
	gone       bool
	watchers   Subscribers

	accmode  int
	append   atomic.Bool
	nonblock atomic.Bool

	pos   int64
	after uint64

	tells      bool
	ready      atomic.Int32
	foreground atomic.Int32
	tostop     atomic.Bool

	locks  *LockTable
	file   FileKey
	locked atomic.Bool
}

func (n *RemoteNode) revive() bool {
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

func (n *RemoteNode) ref() {
	n.refs.Add(1)
}

func (n *RemoteNode) unref() {
	if n.refs.Add(-1) == 0 {
		if n.locked.Load() {
			n.locks.forget(n)
		}

		n.end.unref()
	}
}

func (n *RemoteNode) subscribe(s *Subscription, events int16) {
	if n.tells {
		n.mu.Lock()
		n.watchers.add(&s.in)
		n.mu.Unlock()
	}
}

func (n *RemoteNode) unsubscribe(s *Subscription) {
	if n.tells {
		n.mu.Lock()
		n.watchers.remove(&s.in)
		n.mu.Unlock()
	}
}

func (n *RemoteNode) poll(events int16) int16 {
	if !n.tells {
		return events & (POLLIN | POLLOUT | POLLRDNORM | POLLWRNORM)
	}

	return n.readiness() & (events | POLLERR | POLLHUP)
}

func (n *RemoteNode) readiness() int16 {
	return int16(n.ready.Load())
}

type Connector struct {
	w *Waiter[Received]
}

func (n *RemoteNode) deliver(pk *Packet) {
	if pk.kind == packetEnd {
		n.hungUp()

		return
	}

	if len(pk.data) < remoteHeader {
		dropFiles(pk.files)

		return
	}

	switch binary.LittleEndian.Uint32(pk.data) {
	case rReady:
		if n.tells && len(pk.data) >= remoteHeader+8 {
			n.foreground.Store(int32(binary.LittleEndian.Uint32(pk.data[remoteHeader:])))
			n.tostop.Store(binary.LittleEndian.Uint32(pk.data[remoteHeader+4:])&readyTostop != 0)

			events := int16(binary.LittleEndian.Uint32(pk.data[4:])) & (POLLIN | POLLPRI | POLLOUT | POLLERR | POLLHUP)

			if events&POLLIN != 0 {
				events |= POLLRDNORM
			}

			if events&POLLOUT != 0 {
				events |= POLLWRNORM
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
		w.wake(Received{pk: *pk})
		n.mu.Unlock()

		return
	}

	dropFiles(pk.files)
}

func (n *RemoteNode) hungUp() {
	n.ready.Store(int32(POLLHUP | POLLERR))
	n.mu.Lock()
	n.gone = true

	waiting := n.connecting

	n.connecting = nil

	for _, c := range waiting {
		if w := c.w; w != nil {
			c.w = nil
			w.wake(Received{eof: true})
		}
	}

	n.watchers.tell()
	n.mu.Unlock()
}

func (n *RemoteNode) connect(p *Process) (*CallEnd, error) {
	data := make([]byte, remoteHeader)

	p.head(data, rConnect, 0)

	pk := Packet{data: data}
	w := p.receives.get(p)
	place := &Connector{w: w}

	defer func() {
		n.mu.Lock()
		place.w = nil
		n.mu.Unlock()
		p.receives.put(w)
	}()

	for {
		n.mu.Lock()

		if n.gone {
			n.mu.Unlock()

			return nil, EIO
		}

		full, err := n.end.offer(&pk)

		if err == nil && !full {
			n.connecting = append(n.connecting, place)
		}

		n.mu.Unlock()

		if err != nil {
			return nil, EIO
		}

		if !full {
			break
		}

		n.end.awaitRoom(p)
	}

	p.await(&w.done, time.Time{}, waitsOn)

	got := w.answer

	if err := replyOf(&got.pk, got.eof); err != nil {
		return nil, err
	}

	if len(got.pk.files) == 0 {
		return nil, EIO
	}

	dropFiles(got.pk.files[1:])

	c := got.pk.files[0].asCall()

	if c == nil || c.serves {
		got.pk.files[0].unref()

		return nil, EIO
	}

	return c, nil
}

func (n *RemoteNode) flags() (int, error) {
	fl := n.accmode

	if n.append.Load() {
		fl |= O_APPEND
	}

	if n.nonblock.Load() {
		fl |= O_NONBLOCK
	}

	return fl, nil
}

func (n *RemoteNode) setFlags(fl int) error {
	n.nonblock.Store(fl&O_NONBLOCK != 0)
	n.append.Store(fl&O_APPEND != 0)

	return nil
}

type KeptConnection struct {
	node *RemoteNode
	c    *CallEnd
}

func (n *RemoteNode) connected(p *Process) (*CallEnd, error) {
	for i := range p.kept {
		if k := &p.kept[i]; k.node == n {
			c := k.c

			copy(p.kept[i:], p.kept[i+1:])
			p.kept[keptConnections-1] = KeptConnection{}

			if c.idle() {
				return c, nil
			}

			c.unref()

			break
		}
	}

	return n.connect(p)
}

func (n *RemoteNode) keep(p *Process, c *CallEnd) {
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
	p.kept[0] = KeptConnection{n, c}
}

func (n *RemoteNode) waiting(p *Process, op, arg uint32, body []byte, files []Object) (pk Packet, restart bool, err error) {
	if p.interrupts(true) {
		return Packet{}, false, EINTR
	}

	c, err := n.connected(p)

	if err != nil {
		return Packet{}, false, err
	}

	defer n.keep(p, c)

	if p.interrupts(true) {
		return Packet{}, false, EINTR
	}

	data := make([]byte, remoteHeader+len(body))

	p.head(data, op, arg)
	copy(data[remoteHeader:], body)

	w := p.receives.get(p)

	defer c.leave(p, w)

	if err := c.ask(w, data, files); err != nil {
		return Packet{}, false, EIO
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

func (n *RemoteNode) call(p *Process, op, arg uint32, body []byte, files []Object) (Packet, error) {
	data := make([]byte, remoteHeader+len(body))

	p.head(data, op, arg)
	copy(data[remoteHeader:], body)

	return n.asked(p, op, data, files)
}

func (n *RemoteNode) asked(p *Process, op uint32, data []byte, files []Object) (Packet, error) {
	c, err := n.connected(p)

	if err != nil {
		return Packet{}, err
	}

	defer n.keep(p, c)

	w := p.receives.get(p)

	defer c.leave(p, w)

	if err := c.ask(w, data, files); err != nil {
		return Packet{}, EIO
	}

	outer, was := p.ask(c)

	defer p.unask(outer, was)

	p.await(&w.done, time.Time{}, waitsOn)

	pk := w.answer.pk

	if err := replyOf(&pk, w.answer.eof); err != nil {
		return Packet{}, err
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

func (n *RemoteNode) on(p *Process, op, arg uint32, body []byte) (Packet, error) {
	return n.call(p, op, arg, body, nil)
}

func (n *RemoteNode) simple(p *Process, op, arg uint32, body []byte) error {
	_, err := n.on(p, op, arg, body)

	return err
}

func (n *RemoteNode) truncate(p *Process, size int64) error {
	var body [8]byte
	binary.LittleEndian.PutUint64(body[:], uint64(size))

	return n.simple(p, rFtruncate, 0, body[:])
}

func (n *RemoteNode) sync(p *Process, data bool) error {
	arg := uint32(0)

	if data {
		arg = 1
	}

	return n.simple(p, rFsync, arg, nil)
}

func (n *RemoteNode) chmod(p *Process, mode uint32) error {
	return n.simple(p, rFchmod, mode, nil)
}

func (n *RemoteNode) chown(p *Process, uid, gid int) error {
	var body [8]byte
	binary.LittleEndian.PutUint32(body[:], uint32(uid))
	binary.LittleEndian.PutUint32(body[4:], uint32(gid))

	return n.simple(p, rFchown, 0, body[:])
}

func (n *RemoteNode) utimes(p *Process, ts []Timespec) error {
	when, body := whens(ts)

	return n.simple(p, rFutimes, when, body)
}

func (n *RemoteNode) stat(p *Process, st *Stat) error {
	pk, err := n.on(p, rFstat, 0, nil)

	if err != nil {
		return err
	}

	return decodeStat(pk.data, st)
}

func (n *RemoteNode) statfs(p *Process, st *Statfs) error {
	pk, err := n.on(p, rFstatfs, 0, nil)

	if err != nil {
		return err
	}

	return decodeStatfs(pk.data, st)
}

func (n *RemoteNode) flock(p *Process, op int) error {
	if n.locks == nil {
		return EINVAL
	}

	switch kind := op &^ LOCK_NB; kind {
	case LOCK_UN:
		n.locks.unlock(n, false, 0, 0)

		return nil
	case LOCK_SH, LOCK_EX:
		return n.locks.lock(p, &FileLock{owner: n, write: kind == LOCK_EX}, op&LOCK_NB == 0)
	}

	return EINVAL
}

func (n *RemoteNode) waits(p *Process, op, arg uint32, body []byte) error {
	for {
		_, restart, err := n.waiting(p, op, arg, body, nil)

		if err == EINTR && restart {
			continue
		}

		return err
	}
}

func (n *RemoteNode) recordLock(p *Process, cmd int, lk *Flock) error {
	if n.locks == nil {
		return EINVAL
	}

	start, length := lk.Start, lk.Len

	switch lk.Whence {
	case 0:
	case 1:
		start += atomic.LoadInt64(&n.pos)
	case 2:
		var st Stat

		if err := n.stat(p, &st); err != nil {
			return err
		}

		start += st.Size
	default:
		return EINVAL
	}

	if length < 0 {
		start, length = start+length, -length
	}

	if start < 0 || lk.Type != F_RDLCK && lk.Type != F_WRLCK && lk.Type != F_UNLCK {
		return EINVAL
	}

	want := &FileLock{owner: n, ranged: true, write: lk.Type == F_WRLCK, start: uint64(start), stop: lockPast}

	if length != 0 && want.start <= lockPast-uint64(length) {
		want.stop = want.start + uint64(length)
	}

	switch {
	case cmd != fOfdGetlk && (lk.Type == F_RDLCK && n.accmode == O_WRONLY || lk.Type == F_WRLCK && n.accmode == O_RDONLY):
		return EBADF
	case cmd == fOfdGetlk:
		lk.Whence, lk.Pid = 0, -1

		way, found := n.locks.way(want)

		if !found {
			lk.Type, lk.Start, lk.Len = F_UNLCK, start, length

			return nil
		}

		lk.Type, lk.Start, lk.Len = F_RDLCK, int64(way.start), 0

		if way.write {
			lk.Type = F_WRLCK
		}

		if way.stop != lockPast {
			lk.Len = int64(way.stop - way.start)
		}

		return nil
	case lk.Type == F_UNLCK:
		n.locks.unlock(n, true, want.start, want.stop)

		return nil
	}

	return n.locks.lock(p, want, cmd == fOfdSetlkw)
}

func (n *RemoteNode) readAt(p *Process, b []byte, off int64) (int, error) {
	if off < 0 || len(b) == 0 {
		return n.readInto(p, nil, 0, 0, off)
	}

	w := window(p, b, O_WRONLY)
	count, err := n.readInto(p, w, 0, len(b), off)

	w.unref()

	return count, err
}

func (n *RemoteNode) readInto(p *Process, w *MemfdFile, woff uint32, size int, off int64) (int, error) {
	if off < 0 {
		return 0, EINVAL
	}

	if size == 0 {
		return 0, nil
	}

	body := ioBody(off, woff, n.ioFlags())

	for {
		pk, cancelled, err := n.waiting(p, rRead, uint32(size), body[:], []Object{w})

		if err == EINTR && cancelled && p.interrupted() {
			continue
		}

		if err != nil {
			return 0, err
		}

		if len(pk.data) < 4 || int(binary.LittleEndian.Uint32(pk.data)) > size {
			return 0, EIO
		}

		return int(binary.LittleEndian.Uint32(pk.data)), nil
	}
}

func ioBody(off int64, woff, flags uint32) [16]byte {
	var b [16]byte
	binary.LittleEndian.PutUint64(b[:], uint64(off))
	binary.LittleEndian.PutUint32(b[8:], woff)
	binary.LittleEndian.PutUint32(b[12:], flags)

	return b
}

func (n *RemoteNode) ioFlags() uint32 {
	if n.nonblock.Load() {
		return ioNonblock
	}

	return 0
}

func (n *RemoteNode) writeAt(p *Process, b []byte, off int64) (int, error) {
	count, _, err := n.put(p, b, off, false)

	return count, err
}

func (n *RemoteNode) put(p *Process, b []byte, off int64, atEnd bool) (int, int64, error) {
	if off < 0 || len(b) == 0 {
		return n.writeFrom(p, nil, 0, 0, off, atEnd)
	}

	w := window(p, b, O_RDONLY)
	count, at, err := n.writeFrom(p, w, 0, len(b), off, atEnd)

	w.unref()

	return count, at, err
}

func (n *RemoteNode) writeFrom(p *Process, w *MemfdFile, woff uint32, size int, off int64, atEnd bool) (int, int64, error) {
	if off < 0 {
		return 0, 0, EINVAL
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

		if err == EINTR && cancelled && p.interrupted() {
			continue
		}

		if err != nil {
			return 0, 0, err
		}

		if len(pk.data) < 12 || int(binary.LittleEndian.Uint32(pk.data)) > size {
			return 0, 0, EIO
		}

		return int(binary.LittleEndian.Uint32(pk.data)), int64(binary.LittleEndian.Uint64(pk.data[4:])), nil
	}
}

func (n *RemoteNode) read(p *Process, b []byte, mode IoMode) (int, error) {
	if p.chaosFail("read") {
		return 0, EIO
	}

	count, err := n.readAt(p, b, atomic.LoadInt64(&n.pos))

	atomic.AddInt64(&n.pos, int64(count))

	return count, err
}

func (n *RemoteNode) write(p *Process, b []byte, mode IoMode) (int, error) {
	if p.chaosFail("write") {
		return 0, EIO
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

func (n *RemoteNode) seek(p *Process, off int64, whence int) (int64, error) {
	base := int64(0)

	switch whence {
	case 0:
	case 1:
		base = atomic.LoadInt64(&n.pos)
	case 2:
		var st Stat

		if err := n.stat(p, &st); err != nil {
			return 0, err
		}

		base = st.Size
	default:
		return 0, EINVAL
	}

	if base+off < 0 {
		return 0, EINVAL
	}

	atomic.StoreInt64(&n.pos, base+off)

	return base + off, nil
}

func (n *RemoteNode) vfs(p *Process, id uint64, op, arg uint32, body []byte, files ...Object) (Packet, error) {
	data := make([]byte, remoteHeader+8+len(body))

	p.head(data, op, arg)
	binary.LittleEndian.PutUint64(data[remoteHeader:], p.rootID(id))
	copy(data[remoteHeader+8:], body)

	return n.asked(p, op, data, files)
}

func (n *RemoteNode) readWindow(p *Process, w *MemfdFile, woff uint32, size int, from *int64) (int, error) {
	if from != nil {
		count, err := n.readInto(p, w, woff, size, *from)

		*from += int64(count)

		return count, err
	}

	count, err := n.readInto(p, w, woff, size, atomic.LoadInt64(&n.pos))

	atomic.AddInt64(&n.pos, int64(count))

	return count, err
}

func (n *RemoteNode) writeWindow(p *Process, w *MemfdFile, woff uint32, size int, to *int64) (int, error) {
	if n.append.Load() {
		return 0, EBADF
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
