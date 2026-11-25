package main

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	waitsOn Signalled = iota
	restarts
	interrupts
)

type Received struct {
	pk  Packet
	eof bool
}

type Turn struct{}

type Waiter[T any] struct {
	prev, next *Waiter[T]
	in         *Waiters[T]
	done       atomic.Bool
	answer     T
	to         *Process
	spare      *Waiter[T]
}

func (w *Waiter[T]) wake(v T) {
	w.answer = v
	w.done.Store(true)
	w.to.ring()
}

type Waiters[T any] struct {
	first, last *Waiter[T]
}

func (l *Waiters[T]) push(w *Waiter[T]) {
	w.in, w.prev, w.next = l, l.last, nil

	if l.last != nil {
		l.last.next = w
	} else {
		l.first = w
	}

	l.last = w
}

func (l *Waiters[T]) remove(w *Waiter[T]) bool {
	if w.in != l {
		return false
	}

	if w.prev != nil {
		w.prev.next = w.next
	} else {
		l.first = w.next
	}

	if w.next != nil {
		w.next.prev = w.prev
	} else {
		l.last = w.prev
	}

	w.in, w.prev, w.next = nil, nil, nil

	return true
}

func (l *Waiters[T]) pop() *Waiter[T] {
	w := l.first

	if w != nil {
		l.remove(w)
	}

	return w
}

func (l *Waiters[T]) wake(v T) {
	for w := l.pop(); w != nil; w = l.pop() {
		w.wake(v)
	}
}

type SpareWaiters[T any] struct {
	first *Waiter[T]
}

func (s *SpareWaiters[T]) get(p *Process) *Waiter[T] {
	w := s.first

	if w == nil {
		w = &Waiter[T]{to: p}
	} else {
		s.first, w.spare = w.spare, nil
	}

	w.done.Store(false)

	return w
}

func (s *SpareWaiters[T]) put(w *Waiter[T]) {
	var none T
	w.answer = none
	w.spare, s.first = s.first, w
}

type Signalled uint8

func attend[T any](p *Process, spare *SpareWaiters[T], mu *sync.Mutex, l *Waiters[T], ready func() bool, end time.Time, how Signalled) (answer T, given, timedOut bool, err error) {
	mu.Lock()

	if ready() {
		mu.Unlock()

		return answer, false, false, nil
	}

	w := spare.get(p)

	l.push(w)
	mu.Unlock()

	defer func() {
		if !w.done.Load() {
			mu.Lock()
			l.remove(w)
			mu.Unlock()
		}

		spare.put(w)
	}()

	if timedOut, err = p.await(&w.done, end, how); timedOut || err != nil {
		mu.Lock()

		taken := l.remove(w)

		mu.Unlock()

		if taken {
			return answer, false, timedOut, err
		}
	}

	return w.answer, true, false, nil
}
