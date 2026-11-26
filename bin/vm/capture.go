package main

import (
	"sync"
)

type Capture struct {
	ObjectIdent

	ino  uint64
	size func(first []byte) int

	mu  sync.Mutex
	out []byte
}

func newCapture(size func(first []byte) int) *Capture {
	c := &Capture{ObjectIdent: newObjectIdent(), ino: pipeInodes.Add(1), size: size}

	return c
}

func (c *Capture) closed(d *Description) {
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

func (c *Capture) poll(d *Description, events int16, through []*Epoll) int16 {
	return events & (POLLOUT | POLLWRNORM)
}

func (c *Capture) subscribe(d *Description, s *Subscription, events int16) {
}

func (c *Capture) unsubscribe(d *Description, s *Subscription) {
}

func (c *Capture) write(p *Process, from *MemfdFile, n int, mode IoMode) (int, error) {
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

func (c *Capture) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, c.stat(p, r.st)
	case *FileWrite:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = c.write(p, r.from.memory(), r.n, r.mode)

		return false, err
	}

	return false, errUnserved
}
