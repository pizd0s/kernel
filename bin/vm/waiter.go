//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
)

//
// A call that blocks on one object subscribes to nothing. It leaves a
// waiter at the object, a node of the process's with the place for what
// the call waits for, and sleeps until that is there. Who changes the
// object takes the first waiter out of the object's list and gives it its
// answer (wake), all with the object's lock: so the answer is the
// waiter's own, a packet goes from who sends it to who waits for it, and
// who is woken has nothing to ask the object. The answer is a flag of the
// waiter's and a ring of the process's bell: no event is put for it, as
// the wait that sleeps asks its waiter. A wait that a signal or its
// deadline ends takes its waiter back with the same lock; one that is in
// the list no more was answered, and the answer is the call's. An answer
// is of one type for a waiter: a packet as it is received (received), or
// nothing, the turn to look again (turn). Subscriptions are of poll,
// select and epoll, which ask whether an object is ready, and of a
// receive that only looks, MSG_PEEK's.

// received is what a receive waits for: a packet, or the end of the data.
type received struct {
	pk  packet
	eof bool
}

// turn is the answer of a waiter that waits for nothing but a change: its
// turn to look at the object again.
type turn struct{}

// A waiter is a blocking call's place at an object: answer what the call
// waits for, there once done is set, to the process it wakes then. It is
// in a list of the object's (waiters), in the one it is in, or the object
// keeps it otherwise, all under the object's lock; spare is the next of
// its process's spare ones.
type waiter[T any] struct {
	prev, next *waiter[T]
	in         *waiters[T]
	done       atomic.Bool
	answer     T
	to         *Process
	spare      *waiter[T]
}

// wake gives a waiter the object keeps no more its answer and wakes its
// process, with the object's lock: the bell alone, as the wait asks the
// waiter.
func (w *waiter[T]) wake(v T) {
	w.answer = v
	w.done.Store(true)
	w.to.ring()
}

// waiters are the waiters of an object for one thing, the oldest first,
// under the object's lock.
type waiters[T any] struct {
	first, last *waiter[T]
}

// push puts a waiter that is in no list at the end of the list.
func (l *waiters[T]) push(w *waiter[T]) {
	w.in, w.prev, w.next = l, l.last, nil

	if l.last != nil {
		l.last.next = w
	} else {
		l.first = w
	}

	l.last = w
}

// remove takes a waiter out of the list: false when it is not there, as
// one that was answered.
func (l *waiters[T]) remove(w *waiter[T]) bool {
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

// pop takes the oldest waiter out of the list, nil for none.
func (l *waiters[T]) pop() *waiter[T] {
	w := l.first

	if w != nil {
		l.remove(w)
	}

	return w
}

// wake gives every waiter of the list the answer, and the list is empty.
func (l *waiters[T]) wake(v T) {
	for w := l.pop(); w != nil; w = l.pop() {
		w.wake(v)
	}
}

// spareWaiters are the waiters of a process's for one kind of answer that
// no call of its has.
type spareWaiters[T any] struct {
	first *waiter[T]
}

// get is a waiter of p's with no answer: a spare one, or a new one.
func (s *spareWaiters[T]) get(p *Process) *waiter[T] {
	w := s.first

	if w == nil {
		w = &waiter[T]{to: p}
	} else {
		s.first, w.spare = w.spare, nil
	}

	w.done.Store(false)

	return w
}

// put gives back a waiter no object keeps: its answer goes.
func (s *spareWaiters[T]) put(w *waiter[T]) {
	var none T
	w.answer = none
	w.spare, s.first = s.first, w
}

// How a wait for an answer takes a signal the call took, in this wait or
// in one before it: it waits on, as a call of the kernel's to a server
// does (waitsOn); it ends with EINTR unless the handlers all asked to
// restart calls (restarts); or it ends with EINTR whatever they asked, as
// a wait with a timeout does (interrupts).
type signalled uint8

const (
	waitsOn signalled = iota
	restarts
	interrupts
)

// attend is the wait of a blocking call of p's at an object, among the
// waiters of its list l, with a waiter of spare's. It waits unless ready
// says the call has something to look at now, asked with the object's
// lock, mu: who changes the object wakes those that wait with that lock,
// so nothing is missed. given says the waiter was answered, with answer;
// timedOut that the deadline passed, end, zero for none; a signal ends
// the wait as how says. A wait that ends unanswered takes its waiter
// back; one answered meanwhile is given all the same.
func attend[T any](p *Process, spare *spareWaiters[T], mu *sync.Mutex, l *waiters[T], ready func() bool, end time.Time, how signalled) (answer T, given, timedOut bool, err error) {
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
