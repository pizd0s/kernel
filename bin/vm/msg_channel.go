//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
)

type MsgChannel struct {
	mu sync.Mutex

	sink     func(pk *Packet)
	closed   func()
	limit    atomic.Int32
	messages []Packet
	head     int

	one      [1]Packet
	held     atomic.Int32
	ended    atomic.Bool
	finished atomic.Bool
	retired  atomic.Bool
	taking   Waiters[Received]
	looking  Waiters[Turn]
	putting  Waiters[Turn]
	readers  Subscribers
	writers  Subscribers
}

func newMsgChannel(limit int) *MsgChannel {
	c := &MsgChannel{}

	c.init(limit)

	return c
}

func (c *MsgChannel) init(limit int) {
	c.limit.Store(int32(limit))
	c.messages = c.one[:0]
}

func (c *MsgChannel) nudge() {
	c.mu.Lock()
	c.taking.wake(Received{eof: true})
	c.looking.wake(Turn{})
	c.readers.tell()
	c.mu.Unlock()
}

func (c *MsgChannel) nudgeWriter() {
	c.mu.Lock()
	c.putting.wake(Turn{})
	c.writers.tell()
	c.mu.Unlock()
}

func (c *MsgChannel) subscribe(l *Hook, writes bool) {
	c.mu.Lock()

	if writes {
		c.writers.add(l)
	} else {
		c.readers.add(l)
	}

	c.mu.Unlock()
}

func (c *MsgChannel) unsubscribe(l *Hook) {
	c.mu.Lock()
	c.readers.remove(l)
	c.writers.remove(l)
	c.mu.Unlock()
}

func (c *MsgChannel) receive(p *Process, look func() bool, end time.Time, how Signalled) (pk Packet, given, timedOut bool, err error) {
	got, given, timedOut, err := attend(p, &p.receives, &c.mu, &c.taking, func() bool {
		return len(c.messages) > c.head || c.retired.Load() || look()
	}, end, how)

	return got.pk, given && !got.eof, timedOut, err
}

func (c *MsgChannel) readable(p *Process, look func() bool, end time.Time, how Signalled) (timedOut bool, err error) {
	_, _, timedOut, err = attend(p, &p.turns, &c.mu, &c.looking, func() bool {
		return len(c.messages) > c.head || c.retired.Load() || look()
	}, end, how)

	return timedOut, err
}

func (c *MsgChannel) writable(p *Process, look func() bool, end time.Time, how Signalled) (timedOut bool, err error) {
	_, _, timedOut, err = attend(p, &p.turns, &c.mu, &c.putting, func() bool {
		return c.held.Load() < c.limit.Load() || c.finished.Load() || c.retired.Load() || look()
	}, end, how)

	return timedOut, err
}

func (c *MsgChannel) setLimit(limit int) {
	if int(c.limit.Swap(int32(limit))) < limit {
		c.nudgeWriter()
	}
}

func (c *MsgChannel) count() int {
	return int(c.held.Load())
}

func (c *MsgChannel) room() bool {
	return c.held.Load() < c.limit.Load()
}

func (c *MsgChannel) gone() bool {
	return c.finished.Load()
}

func (c *MsgChannel) put(pk *Packet) (full bool, err error) {
	c.mu.Lock()

	if c.finished.Load() {
		c.mu.Unlock()

		return false, EPIPE
	}

	if sink := c.sink; sink != nil {
		c.mu.Unlock()
		sink(pk)

		return false, nil
	}

	if w := c.taking.pop(); w != nil {
		w.wake(Received{pk: *pk})
		c.mu.Unlock()

		return false, nil
	}

	if c.held.Load() >= c.limit.Load() {
		c.mu.Unlock()

		return true, nil
	}

	c.messages = append(c.messages, *pk)
	c.held.Add(1)
	c.looking.wake(Turn{})
	c.readers.tell()
	c.mu.Unlock()

	return false, nil
}

func (c *MsgChannel) take() (pk Packet, eof, ok, freed bool) {
	ended := c.ended.Load()

	if c.held.Load() == 0 {
		return Packet{}, ended, ended, false
	}

	c.mu.Lock()

	held := len(c.messages) - c.head

	if held == 0 {
		c.mu.Unlock()

		return Packet{}, ended, ended, false
	}

	pk = c.messages[c.head]
	c.messages[c.head] = Packet{}
	c.head++

	if c.head == len(c.messages) {
		c.messages, c.head = c.messages[:0], 0
	}

	c.held.Add(-1)
	freed = held >= int(c.limit.Load())

	if freed {
		c.putting.wake(Turn{})
		c.writers.tell()
	}

	c.mu.Unlock()

	return pk, false, true, freed
}

func (c *MsgChannel) renew() *MsgChannel {
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

func (c *MsgChannel) takeAll() []Packet {
	c.mu.Lock()

	all := c.messages[c.head:]

	c.messages, c.head = nil, 0
	c.held.Store(0)
	c.mu.Unlock()

	return all
}

func (c *MsgChannel) end() {
	c.ended.Store(true)
	c.nudge()
	c.mu.Lock()

	closed := c.closed

	c.mu.Unlock()

	if closed != nil {
		closed()
	}
}

func (c *MsgChannel) drain(sink func(pk *Packet), closed func()) {
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

func (c *MsgChannel) finish() {
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

func (c *MsgChannel) edge(fill, free bool) {
	if fill {
		c.nudge()
	}

	if free {
		c.nudgeWriter()
	}
}
