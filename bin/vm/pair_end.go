//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
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

func (e *PairEnd) poll(events int16, through []*Epoll) int16 {
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

func (e *PairEnd) send(p *Process, r *SockSend) error {
	if r.to.given {
		return EISCONN
	}

	if e.shared.kind == SOCK_STREAM {
		return e.sendStream(p, r)
	}

	if len(r.pk.data) > packetMax {
		return EMSGSIZE
	}

	if err := e.deliver(p, r.pk, r.mode); err != nil {
		return err
	}

	r.n = len(r.pk.data)

	return nil
}

func (e *PairEnd) deliver(p *Process, pk *Packet, mode IoMode) error {
	if pk.cred == nil && !mode.kernel && p != nil && e.peer.options.passcred.Load() {
		pk.cred = p.ucred()
	}

	for _, f := range pk.files {
		f.ref()
	}

	end, how := time.Time{}, waitsOn

	if !mode.kernel {
		timeout := mode.timeout

		if timeout == nil {
			timeout = &e.options.sndTimeout
		}

		end, how = until(timeout)
	}

	err := e.queueAs(p, pk, !mode.kernel && (mode.nonblock || e.nonblock.Load()), end, how)

	if err != nil {
		dropFiles(pk.files)
	}

	return err
}

func (e *PairEnd) sendStream(p *Process, r *SockSend) error {
	data, files, cred := r.pk.data, r.pk.files, r.pk.cred

	for r.n < len(data) {
		c := min(len(data)-r.n, packetMax)

		if err := e.deliver(p, &Packet{data: data[r.n : r.n+c : r.n+c], files: files, cred: cred}, r.mode); err != nil {
			if err == EPIPE && !r.mode.quiet {
				p.sendSelf(SIGPIPE)
			}

			if r.n > 0 {
				return nil
			}

			return err
		}

		r.n += c
		files, cred = nil, nil
	}

	return nil
}

func totalLength(spans [][]byte) int {
	n := 0

	for _, b := range spans {
		n += len(b)
	}

	return n
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

func (e *PairEnd) queueAs(p *Process, pk *Packet, nonblock bool, end time.Time, how Signalled) error {
	r := e.peer

	r.mu.Lock()

	for {
		full, sink, err := e.offerTo(r, pk)

		if err == nil && full && !nonblock {
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

func (e *PairEnd) take(p *Process, nonblock, peek bool) (*Packet, error) {
	nonblock = nonblock || e.nonblock.Load()

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

func (e *PairEnd) received(r *SockRecv, files []Object, cred *Ucred) {
	r.files = files

	if e.options.passcred.Load() {
		r.cred = cred
	}
}

func (e *PairEnd) recv(p *Process, r *SockRecv) error {
	if e.shared.kind == SOCK_STREAM {
		return e.receiveStream(p, r)
	}

	pk, err := e.take(p, r.flags&MSG_DONTWAIT != 0, r.flags&MSG_PEEK != 0)

	if err != nil || pk == nil {
		return err
	}

	r.n = r.into.put(0, pk.data[:min(len(pk.data), r.want)])
	r.addr = pk.addr

	if r.n < len(pk.data) {
		r.out |= MSG_TRUNC
	}

	if r.flags&MSG_TRUNC != 0 {
		r.n = len(pk.data)
	}

	if r.flags&MSG_PEEK == 0 {
		e.received(r, pk.files, pk.cred)
	}

	return nil
}

func (e *PairEnd) receiveStream(p *Process, r *SockRecv) error {
	nonblock := r.flags&MSG_DONTWAIT != 0 || e.nonblock.Load()
	want := r.want

	if want == 0 {
		return nil
	}

	for {
		n, files, cred, eof, ended := e.streamTake(r.into, r.n, want-r.n, r.flags&MSG_PEEK != 0)

		if r.n == 0 && n > 0 && r.flags&MSG_PEEK == 0 {
			e.received(r, files, cred)
		} else {
			dropFiles(files)
		}

		r.n += n

		if eof || ended || r.n >= want || r.n > 0 && r.flags&MSG_WAITALL == 0 || r.flags&MSG_PEEK != 0 && r.n > 0 {
			return nil
		}

		if nonblock {
			if r.n > 0 {
				return nil
			}

			return EAGAIN
		}

		timedOut, err := e.awaitInput(p)

		if err != nil || timedOut {
			if r.n > 0 {
				return nil
			}

			if err == nil {
				err = EAGAIN
			}

			return err
		}
	}
}

func (e *PairEnd) streamTake(into Into, at, room int, peek bool) (n int, files []Object, cred *Ucred, eof, ended bool) {
	e.mu.Lock()

	defer e.mu.Unlock()

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

		c := into.put(at+n, pk.data[:min(len(pk.data), room-n)])

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

func (e *PairEnd) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	if p.chaosFail("read") {
		return 0, EIO
	}

	r := SockRecv{into: Into{p: p, to: into}, want: n}
	err := e.recv(p, &r)

	dropFiles(r.files)

	return r.n, err
}

func (e *PairEnd) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	if p.chaosFail("write") {
		return 0, EIO
	}

	if e.shared.kind != SOCK_STREAM && n > packetMax {
		return 0, EMSGSIZE
	}

	data := make([]byte, n)
	r := SockSend{pk: &Packet{data: data[:takeFrom(from)(0, data)]}, mode: mode}
	err := e.send(p, &r)

	return r.n, err
}

func takeFrom(from Object) func(at int, b []byte) int {
	return func(at int, b []byte) int {
		n, _ := from.with(int64(at), len(b), false, func(src []byte) (int, error) {
			return copy(b, src), nil
		})

		return n
	}
}

func (e *PairEnd) pread(p *Process, into Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (e *PairEnd) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (e *PairEnd) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (e *PairEnd) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (e *PairEnd) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (e *PairEnd) bind(p *Process, a Address) error {
	return EINVAL
}

func (e *PairEnd) connect(p *Process, a Address) error {
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

func (e *PairEnd) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return e.options.get(p, e.shared.kind, level, name)
}

func (e *PairEnd) setsockopt(level, name uint32, value []byte) error {
	return e.options.set(level, name, value)
}

func senderAddress(path []byte) []byte {
	if len(path) == 0 {
		return nil
	}

	return unixAddress(path)
}

func (e *PairEnd) seek(p *Process, off int64, whence int) (int64, error) {
	return 0, ESPIPE
}

func (e *PairEnd) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (e *PairEnd) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, e.stat(p, r.st)
	case *SockBind:
		return false, e.bind(p, r.addr)
	case *SockConnect:
		return false, e.connect(p, r.addr)
	case *SockListen:
		return false, e.listen(r.backlog)
	case *SockAccept:
		var err error

		r.conn, r.peer, err = e.accept(p, r.flags)

		return false, err
	case *SockSend:
		return false, e.send(p, r)
	case *SockRecv:
		return false, e.recv(p, r)
	case *SockName:
		var err error

		if r.peer {
			r.addr, err = e.peername(p)
		} else {
			r.addr, err = e.sockname(p)
		}

		return false, err
	case *SockGetopt:
		var err error

		r.value, err = e.getsockopt(p, r.level, r.name)

		return false, err
	case *SockSetopt:
		return false, e.setsockopt(r.level, r.name, r.value)
	case *SockShutdown:
		return false, e.shutdown(r.how)
	case *SockType:
		r.kind = e.socketKind()

		return false, nil
	case *SockConsume:
		e.consume(r.sink)

		return false, nil
	case *SockKnock:
		return false, EPROTOTYPE
	}

	return false, errUnserved
}

func (e *PairEnd) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (e *PairEnd) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (e *PairEnd) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
