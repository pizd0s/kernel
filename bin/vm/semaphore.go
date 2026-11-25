package main

import (
	"sync"
	"sync/atomic"
)

type Semaphore struct {
	refs    atomic.Int32
	mu      sync.Mutex
	value   uint32
	waiting Waiters[Turn]

	name     string
	mode     uint32
	uid, gid uint32
}

func newSemaphore(value uint32) *Semaphore {
	s := &Semaphore{value: value}

	s.refs.Store(1)

	return s
}

func (s *Semaphore) ref() {
	s.refs.Add(1)
}

func (s *Semaphore) unref() {
	s.refs.Add(-1)
}

func (s *Semaphore) poll(events int16, through []*Epoll) int16 {
	return 0
}

func (s *Semaphore) subscribe(sub *Subscription, events int16) {
}

func (s *Semaphore) unsubscribe(sub *Subscription) {
}

func (s *Semaphore) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *SyncLock:
		return false, s.take(p, r)
	case *SyncPost:
		return false, s.post()
	case *SyncValue:
		s.mu.Lock()
		r.value = s.value
		s.mu.Unlock()

		return false, nil
	case *SyncCopy:
		if s.name != "" {
			s.ref()
			r.copy = s

			return false, nil
		}

		s.mu.Lock()
		r.copy = newSemaphore(s.value)
		s.mu.Unlock()

		return false, nil
	}

	return false, errUnserved
}

func (s *Semaphore) taken() bool {
	if s.value == 0 {
		return false
	}

	s.value--

	return true
}

func (s *Semaphore) take(p *Process, r *SyncLock) error {
	if r.try {
		s.mu.Lock()

		defer s.mu.Unlock()

		if !s.taken() {
			return EAGAIN
		}

		return nil
	}

	_, _, timedOut, err := attend(p, &p.turns, &s.mu, &s.waiting, s.taken, r.end, restarts)

	switch {
	case timedOut:
		return ETIMEDOUT
	case err != nil:
		return err
	}

	return nil
}

func (s *Semaphore) post() error {
	s.mu.Lock()

	defer s.mu.Unlock()

	if w := s.waiting.pop(); w != nil {
		w.wake(Turn{})

		return nil
	}

	if s.value == SEM_VALUE_MAX {
		return EOVERFLOW
	}

	s.value++

	return nil
}
