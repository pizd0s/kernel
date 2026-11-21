//go:build linux && amd64

package main

import (
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	inEvents  = POLLIN | POLLRDNORM | POLLHUP
	outEvents = POLLOUT | POLLWRNORM | POLLERR
)

type ConsoleEnd struct {
	in, out  *PipeEnd
	refs     atomic.Int32
	nonblock atomic.Bool
}

func newConsoleEnd(in, out *PipeEnd) *ConsoleEnd {
	for _, e := range []*PipeEnd{in, out} {
		if e != nil {
			e.ref()
		}
	}

	c := &ConsoleEnd{in: in, out: out}

	c.refs.Store(1)

	return c
}

func (c *ConsoleEnd) ref() {
	c.refs.Add(1)
}

func (c *ConsoleEnd) unref() {
	if c.refs.Add(-1) != 0 {
		return
	}

	for _, e := range []*PipeEnd{c.in, c.out} {
		if e != nil {
			e.unref()
		}
	}
}

func (c *ConsoleEnd) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: 1, Mode: S_IFCHR | 0o620, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (c *ConsoleEnd) poll(events int16, through []*Epoll) int16 {
	var ready int16

	if c.in != nil {
		ready |= c.in.poll(events&inEvents, through) & inEvents
	}

	if c.out != nil {
		ready |= c.out.poll(events&outEvents, through) & outEvents
	}

	return ready
}

func (c *ConsoleEnd) subscribe(s *Subscription, events int16) {
	if c.in != nil && (events == 0 || events&inEvents != 0) {
		c.in.subscribe(s, events&inEvents)
	}

	if c.out != nil && (events == 0 || events&outEvents != 0) {
		c.out.subscribe(s, events&outEvents)
	}
}

func (c *ConsoleEnd) unsubscribe(s *Subscription) {
	for _, e := range []*PipeEnd{c.in, c.out} {
		if e != nil {
			e.unsubscribe(s)
		}
	}
}

func (c *ConsoleEnd) flags(p *Process) (int, error) {
	fl := O_RDWR

	if c.nonblock.Load() {
		fl |= O_NONBLOCK
	}

	return fl, nil
}

func (c *ConsoleEnd) setFlags(p *Process, fl int) error {
	c.nonblock.Store(fl&O_NONBLOCK != 0)

	return nil
}

func (c *ConsoleEnd) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	if c.in == nil {
		return 0, EBADF
	}

	mode.nonblock = mode.nonblock || c.nonblock.Load()

	return c.in.read(p, into, n, mode)
}

func (c *ConsoleEnd) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	if c.out == nil {
		return 0, EBADF
	}

	mode.nonblock = mode.nonblock || c.nonblock.Load()

	return c.out.write(p, from, n, mode)
}

func (c *ConsoleEnd) pread(p *Process, into Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (c *ConsoleEnd) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (c *ConsoleEnd) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (c *ConsoleEnd) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (c *ConsoleEnd) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (c *ConsoleEnd) ioctl(req uintptr, arg unsafe.Pointer) error {
	if c.in == nil {
		return ENOTTY
	}

	return c.in.ioctl(req, arg)
}

func (c *ConsoleEnd) seek(p *Process, off int64, whence int) (int64, error) {
	return 0, ESPIPE
}

func (c *ConsoleEnd) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (c *ConsoleEnd) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, c.stat(p, r.st)
	case *TtyWindow:
		w, err := hostWindow()

		r.size = w[:]

		return false, err
	case *TtyForeground:
		r.group = uint32(p.k.foreground())

		return false, nil
	case *TtySetForeground:
		p.k.mu.Lock()
		p.k.fg = int32(r.group)
		p.k.mu.Unlock()

		return false, nil
	}

	return false, errUnserved
}

func (c *ConsoleEnd) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (c *ConsoleEnd) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
