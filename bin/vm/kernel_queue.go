//go:build linux && amd64

package main

import (
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Waits. An object tells of its changes whoever subscribed to it, a wait
// or a set of epoll: it wakes each subscription's waker with the id the
// subscription named. A wait subscribes to the objects it waits for, asks
// them, and sleeps on its one channel until an id comes, then asks the
// objects of the ids that came and no other (kernel_object.go
// waitObjects, kernel_epoll.go). An object that poll finds not ready for
// events wakes the subscriptions for them as it may become ready. The
// process's own changes, a signal, a child's, wake the queue of the wait
// it is in with ringID (kernel_signal.go ring), so a wait sleeps on its
// one channel whatever it waits for.

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

// A waitQueue is a wait's: the ids that came since the waiter took them
// last, and the channel the first of them closes for the waiter asleep.
type waitQueue struct {
	mu  sync.Mutex
	ids []uint64
	ch  chan struct{}
}

func (q *waitQueue) wake(id uint64) {
	q.mu.Lock()

	if !slices.Contains(q.ids, id) {
		q.ids = append(q.ids, id)
	}

	if q.ch != nil {
		close(q.ch)
		q.ch = nil
	}

	q.mu.Unlock()
}

// ringID is the id of a wake for the process itself, no object's.
const ringID = ^uint64(0)

// sleep is the channel a waiter sleeps on: closed once an id came.
func (q *waitQueue) sleep() <-chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.ids) > 0 {
		return closedChannel
	}

	if q.ch == nil {
		q.ch = make(chan struct{})
	}

	return q.ch
}

// sleepUntil sleeps until an id comes or end passes, zero for never; it
// tells whether end passed.
func (q *waitQueue) sleepUntil(end time.Time) bool {
	if end.IsZero() {
		<-q.sleep()

		return false
	}

	d := time.Until(end)

	if d <= 0 {
		return true
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-q.sleep():
		return false
	case <-t.C:
		return true
	}
}

// take moves the ids that came into into.
func (q *waitQueue) take(into []uint64) []uint64 {
	q.mu.Lock()
	into = append(into, q.ids...)
	q.ids = q.ids[:0]
	q.mu.Unlock()

	return into
}

// came forgets the ids that came and tells whether id was one of them.
func (q *waitQueue) came(id uint64) bool {
	q.mu.Lock()
	had := slices.Contains(q.ids, id)
	q.ids = q.ids[:0]
	q.mu.Unlock()

	return had
}

// reset forgets the ids that came.
func (q *waitQueue) reset() {
	q.mu.Lock()
	q.ids = q.ids[:0]
	q.mu.Unlock()
}

var closedChannel = func() chan struct{} {
	ch := make(chan struct{})
	close(ch)

	return ch
}()

// msgChannel is a channel of messages: the oldest first, at most limit of
// them, its ends, and the subscriptions it wakes, readers as it may
// become readable, a message or its writer's going, writers as it may
// become writable, a take from a full channel or its reader's going.
// ended says its writer is gone, the end of the data after the messages;
// finished that its reader is, and a write is EPIPE. mu guards the
// messages; held counts them, so a poll asks without taking it.
type msgChannel struct {
	mu       sync.Mutex
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

// kernelWait is a wait of the kernel's own, with no process, until ready
// holds: on subscribers of a channel, which wake it.
func kernelWait(list *subscribers, ready func() bool) {
	q := &waitQueue{}
	list.add(q, 0)
	defer list.remove(q, 0)

	for !ready() {
		<-q.sleep()
		q.reset()
	}
}
