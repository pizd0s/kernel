//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// msgChannel is a channel of messages: the oldest first, at most limit of
// them, its ends, and who waits at it. A receive of messages that waits is
// among taking: a put gives its message to the first of them, and a
// change of what else such a receive sees, the writer's going or a
// shutdown, makes them all look again. A read of a stream waits among
// looking, for its turn to look at what was put, and a write for room
// among putting. The subscriptions it wakes are of poll and epoll,
// readers as it may become readable, a message queued or its writer's
// going, writers as it may become writable, a take from a full channel
// or its reader's going. ended says its writer is gone, the end of the
// data after the messages; finished that its reader is, and a write is
// EPIPE; retired that a FIFO goes on with another channel (renew). mu
// guards the messages and those that wait; held counts the messages, so
// a poll asks without taking it.
type msgChannel struct {
	mu sync.Mutex
	// sink takes the channel's messages as they are put, in place of a
	// reader: what a server sends the kernel on an open file's endpoint
	// (remote_node.go). Nothing is held then, and the channel is never
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
	retired  atomic.Bool
	taking   waiters[received]
	looking  waiters[turn]
	putting  waiters[turn]
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

// nudge wakes who reads, nudgeWriter who writes: something of the
// channel changed for them, which those that wait look at, and the
// subscriptions are told of.
func (c *msgChannel) nudge() {
	c.mu.Lock()
	c.taking.wake(received{eof: true})
	c.looking.wake(turn{})
	c.readers.tell()
	c.mu.Unlock()
}

func (c *msgChannel) nudgeWriter() {
	c.mu.Lock()
	c.putting.wake(turn{})
	c.writers.tell()
	c.mu.Unlock()
}

// subscribe hooks a subscription to the changes for who reads the
// channel, or for who writes it; unsubscribe takes a hook out of either.
func (c *msgChannel) subscribe(l *hook, writes bool) {
	c.mu.Lock()

	if writes {
		c.writers.add(l)
	} else {
		c.readers.add(l)
	}

	c.mu.Unlock()
}

func (c *msgChannel) unsubscribe(l *hook) {
	c.mu.Lock()
	c.readers.remove(l)
	c.writers.remove(l)
	c.mu.Unlock()
}

// receive waits for a message, for a receive of p's that found none:
// given, it is the message, which a put gave it. Not given, with no
// timeout and no error, there is something for the receive to look at: a
// message that came meanwhile, or what look tells of, which a nudge of
// the channel comes with.
func (c *msgChannel) receive(p *Process, look func() bool, end time.Time, how signalled) (pk packet, given, timedOut bool, err error) {
	got, given, timedOut, err := attend(p, &p.receives, &c.mu, &c.taking, func() bool {
		return len(c.messages) > c.head || c.retired.Load() || look()
	}, end, how)

	return got.pk, given && !got.eof, timedOut, err
}

// readable waits for the turn of a stream's read of p's that found
// nothing to look again, as receive waits; writable for that of a write
// that found the channel full.
func (c *msgChannel) readable(p *Process, look func() bool, end time.Time, how signalled) (timedOut bool, err error) {
	_, _, timedOut, err = attend(p, &p.turns, &c.mu, &c.looking, func() bool {
		return len(c.messages) > c.head || c.retired.Load() || look()
	}, end, how)

	return timedOut, err
}

func (c *msgChannel) writable(p *Process, look func() bool, end time.Time, how signalled) (timedOut bool, err error) {
	_, _, timedOut, err = attend(p, &p.turns, &c.mu, &c.putting, func() bool {
		return c.held.Load() < c.limit.Load() || c.finished.Load() || c.retired.Load() || look()
	}, end, how)

	return timedOut, err
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

// put queues a message, a copy of pk, or gives it to a receive that
// waits: EPIPE when the reader is gone, full when there is no room.
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

	if w := c.taking.pop(); w != nil {
		w.wake(received{pk: *pk})
		c.mu.Unlock()

		return false, nil
	}

	if c.held.Load() >= c.limit.Load() {
		c.mu.Unlock()

		return true, nil
	}

	c.messages = append(c.messages, *pk)
	c.held.Add(1)
	c.looking.wake(turn{})
	c.readers.tell()
	c.mu.Unlock()

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

	if freed {
		c.putting.wake(turn{})
		c.writers.tell()
	}

	c.mu.Unlock()

	return pk, false, true, freed
}

// renew is a channel in place of c, a FIFO's that starts again: with c's
// limit and subscriptions, and what c holds unless its reader is gone. c
// is retired: who waits at it, once nudged, goes on at the new one.
func (c *msgChannel) renew() *msgChannel {
	n := newMsgChannel(int(c.limit.Load()))
	c.retired.Store(true)
	c.mu.Lock()
	n.readers.takeOver(&c.readers)
	n.writers.takeOver(&c.writers)
	c.mu.Unlock()

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
