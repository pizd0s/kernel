//go:build linux && amd64

package main

import (
	"sync/atomic"
)

type Cond struct {
	refs  atomic.Int32
	clock uint32
}

func newCond(clock uint32) *Cond {
	c := &Cond{clock: clock}

	c.refs.Store(1)

	return c
}

func (c *Cond) ref() {
	c.refs.Add(1)
}

func (c *Cond) unref() {
	c.refs.Add(-1)
}

func (c *Cond) poll(events int16, through []*Epoll) int16 {
	return 0
}

func (c *Cond) subscribe(s *Subscription, events int16) {
}

func (c *Cond) unsubscribe(s *Subscription) {
}

func (c *Cond) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *SyncWake:
		return false, nil
	case *SyncWait:
		return false, c.wait(p, r)
	case *SyncCopy:
		r.copy = newCond(c.clock)

		return false, nil
	}

	return false, errUnserved
}

func (c *Cond) wait(p *Process, r *SyncWait) error {
	end, err := p.deadlineOf(r.at, c.clock)

	if err != nil {
		return err
	}

	if err := p.request(r.lock, &SyncUnlock{thread: r.thread}); err != nil {
		return err
	}

	p.sleepUntil(end)

	if err := p.request(r.lock, &SyncLock{thread: r.thread}); err != nil {
		return err
	}

	return ETIMEDOUT
}
