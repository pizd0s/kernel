//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type Spliced struct {
	of   Object
	at   int64
	n    int64
	mode int
	refs atomic.Int32

	mu  sync.Mutex
	off int64
}

func newSpliced(of Object, at int64, n int, mode int) *Spliced {
	of.ref()

	s := &Spliced{of: of, at: max(at, 0), n: int64(max(n, 0)), mode: mode}

	s.refs.Store(1)

	return s
}

func (s *Spliced) ref() {
	s.refs.Add(1)
}

func (s *Spliced) unref() {
	if s.refs.Add(-1) == 0 {
		s.of.unref()
	}
}

func (s *Spliced) poll(events int16, through []*Epoll) int16 {
	return s.of.poll(events, through)
}

func (s *Spliced) subscribe(sub *Subscription, events int16) {
	s.of.subscribe(sub, events)
}

func (s *Spliced) unsubscribe(sub *Subscription) {
	s.of.unsubscribe(sub)
}

func (s *Spliced) part(off int64, n int) (int64, int) {
	off = min(max(off, 0), s.n)

	return s.at + off, int(min(int64(max(n, 0)), s.n-off))
}

func (s *Spliced) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	s.mu.Lock()

	defer s.mu.Unlock()

	count, err := s.pread(p, into, n, s.off)

	s.off += int64(count)

	return count, err
}

func (s *Spliced) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	s.mu.Lock()

	defer s.mu.Unlock()

	count, err := s.pwrite(p, from, n, s.off)

	s.off += int64(count)

	return count, err
}

func (s *Spliced) pread(p *Process, into Object, n int, off int64) (int, error) {
	switch {
	case !grants(s.mode, false):
		return 0, EBADF
	case off < 0 || n < 0:
		return 0, EINVAL
	case n == 0 || off >= s.n:
		return 0, nil
	}

	where, n := s.part(off, n)

	return s.of.pread(p, into, n, where)
}

func (s *Spliced) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	switch {
	case !grants(s.mode, true):
		return 0, EBADF
	case off < 0 || n < 0:
		return 0, EINVAL
	case n == 0:
		return 0, nil
	case off >= s.n:
		return 0, ENOSPC
	}

	where, n := s.part(off, n)

	return s.of.pwrite(p, from, n, where)
}

func (s *Spliced) seek(p *Process, off int64, whence int) (int64, error) {
	s.mu.Lock()

	defer s.mu.Unlock()

	switch whence {
	case 0:
	case 1:
		off += s.off
	case 2:
		off += s.n
	default:
		return 0, EINVAL
	}

	if off < 0 {
		return 0, EINVAL
	}

	s.off = off

	return off, nil
}

func (s *Spliced) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	switch {
	case !grants(s.mode, writes):
		return 0, EBADF
	case at < 0 || n < 0:
		return 0, EINVAL
	}

	at, n = s.part(at, n)

	return s.of.with(at, n, writes, fn)
}

func (s *Spliced) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	switch {
	case !grants(s.mode, false):
		return 0, EBADF
	case n == 0 || from >= s.n:
		return 0, nil
	}

	at, n := s.part(from, n)

	return s.of.copyTo(p, out, at, to, n)
}

func (s *Spliced) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	switch {
	case !grants(s.mode, true):
		return 0, EBADF
	case n == 0:
		return 0, nil
	case to >= s.n:
		return 0, ENOSPC
	}

	at, n := s.part(to, n)

	return s.of.copyFrom(p, in, from, at, n)
}

func (s *Spliced) stat(p *Process, st *Stat) error {
	return EBADF
}

func (s *Spliced) flags() (int, error) {
	return 0, EBADF
}

func (s *Spliced) setFlags(fl int) error {
	return EBADF
}

func (s *Spliced) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (s *Spliced) truncate(p *Process, size int64) error {
	return EINVAL
}

func (s *Spliced) sync(p *Process, data bool) error {
	return EINVAL
}

func (s *Spliced) chmod(p *Process, mode uint32) error {
	return EINVAL
}

func (s *Spliced) chown(p *Process, uid, gid int) error {
	return EINVAL
}

func (s *Spliced) utimes(p *Process, ts []Timespec) error {
	return EINVAL
}

func (s *Spliced) statfs(p *Process, st *Statfs) error {
	return EINVAL
}

func (s *Spliced) flock(p *Process, op int) error {
	return EBADF
}

func (s *Spliced) recordLock(p *Process, cmd int, lk *Flock) error {
	return EBADF
}

func (s *Spliced) call(p *Process, op, arg, group uint32, body []byte, files []Object, interruptible bool) (Packet, bool, error) {
	return Packet{}, false, errUnserved
}

func (s *Spliced) token() (uint64, error) {
	return 0, EXDEV
}

func (s *Spliced) device() FileKey {
	return FileKey{}
}

func (s *Spliced) bind(p *Process, a Address) error {
	return ENOTSOCK
}

func (s *Spliced) connect(p *Process, a Address) error {
	return ENOTSOCK
}

func (s *Spliced) listen(backlog int) error {
	return ENOTSOCK
}

func (s *Spliced) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, ENOTSOCK
}

func (s *Spliced) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	return nil, ECONNREFUSED
}

func (s *Spliced) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	return 0, ENOTSOCK
}

func (s *Spliced) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	return 0, Packet{}, 0, ENOTSOCK
}

func (s *Spliced) sockname(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (s *Spliced) peername(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (s *Spliced) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return nil, ENOTSOCK
}

func (s *Spliced) setsockopt(level, name uint32, value []byte) error {
	return ENOTSOCK
}

func (s *Spliced) shutdown(how int) error {
	return ENOTSOCK
}

func (s *Spliced) consume(sink func(pk *Packet)) {
}

func (s *Spliced) socketKind() int {
	return 0
}

func (s *Spliced) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (s *Spliced) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (s *Spliced) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
