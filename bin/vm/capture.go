//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type Capture struct {
	refs atomic.Int32
	ino  uint64
	size func(first []byte) int

	mu  sync.Mutex
	out []byte
}

func newCapture(size func(first []byte) int) *Capture {
	c := &Capture{ino: pipeInodes.Add(1), size: size}

	c.refs.Store(1)

	return c
}

func (c *Capture) ref() {
	c.refs.Add(1)
}

func (c *Capture) unref() {
	c.refs.Add(-1)
}

func (c *Capture) bytes() []byte {
	c.mu.Lock()

	defer c.mu.Unlock()

	return c.out
}

func (c *Capture) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: c.ino, Mode: S_IFIFO | 0o200, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (c *Capture) poll(events int16, through []*Epoll) int16 {
	return events & (POLLOUT | POLLWRNORM)
}

func (c *Capture) subscribe(s *Subscription, events int16) {
}

func (c *Capture) unsubscribe(s *Subscription) {
}

func (c *Capture) flags(p *Process) (int, error) {
	return O_WRONLY, nil
}

func (c *Capture) setFlags(p *Process, fl int) error {
	return nil
}

func (c *Capture) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	return 0, EBADF
}

func (c *Capture) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	return from.with(0, n, false, func(b []byte) (int, error) {
		c.mu.Lock()

		defer c.mu.Unlock()

		if c.out == nil && c.size != nil {
			c.out = make([]byte, 0, max(c.size(b), len(b)))
		}

		c.out = append(c.out, b...)

		return len(b), nil
	})
}

func (c *Capture) pread(p *Process, into Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (c *Capture) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (c *Capture) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (c *Capture) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (c *Capture) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (c *Capture) seek(p *Process, off int64, whence int) (int64, error) {
	return 0, ESPIPE
}

func (c *Capture) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (c *Capture) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, c.stat(p, r.st)
	}

	return false, errUnserved
}

func (c *Capture) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (c *Capture) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (c *Capture) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
