//go:build linux && amd64

package main

import (
	"sync/atomic"
	"time"
	"unsafe"
)

type PipeEnd struct {
	pipe     *Pipe
	reads    bool
	writes   bool
	refs     atomic.Int32
	nonblock atomic.Bool
}

func (e *PipeEnd) ref() {
	e.refs.Add(1)
}

func (e *PipeEnd) unref() {
	if e.refs.Add(-1) != 0 {
		return
	}

	pp := e.pipe

	if pp.leave(e.reads, e.writes) && pp.name != nil {
		pp.name.forget(pp)
	}
}

func (e *PipeEnd) sides(events int16) (in, out bool) {
	in, out = e.reads, e.writes

	if in && out && events&(POLLIN|POLLRDNORM|POLLOUT|POLLWRNORM) != 0 {
		in, out = events&(POLLIN|POLLRDNORM) != 0, events&(POLLOUT|POLLWRNORM) != 0
	}

	return in, out
}

func (e *PipeEnd) subscribe(s *Subscription, events int16) {
	pp := e.pipe
	in, out := e.sides(events)

	pp.mu.Lock()

	if in {
		pp.watchers.add(&s.in)
	}

	if out {
		pp.watchers.add(&s.out)
	}

	pp.mu.Unlock()
}

func (e *PipeEnd) unsubscribe(s *Subscription) {
	pp := e.pipe

	pp.mu.Lock()
	pp.watchers.remove(&s.in)
	pp.watchers.remove(&s.out)
	pp.mu.Unlock()
}

func (e *PipeEnd) stat(p *Process, st *Stat) error {
	pp := e.pipe

	*st = Stat{Ino: pp.ino, Mode: S_IFIFO | 0o600, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: pipeChunk}

	if n := pp.name; n != nil {
		st.Dev, st.Ino, st.Mode = n.key.dev, n.key.ino, n.mode
	}

	st.Atim = nsecToTimespec(pp.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (e *PipeEnd) poll(events int16, through []*Epoll) int16 {
	pp := e.pipe

	pp.mu.Lock()

	unread, readers, writers := pp.bytes, pp.readers, pp.writers

	pp.mu.Unlock()

	mask := int16(0)

	if e.writes {
		if pipeSize-unread >= pipeChunk {
			mask |= POLLOUT | POLLWRNORM
		}

		if readers == 0 {
			mask |= POLLERR
		}
	}

	if e.reads {
		if unread > 0 || writers == 0 {
			mask |= POLLIN | POLLRDNORM
		}

		if writers == 0 {
			mask |= POLLHUP
		}
	}

	return mask & (events | POLLERR | POLLHUP)
}

func (e *PipeEnd) flags() (int, error) {
	fl := O_RDWR

	if !e.writes {
		fl = O_RDONLY
	} else if !e.reads {
		fl = O_WRONLY
	}

	if e.nonblock.Load() {
		fl |= O_NONBLOCK
	}

	return fl, nil
}

func (e *PipeEnd) setFlags(fl int) error {
	e.nonblock.Store(fl&O_NONBLOCK != 0)

	return nil
}

func (e *PipeEnd) ioctl(req uintptr, arg unsafe.Pointer) error {
	if req != FIONREAD || !e.reads {
		return ENOTTY
	}

	*(*int32)(arg) = int32(e.pipe.unread())

	return nil
}

func (e *PipeEnd) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	if p.chaosFail("read") {
		return 0, EIO
	}

	if !e.reads {
		return 0, EBADF
	}

	if n == 0 {
		return 0, nil
	}

	pp := e.pipe

	pp.mu.Lock()

	for {
		eof := false

		got, err := into.with(0, n, true, func(b []byte) (int, error) {
			taken, ended := pp.take(b)

			eof = ended

			return taken, nil
		})

		if err != nil || got > 0 || eof {
			pp.mu.Unlock()

			return got, err
		}

		if mode.nonblock || e.nonblock.Load() {
			pp.mu.Unlock()

			return 0, EAGAIN
		}

		if _, err := p.watch(&pp.mu, &pp.watchers, false, time.Time{}, restarts); err != nil {
			pp.mu.Unlock()

			return 0, err
		}
	}
}

func (e *PipeEnd) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	if p.chaosFail("write") {
		return 0, EIO
	}

	if !e.writes {
		return 0, EBADF
	}

	pp := e.pipe
	total := 0

	var err error

	pp.mu.Lock()

	for total < n && err == nil {
		var full, gone bool

		put, failed := from.with(int64(total), min(n-total, pipeChunk), false, func(b []byte) (int, error) {
			if full, gone = pp.put(b); full || gone {
				return 0, nil
			}

			return len(b), nil
		})

		switch {
		case failed != nil:
			err = failed
		case gone:
			err = EPIPE
		case !full:
			total += put

			if put == 0 {
				n = total
			}
		case mode.nonblock || e.nonblock.Load():
			err = EAGAIN
		default:
			_, err = p.watch(&pp.mu, &pp.watchers, true, time.Time{}, restarts)
		}
	}

	pp.mu.Unlock()

	if err == EPIPE && !mode.quiet {
		p.sendSelf(SIGPIPE)
	}

	if total > 0 {
		return total, nil
	}

	return 0, err
}

func (e *PipeEnd) pread(p *Process, into Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (e *PipeEnd) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (e *PipeEnd) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (e *PipeEnd) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (e *PipeEnd) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (e *PipeEnd) seek(p *Process, off int64, whence int) (int64, error) {
	return 0, ESPIPE
}

func (e *PipeEnd) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (e *PipeEnd) truncate(p *Process, size int64) error {
	return EINVAL
}

func (e *PipeEnd) sync(p *Process, data bool) error {
	return EINVAL
}

func (e *PipeEnd) chmod(p *Process, mode uint32) error {
	return EINVAL
}

func (e *PipeEnd) chown(p *Process, uid, gid int) error {
	return EINVAL
}

func (e *PipeEnd) utimes(p *Process, ts []Timespec) error {
	return EINVAL
}

func (e *PipeEnd) statfs(p *Process, st *Statfs) error {
	return EINVAL
}

func (e *PipeEnd) flock(p *Process, op int) error {
	return EBADF
}

func (e *PipeEnd) recordLock(p *Process, cmd int, lk *Flock) error {
	return EBADF
}

func (e *PipeEnd) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	return false, errUnserved
}

func (e *PipeEnd) token() (uint64, error) {
	return 0, EXDEV
}

func (e *PipeEnd) device() FileKey {
	return FileKey{}
}

func (e *PipeEnd) bind(p *Process, a Address) error {
	return ENOTSOCK
}

func (e *PipeEnd) connect(p *Process, a Address) error {
	return ENOTSOCK
}

func (e *PipeEnd) listen(backlog int) error {
	return ENOTSOCK
}

func (e *PipeEnd) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, ENOTSOCK
}

func (e *PipeEnd) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	return nil, ECONNREFUSED
}

func (e *PipeEnd) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	return 0, ENOTSOCK
}

func (e *PipeEnd) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	return 0, Packet{}, 0, ENOTSOCK
}

func (e *PipeEnd) sockname(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (e *PipeEnd) peername(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (e *PipeEnd) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return nil, ENOTSOCK
}

func (e *PipeEnd) setsockopt(level, name uint32, value []byte) error {
	return ENOTSOCK
}

func (e *PipeEnd) shutdown(how int) error {
	return ENOTSOCK
}

func (e *PipeEnd) consume(sink func(pk *Packet)) {
}

func (e *PipeEnd) socketKind() int {
	return 0
}

func (e *PipeEnd) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (e *PipeEnd) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
