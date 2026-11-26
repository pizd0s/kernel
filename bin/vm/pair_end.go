package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

type PairState struct {
	ObjectIdent

	ino  uint64
	made time.Time
	kind int
}

type PairEnd struct {
	shared *PairState
	peer   *PairEnd

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

func newPair() (*PairEnd, *PairEnd) {
	return newPairOf(SOCK_SEQPACKET)
}

func newPairOf(kind int) (*PairEnd, *PairEnd) {
	s := &PairState{ObjectIdent: newObjectIdent(), ino: pairInodes.Add(1), made: time.Now(), kind: kind}
	a, b := &PairEnd{shared: s}, &PairEnd{shared: s}

	a.peer, b.peer = b, a

	return a, b
}

func (e *PairEnd) closed(d *Description) {
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

func (e *PairEnd) subscribe(d *Description, s *Subscription, events int16) {
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

func (e *PairEnd) unsubscribe(d *Description, s *Subscription) {
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

func (e *PairEnd) poll(d *Description, events int16, through []*Epoll) int16 {
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

func (e *PairEnd) send(p *Process, r *FileWrite) error {
	if len(r.to) > 0 && e.shared.kind == SOCK_DGRAM {
		return EISCONN
	}

	if e.shared.kind != SOCK_STREAM && r.n > packetMax {
		return EMSGSIZE
	}

	pk := r.pk

	if pk == nil {
		pk = p.packet()

		buf := pk.data[:cap(pk.data)]

		if r.n > len(buf) {
			buf = make([]byte, r.n)
		}

		pk.data = buf[:r.from.memory().take(0, buf[:r.n])]
	}

	r.pk = nil
	pk.files = append(pk.files[:0], r.files...)

	mode := r.mode

	mode.nonblock = mode.nonblock || r.flags&MSG_DONTWAIT != 0
	mode.quiet = mode.quiet || r.flags&MSG_NOSIGNAL != 0

	if e.shared.kind == SOCK_STREAM {
		return e.sendStream(p, r, pk, mode)
	}

	if err := e.deliver(p, pk, mode); err != nil {
		return err
	}

	r.count = len(pk.data)

	return nil
}

func (e *PairEnd) deliver(p *Process, pk *Packet, mode IoMode) error {
	holdFiles(pk.files)

	end, how := time.Time{}, waitsOn

	if !mode.kernel {
		timeout := mode.timeout

		if timeout == nil {
			timeout = &e.options.sndTimeout
		}

		end, how = until(timeout)
	}

	err := e.queueAs(p, pk, !mode.kernel && mode.nonblock, end, how)

	if err != nil {
		dropFiles(pk.files)
	}

	return err
}

func (e *PairEnd) sendStream(p *Process, r *FileWrite, pk *Packet, mode IoMode) error {
	data, files := pk.data, pk.files

	for r.count < len(data) {
		c := min(len(data)-r.count, packetMax)

		if err := e.deliver(p, &Packet{data: data[r.count : r.count+c : r.count+c], files: files}, mode); err != nil {
			if err == EPIPE && !mode.quiet {
				p.sendSelf(SIGPIPE)
			}

			if r.count > 0 {
				return nil
			}

			return err
		}

		r.count += c
		files = nil
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

func (e *PairEnd) recv(p *Process, r *FileRead) error {
	into, flags := r.into.memory(), r.flags|bit(r.mode.nonblock)*MSG_DONTWAIT

	if e.shared.kind == SOCK_STREAM {
		return e.receiveStream(p, r, into, flags)
	}

	pk, err := e.take(p, flags&MSG_DONTWAIT != 0, flags&MSG_PEEK != 0)

	if err != nil || pk == nil {
		return err
	}

	r.count = into.put(0, pk.data[:min(len(pk.data), r.n)])

	if r.count < len(pk.data) {
		r.out |= MSG_TRUNC
	}

	if flags&MSG_TRUNC != 0 {
		r.count = len(pk.data)
	}

	if flags&MSG_PEEK == 0 {
		r.files, r.pk = pk.files, pk
	}

	return nil
}

func (e *PairEnd) receiveStream(p *Process, r *FileRead, into *MemfdFile, flags uint32) error {
	if r.n == 0 {
		return nil
	}

	for {
		n, files, eof, ended := e.streamTake(into, r.count, r.n-r.count, flags&MSG_PEEK != 0)

		if r.count == 0 && n > 0 && flags&MSG_PEEK == 0 {
			r.files = files
		} else {
			dropFiles(files)
		}

		r.count += n

		if eof || ended || r.count >= r.n || r.count > 0 && flags&MSG_WAITALL == 0 || flags&MSG_PEEK != 0 && r.count > 0 {
			return nil
		}

		if flags&MSG_DONTWAIT != 0 {
			if r.count > 0 {
				return nil
			}

			return EAGAIN
		}

		timedOut, err := e.awaitInput(p)

		if err != nil || timedOut {
			if r.count > 0 {
				return nil
			}

			if err == nil {
				err = EAGAIN
			}

			return err
		}
	}
}

func (e *PairEnd) streamTake(into *MemfdFile, at, room int, peek bool) (n int, files []*Description, eof, ended bool) {
	e.mu.Lock()

	defer e.mu.Unlock()

	for n < room {
		pk, ok := e.in.peek()

		switch {
		case e.noRead.Load() || ok && pk.kind == packetEnd:
			return n, files, n == 0, false
		case !ok:
			return n, files, false, false
		case n > 0 && len(pk.files) > 0:
			return n, files, false, true
		}

		c := into.put(at+n, pk.data[:min(len(pk.data), room-n)])

		if peek {
			return n + c, nil, false, true
		}

		carried := len(pk.files) > 0

		if n == 0 {
			files = pk.files
		}

		n += c

		if c < len(pk.data) {
			pk.data, pk.files = pk.data[c:], nil
		} else if e.in.take(); e.in.len() == queueMax-1 {
			e.watchers.tellOut()
		}

		if carried {
			return n, files, false, true
		}
	}

	return n, files, false, false
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

func (e *PairEnd) ctl(p *Process, r *Ctl) error {
	if len(r.data) < 8 {
		return ENOTTY
	}

	call, arg, body := binary.LittleEndian.Uint32(r.data), binary.LittleEndian.Uint32(r.data[4:]), r.data[8:]

	switch call {
	case ctlBind, ctlListen, ctlAccept:
		return EINVAL
	case ctlConnect:
		return EISCONN
	case ctlSockname:
		return EOPNOTSUPP
	case ctlShutdown:
		return e.shutdown(int(arg))
	case ctlGetsockopt, ctlSetsockopt:
		if len(body) < 8 {
			return EINVAL
		}

		level, name := binary.LittleEndian.Uint32(body), binary.LittleEndian.Uint32(body[4:])

		if call == ctlSetsockopt {
			return e.options.set(level, name, body[8:])
		}

		var err error

		r.answer, err = e.options.get(p, e.shared.kind, level, name)

		return err
	}

	return ENOTTY
}

func (e *PairEnd) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, e.stat(p, r.st)
	case *Ctl:
		return false, e.ctl(p, r)
	case *FileRead:
		switch {
		case r.explicit:
			return false, errUnserved
		case !r.sock && p.chaosFail("read"):
			return false, EIO
		}

		err := e.recv(p, r)

		if !r.sock {
			dropFiles(r.files)

			r.files = nil
		}

		return false, err
	case *FileWrite:
		switch {
		case r.explicit:
			return false, errUnserved
		case !r.sock && p.chaosFail("write"):
			return false, EIO
		}

		return false, e.send(p, r)
	}

	return false, errUnserved
}

func (e *PairEnd) objectID() uint64 {
	return e.shared.objectID()
}
