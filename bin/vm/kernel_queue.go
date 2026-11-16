//go:build linux && amd64

package main

import (
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Waits. A process sleeps in one place, its queue of events: whatever may
// change for a wait of the process's puts an event there, an object it
// subscribed to, as the waker of the subscription, a signal, a child, the
// epoll it sleeps on, a module compiled; putting one never blocks, and
// none is lost. Every wait is one function, wait: it takes the events in
// order, runs each one's callback, and asks the predicate the wait gives
// whether it is over on that event or goes on; with the queue empty the
// process sleeps until an event comes or the wait's deadline passes. A
// wait subscribes to what it waits for before it first looks, so a change
// after the look is an event. An event names what changed, and the
// predicate asks what it is now: an event a predicate passes over,
// another wait's or an earlier one's, is lost to nobody. An object that
// poll finds not ready for events wakes the subscriptions for them as it
// may become ready.

// A waker is what an object wakes as it changes.
type waker interface {
	wake(id uint64)
}

type subscription struct {
	to waker
	id uint64
}

// subscribers are the subscriptions an object wakes for one kind of its
// changes.
type subscribers struct {
	mu   sync.Mutex
	list []subscription
}

func (s *subscribers) add(to waker, id uint64) {
	s.mu.Lock()
	s.list = append(s.list, subscription{to, id})
	s.mu.Unlock()
}

func (s *subscribers) remove(to waker, id uint64) {
	s.mu.Lock()

	for i, sub := range s.list {
		if sub.to == to && sub.id == id {
			last := len(s.list) - 1
			s.list[i], s.list[last] = s.list[last], subscription{}
			s.list = s.list[:last]

			break
		}
	}

	s.mu.Unlock()
}

// tell wakes every subscription.
func (s *subscribers) tell() {
	s.mu.Lock()

	for _, sub := range s.list {
		sub.to.wake(sub.id)
	}

	s.mu.Unlock()
}

// any tells whether there is a subscription.
func (s *subscribers) any() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.list) > 0
}

// takeOver gives the subscriptions of from to s: a channel in place of
// another one.
func (s *subscribers) takeOver(from *subscribers) {
	from.mu.Lock()
	list := slices.Clone(from.list)
	from.mu.Unlock()
	s.mu.Lock()
	s.list = append(s.list, list...)
	s.mu.Unlock()
}

// The kinds of events: an object the process subscribed to changed, id
// the subscription's; a signal came, id its number; the process stopped
// no more; a child changed, id its pid; an epoll the process sleeps on
// marked a member, id the epoll's inode; a module the process waits for
// compiled.
type eventKind uint8

const (
	eventChanged eventKind = iota + 1
	eventSignal
	eventContinued
	eventChild
	eventEpoll
	eventCompiled
)

// An event is what came to a process's queue: its kind and id, and a
// callback the wait that takes it runs on the process first, nil for one
// that only tells.
type event struct {
	kind eventKind
	id   uint64
	run  func(*Process)
}

// eventQueue is a process's queue: its events, the oldest first, and its
// bell, which a put rings and the process sleeps on. An event that only
// tells, queued already, is put once.
type eventQueue struct {
	mu     sync.Mutex
	events []event
	bell   chan struct{}
}

// post puts an event in the process's queue and rings its bell; it never
// blocks.
func (p *Process) post(e event) {
	q := &p.events
	q.mu.Lock()
	queued := e.run == nil && slices.ContainsFunc(q.events, func(o event) bool {
		return o.run == nil && o.kind == e.kind && o.id == e.id
	})

	if !queued {
		q.events = append(q.events, e)
	}

	q.mu.Unlock()

	select {
	case q.bell <- struct{}{}:
	default:
	}
}

// next takes the oldest event of the queue, none when it is empty.
func (q *eventQueue) next() (event, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.events) == 0 {
		return event{}, false
	}

	e := q.events[0]
	q.events[0] = event{}
	q.events = q.events[1:]

	return e, true
}

// wake is the process as the waker of its subscriptions: the event of the
// subscription id.
func (p *Process) wake(id uint64) {
	p.post(event{kind: eventChanged, id: id})
}

// subscriptions are n fresh ids for the subscriptions of a wait: none is
// an earlier wait's, whose events may still come.
func (p *Process) subscriptions(n int) uint64 {
	first := p.lastSubscription + 1
	p.lastSubscription += uint64(n)

	return first
}

// wait is every wait of the process. done says whether the wait is over,
// and with what error: first on no event, nil, then on each event of the
// queue, the oldest first, after its callback ran. With the queue empty
// the process sleeps on its bell until end, zero for never; end passing
// ends the wait, timedOut.
func (p *Process) wait(end time.Time, done func(e *event) (bool, error)) (timedOut bool, err error) {
	if over, err := done(nil); over {
		return false, err
	}

	var expired <-chan time.Time

	if !end.IsZero() {
		t := time.NewTimer(time.Until(end))
		defer t.Stop()

		expired = t.C
	}

	for {
		e, ok := p.events.next()

		if !ok {
			select {
			case <-p.events.bell:
			case <-expired:
				return true, nil
			}

			continue
		}

		if e.run != nil {
			e.run(p)
		}

		if over, err := done(&e); over {
			return false, err
		}
	}
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
	// latest is set for a channel of which only the last message counts,
	// an open file's readiness (kernel_remote.go): a message takes the
	// place of those held, and the channel is never full.
	latest   atomic.Bool
	limit    atomic.Int32
	messages []packet
	head     int
	held     atomic.Int32
	ended    atomic.Bool
	finished atomic.Bool
	readers  subscribers
	writers  subscribers
}

// newMsgChannel is a channel of limit messages.
func newMsgChannel(limit int) *msgChannel {
	c := &msgChannel{}
	c.limit.Store(int32(limit))

	return c
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

// put queues a message: EPIPE when the reader is gone, full when there is
// no room, else it wakes the readers.
func (c *msgChannel) put(pk packet) (full bool, err error) {
	c.mu.Lock()

	if c.finished.Load() {
		c.mu.Unlock()

		return false, syscall.EPIPE
	}

	if c.latest.Load() {
		for _, old := range c.messages[c.head:] {
			dropFiles(old.files)
		}

		clear(c.messages)
		c.messages, c.head = append(c.messages[:0], pk), 0
		c.held.Store(1)
		c.mu.Unlock()
		c.nudge()

		return false, nil
	}

	if c.held.Load() >= c.limit.Load() {
		c.mu.Unlock()

		return true, nil
	}

	c.messages = append(c.messages, pk)
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
