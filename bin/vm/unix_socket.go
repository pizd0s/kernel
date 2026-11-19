//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const somaxconn = 4096

type UnixSocket struct {
	refs     atomic.Int32
	nonblock atomic.Bool
	kind     int
	mu       sync.Mutex
	name     string
	listener *UnixListener
	conn     *PairEnd
	peer     string

	readers, writers Subscribers
	inner            Subscription

	options SockOptions
}

func newUnixSocket(kind int, nonblock bool) *UnixSocket {
	s := &UnixSocket{kind: kind}

	s.refs.Store(1)
	s.nonblock.Store(nonblock)
	s.inner.init(s, 0)

	return s
}

func (s *UnixSocket) ref() {
	s.refs.Add(1)
}

func (s *UnixSocket) unref() {
	if s.refs.Add(-1) != 0 {
		return
	}

	s.mu.Lock()

	l, c := s.listener, s.conn

	s.listener, s.conn = nil, nil
	s.mu.Unlock()

	if l != nil {
		l.unsubscribe(&s.inner)
		l.shut()
		l.unref()
	}

	if c != nil {
		c.unsubscribe(&s.inner)
		c.unref()
	}
}

func (s *UnixSocket) asFile() File {
	return s
}

func (s *UnixSocket) asTransfer() Transfer {
	return s
}

func (s *UnixSocket) asSeeker() Seeker {
	return nil
}

func (s *UnixSocket) asDir() Dir {
	return nil
}

func (s *UnixSocket) asTerminal() Terminal {
	return nil
}

func (s *UnixSocket) asWindowed() Windowed {
	return nil
}

func (s *UnixSocket) asLink() Link {
	return nil
}

func (s *UnixSocket) asAttrs() Attrs {
	return nil
}

func (s *UnixSocket) asSocket() Socket {
	return s
}

func (s *UnixSocket) asIoctl() Ioctl {
	return nil
}

func (s *UnixSocket) asLocker() Locker {
	return nil
}

func (s *UnixSocket) asEpoll() *Epoll {
	return nil
}

func (s *UnixSocket) asMemfd() *MemfdFile {
	return nil
}

func (s *UnixSocket) asPair() *PairEnd {
	return nil
}

func (s *UnixSocket) asListener() *UnixListener {
	return nil
}

func (s *UnixSocket) asConsole() *ConsoleEnd {
	return nil
}

func (s *UnixSocket) asVFS() *RemoteDir {
	return nil
}

func (s *UnixSocket) asCall() *CallEnd {
	return nil
}

func (s *UnixSocket) state() (*UnixListener, *PairEnd) {
	s.mu.Lock()

	defer s.mu.Unlock()

	return s.listener, s.conn
}

func (s *UnixSocket) stat(p *Process, st *syscall.Stat_t) error {
	if _, c := s.state(); c != nil {
		return c.stat(p, st)
	}

	*st = syscall.Stat_t{Mode: syscall.S_IFSOCK | 0o777, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (s *UnixSocket) poll(events int16) int16 {
	l, c := s.state()

	switch {
	case c != nil:
		return c.poll(events)
	case l != nil:
		return l.poll(events) & (pollIn | pollRdnorm | pollErr | pollHup)
	}

	return 0
}

func (s *UnixSocket) subscribe(sub *Subscription, events int16) {
	in, out := ioSides(events)

	s.mu.Lock()

	if in {
		s.readers.add(&sub.in)
	}

	if out {
		s.writers.add(&sub.out)
	}

	s.mu.Unlock()
}

func (s *UnixSocket) unsubscribe(sub *Subscription) {
	s.mu.Lock()
	s.readers.remove(&sub.in)
	s.writers.remove(&sub.out)
	s.mu.Unlock()
}

func (s *UnixSocket) attach(to Object) {
	to.subscribe(&s.inner, pollIn|pollOut)
	s.mu.Lock()
	s.readers.tell()
	s.writers.tell()
	s.mu.Unlock()
}

func (s *UnixSocket) wake(l *Hook) {
	s.mu.Lock()

	if l == &s.inner.in {
		s.readers.tell()
	} else {
		s.writers.tell()
	}

	s.mu.Unlock()
}

func (s *UnixSocket) flags() (int, error) {
	fl := syscall.O_RDWR

	if s.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (s *UnixSocket) setFlags(fl int) error {
	s.nonblock.Store(fl&syscall.O_NONBLOCK != 0)

	if _, c := s.state(); c != nil {
		return c.setFlags(fl)
	}

	return nil
}

func (s *UnixSocket) bind(p *Process, at, n uint32) error {
	path, err := p.unixSocketPath(at, n)

	if err != nil {
		return err
	}

	if l, c := s.state(); l != nil || c != nil {
		return syscall.EINVAL
	}

	l := &UnixListener{ino: pairInodes.Add(1), conns: newMsgChannel(1), kind: s.kind}

	l.refs.Store(1)

	err = p.onPath(atFdcwd, path, func(v Dir) error {
		return v.bindPath(p, path, 0o777&^p.umask, l)
	})

	if err == syscall.EEXIST {
		err = syscall.EADDRINUSE
	}

	if err != nil {
		l.unref()

		return err
	}

	s.mu.Lock()
	s.name, s.listener = path, l
	s.mu.Unlock()
	s.attach(l)

	return nil
}

func (s *UnixSocket) listen(backlog int) error {
	l, c := s.state()

	if l == nil || c != nil {
		return syscall.EINVAL
	}

	l.mu.Lock()
	l.listening = true
	l.mu.Unlock()
	l.conns.setLimit(min(max(backlog, 1), somaxconn))
	l.conns.nudge()
	l.conns.nudgeWriter()

	return nil
}

func (s *UnixSocket) accept(p *Process, flags uint32) (Object, []byte, error) {
	l, _ := s.state()

	if l == nil || !l.listens() {
		return nil, nil, syscall.EINVAL
	}

	for {
		e := l.pop()

		if e == nil && !l.listens() {
			return nil, nil, syscall.EINVAL
		}

		if e == nil && s.nonblock.Load() {
			return nil, nil, syscall.EAGAIN
		}

		if e == nil {
			pk, given, _, err := l.conns.receive(p, func() bool { return !l.listens() }, time.Time{}, restarts)

			if err != nil {
				return nil, nil, err
			}

			if given && len(pk.files) > 0 {
				e = pk.files[0].asPair()
			}
		}

		if e != nil {
			e.nonblock.Store(flags&sockNonblock != 0)
			e.options.copyOptions(&s.options)

			return e, unixAddress(nil), nil
		}
	}
}

func (s *UnixSocket) connect(p *Process, at, n uint32) error {
	path, err := p.unixSocketPath(at, n)

	if err != nil {
		return err
	}

	if l, c := s.state(); c != nil {
		return syscall.EISCONN
	} else if l != nil {
		return syscall.EINVAL
	}

	var conn *PairEnd

	err = p.onPath(atFdcwd, path, func(v Dir) error {
		var err error
		conn, err = p.connectUnix(v, path, s.kind, s.nonblock.Load(), &s.options.sndTimeout)

		return err
	})

	if err != nil {
		return err
	}

	conn.options.copyOptions(&s.options)
	s.mu.Lock()
	s.conn, s.peer = conn, path
	s.mu.Unlock()
	s.attach(conn)

	return nil
}

func (s *UnixSocket) sockname(p *Process) ([]byte, error) {
	s.mu.Lock()

	defer s.mu.Unlock()

	return unixAddress([]byte(s.name)), nil
}

func (s *UnixSocket) peername(p *Process) ([]byte, error) {
	s.mu.Lock()

	defer s.mu.Unlock()

	if s.conn == nil {
		return nil, syscall.ENOTCONN
	}

	return unixAddress([]byte(s.peer)), nil
}

func (s *UnixSocket) connection() (*PairEnd, error) {
	l, c := s.state()

	switch {
	case c != nil:
		return c, nil
	case l != nil && l.listens():
		return nil, syscall.EINVAL
	}

	return nil, syscall.ENOTCONN
}

func (s *UnixSocket) socketKind() int {
	return s.kind
}

func (s *UnixSocket) shutdown(how int) error {
	c, err := s.connection()

	if err != nil {
		return err
	}

	return c.shutdown(how)
}

func (s *UnixSocket) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	if l, c := s.state(); c != nil {
		return c.getsockopt(p, level, name, at, lenAt)
	} else if level == solSocket && name == soAcceptconn {
		value := uint32(0)

		if l != nil && l.listens() {
			value = 1
		}

		length := min(p.u32(lenAt), 4)

		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], value)
		copy(p.span(at, length), b[:])
		p.put32(lenAt, length)

		return nil
	}

	return s.options.get(p, s.kind, level, name, at, lenAt)
}

func (s *UnixSocket) setsockopt(p *Process, level, name, at, n uint32) error {
	if _, c := s.state(); c != nil {
		return c.setsockopt(p, level, name, at, n)
	}

	return s.options.set(p, level, name, at, n)
}

func (s *UnixSocket) sendmsg(p *Process, at, flags uint32) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.sendmsg(p, at, flags)
}

func (s *UnixSocket) recvmsg(p *Process, at, flags uint32) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.recvmsg(p, at, flags)
}

func (s *UnixSocket) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.sendto(p, buf, n, flags, addr, alen)
}

func (s *UnixSocket) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	c, err := s.connection()

	if err != nil {
		return 0, nil, err
	}

	return c.recvfrom(p, buf, n, flags)
}

func (s *UnixSocket) read(p *Process, b []byte, mode IoMode) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.read(p, b, mode)
}

func (s *UnixSocket) write(p *Process, b []byte, mode IoMode) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.write(p, b, mode)
}

func (s *UnixSocket) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (s *UnixSocket) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}
