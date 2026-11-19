//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
)

var pipeInodes atomic.Uint64

const pipeChunk = 4096

type Pipe struct {
	mu          sync.Mutex
	ch          *MsgChannel
	eof         bool
	front       []byte
	bytes       atomic.Int64
	readers     int
	writers     int
	readerOpens uint64
	writerOpens uint64

	opened Waiters[Turn]
	name   *FifoName
	ino    uint64
	made   time.Time
}

func newPipe() *Pipe {
	return &Pipe{ch: newMsgChannel(queueMax), ino: pipeInodes.Add(1), made: time.Now()}
}

func (pp *Pipe) channel() *MsgChannel {
	pp.mu.Lock()

	defer pp.mu.Unlock()

	return pp.ch
}

func (pp *Pipe) join(reads, writes bool) (uint64, uint64) {
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

	pp.opened.wake(Turn{})
	pp.mu.Unlock()

	if renew {
		old.nudge()
		old.nudgeWriter()
	}

	return readerOpens, writerOpens
}

func (pp *Pipe) meet(p *Process, reads bool, readerOpens, writerOpens uint64) error {
	for {
		_, woken, _, err := attend(p, &p.turns, &pp.mu, &pp.opened, func() bool { return pp.met(reads, readerOpens, writerOpens) }, time.Time{}, restarts)

		if err != nil || !woken {
			return err
		}
	}
}

func (pp *Pipe) met(reads bool, readerOpens, writerOpens uint64) bool {
	if reads {
		return pp.writers > 0 || pp.writerOpens > writerOpens
	}

	return pp.readers > 0 || pp.readerOpens > readerOpens
}

func (pp *Pipe) take(b []byte) (n int, eof, ok bool) {
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
