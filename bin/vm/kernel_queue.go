//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"syscall"
)

// A process's queue and the channels to it. Every channel to a process
// keeps its messages by itself, under its own name, and wakes the process
// by one change, the queue's: the queue multiplexes the channels to one
// process, so a wait on any number of them, which selects on their
// changes, selects on one. A channel is its reader's: it wakes the queue
// of the process that took from it last. A take by another process moves
// it to that one's queue, which is cheap, as the messages stay where they
// are, and wakes who waited on the old one, who ask again. One that no
// process took from yet wakes the queue it was made with, a pair's own.
type procQueue struct {
	changed changed
}

// procChannel is a channel to a process: the queue it wakes, its messages,
// the oldest first, at most queueMax of them, and its ends. ended says its
// writer is gone, the end of the data after the messages; finished that
// its reader is, and a write is EPIPE. fills counts what may make it
// readable, a message or its writer's going; frees what may make it
// writable, a take from a full channel or its reader's going: the edges of
// an edge-triggered wait. mu guards the messages; held counts them, so a
// poll asks without taking it.
type procChannel struct {
	mu       sync.Mutex
	queue    atomic.Pointer[procQueue]
	messages []packet
	head     int
	held     atomic.Int32
	ended    atomic.Bool
	finished atomic.Bool
	fills    atomic.Uint64
	frees    atomic.Uint64
}

func newProcChannel(q *procQueue) *procChannel {
	c := &procChannel{}
	c.queue.Store(q)

	return c
}

// wait is the change of the channel's queue, to select on.
func (c *procChannel) wait() <-chan struct{} {
	return c.queue.Load().changed.wait()
}

// nudge wakes the channel's queue: something of the channel changed for
// its reader, or for the writer whose queue it is.
func (c *procChannel) nudge() {
	c.queue.Load().changed.bump()
}

// moveTo makes the channel wake q from now on; who waited on the old
// queue wakes and asks again.
func (c *procChannel) moveTo(q *procQueue) {
	if q == nil || c.queue.Load() == q {
		return
	}

	if old := c.queue.Swap(q); old != q {
		old.changed.bump()
	}
}

// count is how many messages the channel holds.
func (c *procChannel) count() int {
	return int(c.held.Load())
}

// gone tells whether the channel's reader is gone.
func (c *procChannel) gone() bool {
	return c.finished.Load()
}

// put queues a message: EPIPE when the reader is gone, full when there is
// no room, else it wakes the reader's queue.
func (c *procChannel) put(pk packet) (full bool, err error) {
	c.mu.Lock()

	if c.finished.Load() {
		c.mu.Unlock()

		return false, syscall.EPIPE
	}

	if c.held.Load() >= queueMax {
		c.mu.Unlock()

		return true, nil
	}

	c.messages = append(c.messages, pk)
	c.held.Add(1)
	c.fills.Add(1)
	c.mu.Unlock()
	c.nudge()

	return false, nil
}

// take is the oldest message, ok false when there is none: eof when the
// writer is gone too. freed says the take made room in a full channel,
// for the writer to learn of.
func (c *procChannel) take() (pk packet, eof, ok, freed bool) {
	if c.held.Load() == 0 {
		ended := c.ended.Load()

		return packet{}, ended, ended, false
	}

	c.mu.Lock()
	held := len(c.messages) - c.head

	if held == 0 {
		c.mu.Unlock()
		ended := c.ended.Load()

		return packet{}, ended, ended, false
	}

	pk = c.messages[c.head]
	c.messages[c.head] = packet{}
	c.head++

	if c.head == len(c.messages) {
		c.messages, c.head = c.messages[:0], 0
	}

	c.held.Add(-1)
	freed = held == queueMax
	c.mu.Unlock()

	if freed {
		c.frees.Add(1)
	}

	return pk, false, true, freed
}

// end is the writer's going: the end of the data, after the messages.
func (c *procChannel) end() {
	c.ended.Store(true)
	c.fills.Add(1)
	c.nudge()
}

// finish is the reader's going: what is queued goes, files and all, and a
// write is EPIPE from now on.
func (c *procChannel) finish() {
	c.mu.Lock()
	dropped := c.messages[c.head:]
	c.messages, c.head = nil, 0
	c.held.Store(0)
	c.finished.Store(true)
	c.mu.Unlock()
	c.frees.Add(1)

	for _, pk := range dropped {
		dropFiles(pk.files)
	}
}

// edge counts a change of the channel's ends for an edge-triggered wait:
// fill for its reader, free for its writer.
func (c *procChannel) edge(fill, free bool) {
	if fill {
		c.fills.Add(1)
	}

	if free {
		c.frees.Add(1)
	}
}

// edges are the channel's counts: what filled it, what freed it.
func (c *procChannel) edges() (fills, frees uint64) {
	return c.fills.Load(), c.frees.Load()
}
