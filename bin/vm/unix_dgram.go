//go:build linux && amd64

package main

import (
	"encoding/binary"
	"slices"
	"sync"
	"sync/atomic"
)

type UnixDgram struct {
	refs         atomic.Int32
	nonblock     atomic.Bool
	inbox, mouth *PairEnd
	mu           sync.Mutex
	name, peer   string
	target       *PairEnd

	writers Subscribers
	inner   Subscription
}

func newUnixDgram(nonblock bool) *UnixDgram {
	s := &UnixDgram{}

	s.refs.Store(1)
	s.nonblock.Store(nonblock)
	s.inbox, s.mouth = newPairOf(SOCK_DGRAM, false)
	s.inner.init(s, 0)

	return s
}

func (s *UnixDgram) ref() {
	s.refs.Add(1)
}

func (s *UnixDgram) unref() {
	if s.refs.Add(-1) != 0 {
		return
	}

	s.mu.Lock()

	t := s.target

	s.target = nil
	s.mu.Unlock()

	if t != nil {
		t.unsubscribe(&s.inner)
		t.unref()
	}

	s.inbox.unref()
	s.mouth.unref()
}

func (s *UnixDgram) asFile() File {
	return s
}

func (s *UnixDgram) asTransfer() Transfer {
	return s
}

func (s *UnixDgram) asSeeker() Seeker {
	return nil
}

func (s *UnixDgram) asDir() Dir {
	return nil
}

func (s *UnixDgram) asTerminal() Terminal {
	return nil
}

func (s *UnixDgram) asLink() Link {
	return nil
}

func (s *UnixDgram) asAttrs() Attrs {
	return nil
}

func (s *UnixDgram) asSocket() Socket {
	return s
}

func (s *UnixDgram) asIoctl() Ioctl {
	return nil
}

func (s *UnixDgram) asLocker() Locker {
	return nil
}

func (s *UnixDgram) asEpoll() *Epoll {
	return nil
}

func (s *UnixDgram) asPair() *PairEnd {
	return nil
}

func (s *UnixDgram) asListener() *UnixListener {
	return nil
}

func (s *UnixDgram) asConsole() *ConsoleEnd {
	return nil
}

func (s *UnixDgram) asRemote() *RemoteFD {
	return nil
}

func (s *UnixDgram) targetEnd() *PairEnd {
	s.mu.Lock()

	defer s.mu.Unlock()

	if s.target != nil {
		s.target.ref()
	}

	return s.target
}

func (s *UnixDgram) stat(p *Process, st *Stat) error {
	return s.inbox.stat(p, st)
}

func (s *UnixDgram) poll(events int16, through []*Epoll) int16 {
	out := events & (POLLOUT | POLLWRNORM)
	ready := s.inbox.poll(events, through) &^ (POLLOUT | POLLWRNORM)

	if t := s.targetEnd(); t != nil {
		defer t.unref()

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

func (s *UnixDgram) flags() (int, error) {
	fl := O_RDWR

	if s.nonblock.Load() {
		fl |= O_NONBLOCK
	}

	return fl, nil
}

func (s *UnixDgram) setFlags(fl int) error {
	s.nonblock.Store(fl&O_NONBLOCK != 0)

	return nil
}

func (s *UnixDgram) bind(p *Process, at, n uint32) error {
	path, err := p.unixSocketPath(at, n)

	if err != nil {
		return err
	}

	s.mu.Lock()

	named := s.name != ""

	s.mu.Unlock()

	if named {
		return EINVAL
	}

	err = p.onPath(AT_FDCWD, path, func(v Dir) error {
		return v.bindPath(p, path, 0o777&^p.umask, s.mouth)
	})

	if err == EEXIST {
		err = EADDRINUSE
	}

	if err == nil {
		s.mu.Lock()
		s.name = path
		s.mu.Unlock()
	}

	return err
}

func (s *UnixDgram) connect(p *Process, at, n uint32) error {
	path, err := p.unixSocketPath(at, n)

	if err != nil {
		return err
	}

	mouth, err := p.mouthOf(path)

	if err != nil {
		return err
	}

	s.mu.Lock()

	old := s.target

	s.target, s.peer = mouth, path
	s.mu.Unlock()

	if old != nil {
		old.unsubscribe(&s.inner)
		old.unref()
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

func (s *UnixDgram) accept(p *Process, flags uint32) (Object, []byte, error) {
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

func (s *UnixDgram) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	return s.inbox.getsockopt(p, level, name, at, lenAt)
}

func (s *UnixDgram) setsockopt(p *Process, level, name, at, n uint32) error {
	return s.inbox.setsockopt(p, level, name, at, n)
}

func (s *UnixDgram) send(p *Process, data []byte, files []Object, cred *Ucred, nonblock bool, path string) (int, error) {
	if s.inbox.noWrite.Load() {
		return 0, EPIPE
	}

	if len(data) > packetMax {
		return 0, EMSGSIZE
	}

	var to *PairEnd

	if path != "" {
		var err error

		if to, err = p.mouthOf(path); err != nil {
			return 0, err
		}
	} else if to = s.targetEnd(); to == nil {
		return 0, ENOTCONN
	}

	defer to.unref()

	if cred == nil && to.peer.options.passcred.Load() {
		cred = p.ucred()
	}

	s.mu.Lock()

	name := s.name

	s.mu.Unlock()

	for _, f := range files {
		f.ref()
	}

	pk := &Packet{data: data, files: files, cred: cred}

	if name != "" {
		pk.addr = []byte(name)
	}

	if err := to.queueAs(p, pk, nonblock || s.nonblock.Load(), &s.inbox.options.sndTimeout); err != nil {
		dropFiles(files)

		return 0, err
	}

	return len(data), nil
}

func (s *UnixDgram) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	path, err := p.destination(addr, alen)

	if err != nil {
		return 0, err
	}

	return s.send(p, slices.Clone(p.span(buf, n)), nil, nil, flags&MSG_DONTWAIT != 0, path)
}

func (s *UnixDgram) sendmsg(p *Process, at, flags uint32) (int, error) {
	h := p.span(at, sizeofMsghdr)
	path, err := p.destination(binary.LittleEndian.Uint32(h), binary.LittleEndian.Uint32(h[4:]))

	if err != nil {
		return 0, err
	}

	files, cred, err := p.rights(binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:]))

	if err != nil {
		return 0, err
	}

	data, err := p.gather(binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:]), packetMax)

	if err != nil {
		return 0, err
	}

	return s.send(p, data, files, cred, flags&MSG_DONTWAIT != 0, path)
}

func (s *UnixDgram) recvmsg(p *Process, at, flags uint32) (int, error) {
	return s.inbox.recvmsg(p, at, flags|s.dontwait())
}

func (s *UnixDgram) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	return s.inbox.recvfrom(p, buf, n, flags|s.dontwait())
}

func (s *UnixDgram) dontwait() uint32 {
	if s.nonblock.Load() {
		return MSG_DONTWAIT
	}

	return 0
}

func (s *UnixDgram) read(p *Process, b []byte, mode IoMode) (int, error) {
	n, _, _, _, err := s.inbox.receive(p, [][]byte{b}, s.dontwait()|bit(mode.nonblock)*MSG_DONTWAIT, 0, 0)

	return n, err
}

func (s *UnixDgram) write(p *Process, b []byte, mode IoMode) (int, error) {
	return s.send(p, slices.Clone(b), nil, nil, mode.nonblock, "")
}

func (s *UnixDgram) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (s *UnixDgram) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (s *UnixDgram) copyFileRange(p *Process, in, out Transfer, from, to int64, n int) (int, error) {
	return 0, EINVAL
}
