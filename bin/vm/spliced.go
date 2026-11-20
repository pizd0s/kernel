//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
)

type Spliced struct {
	of   Transfer
	at   int64
	n    int64
	mode int
	refs atomic.Int32

	mu  sync.Mutex
	off int64
}

func newSpliced(of Transfer, at int64, n int, mode int) *Spliced {
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

func (s *Spliced) asFile() File {
	return nil
}

func (s *Spliced) asTransfer() Transfer {
	return s
}

func (s *Spliced) asSeeker() Seeker {
	return s
}

func (s *Spliced) asDir() Dir {
	return nil
}

func (s *Spliced) asTerminal() Terminal {
	return nil
}

func (s *Spliced) asLink() Link {
	return nil
}

func (s *Spliced) asAttrs() Attrs {
	return nil
}

func (s *Spliced) asSocket() Socket {
	return nil
}

func (s *Spliced) asIoctl() Ioctl {
	return nil
}

func (s *Spliced) asLocker() Locker {
	return nil
}

func (s *Spliced) asEpoll() *Epoll {
	return nil
}

func (s *Spliced) asPair() *PairEnd {
	return nil
}

func (s *Spliced) asListener() *UnixListener {
	return nil
}

func (s *Spliced) asConsole() *ConsoleEnd {
	return nil
}

func (s *Spliced) asRemote() *RemoteFD {
	return nil
}

func (s *Spliced) part(off int64, n int) (int64, int) {
	off = min(max(off, 0), s.n)

	return s.at + off, int(min(int64(max(n, 0)), s.n-off))
}

func (s *Spliced) read(p *Process, into Transfer, n int, mode IoMode) (int, error) {
	s.mu.Lock()

	defer s.mu.Unlock()

	count, err := s.pread(p, into, n, s.off)

	s.off += int64(count)

	return count, err
}

func (s *Spliced) write(p *Process, from Transfer, n int, mode IoMode) (int, error) {
	s.mu.Lock()

	defer s.mu.Unlock()

	count, err := s.pwrite(p, from, n, s.off)

	s.off += int64(count)

	return count, err
}

func (s *Spliced) pread(p *Process, into Transfer, n int, off int64) (int, error) {
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

func (s *Spliced) pwrite(p *Process, from Transfer, n int, off int64) (int, error) {
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

func (s *Spliced) splice(at int64, n int, mode int) Transfer {
	at, n = s.part(at, n)

	return newSpliced(s.of, at, n, narrowed(s.mode, mode))
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

func (s *Spliced) copyTo(p *Process, out Transfer, from, to int64, n int) (int, error) {
	switch {
	case !grants(s.mode, false):
		return 0, EBADF
	case n == 0 || from >= s.n:
		return 0, nil
	}

	at, n := s.part(from, n)

	return s.of.copyTo(p, out, at, to, n)
}

func (s *Spliced) copyFrom(p *Process, in Transfer, from, to int64, n int) (int, error) {
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
