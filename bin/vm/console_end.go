package main

import (
	"encoding/binary"
	"unsafe"
)

const (
	inEvents  = POLLIN | POLLRDNORM | POLLHUP
	outEvents = POLLOUT | POLLWRNORM | POLLERR
)

type ConsoleEnd struct {
	ObjectIdent

	in, out *Pipe
}

func newConsoleEnd(in, out *Pipe) *ConsoleEnd {
	if in != nil {
		in.join(true, false)
	}

	if out != nil {
		out.join(false, true)
	}

	return &ConsoleEnd{ObjectIdent: newObjectIdent(), in: in, out: out}
}

func (c *ConsoleEnd) closed(d *Description) {
	if c.in != nil {
		c.in.quit(true, false)
	}

	if c.out != nil {
		c.out.quit(false, true)
	}
}

func (c *ConsoleEnd) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: 1, Mode: S_IFCHR | 0o620, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (c *ConsoleEnd) poll(d *Description, events int16, through []*Epoll) int16 {
	var ready int16

	if c.in != nil {
		ready |= c.in.ready(true, false, events&inEvents) & inEvents
	}

	if c.out != nil {
		ready |= c.out.ready(false, true, events&outEvents) & outEvents
	}

	return ready
}

func (c *ConsoleEnd) subscribe(d *Description, s *Subscription, events int16) {
	if c.in != nil && (events == 0 || events&inEvents != 0) {
		c.in.hang(true, false, s, events&inEvents)
	}

	if c.out != nil && (events == 0 || events&outEvents != 0) {
		c.out.hang(false, true, s, events&outEvents)
	}
}

func (c *ConsoleEnd) unsubscribe(d *Description, s *Subscription) {
	for _, pp := range []*Pipe{c.in, c.out} {
		if pp != nil {
			pp.unhang(s)
		}
	}
}

func (c *ConsoleEnd) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
	if c.in == nil {
		return 0, EBADF
	}

	return c.in.read(p, into, n, mode)
}

func (c *ConsoleEnd) write(p *Process, from *MemfdFile, n int, mode IoMode) (int, error) {
	if c.out == nil {
		return 0, EBADF
	}

	return c.out.write(p, from, n, mode)
}

func (c *ConsoleEnd) ioctl(req uintptr, arg unsafe.Pointer) error {
	if c.in == nil {
		return ENOTTY
	}

	return c.in.ioctl(true, req, arg)
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

func (c *ConsoleEnd) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, c.stat(p, r.st)
	case *Ctl:
		return false, c.ctl(p, r)
	case *FileRead:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = c.read(p, r.into.memory(), r.n, r.mode)

		return false, err
	case *FileWrite:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = c.write(p, r.from.memory(), r.n, r.mode)

		return false, err
	case *FileIoctl:
		return false, c.ioctl(r.req, r.arg)
	}

	return false, errUnserved
}
