//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
)

type UnixSocket struct {
	refs     atomic.Int32
	nonblock atomic.Bool
	kind     int
	mu       sync.Mutex
	name     string
	listener *UnixListener
	conn     *PairEnd
	peer     string

	watchers Subscribers
	inner    Subscription

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

func (s *UnixSocket) asPair() *PairEnd {
	return nil
}

func (s *UnixSocket) asListener() *UnixListener {
	return nil
}

func (s *UnixSocket) asConsole() *ConsoleEnd {
	return nil
}

func (s *UnixSocket) asRemote() *RemoteFD {
	return nil
}

func (s *UnixSocket) state() (*UnixListener, *PairEnd) {
	s.mu.Lock()

	defer s.mu.Unlock()

	return s.listener, s.conn
}

func (s *UnixSocket) stat(p *Process, st *Stat) error {
	if _, c := s.state(); c != nil {
		return c.stat(p, st)
	}

	*st = Stat{Mode: S_IFSOCK | 0o777, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (s *UnixSocket) poll(events int16, through []*Epoll) int16 {
	l, c := s.state()

	switch {
	case c != nil:
		return c.poll(events, through)
	case l != nil:
		return l.poll(events, through) & (POLLIN | POLLRDNORM | POLLERR | POLLHUP)
	}

	return 0
}

func (s *UnixSocket) subscribe(sub *Subscription, events int16) {
	in, out := ioSides(events)

	s.mu.Lock()

	if in {
		s.watchers.add(&sub.in)
	}

	if out {
		s.watchers.add(&sub.out)
	}

	s.mu.Unlock()
}

func (s *UnixSocket) unsubscribe(sub *Subscription) {
	s.mu.Lock()
	s.watchers.remove(&sub.in)
	s.watchers.remove(&sub.out)
	s.mu.Unlock()
}

func (s *UnixSocket) attach(to Object) {
	to.subscribe(&s.inner, POLLIN|POLLOUT)
	s.mu.Lock()
	s.watchers.tell()
	s.mu.Unlock()
}

func (s *UnixSocket) wake(l *Hook) {
	s.mu.Lock()

	if l == &s.inner.in {
		s.watchers.tellIn()
	} else {
		s.watchers.tellOut()
	}

	s.mu.Unlock()
}

func (s *UnixSocket) flags() (int, error) {
	fl := O_RDWR

	if s.nonblock.Load() {
		fl |= O_NONBLOCK
	}

	return fl, nil
}

func (s *UnixSocket) setFlags(fl int) error {
	s.nonblock.Store(fl&O_NONBLOCK != 0)

	if _, c := s.state(); c != nil {
		return c.setFlags(fl)
	}

	return nil
}

func (s *UnixSocket) bind(p *Process, a Address) error {
	path, err := a.path, a.err

	if err != nil {
		return err
	}

	if l, c := s.state(); l != nil || c != nil {
		return EINVAL
	}

	l := &UnixListener{ino: pairInodes.Add(1), backlog: 1, kind: s.kind}

	l.refs.Store(1)

	err = p.onPath(AT_FDCWD, path, func(v Dir) error {
		return v.bindPath(p, path, 0o777&^p.umask, l)
	})

	if err == EEXIST {
		err = EADDRINUSE
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
		return EINVAL
	}

	l.listen(backlog)

	return nil
}

func (s *UnixSocket) accept(p *Process, flags uint32) (Object, []byte, error) {
	l, _ := s.state()

	if l == nil {
		return nil, nil, EINVAL
	}

	e, err := l.accept(p, s.nonblock.Load())

	if err != nil {
		return nil, nil, err
	}

	e.nonblock.Store(flags&SOCK_NONBLOCK != 0)
	e.options.copyOptions(&s.options)

	return e, unixAddress(nil), nil
}

func (s *UnixSocket) connect(p *Process, a Address) error {
	path, err := a.path, a.err

	if err != nil {
		return err
	}

	if l, c := s.state(); c != nil {
		return EISCONN
	} else if l != nil {
		return EINVAL
	}

	var conn *PairEnd

	err = p.onPath(AT_FDCWD, path, func(v Dir) error {
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
		return nil, ENOTCONN
	}

	return unixAddress([]byte(s.peer)), nil
}

func (s *UnixSocket) connection() (*PairEnd, error) {
	l, c := s.state()

	switch {
	case c != nil:
		return c, nil
	case l != nil && l.listens():
		return nil, EINVAL
	}

	return nil, ENOTCONN
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

func (s *UnixSocket) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	if l, c := s.state(); c != nil {
		return c.getsockopt(p, level, name)
	} else if level == SOL_SOCKET && name == SO_ACCEPTCONN {
		return binary.LittleEndian.AppendUint32(nil, bit(l != nil && l.listens())), nil
	}

	return s.options.get(p, s.kind, level, name)
}

func (s *UnixSocket) setsockopt(level, name uint32, value []byte) error {
	if _, c := s.state(); c != nil {
		return c.setsockopt(level, name, value)
	}

	return s.options.set(level, name, value)
}

func (s *UnixSocket) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.send(p, pk, to, mode)
}

func (s *UnixSocket) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	c, err := s.connection()

	if err != nil {
		return 0, Packet{}, 0, err
	}

	return c.recv(p, into, want, flags)
}

func (s *UnixSocket) consume(sink func(pk *Packet)) {
	if c, err := s.connection(); err == nil {
		c.consume(sink)
	}
}

func (s *UnixSocket) read(p *Process, into Transfer, n int, mode IoMode) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.read(p, into, n, mode)
}

func (s *UnixSocket) write(p *Process, from Transfer, n int, mode IoMode) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.write(p, from, n, mode)
}

func (s *UnixSocket) pread(p *Process, into Transfer, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (s *UnixSocket) pwrite(p *Process, from Transfer, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (s *UnixSocket) splice(at int64, n int, mode int) Transfer {
	return newSpliced(s, at, n, mode)
}

func (s *UnixSocket) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (s *UnixSocket) copyTo(p *Process, out Transfer, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (s *UnixSocket) copyFrom(p *Process, in Transfer, from, to int64, n int) (int, error) {
	return 0, EINVAL
}
