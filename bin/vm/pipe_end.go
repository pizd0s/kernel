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

func (e *PipeEnd) asFile() File {
	return e
}

func (e *PipeEnd) asTransfer() Transfer {
	return e
}

func (e *PipeEnd) asSeeker() Seeker {
	return nil
}

func (e *PipeEnd) asDir() Dir {
	return nil
}

func (e *PipeEnd) asTerminal() Terminal {
	return nil
}

func (e *PipeEnd) asWindowed() Windowed {
	return nil
}

func (e *PipeEnd) asLink() Link {
	return nil
}

func (e *PipeEnd) asAttrs() Attrs {
	return nil
}

func (e *PipeEnd) asSocket() Socket {
	return nil
}

func (e *PipeEnd) asIoctl() Ioctl {
	return e
}

func (e *PipeEnd) asLocker() Locker {
	return nil
}

func (e *PipeEnd) asEpoll() *Epoll {
	return nil
}

func (e *PipeEnd) asMemfd() *MemfdFile {
	return nil
}

func (e *PipeEnd) asPair() *PairEnd {
	return nil
}

func (e *PipeEnd) asListener() *UnixListener {
	return nil
}

func (e *PipeEnd) asConsole() *ConsoleEnd {
	return nil
}

func (e *PipeEnd) asRemote() *RemoteFD {
	return nil
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

func (e *PipeEnd) poll(events int16) int16 {
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

func (e *PipeEnd) read(p *Process, b []byte, mode IoMode) (int, error) {
	if p.chaosFail("read") {
		return 0, EIO
	}

	if !e.reads {
		return 0, EBADF
	}

	if len(b) == 0 {
		return 0, nil
	}

	pp := e.pipe

	pp.mu.Lock()

	for {
		n, eof := pp.take(b)

		if n > 0 || eof {
			pp.mu.Unlock()

			return n, nil
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

func (e *PipeEnd) write(p *Process, b []byte, mode IoMode) (int, error) {
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

	for total < len(b) && err == nil {
		chunk := b[total:min(len(b), total+pipeChunk)]
		full, gone := pp.put(chunk)

		switch {
		case gone:
			err = EPIPE
		case !full:
			total += len(chunk)
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

func (e *PipeEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (e *PipeEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}
