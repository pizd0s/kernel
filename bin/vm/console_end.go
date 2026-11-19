//go:build linux && amd64

package main

import (
	"sync/atomic"
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

func (c *ConsoleEnd) asFile() File {
	return c
}

func (c *ConsoleEnd) asTransfer() Transfer {
	return c
}

func (c *ConsoleEnd) asSeeker() Seeker {
	return nil
}

func (c *ConsoleEnd) asDir() Dir {
	return nil
}

func (c *ConsoleEnd) asTerminal() Terminal {
	return nil
}

func (c *ConsoleEnd) asWindowed() Windowed {
	return nil
}

func (c *ConsoleEnd) asLink() Link {
	return nil
}

func (c *ConsoleEnd) asAttrs() Attrs {
	return nil
}

func (c *ConsoleEnd) asSocket() Socket {
	return nil
}

func (c *ConsoleEnd) asIoctl() Ioctl {
	return c
}

func (c *ConsoleEnd) asLocker() Locker {
	return nil
}

func (c *ConsoleEnd) asEpoll() *Epoll {
	return nil
}

func (c *ConsoleEnd) asMemfd() *MemfdFile {
	return nil
}

func (c *ConsoleEnd) asPair() *PairEnd {
	return nil
}

func (c *ConsoleEnd) asListener() *UnixListener {
	return nil
}

func (c *ConsoleEnd) asConsole() *ConsoleEnd {
	return c
}

func (c *ConsoleEnd) asVFS() *RemoteDir {
	return nil
}

func (c *ConsoleEnd) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: 1, Mode: S_IFCHR | 0o620, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (c *ConsoleEnd) poll(events int16) int16 {
	var ready int16

	if c.in != nil {
		ready |= c.in.poll(events&inEvents) & inEvents
	}

	if c.out != nil {
		ready |= c.out.poll(events&outEvents) & outEvents
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

func (c *ConsoleEnd) flags() (int, error) {
	fl := O_RDWR

	if c.nonblock.Load() {
		fl |= O_NONBLOCK
	}

	return fl, nil
}

func (c *ConsoleEnd) setFlags(fl int) error {
	c.nonblock.Store(fl&O_NONBLOCK != 0)

	return nil
}

func (c *ConsoleEnd) read(p *Process, b []byte, mode IoMode) (int, error) {
	if c.in == nil {
		return 0, EBADF
	}

	mode.nonblock = mode.nonblock || c.nonblock.Load()

	return c.in.read(p, b, mode)
}

func (c *ConsoleEnd) write(p *Process, b []byte, mode IoMode) (int, error) {
	if c.out == nil {
		return 0, EBADF
	}

	mode.nonblock = mode.nonblock || c.nonblock.Load()

	return c.out.write(p, b, mode)
}

func (c *ConsoleEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (c *ConsoleEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (c *ConsoleEnd) ioctl(req uintptr, arg unsafe.Pointer) error {
	if c.in == nil {
		return ENOTTY
	}

	return c.in.ioctl(req, arg)
}
