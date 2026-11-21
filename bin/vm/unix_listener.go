//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type UnixListener struct {
	refs      atomic.Int32
	mu        sync.Mutex
	conns     MsgChannel[*PairEnd]
	backlog   int
	watchers  Subscribers
	listening bool
	closed    bool
	ino       uint64
	kind      int
}

func (l *UnixListener) ref() {
	l.refs.Add(1)
}

func (l *UnixListener) unref() {
	if l.refs.Add(-1) == 0 {
		l.shut()
	}
}

func (l *UnixListener) shut() {
	var dropped []*PairEnd

	l.mu.Lock()
	l.closed = true

	for e, ok := l.conns.take(); ok; e, ok = l.conns.take() {
		dropped = append(dropped, e)
	}

	l.watchers.tell()
	l.mu.Unlock()

	for _, e := range dropped {
		e.unref()
	}
}

func (l *UnixListener) listens() bool {
	l.mu.Lock()

	defer l.mu.Unlock()

	return l.listening && !l.closed
}

func (l *UnixListener) open(backlog int) {
	l.mu.Lock()
	l.listening, l.backlog = true, min(max(backlog, 1), SOMAXCONN)
	l.watchers.tell()
	l.mu.Unlock()
}

func (l *UnixListener) next(p *Process, nonblock bool) (*PairEnd, error) {
	l.mu.Lock()

	for {
		e, ok := l.conns.take()

		var err error

		switch {
		case !l.listening || l.closed:
			err = EINVAL
		case ok:
			l.watchers.tellOut()
		case nonblock:
			err = EAGAIN
		default:
			_, err = p.watch(&l.mu, &l.watchers, false, time.Time{}, restarts)

			if err == nil {
				continue
			}
		}

		l.mu.Unlock()

		if err != nil && ok {
			e.unref()
		}

		if err != nil {
			return nil, err
		}

		return e, nil
	}
}

func (l *UnixListener) push(e *PairEnd) (queued, refused bool) {
	l.mu.Lock()

	defer l.mu.Unlock()

	if !l.listening || l.closed {
		return false, true
	}

	if l.conns.len() >= l.backlog {
		return false, false
	}

	l.conns.put(e)
	l.watchers.tellIn()

	return true, false
}

func (l *UnixListener) awaitRoom(p *Process, end time.Time, how Signalled) (timedOut bool, err error) {
	l.mu.Lock()

	if l.listening && !l.closed && l.conns.len() >= l.backlog {
		timedOut, err = p.watch(&l.mu, &l.watchers, true, end, how)
	}

	l.mu.Unlock()

	return timedOut, err
}

func (l *UnixListener) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: l.ino, Mode: S_IFSOCK | 0o777, Nlink: 1, Blksize: 4096}

	return nil
}

func (l *UnixListener) poll(events int16, through []*Epoll) int16 {
	l.mu.Lock()

	shut, n, room := l.closed, l.conns.len(), l.conns.len() < l.backlog

	l.mu.Unlock()

	mask := int16(0)

	if n > 0 {
		mask |= POLLIN | POLLRDNORM
	}

	if room || shut {
		mask |= POLLOUT | POLLWRNORM
	}

	if shut {
		mask |= POLLHUP
	}

	return mask & (events | POLLERR | POLLHUP)
}

func (l *UnixListener) subscribe(s *Subscription, events int16) {
	in, out := l.sides(events)

	l.mu.Lock()

	if in {
		l.watchers.add(&s.in)
	}

	if out {
		l.watchers.add(&s.out)
	}

	l.mu.Unlock()
}

func (l *UnixListener) unsubscribe(s *Subscription) {
	l.mu.Lock()
	l.watchers.remove(&s.in)
	l.watchers.remove(&s.out)
	l.mu.Unlock()
}

func (l *UnixListener) sides(events int16) (in, out bool) {
	in, out = events&(POLLIN|POLLRDNORM) != 0, events&(POLLOUT|POLLWRNORM) != 0

	if !in && !out {
		in, out = true, true
	}

	return in, out
}

func (l *UnixListener) flags() (int, error) {
	return O_RDWR, nil
}

func (l *UnixListener) setFlags(fl int) error {
	return nil
}

func (l *UnixListener) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	return 0, EBADF
}

func (l *UnixListener) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	return 0, EBADF
}

func (l *UnixListener) pread(p *Process, into Object, n int, off int64) (int, error) {
	return 0, EBADF
}

func (l *UnixListener) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	return 0, EBADF
}

func (l *UnixListener) seek(p *Process, off int64, whence int) (int64, error) {
	return 0, ESPIPE
}

func (l *UnixListener) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (l *UnixListener) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (l *UnixListener) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	return 0, EBADF
}

func (l *UnixListener) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return 0, EBADF
}

func (l *UnixListener) truncate(p *Process, size int64) error {
	return EINVAL
}

func (l *UnixListener) sync(p *Process, data bool) error {
	return EINVAL
}

func (l *UnixListener) chmod(p *Process, mode uint32) error {
	return EINVAL
}

func (l *UnixListener) chown(p *Process, uid, gid int) error {
	return EINVAL
}

func (l *UnixListener) utimes(p *Process, ts []Timespec) error {
	return EINVAL
}

func (l *UnixListener) statfs(p *Process, st *Statfs) error {
	return EINVAL
}

func (l *UnixListener) flock(p *Process, op int) error {
	return EBADF
}

func (l *UnixListener) recordLock(p *Process, cmd int, lk *Flock) error {
	return EBADF
}

func (l *UnixListener) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	if r, ok := r.(*SockKnock); ok {
		var err error

		r.conn, err = l.knock(p, r.kind, r.nonblock, r.timeout)

		return false, err
	}

	return false, errUnserved
}

func (l *UnixListener) token() (uint64, error) {
	return 0, EXDEV
}

func (l *UnixListener) device() FileKey {
	return FileKey{}
}

func (l *UnixListener) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	if l.kind != kind {
		return nil, EPROTOTYPE
	}

	mine, theirs := newPairOf(kind, nonblock)

	for {
		queued, refused := l.push(theirs)

		if queued {
			return mine, nil
		}

		var err error

		if !refused && !nonblock {
			end, how := until(timeout)

			var timedOut bool

			if timedOut, err = l.awaitRoom(p, end, how); err == nil && !timedOut {
				continue
			} else if err == nil {
				err = EAGAIN
			}
		} else if refused {
			err = ECONNREFUSED
		} else {
			err = EAGAIN
		}

		mine.unref()
		theirs.unref()

		return nil, err
	}
}

func (l *UnixListener) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (l *UnixListener) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (l *UnixListener) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
