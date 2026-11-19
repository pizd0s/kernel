//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

type PairState struct {
	ino  uint64
	made time.Time
	kind int
}

type PairEnd struct {
	shared *PairState
	peer   *PairEnd

	refs     atomic.Int32
	nonblock atomic.Bool

	dead    atomic.Bool
	noWrite atomic.Bool
	noRead  atomic.Bool

	mu       sync.Mutex
	in       MsgChannel[*Packet]
	over     bool
	watchers Subscribers
	sink     func(pk *Packet)

	options SockOptions

	making sync.Mutex
	dir    *RemoteDir
}

func newPair(nonblock bool) (*PairEnd, *PairEnd) {
	return newPairOf(SOCK_SEQPACKET, nonblock)
}

func newPairOf(kind int, nonblock bool) (*PairEnd, *PairEnd) {
	s := &PairState{ino: pairInodes.Add(1), made: time.Now(), kind: kind}
	a, b := &PairEnd{shared: s}, &PairEnd{shared: s}

	a.peer, b.peer = b, a

	for _, e := range []*PairEnd{a, b} {
		e.refs.Store(1)
		e.nonblock.Store(nonblock)
	}

	return a, b
}

func (e *PairEnd) ref() {
	e.refs.Add(1)
}

func (e *PairEnd) unref() {
	if e.refs.Add(-1) != 0 {
		return
	}

	var dropped []*Packet

	e.dead.Store(true)
	e.mu.Lock()

	for pk, ok := e.in.take(); ok; pk, ok = e.in.take() {
		if len(pk.files) > 0 {
			dropped = append(dropped, pk)
		}
	}

	e.watchers.tell()
	e.mu.Unlock()

	for _, pk := range dropped {
		dropFiles(pk.files)
	}

	e.peer.ended()
}

func (e *PairEnd) ended() {
	e.mu.Lock()

	sink := e.sink
	first := !e.over && e.shared.kind != SOCK_DGRAM

	if first {
		e.over = true

		if sink == nil {
			e.in.put(&Packet{kind: packetEnd})
		}
	}

	e.watchers.tell()
	e.mu.Unlock()

	if first && sink != nil {
		sink(&Packet{kind: packetEnd})
	}
}

func (e *PairEnd) consume(sink func(pk *Packet)) {
	var queued []*Packet

	e.mu.Lock()

	for pk, ok := e.in.take(); ok; pk, ok = e.in.take() {
		queued = append(queued, pk)
	}

	e.sink = sink
	e.mu.Unlock()

	for _, pk := range queued {
		sink(pk)
	}
}

func (e *PairEnd) asFile() File {
	return e
}

func (e *PairEnd) asTransfer() Transfer {
	return e
}

func (e *PairEnd) asSeeker() Seeker {
	return nil
}

func (e *PairEnd) asDir() Dir {
	return nil
}

func (e *PairEnd) asTerminal() Terminal {
	return nil
}

func (e *PairEnd) asWindowed() Windowed {
	return nil
}

func (e *PairEnd) asLink() Link {
	return nil
}

func (e *PairEnd) asAttrs() Attrs {
	return nil
}

func (e *PairEnd) asSocket() Socket {
	return e
}

func (e *PairEnd) asIoctl() Ioctl {
	return nil
}

func (e *PairEnd) asLocker() Locker {
	return nil
}

func (e *PairEnd) asEpoll() *Epoll {
	return nil
}

func (e *PairEnd) asMemfd() *MemfdFile {
	return nil
}

func (e *PairEnd) asPair() *PairEnd {
	return e
}

func (e *PairEnd) asListener() *UnixListener {
	return nil
}

func (e *PairEnd) asConsole() *ConsoleEnd {
	return nil
}

func (e *PairEnd) asVFS() *RemoteDir {
	return nil
}

func (e *PairEnd) subscribe(s *Subscription, events int16) {
	in, out := ioSides(events)

	if in {
		e.mu.Lock()
		e.watchers.add(&s.in)
		e.mu.Unlock()
	}

	if out {
		r := e.peer

		r.mu.Lock()
		r.watchers.add(&s.out)
		r.mu.Unlock()
	}
}

func (e *PairEnd) unsubscribe(s *Subscription) {
	r := e.peer

	e.mu.Lock()
	e.watchers.remove(&s.in)
	e.mu.Unlock()
	r.mu.Lock()
	r.watchers.remove(&s.out)
	r.mu.Unlock()
}

func (e *PairEnd) stat(p *Process, st *Stat) error {
	s := e.shared

	*st = Stat{Ino: s.ino, Mode: S_IFSOCK | 0o777, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = nsecToTimespec(s.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (e *PairEnd) poll(events int16) int16 {
	mask := int16(0)
	dgram := e.shared.kind == SOCK_DGRAM
	r := e.peer
	gone := r.dead.Load()

	if events&(POLLIN|POLLRDNORM) != 0 {
		e.mu.Lock()

		queued := e.in.len()

		e.mu.Unlock()

		if queued > 0 || e.noRead.Load() || !dgram && (gone || r.noWrite.Load()) {
			mask |= POLLIN | POLLRDNORM
		}
	}

	if gone {
		mask |= POLLOUT | POLLWRNORM

		if !dgram {
			mask |= POLLHUP
		}
	} else if events&(POLLOUT|POLLWRNORM) != 0 {
		r.mu.Lock()

		room := r.in.len() < queueMax || r.sink != nil

		r.mu.Unlock()

		if room {
			mask |= POLLOUT | POLLWRNORM
		}
	}

	return mask & (events | POLLERR | POLLHUP)
}

func (e *PairEnd) flags() (int, error) {
	fl := O_RDWR

	if e.nonblock.Load() {
		fl |= O_NONBLOCK
	}

	return fl, nil
}

func (e *PairEnd) setFlags(fl int) error {
	e.nonblock.Store(fl&O_NONBLOCK != 0)

	return nil
}

func (e *PairEnd) send(p *Process, data []byte, files []Object, mode IoMode) error {
	if len(data) > packetMax {
		return EMSGSIZE
	}

	return e.sendCred(p, append([]byte(nil), data...), files, nil, mode)
}

func (e *PairEnd) sendCred(p *Process, data []byte, files []Object, cred *Ucred, mode IoMode) error {
	if len(data) > packetMax {
		return EMSGSIZE
	}

	if cred == nil && p != nil && e.peer.options.passcred.Load() {
		cred = p.ucred()
	}

	for _, f := range files {
		f.ref()
	}

	err := e.queueAs(p, &Packet{data: data, files: files, cred: cred}, mode.nonblock || e.nonblock.Load(), &e.options.sndTimeout)

	if err != nil {
		dropFiles(files)
	}

	return err
}

func (e *PairEnd) sendStream(p *Process, spans [][]byte, files []Object, cred *Ucred, mode IoMode) (int, error) {
	sent := 0

	for len(spans) > 0 {
		chunk := make([]byte, 0, min(totalLength(spans), packetMax))

		for len(spans) > 0 && len(chunk) < cap(chunk) {
			c := min(len(spans[0]), cap(chunk)-len(chunk))

			chunk = append(chunk, spans[0][:c]...)

			if spans[0] = spans[0][c:]; len(spans[0]) == 0 {
				spans = spans[1:]
			}
		}

		if len(chunk) == 0 {
			continue
		}

		if err := e.sendCred(p, chunk, files, cred, mode); err != nil {
			if err == EPIPE && !mode.quiet {
				p.sendSelf(SIGPIPE)
			}

			if sent > 0 {
				return sent, nil
			}

			return 0, err
		}

		sent += len(chunk)
		files, cred = nil, nil
	}

	return sent, nil
}

func totalLength(spans [][]byte) int {
	n := 0

	for _, b := range spans {
		n += len(b)
	}

	return n
}

func (e *PairEnd) offer(pk *Packet) (full bool, err error) {
	r := e.peer

	r.mu.Lock()

	full, sink, err := e.offerTo(r, pk)

	r.mu.Unlock()

	if sink != nil {
		sink(pk)
	}

	return full, err
}

func (e *PairEnd) offerTo(r *PairEnd, pk *Packet) (full bool, sink func(pk *Packet), err error) {
	switch {
	case e.noWrite.Load():
		return false, nil, EPIPE
	case r.dead.Load() && e.shared.kind == SOCK_DGRAM:
		return false, nil, ECONNREFUSED
	case r.dead.Load():
		return false, nil, EPIPE
	case r.sink != nil:
		return false, r.sink, nil
	case r.in.len() >= queueMax:
		return true, nil, nil
	}

	r.in.put(pk)
	r.watchers.tellIn()

	return false, nil, nil
}

func (e *PairEnd) awaitRoom(p *Process) {
	r := e.peer

	r.mu.Lock()

	if r.in.len() >= queueMax && r.sink == nil && !r.dead.Load() {
		p.watch(&r.mu, &r.watchers, true, time.Time{}, waitsOn)
	}

	r.mu.Unlock()
}

func (e *PairEnd) kernelSend(p *Process, pk *Packet) error {
	for _, f := range pk.files {
		f.ref()
	}

	for {
		full, err := e.offer(pk)

		if err != nil {
			dropFiles(pk.files)

			return err
		}

		if !full {
			return nil
		}

		e.awaitRoom(p)
	}
}

func (e *PairEnd) queueAs(p *Process, pk *Packet, nonblock bool, timeout *atomic.Int64) error {
	r := e.peer

	r.mu.Lock()

	for {
		full, sink, err := e.offerTo(r, pk)

		if err == nil && full && !nonblock {
			end, how := until(timeout)

			if timedOut, failed := p.watch(&r.mu, &r.watchers, true, end, how); failed == nil && !timedOut {
				continue
			} else if err = failed; timedOut {
				err = EAGAIN
			}
		} else if full {
			err = EAGAIN
		}

		r.mu.Unlock()

		if sink != nil {
			sink(pk)
		}

		return err
	}
}

func (e *PairEnd) recv(p *Process, mode IoMode, peek bool) (*Packet, error) {
	nonblock := mode.nonblock || e.nonblock.Load()

	e.mu.Lock()

	for {
		pk, ok := e.in.peek()

		switch {
		case e.noRead.Load() || ok && pk.kind == packetEnd:
			e.mu.Unlock()

			return nil, nil
		case ok:
			if !peek {
				e.in.take()

				if e.in.len() == queueMax-1 {
					e.watchers.tellOut()
				}
			}

			e.mu.Unlock()

			return pk, nil
		case nonblock:
			e.mu.Unlock()

			return nil, EAGAIN
		}

		end, how := until(&e.options.rcvTimeout)
		timedOut, err := p.watch(&e.mu, &e.watchers, false, end, how)

		if err == nil && timedOut {
			err = EAGAIN
		}

		if err != nil {
			e.mu.Unlock()

			return nil, err
		}
	}
}

func (e *PairEnd) awaitInput(p *Process) (timedOut bool, err error) {
	e.mu.Lock()

	if e.in.len() == 0 && !e.noRead.Load() {
		end, how := until(&e.options.rcvTimeout)

		timedOut, err = p.watch(&e.mu, &e.watchers, false, end, how)
	}

	e.mu.Unlock()

	return timedOut, err
}

func (e *PairEnd) receive(p *Process, bufs [][]byte, flags uint32, control, room uint32) (int, uint32, uint32, []byte, error) {
	if e.shared.kind == SOCK_STREAM {
		n, written, out, err := e.receiveStream(p, bufs, flags, control, room)

		return n, written, out, nil, err
	}

	mode := IoMode{nonblock: flags&MSG_DONTWAIT != 0}
	pk, err := e.recv(p, mode, flags&MSG_PEEK != 0)

	if err != nil || pk == nil {
		return 0, 0, 0, nil, err
	}

	n, out := 0, uint32(0)
	data := pk.data

	for _, b := range bufs {
		c := copy(b, data)

		data = data[c:]
		n += c
	}

	if len(data) > 0 {
		out |= MSG_TRUNC
	}

	if flags&MSG_TRUNC != 0 {
		n = len(pk.data)
	}

	if flags&MSG_PEEK != 0 {
		return n, 0, out, pk.addr, nil
	}

	written, cut := e.putControl(p, pk.files, pk.cred, control, room, flags&MSG_CMSG_CLOEXEC != 0)

	return n, written, out | cut, pk.addr, nil
}

func (e *PairEnd) receiveStream(p *Process, bufs [][]byte, flags uint32, control, room uint32) (int, uint32, uint32, error) {
	nonblock := flags&MSG_DONTWAIT != 0 || e.nonblock.Load()
	want := totalLength(bufs)
	got := 0

	var written, cut uint32

	if want == 0 {
		return 0, 0, 0, nil
	}

	for {
		n, files, cred, eof, ended := e.streamTake(advance(bufs, got), flags&MSG_PEEK != 0)

		if got == 0 && n > 0 && flags&MSG_PEEK == 0 {
			written, cut = e.putControl(p, files, cred, control, room, flags&MSG_CMSG_CLOEXEC != 0)
		} else {
			dropFiles(files)
		}

		got += n

		if eof || ended || got >= want || got > 0 && flags&MSG_WAITALL == 0 || flags&MSG_PEEK != 0 && got > 0 {
			return got, written, cut, nil
		}

		if nonblock {
			if got > 0 {
				return got, written, cut, nil
			}

			return 0, 0, 0, EAGAIN
		}

		timedOut, err := e.awaitInput(p)

		if err != nil || timedOut {
			if got > 0 {
				return got, written, cut, nil
			}

			if err == nil {
				err = EAGAIN
			}

			return 0, 0, 0, err
		}
	}
}

func (e *PairEnd) streamTake(bufs [][]byte, peek bool) (n int, files []Object, cred *Ucred, eof, ended bool) {
	e.mu.Lock()

	defer e.mu.Unlock()

	room := totalLength(bufs)

	for n < room {
		pk, ok := e.in.peek()

		switch {
		case e.noRead.Load() || ok && pk.kind == packetEnd:
			return n, files, cred, n == 0, false
		case !ok:
			return n, files, cred, false, false
		case n > 0 && len(pk.files) > 0:
			return n, files, cred, false, true
		}

		c := scatterInto(advance(bufs, n), pk.data)

		if peek {
			return n + c, nil, nil, false, true
		}

		carried := len(pk.files) > 0

		if n == 0 {
			files, cred = pk.files, pk.cred
		}

		n += c

		if c < len(pk.data) {
			pk.data, pk.files = pk.data[c:], nil
		} else if e.in.take(); e.in.len() == queueMax-1 {
			e.watchers.tellOut()
		}

		if carried {
			return n, files, cred, false, true
		}
	}

	return n, files, cred, false, false
}

func advance(bufs [][]byte, n int) [][]byte {
	for len(bufs) > 0 && n >= len(bufs[0]) {
		n -= len(bufs[0])
		bufs = bufs[1:]
	}

	if n == 0 {
		return bufs
	}

	return append([][]byte{bufs[0][n:]}, bufs[1:]...)
}

func scatterInto(bufs [][]byte, data []byte) int {
	n := 0

	for _, b := range bufs {
		c := copy(b, data)

		data = data[c:]
		n += c

		if len(data) == 0 {
			break
		}
	}

	return n
}

func (e *PairEnd) putControl(p *Process, files []Object, cred *Ucred, control, room uint32, cloexec bool) (uint32, uint32) {
	written, cut := p.putRights(files, control, room, cloexec)

	if cred == nil || !e.options.passcred.Load() {
		return written, cut
	}

	size := sizeofCmsghdr + uint32(sizeofUcred)

	if room-written < size {
		return written, cut | MSG_CTRUNC
	}

	at := control + written

	p.put32(at, size)
	p.put32(at+4, SOL_SOCKET)
	p.put32(at+8, SCM_CREDENTIALS)
	p.put32(at+12, cred.pid)
	p.put32(at+16, cred.uid)
	p.put32(at+20, cred.gid)

	return written + min(guestAlign(size), room-written), cut
}

func (e *PairEnd) read(p *Process, b []byte, mode IoMode) (int, error) {
	if p.chaosFail("read") {
		return 0, EIO
	}

	n, _, _, _, err := e.receive(p, [][]byte{b}, 0, 0, 0)

	return n, err
}

func (e *PairEnd) write(p *Process, b []byte, mode IoMode) (int, error) {
	if p.chaosFail("write") {
		return 0, EIO
	}

	if e.shared.kind == SOCK_STREAM {
		return e.sendStream(p, [][]byte{b}, nil, nil, mode)
	}

	if err := e.send(p, b, nil, mode); err != nil {
		return 0, err
	}

	return len(b), nil
}

func (e *PairEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (e *PairEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (e *PairEnd) bind(p *Process, at, n uint32) error {
	return EINVAL
}

func (e *PairEnd) connect(p *Process, at, n uint32) error {
	return EISCONN
}

func (e *PairEnd) listen(backlog int) error {
	return EINVAL
}

func (e *PairEnd) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, EINVAL
}

func (e *PairEnd) sockname(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (e *PairEnd) peername(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (e *PairEnd) socketKind() int {
	return e.shared.kind
}

func (e *PairEnd) shutdown(how int) error {
	if how < SHUT_RD || how > SHUT_RDWR {
		return EINVAL
	}

	if how != SHUT_WR {
		e.noRead.Store(true)
		e.mu.Lock()
		e.watchers.tell()
		e.mu.Unlock()
	}

	if how != SHUT_RD {
		e.noWrite.Store(true)
		e.peer.ended()
	}

	return nil
}

func (e *PairEnd) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	return e.options.get(p, e.shared.kind, level, name, at, lenAt)
}

func (e *PairEnd) setsockopt(p *Process, level, name, at, n uint32) error {
	return e.options.set(p, level, name, at, n)
}

func (e *PairEnd) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	if addr != 0 {
		return 0, EISCONN
	}

	mode := IoMode{nonblock: flags&MSG_DONTWAIT != 0, quiet: flags&MSG_NOSIGNAL != 0}

	if e.shared.kind == SOCK_STREAM {
		return e.sendStream(p, [][]byte{p.span(buf, n)}, nil, nil, mode)
	}

	if err := e.send(p, p.span(buf, n), nil, mode); err != nil {
		return 0, err
	}

	return int(n), nil
}

func (e *PairEnd) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	got, _, _, from, err := e.receive(p, [][]byte{p.span(buf, n)}, flags, 0, 0)

	return got, senderAddress(from), err
}

func senderAddress(path []byte) []byte {
	if len(path) == 0 {
		return nil
	}

	return unixAddress(path)
}

func (e *PairEnd) sendmsg(p *Process, at, flags uint32) (int, error) {
	h := p.span(at, sizeofMsghdr)

	if binary.LittleEndian.Uint32(h) != 0 {
		return 0, EISCONN
	}

	files, cred, err := p.rights(binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:]))

	if err != nil {
		return 0, err
	}

	mode := IoMode{nonblock: flags&MSG_DONTWAIT != 0, quiet: flags&MSG_NOSIGNAL != 0}

	if e.shared.kind == SOCK_STREAM {
		spans, err := p.ioSpans(binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:]))

		if err != nil {
			return 0, err
		}

		return e.sendStream(p, spans, files, cred, mode)
	}

	data, err := p.gather(binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:]), packetMax)

	if err != nil {
		return 0, err
	}

	if err := e.sendCred(p, data, files, cred, mode); err != nil {
		return 0, err
	}

	return len(data), nil
}

func (e *PairEnd) recvmsg(p *Process, at, flags uint32) (int, error) {
	var few [4][]byte

	spans, err := p.ioSpansIn(few[:0], p.u32(at+8), p.u32(at+12))

	if err != nil {
		return 0, err
	}

	n, written, out, from, err := e.receive(p, spans, flags, p.u32(at+16), p.u32(at+20))

	if err != nil {
		return 0, err
	}

	if name := p.u32(at); name != 0 {
		addr := senderAddress(from)

		copy(p.span(name, min(p.u32(at+4), uint32(len(addr)))), addr)
		p.put32(at+4, uint32(len(addr)))
	}

	p.put32(at+20, written)
	p.put32(at+24, out|flags&MSG_CMSG_CLOEXEC)

	return n, nil
}

func (e *PairEnd) directory(p *Process) *RemoteDir {
	e.making.Lock()

	defer e.making.Unlock()

	if d := e.dir; d != nil && d.node.revive() {
		return d
	}

	e.ref()

	f, _ := served(p, openedDir, O_RDONLY, 0, 0, 0, e)

	e.dir = f.asVFS()

	return e.dir
}
