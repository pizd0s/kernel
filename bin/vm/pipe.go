package main

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

var pipeInodes atomic.Uint64

const (
	pipeChunk = 4096
	pipeSize  = 65536
)

type Pipe struct {
	ObjectIdent

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
	return &Pipe{ObjectIdent: newObjectIdent(), ino: pipeInodes.Add(1), made: time.Now()}
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

func (pp *Pipe) quit(reads, writes bool) {
	if pp.leave(reads, writes) && pp.name != nil {
		pp.name.forget(pp)
	}
}

func (pp *Pipe) closed(d *Description) {
	pp.quit(d.reads(), d.writes())
}

func (pp *Pipe) hang(reads, writes bool, s *Subscription, events int16) {
	if reads && writes && events&(POLLIN|POLLRDNORM|POLLOUT|POLLWRNORM) != 0 {
		reads, writes = events&(POLLIN|POLLRDNORM) != 0, events&(POLLOUT|POLLWRNORM) != 0
	}

	pp.mu.Lock()

	if reads {
		pp.watchers.add(&s.in)
	}

	if writes {
		pp.watchers.add(&s.out)
	}

	pp.mu.Unlock()
}

func (pp *Pipe) unhang(s *Subscription) {
	pp.mu.Lock()
	pp.watchers.remove(&s.in)
	pp.watchers.remove(&s.out)
	pp.mu.Unlock()
}

func (pp *Pipe) subscribe(d *Description, s *Subscription, events int16) {
	pp.hang(d.reads(), d.writes(), s, events)
}

func (pp *Pipe) unsubscribe(d *Description, s *Subscription) {
	pp.unhang(s)
}

func (pp *Pipe) poll(d *Description, events int16, through []*Epoll) int16 {
	return pp.ready(d.reads(), d.writes(), events)
}

func (pp *Pipe) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: pp.ino, Mode: S_IFIFO | 0o600, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: pipeChunk}

	if n := pp.name; n != nil {
		st.Dev, st.Ino, st.Mode = n.key.dev, n.key.ino, n.mode
	}

	st.Atim = nsecToTimespec(pp.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (pp *Pipe) ready(reads, writes bool, events int16) int16 {
	pp.mu.Lock()

	unread, readers, writers := pp.bytes, pp.readers, pp.writers

	pp.mu.Unlock()

	mask := int16(0)

	if writes {
		if pipeSize-unread >= pipeChunk {
			mask |= POLLOUT | POLLWRNORM
		}

		if readers == 0 {
			mask |= POLLERR
		}
	}

	if reads {
		if unread > 0 || writers == 0 {
			mask |= POLLIN | POLLRDNORM
		}

		if writers == 0 {
			mask |= POLLHUP
		}
	}

	return mask & (events | POLLERR | POLLHUP)
}

func (pp *Pipe) ioctl(reads bool, req uintptr, arg unsafe.Pointer) error {
	if req != FIONREAD || !reads {
		return ENOTTY
	}

	*(*int32)(arg) = int32(pp.unread())

	return nil
}

func (pp *Pipe) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
	if p.chaosFail("read") {
		return 0, EIO
	}

	if n == 0 {
		return 0, nil
	}

	pp.mu.Lock()

	for {
		eof := false

		got, err := into.with(0, n, true, func(b []byte) (int, error) {
			taken, ended := pp.take(b)

			eof = ended

			return taken, nil
		})

		if err != nil || got > 0 || eof {
			pp.mu.Unlock()

			return got, err
		}

		if mode.nonblock {
			pp.mu.Unlock()

			return 0, EAGAIN
		}

		if _, err := p.watch(&pp.mu, &pp.watchers, false, time.Time{}, restarts); err != nil {
			pp.mu.Unlock()

			return 0, err
		}
	}
}

func (pp *Pipe) write(p *Process, from *MemfdFile, n int, mode IoMode) (int, error) {
	if p.chaosFail("write") {
		return 0, EIO
	}

	total := 0

	var err error

	pp.mu.Lock()

	for total < n && err == nil {
		var full, gone bool

		put, failed := from.with(int64(total), min(n-total, pipeChunk), false, func(b []byte) (int, error) {
			if full, gone = pp.put(b); full || gone {
				return 0, nil
			}

			return len(b), nil
		})

		switch {
		case failed != nil:
			err = failed
		case gone:
			err = EPIPE
		case !full:
			total += put

			if put == 0 {
				n = total
			}
		case mode.nonblock:
			err = EAGAIN
		default:
			_, err = p.watch(&pp.mu, &pp.watchers, true, time.Time{}, restarts)
		}
	}

	pp.mu.Unlock()

	if err == EPIPE && !mode.quiet {
		p.sendSelf(SIGPIPE)
	}

	if total > 0 {
		return total, nil
	}

	return 0, err
}

func (pp *Pipe) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, pp.stat(p, r.st)
	case *FileRead:
		switch {
		case r.explicit:
			return false, errUnserved
		case !d.reads():
			return false, EBADF
		}

		var err error

		r.count, err = pp.read(p, r.into.memory(), r.n, r.mode)

		return false, err
	case *FileWrite:
		switch {
		case r.explicit:
			return false, errUnserved
		case !d.writes():
			return false, EBADF
		}

		var err error

		r.count, err = pp.write(p, r.from.memory(), r.n, r.mode)

		return false, err
	case *FileIoctl:
		return false, pp.ioctl(d.reads(), r.req, r.arg)
	}

	return false, errUnserved
}
