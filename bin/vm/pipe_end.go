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

	pp.mu.Lock()

	ch := pp.ch

	if e.writes {
		pp.writers--

		if pp.writers == 0 {
			ch.end()
			pp.eof = true
		}
	}

	if e.reads {
		pp.readers--

		if pp.readers == 0 {
			ch.finish()
			pp.front = nil
			pp.bytes.Store(0)
		}
	}

	gone := pp.readers == 0 && pp.writers == 0

	pp.mu.Unlock()

	if gone && pp.name != nil {
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

func (e *PipeEnd) asVFS() *RemoteDir {
	return nil
}

func (e *PipeEnd) asCall() *CallEnd {
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

	defer pp.mu.Unlock()

	if in {
		pp.ch.subscribe(&s.in, false)
	}

	if out {
		pp.ch.subscribe(&s.out, true)
	}
}

func (e *PipeEnd) unsubscribe(s *Subscription) {
	pp := e.pipe

	pp.mu.Lock()

	defer pp.mu.Unlock()

	pp.ch.unsubscribe(&s.in)
	pp.ch.unsubscribe(&s.out)
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

	ch, front, readers, writers := pp.ch, len(pp.front), pp.readers, pp.writers

	pp.mu.Unlock()

	queued := ch.count()
	mask := int16(0)

	if e.writes {
		if ch.room() {
			mask |= POLLOUT | POLLWRNORM
		}

		if readers == 0 {
			mask |= POLLERR
		}
	}

	if e.reads {
		if front > 0 || queued > 0 || writers == 0 {
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

	*(*int32)(arg) = int32(e.pipe.bytes.Load())

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

	for {
		n, _, ok := pp.take(b)

		if ok {
			return n, nil
		}

		if mode.nonblock || e.nonblock.Load() {
			return 0, EAGAIN
		}

		ch := pp.channel()

		if _, err := ch.readable(p, func() bool { return ch.ended.Load() }, time.Time{}, restarts); err != nil {
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

	for total < len(b) {
		chunk := b[total:min(len(b), total+pipeChunk)]
		pk := Packet{data: append([]byte(nil), chunk...)}

		pp.mu.Lock()

		full, err := pp.ch.put(&pk)
		finished, sent := err != nil, err == nil && !full

		if sent {
			pp.bytes.Add(int64(len(chunk)))
		}

		pp.mu.Unlock()

		if finished {
			if !mode.quiet {
				p.sendSelf(SIGPIPE)
			}

			if total > 0 {
				return total, nil
			}

			return 0, EPIPE
		}

		if sent {
			total += len(chunk)

			continue
		}

		if mode.nonblock || e.nonblock.Load() {
			if total > 0 {
				return total, nil
			}

			return 0, EAGAIN
		}

		if _, err := pp.channel().writable(p, func() bool { return false }, time.Time{}, restarts); err != nil {
			if total > 0 {
				return total, nil
			}

			return 0, err
		}
	}

	return total, nil
}

func (e *PipeEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (e *PipeEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}
