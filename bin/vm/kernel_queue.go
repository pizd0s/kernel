//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"syscall"
	"time"
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
// with the queue empty the process sleeps on the queue's bell. A wait
// subscribes to what it waits for before it first looks, so a change
// after the look is an event; taking a subscription back takes its event
// out of the queue, so none outlives what it is of. An event names what
// changed, and the predicate asks what it is now. An object that poll
// finds not ready for events wakes the subscriptions for them as it may
// become ready. Between its waits the process takes the events too, where
// its own code is interrupted (interrupt, kernel_signal.go): all that are
// there, with their callbacks and no predicate, so one a wait would have
// had is gone before it; the wait asks first, and loses nothing.
//
// A subscription is its subscriber's, and so are its hooks, by which an
// object keeps it in its lists: subscribing and taking back hook and
// unhook, and allocate nothing.

//
// A call that blocks on one object subscribes to nothing. It leaves a
// waiter at the object, a node of the process's with the place for what
// the call waits for, and sleeps until that is there. Who changes the
// object takes the first waiter out of the object's list and gives it its
// answer (wake), all with the object's lock: so the answer is the
// waiter's own, a packet goes from who sends it to who waits for it, and
// who is woken has nothing to ask the object. A wait that a signal or its
// deadline ends takes its waiter back with the same lock; one that is in
// the list no more was answered, and the answer is the call's. An answer
// is of one type for a waiter: a packet as it is received (received), or
// nothing, the turn to look again (turn). Subscriptions are of poll,
// select and epoll, which ask whether an object is ready, and of a
// receive that only looks, MSG_PEEK's.

// A waker is what an object wakes as it changes: by the hook of the
// subscription, which says by which of its lists.
type waker interface {
	wake(l *hook)
}

// A hook is a subscription's place in one list of an object's
// (subscribers). A subscription has two: in, for the changes of what the
// object reads, and out, for those of what it writes; an object of one
// kind of change takes in.
type hook struct {
	prev, next *hook
	list       *subscribers
	sub        *subscription
}

// A subscription is a subscriber's: what it wakes, its id there, its two
// hooks and its event, for a process it wakes. Its subscriber makes it
// before it subscribes and keeps it until it took it back; spare is the
// next of a process's spare ones.
type subscription struct {
	to      waker
	id      uint64
	in, out hook
	event   event
	spare   *subscription
}

// init makes s a subscription that wakes to with id.
func (s *subscription) init(to waker, id uint64) {
	s.to, s.id = to, id
	s.in.sub, s.out.sub = s, s
	s.event.kind, s.event.id = eventChanged, id
}

// subscribers are the subscriptions an object wakes for one kind of its
// changes: a list of their hooks. count is how many there are, for a tell
// to find none without the lock: who subscribes looks at the object again
// after, so a change told before its hook was counted is not lost.
type subscribers struct {
	mu    sync.Mutex
	first *hook
	count atomic.Int32
}

// add puts a hook that is in no list in the list.
func (s *subscribers) add(l *hook) {
	s.mu.Lock()
	l.list, l.prev, l.next = s, nil, s.first

	if s.first != nil {
		s.first.prev = l
	}

	s.first = l
	s.count.Add(1)
	s.mu.Unlock()
}

// remove takes a hook out of the list, when it is there.
func (s *subscribers) remove(l *hook) {
	s.mu.Lock()

	if l.list == s {
		if l.prev != nil {
			l.prev.next = l.next
		} else {
			s.first = l.next
		}

		if l.next != nil {
			l.next.prev = l.prev
		}

		l.list, l.prev, l.next = nil, nil, nil
		s.count.Add(-1)
	}

	s.mu.Unlock()
}

// tell wakes every subscription of the list.
func (s *subscribers) tell() {
	if s.count.Load() == 0 {
		return
	}

	s.mu.Lock()

	for l := s.first; l != nil; l = l.next {
		l.sub.to.wake(l)
	}

	s.mu.Unlock()
}

// takeOver gives the hooks of from to s, a channel's in place of another
// one's, as the caller keeps both from changing otherwise.
func (s *subscribers) takeOver(from *subscribers) {
	from.mu.Lock()
	s.mu.Lock()

	for l := from.first; l != nil; {
		next := l.next
		l.list, l.prev, l.next = s, nil, s.first

		if s.first != nil {
			s.first.prev = l
		}

		s.first, l = l, next
	}

	from.first = nil
	s.count.Add(from.count.Swap(0))
	s.mu.Unlock()
	from.mu.Unlock()
}

// received is what a receive waits for: a packet, or the end of the data.
type received struct {
	pk  packet
	eof bool
}

// turn is the answer of a waiter that waits for nothing but a change: its
// turn to look at the object again.
type turn struct{}

// A waiter is a blocking call's place at an object: answer what the call
// waits for, there once done is set, to the process it wakes then, by its
// event. It is in a list of the object's (waiters), in the one it is in,
// or the object keeps it otherwise, all under the object's lock; spare is
// the next of its process's spare ones.
type waiter[T any] struct {
	prev, next *waiter[T]
	in         *waiters[T]
	done       atomic.Bool
	answer     T
	to         *Process
	event      event
	spare      *waiter[T]
}

// wake gives a waiter the object keeps no more its answer and wakes its
// process, with the object's lock.
func (w *waiter[T]) wake(v T) {
	w.answer = v
	w.done.Store(true)
	w.to.post(&w.event)
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
		w.event.kind = eventAnswered
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

// The kinds of events: an object the process subscribed to changed, id
// the subscription's; a signal came; the process stopped no more; a child
// changed; a module the process waits for compiled; a wait's deadline
// passed; a timer of the process's fired (kernel_time.go); a waiter of
// the process's has its answer.
type eventKind uint8

const (
	eventChanged eventKind = iota + 1
	eventSignal
	eventContinued
	eventChild
	eventCompiled
	eventDeadline
	eventTimer
	eventAnswered
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

// post puts an event at the end of the process's queue, unless it is
// there, and rings the bell; it never blocks.
func (p *Process) post(e *event) {
	q := &p.events
	q.mu.Lock()

	if !e.queued {
		e.queued, e.prev, e.next = true, q.last, nil

		if q.last != nil {
			q.last.next = e
		} else {
			q.first = e
		}

		q.last = e
		q.count.Add(1)
	}

	q.mu.Unlock()

	select {
	case q.bell <- struct{}{}:
	default:
	}
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

// wake is the process as the waker of its subscriptions: the event of the
// subscription.
func (p *Process) wake(l *hook) {
	p.post(&l.sub.event)
}

// subscription is a spare subscription of the process's, or a new one,
// which wakes the process with id.
func (p *Process) subscription(id uint64) *subscription {
	s := p.spare

	if s == nil {
		s = &subscription{}
	} else {
		p.spare, s.spare = s.spare, nil
	}

	s.init(p, id)

	return s
}

// release gives back a subscription of the process's that nothing has any
// more: its event goes out of the queue.
func (p *Process) release(s *subscription) {
	p.events.drop(&s.event)
	s.spare, p.spare = p.spare, s
}

// wait is every wait of the process. done says whether the wait is over,
// and with what error: first on no event, nil, then on each event of the
// queue, the oldest first, after its callback ran. With the queue empty
// the process sleeps on its bell. A deadline, end, zero for none, is an
// event of the wait's, which a timer puts in the queue: on it the wait
// ends, timedOut. Another's, an earlier wait's or one a wait in a signal
// handler of this one's took, it passes over; so the wait looks at the
// time too before it sleeps.
func (p *Process) wait(end time.Time, done func(e *event) (bool, error)) (timedOut bool, err error) {
	if over, err := done(nil); over {
		return false, err
	}

	var deadline *event

	if !end.IsZero() {
		deadline = &event{kind: eventDeadline}
		t := time.AfterFunc(time.Until(end), func() { p.post(deadline) })

		defer func() {
			t.Stop()
			p.events.drop(deadline)
		}()
	}

	for {
		e := p.events.next()

		switch {
		case e == nil:
			if deadline != nil && !time.Now().Before(end) {
				return true, nil
			}

			<-p.events.bell

			continue
		case e.kind == eventDeadline:
			if e == deadline {
				return true, nil
			}

			continue
		case e.run != nil:
			e.run(p)
		}

		if over, err := done(e); over {
			return false, err
		}
	}
}

// await waits until a waiter of the process's has its answer, done, or the
// deadline, end, zero for none: timedOut then. A signal the call took
// interrupts it, in this wait or in one before it: then the error is
// EINTR, unless its handlers all asked to restart calls and restart is
// set. The waiter is at its object still after either, for the caller to
// take back.
func (p *Process) await(done *atomic.Bool, end time.Time, restart bool) (timedOut bool, err error) {
	return p.wait(end, func(*event) (bool, error) {
		if done.Load() {
			return true, nil
		}

		return p.interrupts(restart), syscall.EINTR
	})
}

// msgChannel is a channel of messages: the oldest first, at most limit of
// them, its ends, and the subscriptions it wakes, readers as it may
// become readable, a message or its writer's going, writers as it may
// become writable, a take from a full channel or its reader's going.
// ended says its writer is gone, the end of the data after the messages;
// finished that its reader is, and a write is EPIPE. mu guards the
// messages; held counts them, so a poll asks without taking it.
type msgChannel struct {
	mu sync.Mutex
	// sink takes the channel's messages as they are put, in place of a
	// reader: what a server sends the kernel on an open file's endpoint
	// (kernel_remote.go). Nothing is held then, and the channel is never
	// full. closed hears of the writer's going.
	sink     func(pk *packet)
	closed   func()
	limit    atomic.Int32
	messages []packet
	head     int
	// one is where the first message is kept: a channel that never holds
	// two, a reply's, allocates nothing for them.
	one      [1]packet
	held     atomic.Int32
	ended    atomic.Bool
	finished atomic.Bool
	readers  subscribers
	writers  subscribers
}

// newMsgChannel is a channel of limit messages.
func newMsgChannel(limit int) *msgChannel {
	c := &msgChannel{}
	c.init(limit)

	return c
}

func (c *msgChannel) init(limit int) {
	c.limit.Store(int32(limit))
	c.messages = c.one[:0]
}

// nudge wakes the readers, nudgeWriter the writers: something of the
// channel changed for them.
func (c *msgChannel) nudge() {
	c.readers.tell()
}

func (c *msgChannel) nudgeWriter() {
	c.writers.tell()
}

// setLimit is the most messages the channel holds from now on: more room
// is a change to its writer.
func (c *msgChannel) setLimit(limit int) {
	if int(c.limit.Swap(int32(limit))) < limit {
		c.nudgeWriter()
	}
}

// count is how many messages the channel holds; room tells whether it
// takes another.
func (c *msgChannel) count() int {
	return int(c.held.Load())
}

func (c *msgChannel) room() bool {
	return c.held.Load() < c.limit.Load()
}

// gone tells whether the channel's reader is gone.
func (c *msgChannel) gone() bool {
	return c.finished.Load()
}

// put queues a message, a copy of pk: EPIPE when the reader is gone, full
// when there is no room, else it wakes the readers.
func (c *msgChannel) put(pk *packet) (full bool, err error) {
	c.mu.Lock()

	if c.finished.Load() {
		c.mu.Unlock()

		return false, syscall.EPIPE
	}

	if sink := c.sink; sink != nil {
		c.mu.Unlock()
		sink(pk)

		return false, nil
	}

	if c.held.Load() >= c.limit.Load() {
		c.mu.Unlock()

		return true, nil
	}

	c.messages = append(c.messages, *pk)
	c.held.Add(1)
	c.mu.Unlock()
	c.nudge()

	return false, nil
}

// take is the oldest message, ok false when there is none: eof when the
// writer is gone too. freed says the take made room in a full channel,
// which wakes the writers. The end is asked before the messages: a writer
// puts its messages before it ends, so an end seen with none held is the
// end of the data, never a message that came in between.
func (c *msgChannel) take() (pk packet, eof, ok, freed bool) {
	ended := c.ended.Load()

	if c.held.Load() == 0 {
		return packet{}, ended, ended, false
	}

	c.mu.Lock()
	held := len(c.messages) - c.head

	if held == 0 {
		c.mu.Unlock()

		return packet{}, ended, ended, false
	}

	pk = c.messages[c.head]
	c.messages[c.head] = packet{}
	c.head++

	if c.head == len(c.messages) {
		c.messages, c.head = c.messages[:0], 0
	}

	c.held.Add(-1)
	freed = held >= int(c.limit.Load())
	c.mu.Unlock()

	if freed {
		c.nudgeWriter()
	}

	return pk, false, true, freed
}

// renew is a channel in place of c, a FIFO's that starts again: with c's
// limit and subscriptions, and what c holds unless its reader is gone.
func (c *msgChannel) renew() *msgChannel {
	n := newMsgChannel(int(c.limit.Load()))
	n.readers.takeOver(&c.readers)
	n.writers.takeOver(&c.writers)

	if !c.gone() {
		n.messages = c.takeAll()
		n.held.Store(int32(len(n.messages)))
	}

	return n
}

// takeAll takes every message the channel holds, the oldest first.
func (c *msgChannel) takeAll() []packet {
	c.mu.Lock()
	all := c.messages[c.head:]
	c.messages, c.head = nil, 0
	c.held.Store(0)
	c.mu.Unlock()

	return all
}

// end is the writer's going: the end of the data, after the messages.
func (c *msgChannel) end() {
	c.ended.Store(true)
	c.nudge()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()

	if closed != nil {
		closed()
	}
}

// drain makes sink take the channel's messages from now on, those it
// holds first, and closed hear of its writer's going, at once when the
// writer is gone.
func (c *msgChannel) drain(sink func(pk *packet), closed func()) {
	c.mu.Lock()
	held := c.messages[c.head:]
	c.messages, c.head = nil, 0
	c.held.Store(0)
	c.sink, c.closed = sink, closed
	c.mu.Unlock()

	for i := range held {
		sink(&held[i])
	}

	if c.ended.Load() {
		closed()
	}
}

// finish is the reader's going: what is queued goes, files and all, and a
// write is EPIPE from now on, which wakes the writers.
func (c *msgChannel) finish() {
	c.mu.Lock()
	dropped := c.messages[c.head:]
	c.messages, c.head = nil, 0
	c.held.Store(0)
	c.finished.Store(true)
	c.mu.Unlock()

	for _, pk := range dropped {
		dropFiles(pk.files)
	}

	c.nudgeWriter()
}

// edge wakes the readers, the writers, or both: a change of the channel's
// ends, a shutdown's.
func (c *msgChannel) edge(fill, free bool) {
	if fill {
		c.nudge()
	}

	if free {
		c.nudgeWriter()
	}
}
