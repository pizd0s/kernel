//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
)

// Waits. A process sleeps in one place, its queue of events: whatever may
// change for a wait of the process's puts an event there. An event is a
// node its source keeps, in the queue once at most, as Zircon's port
// packet is its observer's: a subscription's, the process's own for its
// signals, its continuing, its children and a module compiled, and a
// wait's for its deadline. Putting one never blocks, and one put again
// while it waits there stays as it is. Every wait is one function, wait:
// it takes the events in order, runs each one's callback, and asks the
// predicate the wait gives whether it is over on that event or goes on;
// with the queue empty the process sleeps on the queue's bell, and a ring
// that brings no event, an answer to a waiter of the process's, asks the
// predicate too. A wait
// subscribes to what it waits for before it first looks, so a change
// after the look is an event; taking a subscription back takes its event
// out of the queue, so none outlives what it is of. An event names what
// changed, and the predicate asks what it is now. An object that poll
// finds not ready for events wakes the subscriptions for them as it may
// become ready. Between its waits the process takes the events too, where
// its own code is interrupted (interrupt, process.go): all that are
// there, with their callbacks and no predicate, so one a wait would have
// had is gone before it; the wait asks first, and loses nothing.
//
// A subscription is its subscriber's, and so are its hooks, by which an
// object keeps it in its lists: subscribing and taking back hook and
// unhook, and allocate nothing.

// The kinds of events: an object the process subscribed to changed, id
// the subscription's; a signal came; the process stopped no more; a child
// changed; a module the process waits for compiled; a wait's deadline
// passed; a timer of the process's fired (process.go).
type eventKind uint8

const (
	eventChanged eventKind = iota + 1
	eventSignal
	eventContinued
	eventChild
	eventCompiled
	eventDeadline
	eventTimer
)

// An event is a node of a process's queue: its kind and id, a callback
// the wait that takes it runs on the process first, nil for none, and its
// place in the queue, queued while it is there.
type event struct {
	kind       eventKind
	id         uint64
	run        func(*Process)
	prev, next *event
	queued     bool
}

// eventQueue is a process's queue of events, the oldest first, and its
// bell, which a put rings and the process sleeps on.
type eventQueue struct {
	mu          sync.Mutex
	first, last *event
	bell        chan struct{}
	// count is how many events are queued, for a next to find none
	// without the lock: a post rings the bell after, so one missed so is
	// found on the ring.
	count atomic.Int32
}

// ownEvents are the events of a process's own: a signal came, it stopped
// no more, a child changed, a module it waits for compiled.
type ownEvents struct {
	signal, continued, child, compiled event
}

// unlink takes an event out of the queue, with the lock.
func (q *eventQueue) unlink(e *event) {
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

// next takes the oldest event out of the queue, nil when it is empty.
func (q *eventQueue) next() *event {
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

// drop takes an event out of the queue, when it is there.
func (q *eventQueue) drop(e *event) {
	q.mu.Lock()

	if e.queued {
		q.unlink(e)
	}

	q.mu.Unlock()
}
