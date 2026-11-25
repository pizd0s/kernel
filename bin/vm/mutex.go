package main

import (
	"math"
	"sync"
	"sync/atomic"
)

type Mutex struct {
	ObjectIdent

	refs  atomic.Int32
	mu    sync.Mutex
	kind  uint32
	owner int32
	count uint32
}

func newMutex(kind uint32) *Mutex {
	m := &Mutex{ObjectIdent: newObjectIdent(), kind: kind}

	m.refs.Store(1)

	return m
}

func (m *Mutex) ref() {
	m.refs.Add(1)
}

func (m *Mutex) unref() {
	m.refs.Add(-1)
}

func (m *Mutex) poll(events int16, through []*Epoll) int16 {
	return 0
}

func (m *Mutex) subscribe(s *Subscription, events int16) {
}

func (m *Mutex) unsubscribe(s *Subscription) {
}

func (m *Mutex) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *SyncLock:
		return false, m.lock(p, r)
	case *SyncUnlock:
		return false, m.unlock(r.thread)
	case *SyncCopy:
		m.mu.Lock()

		c := newMutex(m.kind)

		c.owner, c.count = m.owner, m.count
		m.mu.Unlock()
		r.copy = c

		return false, nil
	}

	return false, errUnserved
}

func (m *Mutex) take(thread int32, try bool) (bool, error) {
	m.mu.Lock()

	defer m.mu.Unlock()

	switch {
	case m.owner == 0:
		m.owner, m.count = thread, 1

		return true, nil
	case m.owner == thread && m.kind == PTHREAD_MUTEX_RECURSIVE:
		if m.count == math.MaxUint32 {
			return true, EAGAIN
		}

		m.count++

		return true, nil
	case m.owner == thread && m.kind == PTHREAD_MUTEX_ERRORCHECK:
		if try {
			return true, EBUSY
		}

		return true, EDEADLK
	}

	return false, nil
}

func (m *Mutex) lock(p *Process, r *SyncLock) error {
	for {
		if done, err := m.take(r.thread, r.try); done {
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

func (m *Mutex) unlock(thread int32) error {
	m.mu.Lock()

	defer m.mu.Unlock()

	switch {
	case m.kind == PTHREAD_MUTEX_NORMAL:
		m.owner, m.count = 0, 0
	case m.owner == 0 || m.owner != thread:
		return EPERM
	case m.count > 1:
		m.count--
	default:
		m.owner, m.count = 0, 0
	}

	return nil
}
