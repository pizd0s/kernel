package main

import (
	"sync"
)

type SpinLock struct {
	ObjectIdent

	mu    sync.Mutex
	owner int32
}

func newSpinLock() *SpinLock {
	l := &SpinLock{ObjectIdent: newObjectIdent()}

	return l
}

func (l *SpinLock) closed() {
}

func (l *SpinLock) poll(events int16, through []*Epoll) int16 {
	return 0
}

func (l *SpinLock) subscribe(s *Subscription, events int16) {
}

func (l *SpinLock) unsubscribe(s *Subscription) {
}

func (l *SpinLock) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	l.mu.Lock()

	defer l.mu.Unlock()

	switch r := r.(type) {
	case *SyncLock:
		switch {
		case l.owner == 0:
			l.owner = r.thread

			return false, nil
		case r.try:
			return false, EBUSY
		}

		return false, EDEADLK
	case *SyncUnlock:
		l.owner = 0

		return false, nil
	case *SyncCopy:
		c := newSpinLock()

		c.owner = l.owner
		r.copy = c

		return false, nil
	}

	return false, errUnserved
}
