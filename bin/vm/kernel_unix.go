//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/binary"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
)

// AF_UNIX's SOCK_SEQPACKET sockets of a process of the system's, whose
// root is the VFS's: the kernel's own, as its socketpairs are
// (kernel_endpoint.go), so a message carries the files of the kernel as
// well as the host's. bind names a socket with a path of the VFS's: the
// file system of the path's directory makes a socket node there, which
// holds the socket's listener (BIND, kernel_vfs.go). connect walks a path
// to such a node and takes the listener its file system gives back
// (CONNECT): one end of a new pair goes into the listener's queue, accept
// takes it from there, and the other is the connecting socket's from then
// on. A listener whose socket closed, or that does not listen, refuses:
// ECONNREFUSED. An abstract name is none of the VFS's, EOPNOTSUPP; the
// other kinds of socket, and a process of the host's, have the host's
// (kernel_socket.go).
type unixSocket struct {
	refs     atomic.Int32
	nonblock atomic.Bool
	mu       sync.Mutex
	name     string
	listener *unixListener
	conn     *pairEnd
	peer     string
	// subs are the socket's subscriptions, which go on to its connection
	// or its listener as it gets one.
	subs []socketSub
}

type socketSub struct {
	to     waker
	id     uint64
	events int16
}

// unixListener is a bound socket's: the connections that wait for accept,
// as many as listen's backlog lets, while its socket is open and listens,
// in a channel (kernel_queue.go) whose messages are the connections' ends.
// The socket node of its name holds it too.
type unixListener struct {
	refs      atomic.Int32
	mu        sync.Mutex
	conns     *msgChannel
	listening bool
	closed    bool
	ino       uint64
}

var (
	_ Socket   = (*unixSocket)(nil)
	_ Transfer = (*unixSocket)(nil)
	_ File     = (*unixListener)(nil)
)

// somaxconn is the most connections a listener holds, Linux's default.
const somaxconn = 4096

func newUnixSocket(nonblock bool) *unixSocket {
	s := &unixSocket{}
	s.refs.Store(1)
	s.nonblock.Store(nonblock)

	return s
}

// systemSocket tells whether a socket of a kind is the kernel's: an
// AF_UNIX SOCK_SEQPACKET one of a process whose root is the VFS's.
func (p *Process) systemSocket(domain, kind uint32) bool {
	_, system := p.root.(*remoteDir)

	return system && domain == afUnix && kind&^(sockNonblock|sockCloexec) == sockSeqpacket
}

// unixSocketPath is the path of a guest's sockaddr_un: an abstract name is
// none of the VFS's.
func (p *Process) unixSocketPath(at, n uint32) (string, error) {
	if n <= 2 || n > sockaddrMax || p.u16(at) != afUnix {
		return "", syscall.EINVAL
	}

	path := p.span(at+2, n-2)

	if path[0] == 0 {
		return "", syscall.EOPNOTSUPP
	}

	if i := bytes.IndexByte(path, 0); i >= 0 {
		path = path[:i]
	}

	if len(path) == 0 || len(path) >= sunPathMax {
		return "", syscall.EINVAL
	}

	return string(path), nil
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
		l.shut()
		l.unref()
	}

	if c != nil {
		c.unref()
	}
}

func (s *unixSocket) state() (*unixListener, *pairEnd) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.listener, s.conn
}

func (s *unixSocket) stat(p *Process, st *syscall.Stat_t) error {
	if _, c := s.state(); c != nil {
		return c.stat(p, st)
	}

	*st = syscall.Stat_t{Mode: syscall.S_IFSOCK | 0o777, Nlink: 1, Uid: uint32(syscall.Getuid()), Gid: uint32(syscall.Getgid()), Blksize: 4096}

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

// subscribe is to the connection or the listener, and to the one the
// socket gets when it has neither yet.
func (s *unixSocket) subscribe(to waker, id uint64, events int16) bool {
	s.mu.Lock()
	s.subs = append(s.subs, socketSub{to, id, events})
	l, c := s.listener, s.conn
	s.mu.Unlock()

	switch {
	case c != nil:
		c.subscribe(to, id, events)
	case l != nil:
		l.subscribe(to, id, events)
	}

	return true
}

func (s *unixSocket) unsubscribe(to waker, id uint64) {
	s.mu.Lock()
	s.subs = slices.DeleteFunc(s.subs, func(sub socketSub) bool { return sub.to == to && sub.id == id })
	l, c := s.listener, s.conn
	s.mu.Unlock()

	if c != nil {
		c.unsubscribe(to, id)
	}

	if l != nil {
		l.unsubscribe(to, id)
	}
}

// attach gives the subscriptions of the socket to what it now is, which
// it took in the same hold of its lock, and wakes them.
func attach(subs []socketSub, f File) {
	for _, sub := range subs {
		f.subscribe(sub.to, sub.id, sub.events)
		sub.to.wake(sub.id)
	}
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

	l := &unixListener{ino: pairInodes.Add(1), conns: newMsgChannel(1)}
	l.refs.Store(1)
	handled, err := p.onVFS(atFdcwd, path, func(v *remoteDir) error {
		return v.bindPath(p, path, 0o777&^p.umask, l)
	})

	if !handled {
		err = syscall.EOPNOTSUPP
	}

	if err == syscall.EEXIST {
		err = syscall.EADDRINUSE
	}

	if err != nil {
		l.unref()

		return err
	}

	s.mu.Lock()
	s.name, s.listener = path, l
	subs := slices.Clone(s.subs)
	s.mu.Unlock()
	attach(subs, l)

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
func (s *unixSocket) accept(p *Process, flags uint32) (File, []byte, error) {
	l, _ := s.state()

	if l == nil || !l.listens() {
		return nil, nil, syscall.EINVAL
	}

	for {
		if e := l.pop(); e != nil {
			e.nonblock.Store(flags&sockNonblock != 0)

			return e, unixAddress(nil), nil
		}

		if s.nonblock.Load() {
			return nil, nil, syscall.EAGAIN
		}

		if _, err := p.waitObject(s, pollIn, -1, true); err != nil {
			return nil, nil, err
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
	handled, err := p.onVFS(atFdcwd, path, func(v *remoteDir) error {
		var err error
		conn, err = p.connectUnix(v, path, s.nonblock.Load())

		return err
	})

	if !handled {
		err = syscall.EOPNOTSUPP
	}

	if err != nil {
		return err
	}

	s.mu.Lock()
	s.conn, s.peer = conn, path
	subs := slices.Clone(s.subs)
	s.mu.Unlock()
	attach(subs, conn)

	return nil
}

// connectUnix makes a connection to the socket a path of the VFS's names:
// the end of the pair that is the caller's; the other is in the queue of
// the socket's listener. A full queue is EAGAIN for nonblock, else a wait
// for room.
func (p *Process) connectUnix(v *remoteDir, path string, nonblock bool) (*pairEnd, error) {
	f, err := v.connectPath(p, path)

	if err != nil {
		return nil, err
	}

	l, ok := f.(*unixListener)

	if !ok {
		f.unref()

		return nil, syscall.ECONNREFUSED
	}

	defer l.unref()
	mine, theirs := newPair(nonblock)

	for {
		queued, refused := l.push(theirs)

		if queued {
			return mine, nil
		}

		if !refused && !nonblock {
			if _, err = p.waitObject(l, pollOut, -1, true); err == nil {
				continue
			}
		} else if refused {
			err = syscall.ECONNREFUSED
		} else {
			err = syscall.EAGAIN
		}

		mine.unref()
		theirs.unref()

		return nil, err
	}
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

	// Unconnected, the options are those of an end of a pair.
	unconnected, other := newPair(false)
	defer unconnected.unref()
	defer other.unref()

	return unconnected.getsockopt(p, level, name, at, lenAt)
}

func (s *unixSocket) setsockopt(p *Process, level, name, at, n uint32) error {
	if level != solSocket {
		return syscall.ENOPROTOOPT
	}

	return nil
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

func (l *unixListener) ref() {
	l.refs.Add(1)
}

func (l *unixListener) unref() {
	if l.refs.Add(-1) == 0 {
		l.shut()
	}
}

// shut ends the listener: it refuses from then on, and the connections
// that waited are reset.
func (l *unixListener) shut() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.conns.finish()
	l.conns.nudge()
}

func (l *unixListener) listens() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.listening && !l.closed
}

// pop is the first connection that waits, or none.
func (l *unixListener) pop() *pairEnd {
	pk, _, ok, _ := l.conns.take()

	if !ok || len(pk.files) == 0 {
		return nil
	}

	return pk.files[0].(*pairEnd)
}

// push queues a connection: refused when the listener is shut or does
// not listen, not queued when its backlog is full.
func (l *unixListener) push(e *pairEnd) (queued, refused bool) {
	l.mu.Lock()
	listens := l.listening && !l.closed
	l.mu.Unlock()

	if !listens {
		return false, true
	}

	full, err := l.conns.put(packet{files: []File{e}})

	if err != nil {
		return false, true
	}

	return !full, false
}

func (l *unixListener) stat(p *Process, st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Ino: l.ino, Mode: syscall.S_IFSOCK | 0o777, Nlink: 1, Blksize: 4096}

	return nil
}

// poll is accept's input, a connection waits, and connect's output, the
// queue has room.
func (l *unixListener) poll(events int16) int16 {
	l.mu.Lock()
	shut := l.closed
	l.mu.Unlock()
	n, room := l.conns.count(), l.conns.room()
	mask := int16(0)

	if n > 0 {
		mask |= pollIn | pollRdnorm
	}

	if room || shut {
		mask |= pollOut | pollWrnorm
	}

	if shut {
		mask |= pollHup
	}

	return mask & (events | pollErr | pollHup)
}

// subscribe makes to learn of the connections that come, for accept's
// input, and of the room for them, for connect's output.
func (l *unixListener) subscribe(to waker, id uint64, events int16) bool {
	in, out := l.sides(events)

	if in {
		l.conns.readers.add(to, id)
	}

	if out {
		l.conns.writers.add(to, id)
	}

	return true
}

func (l *unixListener) unsubscribe(to waker, id uint64) {
	l.conns.readers.remove(to, id)
	l.conns.writers.remove(to, id)
}

// sides are what of the listener a wait for events is on: accept's input,
// connect's output, both when neither is asked.
func (l *unixListener) sides(events int16) (in, out bool) {
	in, out = events&(pollIn|pollRdnorm) != 0, events&(pollOut|pollWrnorm) != 0

	if !in && !out {
		in, out = true, true
	}

	return in, out
}

func (l *unixListener) flags() (int, error) {
	return syscall.O_RDWR, nil
}

func (l *unixListener) setFlags(fl int) error {
	return nil
}
