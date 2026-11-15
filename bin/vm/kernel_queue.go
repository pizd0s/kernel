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
// changes, selects on one. A channel is its reader's and its writer's: a
// message and the writer's going wake the queue of the process that read
// it last, room and the reader's going the queue of the one that wrote it
// last. A read or a write by another process moves the channel to that
// one's queue, which is cheap, as the messages stay where they are, and
// wakes who waited on the old one, who ask again. A channel no process
// read or wrote yet wakes the queue it was made with: a pair's, a pipe's
// or a listener's own, or a goroutine's of the host layer, which writes an
// inbox.
type procQueue struct {
	changed changed
}

// procChannel is a channel to a process: the queues it wakes, its messages,
// the oldest first, at most limit of them, and its ends. ended says its
// writer is gone, the end of the data after the messages; finished that
// its reader is, and a write is EPIPE. fills counts what may make it
// readable, a message or its writer's going; frees what may make it
// writable, a take from a full channel or its reader's going: the edges of
// an edge-triggered wait. mu guards the messages; held counts them, so a
// poll asks without taking it.
type procChannel struct {
	mu       sync.Mutex
	reader   atomic.Pointer[procQueue]
	writer   atomic.Pointer[procQueue]
	limit    atomic.Int32
	messages []packet
	head     int
	held     atomic.Int32
	ended    atomic.Bool
	finished atomic.Bool
	fills    atomic.Uint64
	frees    atomic.Uint64
}

// newProcChannel is a channel of limit messages that wakes q until a
// process reads or writes it.
func newProcChannel(q *procQueue, limit int) *procChannel {
	c := &procChannel{}
	c.reader.Store(q)
	c.writer.Store(q)
	c.limit.Store(int32(limit))

	return c
}

// wait is the change of the reader's queue, to select on for input;
// writerWait the writer's, for room.
func (c *procChannel) wait() <-chan struct{} {
	return c.reader.Load().changed.wait()
}

func (c *procChannel) writerWait() <-chan struct{} {
	return c.writer.Load().changed.wait()
}

// nudge wakes the reader's queue, nudgeWriter the writer's: something of
// the channel changed for them.
func (c *procChannel) nudge() {
	c.reader.Load().changed.bump()
}

func (c *procChannel) nudgeWriter() {
	c.writer.Load().changed.bump()
}

// moveTo makes the channel wake q for its reader from now on, moveWriter
// for its writer; who waited on the old queue wakes and asks again.
func (c *procChannel) moveTo(q *procQueue) {
	move(&c.reader, q)
}

func (c *procChannel) moveWriter(q *procQueue) {
	move(&c.writer, q)
}

func move(at *atomic.Pointer[procQueue], q *procQueue) {
	if q == nil || at.Load() == q {
		return
	}

	if old := at.Swap(q); old != q {
		old.changed.bump()
	}
}

// setLimit is the most messages the channel holds from now on: more room
// is a change to its writer.
func (c *procChannel) setLimit(limit int) {
	if int(c.limit.Swap(int32(limit))) < limit {
		c.frees.Add(1)
		c.nudgeWriter()
	}
}

// count is how many messages the channel holds; room tells whether it
// takes another.
func (c *procChannel) count() int {
	return int(c.held.Load())
}

func (c *procChannel) room() bool {
	return c.held.Load() < c.limit.Load()
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

	if c.held.Load() >= c.limit.Load() {
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
// which wakes the writer's queue. The end is asked before the messages: a
// writer puts its messages before it ends, so an end seen with none held
// is the end of the data, never a message that came in between.
func (c *procChannel) take() (pk packet, eof, ok, freed bool) {
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
		c.frees.Add(1)
		c.nudgeWriter()
	}

	return pk, false, true, freed
}

// renew is a channel in place of c, a FIFO's that starts again: with c's
// queues, limit and counts, and what c holds unless its reader is gone.
func (c *procChannel) renew() *procChannel {
	n := newProcChannel(c.reader.Load(), int(c.limit.Load()))
	n.writer.Store(c.writer.Load())
	n.fills.Store(c.fills.Load() + 1)
	n.frees.Store(c.frees.Load() + 1)

	if !c.gone() {
		n.messages = c.takeAll()
		n.held.Store(int32(len(n.messages)))
	}

	return n
}

// takeAll takes every message the channel holds, the oldest first.
func (c *procChannel) takeAll() []packet {
	c.mu.Lock()
	all := c.messages[c.head:]
	c.messages, c.head = nil, 0
	c.held.Store(0)
	c.mu.Unlock()

	return all
}

// end is the writer's going: the end of the data, after the messages.
func (c *procChannel) end() {
	c.ended.Store(true)
	c.fills.Add(1)
	c.nudge()
}

// finish is the reader's going: what is queued goes, files and all, and a
// write is EPIPE from now on, which wakes the writer's queue.
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

	c.nudgeWriter()
}

// edge counts a change of the channel's ends for an edge-triggered wait,
// fill for its reader, free for its writer, and wakes them.
func (c *procChannel) edge(fill, free bool) {
	if fill {
		c.fills.Add(1)
		c.nudge()
	}

	if free {
		c.frees.Add(1)
		c.nudgeWriter()
	}
}

// edges are the channel's counts: what filled it, what freed it.
func (c *procChannel) edges() (fills, frees uint64) {
	return c.fills.Load(), c.frees.Load()
}
