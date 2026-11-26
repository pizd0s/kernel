package main

import (
	"sync"
)

const (
	onceNotRun = iota
	onceRunning
	onceDone
)

type Once struct {
	ObjectIdent

	mu    sync.Mutex
	state uint32
}

func newOnce(state uint32) *Once {
	o := &Once{ObjectIdent: newObjectIdent(), state: state}

	return o
}

func (o *Once) closed(d *Description) {
}

func (o *Once) poll(d *Description, events int16, through []*Epoll) int16 {
	return 0
}

func (o *Once) subscribe(d *Description, s *Subscription, events int16) {
}

func (o *Once) unsubscribe(d *Description, s *Subscription) {
}

func (o *Once) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
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
