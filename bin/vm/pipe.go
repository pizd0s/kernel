//go:build linux && amd64

package main

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"
)

var pipeInodes atomic.Uint64

const (
	pipeChunk = 4096
	pipeSize  = 65536
)

type Pipe struct {
	mu          sync.Mutex
	ch          MsgChannel[*Packet]
	tail        *Packet
	reading     *Packet
	spare       *Packet
	front       []byte
	bytes       int
	readers     int
	writers     int
	readerOpens uint64
	writerOpens uint64
	watchers    Subscribers

	opened Waiters[Turn]
	name   *FifoName
	ino    uint64
	made   time.Time
}

func newPipe() *Pipe {
	return &Pipe{ino: pipeInodes.Add(1), made: time.Now()}
}

func (pp *Pipe) join(reads, writes bool) (uint64, uint64) {
	pp.mu.Lock()

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

	if reads && pp.readers == 1 || writes && pp.writers == 1 {
		pp.watchers.tell()
	}

	pp.mu.Unlock()

	return readerOpens, writerOpens
}

func (pp *Pipe) leave(reads, writes bool) (gone bool) {
	pp.mu.Lock()

	if writes {
		pp.writers--

		if pp.writers == 0 && pp.readers > 0 {
			pp.ch.put(&Packet{kind: packetEnd})
			pp.tail = nil
		}
	}

	if reads {
		pp.readers--

		if pp.readers == 0 {
			for _, ok := pp.ch.take(); ok; _, ok = pp.ch.take() {
			}

			pp.front, pp.tail, pp.reading, pp.spare, pp.bytes = nil, nil, nil, nil, 0
		}
	}

	gone = pp.readers == 0 && pp.writers == 0

	pp.watchers.tell()
	pp.mu.Unlock()

	return gone
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

func (pp *Pipe) put(chunk []byte) (full, gone bool) {
	if pp.readers == 0 {
		return false, true
	}

	if pp.bytes > 0 && pp.bytes+len(chunk) > pipeSize {
		return true, false
	}

	if t := pp.tail; t != nil && len(t.data)+len(chunk) <= pipeChunk {
		t.data = append(t.data, chunk...)
	} else if t = pp.spare; t != nil {
		pp.spare = nil
		*t = Packet{data: append(t.data[:0], chunk...)}
		pp.ch.put(t)
		pp.tail = t
	} else {
		t = &Packet{data: bytes.Clone(chunk)}
		pp.ch.put(t)
		pp.tail = t
	}

	pp.bytes += len(chunk)
	pp.watchers.tellIn()

	return false, false
}

func (pp *Pipe) take(b []byte) (n int, eof bool) {
	for n < len(b) {
		if len(pp.front) == 0 {
			pk, ok := pp.ch.peek()

			if !ok {
				break
			}

			if pk.kind == packetEnd && pp.ch.len() == 1 && pp.writers == 0 {
				eof = true

				break
			}

			pp.ch.take()

			if pk == pp.tail {
				pp.tail = nil
			}

			pp.front, pp.reading = pk.data, pk
		}

		c := copy(b[n:], pp.front)

		pp.front = pp.front[c:]
		n += c

		if len(pp.front) == 0 && pp.reading != nil {
			pp.spare, pp.reading = pp.reading, nil
		}
	}

	if n > 0 {
		before := pp.bytes

		pp.bytes -= n

		if before > pipeSize-pipeChunk {
			pp.watchers.tellOut()
		}
	}

	return n, eof && n == 0
}

func (pp *Pipe) unread() int {
	pp.mu.Lock()

	defer pp.mu.Unlock()

	return pp.bytes
}
