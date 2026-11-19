//go:build linux && amd64

package main

import (
	"sync/atomic"
	"syscall"
	"unsafe"
)

// consoleEnd is a descriptor of the console, open to read and write, as a
// terminal's are: it reads the pipe of the input, in, and writes the pipe
// of its output, out, the standard output's or the standard error's;
// either is nil when vm has no such descriptor.
type consoleEnd struct {
	in, out  *pipeEnd
	refs     atomic.Int32
	nonblock atomic.Bool
}

func newConsoleEnd(in, out *pipeEnd) *consoleEnd {
	for _, e := range []*pipeEnd{in, out} {
		if e != nil {
			e.ref()
		}
	}

	c := &consoleEnd{in: in, out: out}
	c.refs.Store(1)

	return c
}

func (c *consoleEnd) ref() {
	c.refs.Add(1)
}

func (c *consoleEnd) unref() {
	if c.refs.Add(-1) != 0 {
		return
	}

	for _, e := range []*pipeEnd{c.in, c.out} {
		if e != nil {
			e.unref()
		}
	}
}

// A consoleEnd is a File, a Transfer and an Ioctl, and itself to a call
// that wants one.
func (c *consoleEnd) asFile() File              { return c }
func (c *consoleEnd) asTransfer() Transfer      { return c }
func (c *consoleEnd) asSeeker() Seeker          { return nil }
func (c *consoleEnd) asDir() Dir                { return nil }
func (c *consoleEnd) asTerminal() Terminal      { return nil }
func (c *consoleEnd) asWindowed() Windowed      { return nil }
func (c *consoleEnd) asLink() Link              { return nil }
func (c *consoleEnd) asAttrs() Attrs            { return nil }
func (c *consoleEnd) asSocket() Socket          { return nil }
func (c *consoleEnd) asIoctl() Ioctl            { return c }
func (c *consoleEnd) asLocker() Locker          { return nil }
func (c *consoleEnd) asEpoll() *epoll           { return nil }
func (c *consoleEnd) asMemfd() *memfdFile       { return nil }
func (c *consoleEnd) asPair() *pairEnd          { return nil }
func (c *consoleEnd) asListener() *unixListener { return nil }
func (c *consoleEnd) asConsole() *consoleEnd    { return c }
func (c *consoleEnd) asVFS() *remoteDir         { return nil }
func (c *consoleEnd) asCall() *callEnd          { return nil }

// stat is a character device's, as a terminal's.
func (c *consoleEnd) stat(p *Process, st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Ino: 1, Mode: syscall.S_IFCHR | 0o620, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

const (
	inEvents  = pollIn | pollRdnorm | pollHup
	outEvents = pollOut | pollWrnorm | pollErr
)

func (c *consoleEnd) poll(events int16) int16 {
	var ready int16

	if c.in != nil {
		ready |= c.in.poll(events&inEvents) & inEvents
	}

	if c.out != nil {
		ready |= c.out.poll(events&outEvents) & outEvents
	}

	return ready
}

func (c *consoleEnd) subscribe(s *subscription, events int16) {
	if c.in != nil && (events == 0 || events&inEvents != 0) {
		c.in.subscribe(s, events&inEvents)
	}

	if c.out != nil && (events == 0 || events&outEvents != 0) {
		c.out.subscribe(s, events&outEvents)
	}
}

func (c *consoleEnd) unsubscribe(s *subscription) {
	for _, e := range []*pipeEnd{c.in, c.out} {
		if e != nil {
			e.unsubscribe(s)
		}
	}
}

func (c *consoleEnd) flags() (int, error) {
	fl := syscall.O_RDWR

	if c.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (c *consoleEnd) setFlags(fl int) error {
	c.nonblock.Store(fl&syscall.O_NONBLOCK != 0)

	return nil
}

func (c *consoleEnd) read(p *Process, b []byte, mode ioMode) (int, error) {
	if c.in == nil {
		return 0, syscall.EBADF
	}

	mode.nonblock = mode.nonblock || c.nonblock.Load()

	return c.in.read(p, b, mode)
}

func (c *consoleEnd) write(p *Process, b []byte, mode ioMode) (int, error) {
	if c.out == nil {
		return 0, syscall.EBADF
	}

	mode.nonblock = mode.nonblock || c.nonblock.Load()

	return c.out.write(p, b, mode)
}

func (c *consoleEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (c *consoleEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

// ioctl knows FIONREAD, what the input has.
func (c *consoleEnd) ioctl(req uintptr, arg unsafe.Pointer) error {
	if c.in == nil {
		return syscall.ENOTTY
	}

	return c.in.ioctl(req, arg)
}
