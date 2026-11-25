package main

import (
	"sync"
	"sync/atomic"
)

const (
	onceNotRun = iota
	onceRunning
	onceDone
)

type Once struct {
	ObjectIdent

	refs  atomic.Int32
	mu    sync.Mutex
	state uint32
}

func newOnce(state uint32) *Once {
	o := &Once{ObjectIdent: newObjectIdent(), state: state}

	o.refs.Store(1)

	return o
}

func (o *Once) ref() {
	o.refs.Add(1)
}

func (o *Once) unref() {
	o.refs.Add(-1)
}

func (o *Once) poll(events int16, through []*Epoll) int16 {
	return 0
}

func (o *Once) subscribe(s *Subscription, events int16) {
}

func (o *Once) unsubscribe(s *Subscription) {
}

func (o *Once) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	o.mu.Lock()

	defer o.mu.Unlock()

	switch r := r.(type) {
	case *SyncLock:
		r.run = o.state == onceNotRun

		if r.run {
			o.state = onceRunning
		}

		return false, nil
	case *SyncUnlock:
		o.state = onceDone

		return false, nil
	case *SyncCopy:
		r.copy = newOnce(o.state)

		return false, nil
	}

	return false, errUnserved
}
