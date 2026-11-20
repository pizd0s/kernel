//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
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

func (c *Capture) asFile() File {
	return c
}

func (c *Capture) asTransfer() Transfer {
	return c
}

func (c *Capture) asSeeker() Seeker {
	return nil
}

func (c *Capture) asDir() Dir {
	return nil
}

func (c *Capture) asTerminal() Terminal {
	return nil
}

func (c *Capture) asLink() Link {
	return nil
}

func (c *Capture) asAttrs() Attrs {
	return nil
}

func (c *Capture) asSocket() Socket {
	return nil
}

func (c *Capture) asIoctl() Ioctl {
	return nil
}

func (c *Capture) asLocker() Locker {
	return nil
}

func (c *Capture) asEpoll() *Epoll {
	return nil
}

func (c *Capture) asPair() *PairEnd {
	return nil
}

func (c *Capture) asListener() *UnixListener {
	return nil
}

func (c *Capture) asConsole() *ConsoleEnd {
	return nil
}

func (c *Capture) asRemote() *RemoteFD {
	return nil
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

func (c *Capture) flags() (int, error) {
	return O_WRONLY, nil
}

func (c *Capture) setFlags(fl int) error {
	return nil
}

func (c *Capture) read(p *Process, into Transfer, n int, mode IoMode) (int, error) {
	return 0, EBADF
}

func (c *Capture) write(p *Process, from Transfer, n int, mode IoMode) (int, error) {
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

func (c *Capture) pread(p *Process, into Transfer, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (c *Capture) pwrite(p *Process, from Transfer, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (c *Capture) splice(at int64, n int, mode int) Transfer {
	return newSpliced(c, at, n, mode)
}

func (c *Capture) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (c *Capture) copyTo(p *Process, out Transfer, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (c *Capture) copyFrom(p *Process, in Transfer, from, to int64, n int) (int, error) {
	return 0, EINVAL
}
