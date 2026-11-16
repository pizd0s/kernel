//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/binary"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// AF_UNIX's sockets are the kernel's own, as its socketpairs are
// (kernel_endpoint.go): SOCK_STREAM and SOCK_SEQPACKET ones here, SOCK_DGRAM
// ones unixDgram. bind names a socket with a path of the VFS's: the file
// system of the path's directory makes a socket node there, which holds
// what the socket's name stands for (BIND, kernel_vfs.go), the listener
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
	// subs are the socket's subscriptions, which go on to its connection
	// or its listener as it gets one.
	subs []socketSub
	// The options are the socket's until it is connected, and its
	// connection's then, which gets them, as a connection accept takes
	// gets the listening socket's.
	options sockOptions
}

type socketSub struct {
	to     waker
	id     uint64
	events int16
}

// unixListener is a bound socket's: the connections that wait for accept,
// as many as listen's backlog lets, while its socket is open and listens,
// in a channel (kernel_queue.go) whose messages are the connections' ends,
// of its socket's kind. The socket node of its name holds it too.
type unixListener struct {
	refs      atomic.Int32
	mu        sync.Mutex
	conns     *msgChannel
	listening bool
	closed    bool
	ino       uint64
	kind      int
}

// somaxconn is the most connections a listener holds, Linux's default.
const somaxconn = 4096

func newUnixSocket(kind int, nonblock bool) *unixSocket {
	s := &unixSocket{kind: kind}
	s.refs.Store(1)
	s.nonblock.Store(nonblock)

	return s
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

// subscribe is to the connection or the listener, and to the one the
// socket gets when it has neither yet.
func (s *unixSocket) subscribe(to waker, id uint64, events int16) {
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
func attach(subs []socketSub, f Object) {
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
func (s *unixSocket) accept(p *Process, flags uint32) (Object, []byte, error) {
	l, _ := s.state()

	if l == nil || !l.listens() {
		return nil, nil, syscall.EINVAL
	}

	for {
		if e := l.pop(); e != nil {
			e.nonblock.Store(flags&sockNonblock != 0)
			e.options.copyOptions(&s.options)

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
	subs := slices.Clone(s.subs)
	s.mu.Unlock()
	attach(subs, conn)

	return nil
}

// connectUnix makes a connection of a kind to the socket a path of the
// VFS's names: the end of the pair that is the caller's; the other is in
// the queue of the socket's listener. A full queue is EAGAIN for nonblock,
// else a wait for room, which the timeout of to ends, SO_SNDTIMEO's,
// EAGAIN too.
func (p *Process) connectUnix(v Dir, path string, kind int, nonblock bool, to *atomic.Int64) (*pairEnd, error) {
	f, err := v.connectPath(p, path)

	if err != nil {
		return nil, err
	}

	l := f.asListener()

	if l == nil || l.kind != kind {
		f.unref()

		if f.asPair() != nil || l != nil {
			return nil, syscall.EPROTOTYPE
		}

		return nil, syscall.ECONNREFUSED
	}

	defer l.unref()
	mine, theirs := newPairOf(kind, nonblock)

	for {
		queued, refused := l.push(theirs)

		if queued {
			return mine, nil
		}

		if !refused && !nonblock {
			timeout := -1

			if ns := to.Load(); ns > 0 {
				timeout = waitMs(time.Duration(ns))
			}

			var ready int

			if ready, err = p.waitObject(l, pollOut, timeout, timeout < 0); err == nil && ready > 0 {
				continue
			} else if err == nil {
				err = syscall.EAGAIN
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

func (l *unixListener) ref() {
	l.refs.Add(1)
}

func (l *unixListener) unref() {
	if l.refs.Add(-1) == 0 {
		l.shut()
	}
}

// A unixListener is a File, and itself to a call that wants one.
func (l *unixListener) asFile() File              { return l }
func (l *unixListener) asTransfer() Transfer      { return nil }
func (l *unixListener) asSeeker() Seeker          { return nil }
func (l *unixListener) asDir() Dir                { return nil }
func (l *unixListener) asTerminal() Terminal      { return nil }
func (l *unixListener) asWindowed() Windowed      { return nil }
func (l *unixListener) asLink() Link              { return nil }
func (l *unixListener) asAttrs() Attrs            { return nil }
func (l *unixListener) asSocket() Socket          { return nil }
func (l *unixListener) asIoctl() Ioctl            { return nil }
func (l *unixListener) asLocker() Locker          { return nil }
func (l *unixListener) asEpoll() *epoll           { return nil }
func (l *unixListener) asMemfd() *memfdFile       { return nil }
func (l *unixListener) asPair() *pairEnd          { return nil }
func (l *unixListener) asListener() *unixListener { return l }
func (l *unixListener) asConsole() *consoleEnd    { return nil }
func (l *unixListener) asVFS() *remoteDir         { return nil }

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

	return pk.files[0].asPair()
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

	full, err := l.conns.put(packet{files: []Object{e}})

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
func (l *unixListener) subscribe(to waker, id uint64, events int16) {
	in, out := l.sides(events)

	if in {
		l.conns.readers.add(to, id)
	}

	if out {
		l.conns.writers.add(to, id)
	}
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
	subs         []socketSub
}

func newUnixDgram(nonblock bool) *unixDgram {
	s := &unixDgram{}
	s.refs.Store(1)
	s.nonblock.Store(nonblock)
	s.inbox, s.mouth = newPairOf(sockDgram, false)

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

func (s *unixDgram) subscribe(to waker, id uint64, events int16) {
	s.mu.Lock()
	s.subs = append(s.subs, socketSub{to, id, events})
	s.mu.Unlock()
	s.inbox.subscribe(to, id, events)

	if t := s.targetEnd(); t != nil {
		t.subscribe(to, id, pollOut)
		t.unref()
	}
}

func (s *unixDgram) unsubscribe(to waker, id uint64) {
	s.mu.Lock()
	s.subs = slices.DeleteFunc(s.subs, func(sub socketSub) bool { return sub.to == to && sub.id == id })
	s.mu.Unlock()
	s.inbox.unsubscribe(to, id)

	if t := s.targetEnd(); t != nil {
		t.unsubscribe(to, id)
		t.unref()
	}
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

// mouthOf is the mouth of the datagram socket a path names, held.
func (p *Process) mouthOf(path string) (*pairEnd, error) {
	var mouth *pairEnd
	err := p.onPath(atFdcwd, path, func(v Dir) error {
		f, err := v.connectPath(p, path)

		if err != nil {
			return err
		}

		if e := f.asPair(); e != nil && e.shared.kind == sockDgram {
			mouth = e

			return nil
		}

		f.unref()

		return syscall.EPROTOTYPE
	})

	return mouth, err
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
	subs := slices.Clone(s.subs)
	s.mu.Unlock()

	if old != nil {
		for _, sub := range subs {
			old.unsubscribe(sub.to, sub.id)
		}

		old.unref()
	}

	for _, sub := range subs {
		mouth.subscribe(sub.to, sub.id, pollOut)
		sub.to.wake(sub.id)
	}

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

// destination is the path of sendto's or sendmsg's address, none without
// one.
func (p *Process) destination(addr, alen uint32) (string, error) {
	if addr == 0 {
		return "", nil
	}

	return p.unixSocketPath(addr, alen)
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
