//go:build linux && amd64

package main

import (
	"encoding/binary"
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

func (c *ConsoleEnd) truncate(p *Process, size int64) error {
	return EINVAL
}

func (c *ConsoleEnd) sync(p *Process, data bool) error {
	return EINVAL
}

func (c *ConsoleEnd) chmod(p *Process, mode uint32) error {
	return EINVAL
}

func (c *ConsoleEnd) chown(p *Process, uid, gid int) error {
	return EINVAL
}

func (c *ConsoleEnd) utimes(p *Process, ts []Timespec) error {
	return EINVAL
}

func (c *ConsoleEnd) statfs(p *Process, st *Statfs) error {
	return EINVAL
}

func (c *ConsoleEnd) flock(p *Process, op int) error {
	return EBADF
}

func (c *ConsoleEnd) recordLock(p *Process, cmd int, lk *Flock) error {
	return EBADF
}

func (c *ConsoleEnd) call(p *Process, op, arg, group uint32, body []byte, files []Object, interruptible bool) (Packet, bool, error) {
	switch op {
	case rGetWinsize:
		w, err := hostWindow()

		return Packet{data: w[:]}, false, err
	case rGetPgrp:
		return Packet{data: binary.LittleEndian.AppendUint64(nil, uint64(uint32(p.k.foreground())))}, false, nil
	case rSetPgrp:
		p.k.mu.Lock()
		p.k.fg = int32(arg)
		p.k.mu.Unlock()

		return Packet{}, false, nil
	}

	return Packet{}, false, errUnserved
}

func (c *ConsoleEnd) token() (uint64, error) {
	return 0, EXDEV
}

func (c *ConsoleEnd) device() FileKey {
	return FileKey{}
}

func (c *ConsoleEnd) bind(p *Process, a Address) error {
	return ENOTSOCK
}

func (c *ConsoleEnd) connect(p *Process, a Address) error {
	return ENOTSOCK
}

func (c *ConsoleEnd) listen(backlog int) error {
	return ENOTSOCK
}

func (c *ConsoleEnd) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, ENOTSOCK
}

func (c *ConsoleEnd) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	return nil, ECONNREFUSED
}

func (c *ConsoleEnd) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	return 0, ENOTSOCK
}

func (c *ConsoleEnd) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	return 0, Packet{}, 0, ENOTSOCK
}

func (c *ConsoleEnd) sockname(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (c *ConsoleEnd) peername(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (c *ConsoleEnd) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return nil, ENOTSOCK
}

func (c *ConsoleEnd) setsockopt(level, name uint32, value []byte) error {
	return ENOTSOCK
}

func (c *ConsoleEnd) shutdown(how int) error {
	return ENOTSOCK
}

func (c *ConsoleEnd) consume(sink func(pk *Packet)) {
}

func (c *ConsoleEnd) socketKind() int {
	return 0
}

func (c *ConsoleEnd) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (c *ConsoleEnd) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
