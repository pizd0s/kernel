//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// AF_UNIX's sockets are the kernel's own, as its socketpairs are
// (pair_end.go): SOCK_STREAM and SOCK_SEQPACKET ones here, SOCK_DGRAM
// ones unixDgram. bind names a socket with a path of the VFS's: the file
// system of the path's directory makes a socket node there, which holds
// what the socket's name stands for (BIND, remote_dir.go), the listener
// of a socket of a connection. connect walks a path to such a node and
// takes what its file system gives back (CONNECT): one end of a new pair
// of the socket's kind goes into the listener's queue, accept takes it
// from there, and the other is the connecting socket's from then on. A
// listener whose socket closed, or that does not listen, refuses:
// ECONNREFUSED; one of another kind, or a datagram socket's name, is
// EPROTOTYPE. An abstract name is none of the VFS's: EOPNOTSUPP.
type unixSocket struct {
	refs     atomic.Int32
	nonblock atomic.Bool
	kind     int
	mu       sync.Mutex
	name     string
	listener *unixListener
	conn     *pairEnd
	peer     string
	// readers and writers are the subscriptions to the socket, which its
	// inner subscription, to its listener or its connection as it has one,
	// wakes as that changes (wake).
	readers, writers subscribers
	inner            subscription
	// The options are the socket's until it is connected, and its
	// connection's then, which gets them, as a connection accept takes
	// gets the listening socket's.
	options sockOptions
}

// somaxconn is the most connections a listener holds, Linux's default.
const somaxconn = 4096

func newUnixSocket(kind int, nonblock bool) *unixSocket {
	s := &unixSocket{kind: kind}
	s.refs.Store(1)
	s.nonblock.Store(nonblock)
	s.inner.init(s, 0)

	return s
}

func (s *unixSocket) ref() {
	s.refs.Add(1)
}

// unref closes the socket with its last reference: a listener refuses
// from then on, and the connections it holds are reset.
func (s *unixSocket) unref() {
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

// A unixSocket is a File, a Transfer and a Socket.
func (s *unixSocket) asFile() File              { return s }
func (s *unixSocket) asTransfer() Transfer      { return s }
func (s *unixSocket) asSeeker() Seeker          { return nil }
func (s *unixSocket) asDir() Dir                { return nil }
func (s *unixSocket) asTerminal() Terminal      { return nil }
func (s *unixSocket) asWindowed() Windowed      { return nil }
func (s *unixSocket) asLink() Link              { return nil }
func (s *unixSocket) asAttrs() Attrs            { return nil }
func (s *unixSocket) asSocket() Socket          { return s }
func (s *unixSocket) asIoctl() Ioctl            { return nil }
func (s *unixSocket) asLocker() Locker          { return nil }
func (s *unixSocket) asEpoll() *epoll           { return nil }
func (s *unixSocket) asMemfd() *memfdFile       { return nil }
func (s *unixSocket) asPair() *pairEnd          { return nil }
func (s *unixSocket) asListener() *unixListener { return nil }
func (s *unixSocket) asConsole() *consoleEnd    { return nil }
func (s *unixSocket) asVFS() *remoteDir         { return nil }
func (s *unixSocket) asCall() *callEnd          { return nil }

func (s *unixSocket) state() (*unixListener, *pairEnd) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.listener, s.conn
}

func (s *unixSocket) stat(p *Process, st *syscall.Stat_t) error {
	if _, c := s.state(); c != nil {
		return c.stat(p, st)
	}

	*st = syscall.Stat_t{Mode: syscall.S_IFSOCK | 0o777, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (s *unixSocket) poll(events int16) int16 {
	l, c := s.state()

	switch {
	case c != nil:
		return c.poll(events)
	case l != nil:
		return l.poll(events) & (pollIn | pollRdnorm | pollErr | pollHup)
	}

	return 0
}

// subscribe is to the socket's own lists, which its listener or its
// connection wakes through its inner subscription (wake).
func (s *unixSocket) subscribe(sub *subscription, events int16) {
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

func (s *unixSocket) unsubscribe(sub *subscription) {
	s.mu.Lock()
	s.readers.remove(&sub.in)
	s.writers.remove(&sub.out)
	s.mu.Unlock()
}

// attach gives the socket's inner subscription to what it now is, its
// listener or its connection, and wakes the socket's subscriptions: it
// may be ready now.
func (s *unixSocket) attach(to Object) {
	to.subscribe(&s.inner, pollIn|pollOut)
	s.mu.Lock()
	s.readers.tell()
	s.writers.tell()
	s.mu.Unlock()
}

// wake is the socket as the waker of its inner subscription: a change of
// its listener's or its connection's goes on to its subscriptions, of
// reading or of writing as it came.
func (s *unixSocket) wake(l *hook) {
	s.mu.Lock()

	if l == &s.inner.in {
		s.readers.tell()
	} else {
		s.writers.tell()
	}

	s.mu.Unlock()
}

func (s *unixSocket) flags() (int, error) {
	fl := syscall.O_RDWR

	if s.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (s *unixSocket) setFlags(fl int) error {
	s.nonblock.Store(fl&syscall.O_NONBLOCK != 0)

	if _, c := s.state(); c != nil {
		return c.setFlags(fl)
	}

	return nil
}

// bind names the socket with a path of the VFS's, its file system's socket
// node of the mode umask leaves: one that is there is EADDRINUSE.
func (s *unixSocket) bind(p *Process, at, n uint32) error {
	path, err := p.unixSocketPath(at, n)

	if err != nil {
		return err
	}

	if l, c := s.state(); l != nil || c != nil {
		return syscall.EINVAL
	}

	l := &unixListener{ino: pairInodes.Add(1), conns: newMsgChannel(1), kind: s.kind}
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

func (s *unixSocket) listen(backlog int) error {
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

// accept takes the connection that came first: the far end of its pair.
func (s *unixSocket) accept(p *Process, flags uint32) (Object, []byte, error) {
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
			// The connection that comes is given to the accept that waits.
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

// connect connects the socket to the one bound to a path of the VFS's.
func (s *unixSocket) connect(p *Process, at, n uint32) error {
	path, err := p.unixSocketPath(at, n)

	if err != nil {
		return err
	}

	if l, c := s.state(); c != nil {
		return syscall.EISCONN
	} else if l != nil {
		return syscall.EINVAL
	}

	var conn *pairEnd
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

func (s *unixSocket) sockname(p *Process) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return unixAddress([]byte(s.name)), nil
}

func (s *unixSocket) peername(p *Process) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn == nil {
		return nil, syscall.ENOTCONN
	}

	return unixAddress([]byte(s.peer)), nil
}

// connection is the socket's pair end, for what only a connected one
// does: ENOTCONN, or EINVAL for a listening one.
func (s *unixSocket) connection() (*pairEnd, error) {
	l, c := s.state()

	switch {
	case c != nil:
		return c, nil
	case l != nil && l.listens():
		return nil, syscall.EINVAL
	}

	return nil, syscall.ENOTCONN
}

func (s *unixSocket) socketKind() int {
	return s.kind
}

func (s *unixSocket) shutdown(how int) error {
	c, err := s.connection()

	if err != nil {
		return err
	}

	return c.shutdown(how)
}

func (s *unixSocket) getsockopt(p *Process, level, name, at, lenAt uint32) error {
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

func (s *unixSocket) setsockopt(p *Process, level, name, at, n uint32) error {
	if _, c := s.state(); c != nil {
		return c.setsockopt(p, level, name, at, n)
	}

	return s.options.set(p, level, name, at, n)
}

func (s *unixSocket) sendmsg(p *Process, at, flags uint32) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.sendmsg(p, at, flags)
}

func (s *unixSocket) recvmsg(p *Process, at, flags uint32) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.recvmsg(p, at, flags)
}

func (s *unixSocket) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.sendto(p, buf, n, flags, addr, alen)
}

func (s *unixSocket) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	c, err := s.connection()

	if err != nil {
		return 0, nil, err
	}

	return c.recvfrom(p, buf, n, flags)
}

func (s *unixSocket) read(p *Process, b []byte, mode ioMode) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.read(p, b, mode)
}

func (s *unixSocket) write(p *Process, b []byte, mode ioMode) (int, error) {
	c, err := s.connection()

	if err != nil {
		return 0, err
	}

	return c.write(p, b, mode)
}

func (s *unixSocket) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (s *unixSocket) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}
