//go:build linux && amd64

package main

import (
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Waits. An object tells of its changes whoever subscribed to it, a
// process or a set of epoll: it wakes each subscription's waker with the
// id the subscription named. A process sleeps on one channel, its mail,
// and on nothing else: whatever may change for a wait of the process's,
// an object it waits on, a signal, a child, the server it called, puts a
// function in (post), and the process, woken, takes all that came,
// applies it to itself and looks again at what it waits for (takeMail,
// waitMail). A wait subscribes to the objects it waits for before it first
// looks, so a change after the look is mail. Mail says to look; what
// changed is in the objects, so mail a full box drops is lost to nobody:
// a full box wakes the process, which looks at everything. An object that
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

// A waitQueue is a wait of a goroutine of the kernel's that is no
// process, an inbox's (kernel_inbox.go): the ids that came since the
// waiter took them last, and the channel the first of them closes for the
// waiter asleep.
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

// mailMax is how much mail a process's box holds.
const mailMax = 16

// look is the mail of a change: the process looks again.
func look(*Process) {}

// post puts mail in the process's box; it never blocks.
func (p *Process) post(m func(*Process)) {
	select {
	case p.mail <- m:
	default:
	}
}

// wake is the process as the waker of its subscriptions.
func (p *Process) wake(id uint64) {
	p.post(look)
}

// takeMail applies to the process the mail that came, all of it.
func (p *Process) takeMail() {
	for {
		select {
		case m := <-p.mail:
			m(p)
		default:
			return
		}
	}
}

// waitMail sleeps until mail comes, which it applies, or end passes, zero
// for never; it tells whether end passed.
func (p *Process) waitMail(end time.Time) bool {
	if end.IsZero() {
		(<-p.mail)(p)

		return false
	}

	d := time.Until(end)

	if d <= 0 {
		return true
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case m := <-p.mail:
		m(p)

		return false
	case <-t.C:
		return true
	}
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
