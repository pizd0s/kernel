//go:build linux && amd64

package main

import (
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// pipeEnd is one end of a pipe, an open file: the reader, the writer, or
// both, a FIFO's opened O_RDWR.
type pipeEnd struct {
	pipe     *pipe
	reads    bool
	writes   bool
	refs     atomic.Int32
	nonblock atomic.Bool
}

func (e *pipeEnd) ref() {
	e.refs.Add(1)
}

// unref closes the end when its last reference goes: the last reader
// finishes the channel, so a writer learns of it, the last writer closes
// it, so a reader sees the end of the data. A FIFO's pipe with no end left
// leaves the kernel's table.
func (e *pipeEnd) unref() {
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

// A pipeEnd is a File, a Transfer and an Ioctl.
func (e *pipeEnd) asFile() File              { return e }
func (e *pipeEnd) asTransfer() Transfer      { return e }
func (e *pipeEnd) asSeeker() Seeker          { return nil }
func (e *pipeEnd) asDir() Dir                { return nil }
func (e *pipeEnd) asTerminal() Terminal      { return nil }
func (e *pipeEnd) asWindowed() Windowed      { return nil }
func (e *pipeEnd) asLink() Link              { return nil }
func (e *pipeEnd) asAttrs() Attrs            { return nil }
func (e *pipeEnd) asSocket() Socket          { return nil }
func (e *pipeEnd) asIoctl() Ioctl            { return e }
func (e *pipeEnd) asLocker() Locker          { return nil }
func (e *pipeEnd) asEpoll() *epoll           { return nil }
func (e *pipeEnd) asMemfd() *memfdFile       { return nil }
func (e *pipeEnd) asPair() *pairEnd          { return nil }
func (e *pipeEnd) asListener() *unixListener { return nil }
func (e *pipeEnd) asConsole() *consoleEnd    { return nil }
func (e *pipeEnd) asVFS() *remoteDir         { return nil }
func (e *pipeEnd) asCall() *callEnd          { return nil }

// sides are what of the end a wait for events is on: its reading, its
// writing, or both, of an end that does both and is asked neither.
func (e *pipeEnd) sides(events int16) (in, out bool) {
	in, out = e.reads, e.writes

	if in && out && events&(pollIn|pollRdnorm|pollOut|pollWrnorm) != 0 {
		in, out = events&(pollIn|pollRdnorm) != 0, events&(pollOut|pollWrnorm) != 0
	}

	return in, out
}

// subscribe makes to learn of what fills the pipe, for the end's reading,
// and of what drains it, for its writing: on the pipe's channel, which a
// FIFO's that starts again gives on (renew), under the pipe's lock.
func (e *pipeEnd) subscribe(s *subscription, events int16) {
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

func (e *pipeEnd) unsubscribe(s *subscription) {
	pp := e.pipe
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.ch.unsubscribe(&s.in)
	pp.ch.unsubscribe(&s.out)
}

// stat is a FIFO's: the ends of a pipe share an inode of the kernel's, the
// ends of a FIFO's pipe the FIFO's.
func (e *pipeEnd) stat(p *Process, st *syscall.Stat_t) error {
	pp := e.pipe
	*st = syscall.Stat_t{Ino: pp.ino, Mode: syscall.S_IFIFO | 0o600, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: pipeChunk}

	if n := pp.name; n != nil {
		st.Dev, st.Ino, st.Mode = n.key.dev, n.key.ino, n.mode
	}

	st.Atim = syscall.NsecToTimespec(pp.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (e *pipeEnd) poll(events int16) int16 {
	pp := e.pipe
	pp.mu.Lock()
	ch, front, readers, writers := pp.ch, len(pp.front), pp.readers, pp.writers
	pp.mu.Unlock()
	queued := ch.count()
	mask := int16(0)

	if e.writes {
		if ch.room() {
			mask |= pollOut | pollWrnorm
		}

		if readers == 0 {
			mask |= pollErr
		}
	}

	if e.reads {
		if front > 0 || queued > 0 || writers == 0 {
			mask |= pollIn | pollRdnorm
		}

		if writers == 0 {
			mask |= pollHup
		}
	}

	return mask & (events | pollErr | pollHup)
}

func (e *pipeEnd) flags() (int, error) {
	fl := syscall.O_RDWR

	if !e.writes {
		fl = syscall.O_RDONLY
	} else if !e.reads {
		fl = syscall.O_WRONLY
	}

	if e.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (e *pipeEnd) setFlags(fl int) error {
	e.nonblock.Store(fl&syscall.O_NONBLOCK != 0)

	return nil
}

// ioctl knows FIONREAD, the bytes to read.
func (e *pipeEnd) ioctl(req uintptr, arg unsafe.Pointer) error {
	if req != fionread || !e.reads {
		return syscall.ENOTTY
	}

	*(*int32)(arg) = int32(e.pipe.bytes.Load())

	return nil
}

func (e *pipeEnd) read(p *Process, b []byte, mode ioMode) (int, error) {
	if p.chaosFail("read") {
		return 0, syscall.EIO
	}

	if !e.reads {
		return 0, syscall.EBADF
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
			return 0, syscall.EAGAIN
		}

		ch := pp.channel()

		if _, err := ch.readable(p, func() bool { return ch.ended.Load() }, time.Time{}, restarts); err != nil {
			return 0, err
		}
	}
}

// write queues b as packets of at most pipeChunk bytes, each whole or not
// at all, waiting for room between them; what was queued before a signal
// or EAGAIN is the count.
func (e *pipeEnd) write(p *Process, b []byte, mode ioMode) (int, error) {
	if p.chaosFail("write") {
		return 0, syscall.EIO
	}

	if !e.writes {
		return 0, syscall.EBADF
	}

	pp := e.pipe
	total := 0

	for total < len(b) {
		chunk := b[total:min(len(b), total+pipeChunk)]
		pk := packet{data: append([]byte(nil), chunk...)}

		// Under the pipe's lock, as take is, so the bytes FIONREAD counts
		// are those queued.
		pp.mu.Lock()
		full, err := pp.ch.put(&pk)
		finished, sent := err != nil, err == nil && !full

		if sent {
			pp.bytes.Add(int64(len(chunk)))
		}

		pp.mu.Unlock()

		if finished {
			if !mode.quiet {
				p.sendSelf(int(syscall.SIGPIPE))
			}

			if total > 0 {
				return total, nil
			}

			return 0, syscall.EPIPE
		}

		if sent {
			total += len(chunk)

			continue
		}

		if mode.nonblock || e.nonblock.Load() {
			if total > 0 {
				return total, nil
			}

			return 0, syscall.EAGAIN
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

func (e *pipeEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (e *pipeEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}
