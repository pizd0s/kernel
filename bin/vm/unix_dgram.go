package main

import (
	"sync"
	"sync/atomic"
)

type UnixDgram struct {
	ObjectIdent

	nonblock     atomic.Bool
	inbox, mouth *PairEnd
	mu           sync.Mutex
	name, peer   string
	bound        *SocketName
	target       Object

	writers Subscribers
	inner   Subscription
}

func newUnixDgram(nonblock bool) *UnixDgram {
	s := &UnixDgram{ObjectIdent: newObjectIdent()}

	s.nonblock.Store(nonblock)
	s.inbox, s.mouth = newPairOf(SOCK_DGRAM, false)
	s.inner.init(s, 0)

	return s
}

func (s *UnixDgram) closed() {
	s.mu.Lock()

	t, b := s.target, s.bound

	s.target, s.bound = nil, nil
	s.mu.Unlock()

	if t != nil {
		t.unsubscribe(&s.inner)
	}

	if b != nil {
		b.forget(s.mouth)
	}

	s.inbox.closed()
	s.mouth.closed()
}

func (s *UnixDgram) targetEnd() Object {
	s.mu.Lock()

	defer s.mu.Unlock()

	return s.target
}

func (s *UnixDgram) stat(p *Process, st *Stat) error {
	return s.inbox.stat(p, st)
}

func (s *UnixDgram) poll(events int16, through []*Epoll) int16 {
	out := events & (POLLOUT | POLLWRNORM)
	ready := s.inbox.poll(events, through) &^ (POLLOUT | POLLWRNORM)

	if t := s.targetEnd(); t != nil {
		return ready | t.poll(out, through)&out
	}

	return ready | out
}

func (s *UnixDgram) subscribe(sub *Subscription, events int16) {
	in, out := ioSides(events)

	if in {
		s.inbox.subscribe(sub, POLLIN)
	}

	if out {
		s.mu.Lock()
		s.writers.add(&sub.out)
		s.mu.Unlock()
	}
}

func (s *UnixDgram) unsubscribe(sub *Subscription) {
	s.inbox.unsubscribe(sub)
	s.mu.Lock()
	s.writers.remove(&sub.out)
	s.mu.Unlock()
}

func (s *UnixDgram) wake(*Hook) {
	s.mu.Lock()
	s.writers.tell()
	s.mu.Unlock()
}

func (s *UnixDgram) flags(p *Process) (int, error) {
	fl := O_RDWR

	if s.nonblock.Load() {
		fl |= O_NONBLOCK
	}

	return fl, nil
}

func (s *UnixDgram) setFlags(p *Process, fl int) error {
	s.nonblock.Store(fl&O_NONBLOCK != 0)

	return nil
}

func (s *UnixDgram) bind(p *Process, a Address) error {
	path, err := a.path, a.err

	if err != nil {
		return err
	}

	s.mu.Lock()

	named := s.name != ""

	s.mu.Unlock()

	if named {
		return EINVAL
	}

	b, err := p.bindName(a, s.mouth)

	if err == nil {
		s.mu.Lock()
		s.name, s.bound = path, b
		s.mu.Unlock()
	}

	return err
}

func (s *UnixDgram) connect(p *Process, a Address) error {
	path, err := a.path, a.err

	if err != nil {
		return err
	}

	mouth, err := p.mouthOf(a.node)

	if err != nil {
		return err
	}

	s.mu.Lock()

	old := s.target

	s.target, s.peer = mouth, path
	s.mu.Unlock()

	if old != nil {
		old.unsubscribe(&s.inner)
	}

	mouth.subscribe(&s.inner, POLLOUT)
	s.mu.Lock()
	s.writers.tell()
	s.mu.Unlock()

	return nil
}

func (s *UnixDgram) listen(backlog int) error {
	return EOPNOTSUPP
}

func (s *UnixDgram) accept(p *Process, flags uint32) (*Description, []byte, error) {
	return nil, nil, EOPNOTSUPP
}

func (s *UnixDgram) sockname(p *Process) ([]byte, error) {
	s.mu.Lock()

	defer s.mu.Unlock()

	return unixAddress([]byte(s.name)), nil
}

func (s *UnixDgram) peername(p *Process) ([]byte, error) {
	s.mu.Lock()

	defer s.mu.Unlock()

	if s.target == nil {
		return nil, ENOTCONN
	}

	return unixAddress([]byte(s.peer)), nil
}

func (s *UnixDgram) socketKind() int {
	return SOCK_DGRAM
}

func (s *UnixDgram) shutdown(how int) error {
	return s.inbox.shutdown(how)
}

func (s *UnixDgram) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return s.inbox.getsockopt(p, level, name)
}

func (s *UnixDgram) setsockopt(level, name uint32, value []byte) error {
	return s.inbox.setsockopt(level, name, value)
}

func (s *UnixDgram) send(p *Process, r *SockSend) error {
	if r.to.err != nil {
		return r.to.err
	}

	if s.inbox.noWrite.Load() {
		return EPIPE
	}

	if len(r.pk.data) > packetMax {
		return EMSGSIZE
	}

	var t Object

	if r.to.given {
		var err error

		if t, err = p.mouthOf(r.to.node); err != nil {
			return err
		}
	} else if t = s.targetEnd(); t == nil {
		return ENOTCONN
	}

	s.mu.Lock()

	if s.name != "" {
		r.pk.addr = []byte(s.name)
	}

	s.mu.Unlock()

	mode := r.mode

	mode.nonblock = mode.nonblock || s.nonblock.Load()
	mode.timeout = &s.inbox.options.sndTimeout

	var err error

	r.n, err = p.sockSend(t, r.pk, Address{}, mode)

	return err
}

func (s *UnixDgram) recv(p *Process, r *SockRecv) error {
	r.flags |= s.dontwait()

	return s.inbox.recv(p, r)
}

func (s *UnixDgram) consume(sink func(pk *Packet)) {
	s.inbox.consume(sink)
}

func (s *UnixDgram) dontwait() uint32 {
	if s.nonblock.Load() {
		return MSG_DONTWAIT
	}

	return 0
}

func (s *UnixDgram) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
	r := SockRecv{into: Into{p: p, to: into}, want: n, flags: s.dontwait() | bit(mode.nonblock)*MSG_DONTWAIT}
	err := s.inbox.recv(p, &r)

	dropFiles(r.files)

	return r.n, err
}

func (s *UnixDgram) write(p *Process, from *MemfdFile, n int, mode IoMode) (int, error) {
	if n > packetMax {
		return 0, EMSGSIZE
	}

	data := make([]byte, n)
	r := SockSend{pk: &Packet{data: data[:takeFrom(from)(0, data)]}, mode: mode}
	err := s.send(p, &r)

	return r.n, err
}

func (s *UnixDgram) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, s.stat(p, r.st)
	case *SockBind:
		return false, s.bind(p, r.addr)
	case *SockConnect:
		return false, s.connect(p, r.addr)
	case *SockListen:
		return false, s.listen(r.backlog)
	case *SockAccept:
		var err error

		r.conn, r.peer, err = s.accept(p, r.flags)

		return false, err
	case *SockSend:
		return false, s.send(p, r)
	case *SockRecv:
		return false, s.recv(p, r)
	case *SockName:
		var err error

		if r.peer {
			r.addr, err = s.peername(p)
		} else {
			r.addr, err = s.sockname(p)
		}

		return false, err
	case *SockGetopt:
		var err error

		r.value, err = s.getsockopt(p, r.level, r.name)

		return false, err
	case *SockSetopt:
		return false, s.setsockopt(r.level, r.name, r.value)
	case *SockShutdown:
		return false, s.shutdown(r.how)
	case *SockType:
		r.kind = s.socketKind()

		return false, nil
	case *SockConsume:
		s.consume(r.sink)

		return false, nil
	case *FileGetfl:
		var err error

		r.flags, err = s.flags(p)

		return false, err
	case *FileSetfl:
		return false, s.setFlags(p, r.flags)
	case *FileRead:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = s.read(p, r.into, r.n, r.mode)

		return false, err
	case *FileWrite:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = s.write(p, r.from, r.n, r.mode)

		return false, err
	}

	return false, errUnserved
}
