//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
)

const (
	eventChanged EventKind = iota + 1
	eventSignal
	eventContinued
	eventChild
	eventCompiled
	eventDeadline
	eventTimer
	eventRelocate
	eventPool
)

type EventKind uint8

type Event struct {
	kind       EventKind
	id         uint64
	run        func(*Process)
	prev, next *Event
	queued     bool
}

type EventQueue struct {
	mu          sync.Mutex
	first, last *Event
	bell        chan struct{}

	count atomic.Int32
}

type OwnEvents struct {
	signal, continued, child, compiled, pool Event
}

func (q *EventQueue) unlink(e *Event) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		q.first = e.next
	}

	if e.next != nil {
		e.next.prev = e.prev
	} else {
		q.last = e.prev
	}

	e.queued, e.prev, e.next = false, nil, nil
	q.count.Add(-1)
}

func (q *EventQueue) next() *Event {
	if q.count.Load() == 0 {
		return nil
	}

	q.mu.Lock()

	e := q.first

	if e != nil {
		q.unlink(e)
	}

	q.mu.Unlock()

	return e
}

func (q *EventQueue) drop(e *Event) bool {
	q.mu.Lock()

	defer q.mu.Unlock()

	if !e.queued {
		return false
	}

	q.unlink(e)

	return true
}
