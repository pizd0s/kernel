//go:build linux && amd64

package main

import (
	"encoding/binary"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
)

// unixDgram is an AF_UNIX SOCK_DGRAM socket of the kernel's. It reads
// inbox, the far end of a datagram pair whose near end, mouth, takes
// what is sent to it: bind gives mouth to the socket node of its path,
// which gives it to a sender that walks there (CONNECT). A datagram goes
// to the mouth of the path sendto names, or of the one connect named,
// target, as the packet of a pair does, the sender's name its address;
// one to a socket that closed is ECONNREFUSED. The options are those of
// inbox, the socket's end: SO_SNDTIMEO is its sends'.
type unixDgram struct {
	refs         atomic.Int32
	nonblock     atomic.Bool
	inbox, mouth *pairEnd
	mu           sync.Mutex
	name, peer   string
	target       *pairEnd
	// writers are the subscriptions to the socket's room, which its inner
	// subscription, to its target's, wakes (wake).
	writers subscribers
	inner   subscription
}

func newUnixDgram(nonblock bool) *unixDgram {
	s := &unixDgram{}
	s.refs.Store(1)
	s.nonblock.Store(nonblock)
	s.inbox, s.mouth = newPairOf(sockDgram, false)
	s.inner.init(s, 0)

	return s
}

func (s *unixDgram) ref() {
	s.refs.Add(1)
}

// unref closes the socket with its last reference: what it has not read
// goes, and what is sent to its name from then on is refused.
func (s *unixDgram) unref() {
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

// A unixDgram is a File, a Transfer and a Socket.
func (s *unixDgram) asFile() File              { return s }
func (s *unixDgram) asTransfer() Transfer      { return s }
func (s *unixDgram) asSeeker() Seeker          { return nil }
func (s *unixDgram) asDir() Dir                { return nil }
func (s *unixDgram) asTerminal() Terminal      { return nil }
func (s *unixDgram) asWindowed() Windowed      { return nil }
func (s *unixDgram) asLink() Link              { return nil }
func (s *unixDgram) asAttrs() Attrs            { return nil }
func (s *unixDgram) asSocket() Socket          { return s }
func (s *unixDgram) asIoctl() Ioctl            { return nil }
func (s *unixDgram) asLocker() Locker          { return nil }
func (s *unixDgram) asEpoll() *epoll           { return nil }
func (s *unixDgram) asMemfd() *memfdFile       { return nil }
func (s *unixDgram) asPair() *pairEnd          { return nil }
func (s *unixDgram) asListener() *unixListener { return nil }
func (s *unixDgram) asConsole() *consoleEnd    { return nil }
func (s *unixDgram) asVFS() *remoteDir         { return nil }
func (s *unixDgram) asCall() *callEnd          { return nil }

// targetEnd is connect's target, with a reference of its own, or nil.
func (s *unixDgram) targetEnd() *pairEnd {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.target != nil {
		s.target.ref()
	}

	return s.target
}

func (s *unixDgram) stat(p *Process, st *syscall.Stat_t) error {
	return s.inbox.stat(p, st)
}

// poll: input is inbox's, room the target's, always without one.
func (s *unixDgram) poll(events int16) int16 {
	out := events & (pollOut | pollWrnorm)
	ready := s.inbox.poll(events) &^ (pollOut | pollWrnorm)

	if t := s.targetEnd(); t != nil {
		defer t.unref()

		return ready | t.poll(out)&out
	}

	return ready | out
}

func (s *unixDgram) subscribe(sub *subscription, events int16) {
	in, out := ioSides(events)

	if in {
		s.inbox.subscribe(sub, pollIn)
	}

	if out {
		s.mu.Lock()
		s.writers.add(&sub.out)
		s.mu.Unlock()
	}
}

func (s *unixDgram) unsubscribe(sub *subscription) {
	s.inbox.unsubscribe(sub)
	s.mu.Lock()
	s.writers.remove(&sub.out)
	s.mu.Unlock()
}

// wake is the socket as the waker of its inner subscription: room in its
// target is room in the socket.
func (s *unixDgram) wake(*hook) {
	s.mu.Lock()
	s.writers.tell()
	s.mu.Unlock()
}

func (s *unixDgram) flags() (int, error) {
	fl := syscall.O_RDWR

	if s.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (s *unixDgram) setFlags(fl int) error {
	s.nonblock.Store(fl&syscall.O_NONBLOCK != 0)

	return nil
}

// bind names the socket with a path of the VFS's, as a socket of a
// connection is named.
func (s *unixDgram) bind(p *Process, at, n uint32) error {
	path, err := p.unixSocketPath(at, n)

	if err != nil {
		return err
	}

	s.mu.Lock()
	named := s.name != ""
	s.mu.Unlock()

	if named {
		return syscall.EINVAL
	}

	err = p.onPath(atFdcwd, path, func(v Dir) error {
		return v.bindPath(p, path, 0o777&^p.umask, s.mouth)
	})

	if err == syscall.EEXIST {
		err = syscall.EADDRINUSE
	}

	if err == nil {
		s.mu.Lock()
		s.name = path
		s.mu.Unlock()
	}

	return err
}

// connect makes the socket a path names the target of the socket's sends.
func (s *unixDgram) connect(p *Process, at, n uint32) error {
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

	mouth.subscribe(&s.inner, pollOut)
	s.mu.Lock()
	s.writers.tell()
	s.mu.Unlock()

	return nil
}

func (s *unixDgram) listen(backlog int) error {
	return syscall.EOPNOTSUPP
}

func (s *unixDgram) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, syscall.EOPNOTSUPP
}

func (s *unixDgram) sockname(p *Process) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return unixAddress([]byte(s.name)), nil
}

func (s *unixDgram) peername(p *Process) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.target == nil {
		return nil, syscall.ENOTCONN
	}

	return unixAddress([]byte(s.peer)), nil
}

func (s *unixDgram) socketKind() int {
	return sockDgram
}

func (s *unixDgram) shutdown(how int) error {
	return s.inbox.shutdown(how)
}

func (s *unixDgram) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	return s.inbox.getsockopt(p, level, name, at, lenAt)
}

func (s *unixDgram) setsockopt(p *Process, level, name, at, n uint32) error {
	return s.inbox.setsockopt(p, level, name, at, n)
}

// send sends a datagram of data, which it keeps, with files and cred to
// the socket of path, or to the target without one: ENOTCONN when there
// is none, EPIPE after shutdown's SHUT_WR.
func (s *unixDgram) send(p *Process, data []byte, files []Object, cred *ucred, nonblock bool, path string) (int, error) {
	if s.inbox.noWrite.Load() {
		return 0, syscall.EPIPE
	}

	if len(data) > packetMax {
		return 0, syscall.EMSGSIZE
	}

	var to *pairEnd

	if path != "" {
		var err error

		if to, err = p.mouthOf(path); err != nil {
			return 0, err
		}
	} else if to = s.targetEnd(); to == nil {
		return 0, syscall.ENOTCONN
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

	pk := packet{data: data, files: files, cred: cred}

	if name != "" {
		pk.addr = []byte(name)
	}

	if err := to.queueAs(p, pk, nonblock || s.nonblock.Load(), &s.inbox.options.sndTimeout); err != nil {
		dropFiles(files)

		return 0, err
	}

	return len(data), nil
}

func (s *unixDgram) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	path, err := p.destination(addr, alen)

	if err != nil {
		return 0, err
	}

	return s.send(p, slices.Clone(p.span(buf, n)), nil, nil, flags&msgDontwait != 0, path)
}

func (s *unixDgram) sendmsg(p *Process, at, flags uint32) (int, error) {
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

	return s.send(p, data, files, cred, flags&msgDontwait != 0, path)
}

func (s *unixDgram) recvmsg(p *Process, at, flags uint32) (int, error) {
	return s.inbox.recvmsg(p, at, flags|s.dontwait())
}

func (s *unixDgram) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	return s.inbox.recvfrom(p, buf, n, flags|s.dontwait())
}

// dontwait is MSG_DONTWAIT for a nonblocking socket, whose inbox blocks.
func (s *unixDgram) dontwait() uint32 {
	if s.nonblock.Load() {
		return msgDontwait
	}

	return 0
}

func (s *unixDgram) read(p *Process, b []byte, mode ioMode) (int, error) {
	n, _, _, _, err := s.inbox.receive(p, [][]byte{b}, s.dontwait()|bit(mode.nonblock)*msgDontwait, 0, 0)

	return n, err
}

func (s *unixDgram) write(p *Process, b []byte, mode ioMode) (int, error) {
	return s.send(p, slices.Clone(b), nil, nil, mode.nonblock, "")
}

func (s *unixDgram) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (s *unixDgram) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}
