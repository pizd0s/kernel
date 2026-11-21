//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type UnixDgram struct {
	refs         atomic.Int32
	nonblock     atomic.Bool
	inbox, mouth *PairEnd
	mu           sync.Mutex
	name, peer   string
	target       Object

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

func (s *UnixDgram) targetEnd() Object {
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

	err = p.onPath(AT_FDCWD, path, func(v Object) error {
		return p.dirBind(v, path, 0o777&^p.umask, s.mouth)
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

func (s *UnixDgram) connect(p *Process, a Address) error {
	path, err := a.path, a.err

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

func (s *UnixDgram) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return s.inbox.getsockopt(p, level, name)
}

func (s *UnixDgram) setsockopt(level, name uint32, value []byte) error {
	return s.inbox.setsockopt(level, name, value)
}

func (s *UnixDgram) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	if to.err != nil {
		return 0, to.err
	}

	if s.inbox.noWrite.Load() {
		return 0, EPIPE
	}

	if len(pk.data) > packetMax {
		return 0, EMSGSIZE
	}

	var t Object

	if to.given {
		var err error

		if t, err = p.mouthOf(to.path); err != nil {
			return 0, err
		}
	} else if t = s.targetEnd(); t == nil {
		return 0, ENOTCONN
	}

	defer t.unref()

	s.mu.Lock()

	if s.name != "" {
		pk.addr = []byte(s.name)
	}

	s.mu.Unlock()

	mode.nonblock = mode.nonblock || s.nonblock.Load()
	mode.timeout = &s.inbox.options.sndTimeout

	return t.send(p, pk, Address{}, mode)
}

func (s *UnixDgram) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	return s.inbox.recv(p, into, want, flags|s.dontwait())
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

func (s *UnixDgram) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	got, from, _, err := s.inbox.recv(p, Into{p: p, to: into}, n, s.dontwait()|bit(mode.nonblock)*MSG_DONTWAIT)

	dropFiles(from.files)

	return got, err
}

func (s *UnixDgram) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	if n > packetMax {
		return 0, EMSGSIZE
	}

	data := make([]byte, n)

	return s.send(p, &Packet{data: data[:takeFrom(from)(0, data)]}, Address{}, mode)
}

func (s *UnixDgram) pread(p *Process, into Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (s *UnixDgram) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (s *UnixDgram) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (s *UnixDgram) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (s *UnixDgram) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (s *UnixDgram) seek(p *Process, off int64, whence int) (int64, error) {
	return 0, ESPIPE
}

func (s *UnixDgram) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (s *UnixDgram) truncate(p *Process, size int64) error {
	return EINVAL
}

func (s *UnixDgram) sync(p *Process, data bool) error {
	return EINVAL
}

func (s *UnixDgram) chmod(p *Process, mode uint32) error {
	return EINVAL
}

func (s *UnixDgram) chown(p *Process, uid, gid int) error {
	return EINVAL
}

func (s *UnixDgram) utimes(p *Process, ts []Timespec) error {
	return EINVAL
}

func (s *UnixDgram) statfs(p *Process, st *Statfs) error {
	return EINVAL
}

func (s *UnixDgram) flock(p *Process, op int) error {
	return EBADF
}

func (s *UnixDgram) recordLock(p *Process, cmd int, lk *Flock) error {
	return EBADF
}

func (s *UnixDgram) call(p *Process, op, arg, group uint32, body []byte, files []Object, interruptible bool) (Packet, bool, error) {
	return Packet{}, false, errUnserved
}

func (s *UnixDgram) token() (uint64, error) {
	return 0, EXDEV
}

func (s *UnixDgram) device() FileKey {
	return FileKey{}
}

func (s *UnixDgram) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	return nil, ECONNREFUSED
}

func (s *UnixDgram) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (s *UnixDgram) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (s *UnixDgram) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
