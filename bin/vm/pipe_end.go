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

func (e *PipeEnd) flags(p *Process) (int, error) {
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

func (e *PipeEnd) setFlags(p *Process, fl int) error {
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

func (e *PipeEnd) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
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

func (e *PipeEnd) write(p *Process, from *MemfdFile, n int, mode IoMode) (int, error) {
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

func (e *PipeEnd) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, e.stat(p, r.st)
	case *FileGetfl:
		var err error

		r.flags, err = e.flags(p)

		return false, err
	case *FileSetfl:
		return false, e.setFlags(p, r.flags)
	case *FileRead:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = e.read(p, r.into, r.n, r.mode)

		return false, err
	case *FileWrite:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = e.write(p, r.from, r.n, r.mode)

		return false, err
	case *FileIoctl:
		return false, e.ioctl(r.req, r.arg)
	}

	return false, errUnserved
}
