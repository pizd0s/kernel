package main

import (
	"encoding/binary"
	"sync/atomic"
	"unsafe"
)

const (
	inEvents  = POLLIN | POLLRDNORM | POLLHUP
	outEvents = POLLOUT | POLLWRNORM | POLLERR
)

type ConsoleEnd struct {
	ObjectIdent

	in, out  *PipeEnd
	nonblock atomic.Bool
}

func newConsoleEnd(in, out *PipeEnd) *ConsoleEnd {
	return &ConsoleEnd{ObjectIdent: newObjectIdent(), in: in.another(), out: out.another()}
}

func (c *ConsoleEnd) closed() {
	for _, e := range []*PipeEnd{c.in, c.out} {
		if e != nil {
			e.closed()
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

func (c *ConsoleEnd) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
	if c.in == nil {
		return 0, EBADF
	}

	mode.nonblock = mode.nonblock || c.nonblock.Load()

	return c.in.read(p, into, n, mode)
}

func (c *ConsoleEnd) write(p *Process, from *MemfdFile, n int, mode IoMode) (int, error) {
	if c.out == nil {
		return 0, EBADF
	}

	mode.nonblock = mode.nonblock || c.nonblock.Load()

	return c.out.write(p, from, n, mode)
}

func (c *ConsoleEnd) ioctl(req uintptr, arg unsafe.Pointer) error {
	if c.in == nil {
		return ENOTTY
	}

	return c.in.ioctl(req, arg)
}

func (c *ConsoleEnd) ctl(p *Process, r *Ctl) error {
	if len(r.data) < 4 {
		return EINVAL
	}

	switch binary.LittleEndian.Uint32(r.data) {
	case ctlGetWinsize:
		w, err := hostWindow()

		r.answer = w[:]

		return err
	case ctlGetPgrp:
		r.answer = binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, uint32(p.k.foreground())), 0)

		return nil
	case ctlSetPgrp:
		if len(r.data) < 8 {
			return EINVAL
		}

		p.k.mu.Lock()
		p.k.fg = int32(binary.LittleEndian.Uint32(r.data[4:]))
		p.k.mu.Unlock()

		return nil
	}

	return ENOTTY
}

func (c *ConsoleEnd) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, c.stat(p, r.st)
	case *Ctl:
		return false, c.ctl(p, r)
	case *FileGetfl:
		var err error

		r.flags, err = c.flags(p)

		return false, err
	case *FileSetfl:
		return false, c.setFlags(p, r.flags)
	case *FileRead:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = c.read(p, r.into, r.n, r.mode)

		return false, err
	case *FileWrite:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = c.write(p, r.from, r.n, r.mode)

		return false, err
	case *FileIoctl:
		return false, c.ioctl(r.req, r.arg)
	}

	return false, errUnserved
}
