//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
)

// A pipe is one direction of a pair (pair_end.go): a channel
// (msg_channel.go) of packets, each a write, read as a stream
// of bytes. A write of at most pipeChunk bytes is one packet, whole or not
// at all, as POSIX asks of a write of at most PIPE_BUF; a larger one goes
// as several, which POSIX allows of a larger write. The pipe holds
// queueMax packets: a capacity in writes, where POSIX asks only that an
// empty pipe take PIPE_BUF. The last writer's close ends the data, the
// last reader's makes a write EPIPE and SIGPIPE. A blocking end waits at
// the pipe's channel for its turn to look again (waiter.go): a
// read's comes with what fills the channel, a write's with what drains
// it.
//
// The pipe of a FIFO (fifo_name.go) has a name, and ends join it as the
// FIFO is opened: a reader that joins after the readers have gone, or a
// writer after the writers, starts it again on a new channel, which takes
// what there was to read.

const pipeChunk = 4096

type pipe struct {
	// ch is the channel, a FIFO's new one when it starts again; eof says
	// its writers closed it. front is what a read left of the packet it
	// took, the next read's; bytes counts it and the packets queued, for
	// FIONREAD; readers and writers count the open ends, readerOpens and
	// writerOpens the ends that ever joined. mu guards them, and the
	// channel's sends and takes, so a take knows whether it made room in a
	// full pipe.
	mu          sync.Mutex
	ch          *msgChannel
	eof         bool
	front       []byte
	bytes       atomic.Int64
	readers     int
	writers     int
	readerOpens uint64
	writerOpens uint64
	// opened are the opens of a FIFO that wait for the other side, which
	// an end's joining wakes to look; name is a FIFO's.
	opened waiters[turn]
	name   *fifoName
	ino    uint64
	made   time.Time
}

var pipeInodes atomic.Uint64

// newPipe is a pipe without an end.
func newPipe() *pipe {
	return &pipe{ch: newMsgChannel(queueMax), ino: pipeInodes.Add(1), made: time.Now()}
}

// channel is the pipe's channel now.
func (pp *pipe) channel() *msgChannel {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	return pp.ch
}

// join adds an end to a FIFO's pipe, and tells how many readers and
// writers have joined it, this one too: a reader that joins after the
// readers have gone, or a writer after the writers, starts the pipe on a
// new channel, with what there was to read.
func (pp *pipe) join(reads, writes bool) (uint64, uint64) {
	pp.mu.Lock()
	old := pp.ch
	renew := reads && pp.readers == 0 && old.gone() || writes && pp.writers == 0 && pp.eof

	if renew {
		pp.ch, pp.eof = old.renew(), false
	}

	if reads {
		pp.readers++
		pp.readerOpens++
	}

	if writes {
		pp.writers++
		pp.writerOpens++
	}

	readerOpens, writerOpens := pp.readerOpens, pp.writerOpens
	pp.opened.wake(turn{})
	pp.mu.Unlock()

	if renew {
		old.nudge()
		old.nudgeWriter()
	}

	return readerOpens, writerOpens
}

// meet waits, for an open of one side of a FIFO, until it has its other
// side (met). A signal interrupts it, unless its handlers restart calls.
func (pp *pipe) meet(p *Process, reads bool, readerOpens, writerOpens uint64) error {
	for {
		_, woken, _, err := attend(p, &p.turns, &pp.mu, &pp.opened, func() bool { return pp.met(reads, readerOpens, writerOpens) }, time.Time{}, restarts)

		if err != nil || !woken {
			return err
		}
	}
}

// met tells whether an open of one side of a FIFO, after the joins it saw,
// has its other side: an end there now, or one that joined since; with
// the pipe's lock.
func (pp *pipe) met(reads bool, readerOpens, writerOpens uint64) bool {
	if reads {
		return pp.writers > 0 || pp.writerOpens > writerOpens
	}

	return pp.readers > 0 || pp.readerOpens > readerOpens
}

// take is what the pipe has for a read now, into b, as far as it fits:
// what a read left first, then the packets queued, of which what does not
// fit is left. ok is false with nothing queued, eof with the writers gone
// and nothing left. A take that makes room in a full pipe wakes its
// writers' queue (waiter.go).
func (pp *pipe) take(b []byte) (n int, eof, ok bool) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	ch := pp.ch

	for n < len(b) {
		if len(pp.front) == 0 {
			pk, end, got, _ := ch.take()

			if end {
				pp.bytes.Add(-int64(n))

				return n, n == 0, true
			}

			if !got {
				pp.bytes.Add(-int64(n))

				return n, false, n > 0
			}

			pp.front = pk.data
		}

		c := copy(b[n:], pp.front)
		pp.front = pp.front[c:]
		n += c
	}

	pp.bytes.Add(-int64(n))

	return n, false, true
}
