package main

import (
	"math"
	"sync"
)

type RWLock struct {
	ObjectIdent

	mu      sync.Mutex
	writer  int32
	readers uint32
}

func newRWLock() *RWLock {
	l := &RWLock{ObjectIdent: newObjectIdent()}

	return l
}

func (l *RWLock) closed() {
}

func (l *RWLock) poll(events int16, through []*Epoll) int16 {
	return 0
}

func (l *RWLock) subscribe(s *Subscription, events int16) {
}

func (l *RWLock) unsubscribe(s *Subscription) {
}

func (l *RWLock) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *SyncLock:
		return false, l.lock(p, r)
	case *SyncUnlock:
		return false, l.unlock()
	case *SyncCopy:
		l.mu.Lock()

		c := newRWLock()

		c.writer, c.readers = l.writer, l.readers
		l.mu.Unlock()
		r.copy = c

		return false, nil
	}

	return false, errUnserved
}

func (l *RWLock) take(r *SyncLock) (bool, error) {
	l.mu.Lock()

	defer l.mu.Unlock()

	switch {
	case l.writer != 0:
		return false, nil
	case !r.shared && l.readers != 0:
		return false, nil
	case !r.shared:
		l.writer = r.thread

		return true, nil
	case l.readers == math.MaxUint32:
		return true, EAGAIN
	}

	l.readers++

	return true, nil
}

func (l *RWLock) lock(p *Process, r *SyncLock) error {
	for {
		if done, err := l.take(r); done {
			return err
		}

		switch {
		case r.try:
			return EBUSY
		case r.end.IsZero():
			return EDEADLK
		case !p.sleepUntil(r.end):
			return ETIMEDOUT
		}
	}
}

func (l *RWLock) unlock() error {
	l.mu.Lock()

	defer l.mu.Unlock()

	switch {
	case l.writer != 0:
		l.writer = 0
	case l.readers != 0:
		l.readers--
	default:
		return EPERM
	}

	return nil
}
