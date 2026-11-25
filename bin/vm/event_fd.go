package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

var eventFdInodes atomic.Uint64

const eventFdMax = ^uint64(0) - 1

type EventFd struct {
	mu        sync.Mutex
	count     uint64
	semaphore bool
	watchers  Subscribers
	refs      atomic.Int32
	nonblock  atomic.Bool
	ino       uint64
	made      time.Time
}

func newEventFd(count uint64, semaphore, nonblock bool) *EventFd {
	e := &EventFd{count: count, semaphore: semaphore, ino: eventFdInodes.Add(1), made: time.Now()}

	e.refs.Store(1)
	e.nonblock.Store(nonblock)

	return e
}

func (e *EventFd) ref() {
	e.refs.Add(1)
}

func (e *EventFd) unref() {
	e.refs.Add(-1)
}

func (e *EventFd) poll(events int16, through []*Epoll) int16 {
	e.mu.Lock()

	count := e.count

	e.mu.Unlock()

	mask := int16(0)

	if count > 0 {
		mask |= POLLIN | POLLRDNORM
	}

	if count < eventFdMax {
		mask |= POLLOUT | POLLWRNORM
	}

	return mask & events
}

func (e *EventFd) subscribe(s *Subscription, events int16) {
	e.mu.Lock()

	if events&(POLLIN|POLLRDNORM) != 0 {
		e.watchers.add(&s.in)
	}

	if events&(POLLOUT|POLLWRNORM) != 0 {
		e.watchers.add(&s.out)
	}

	e.mu.Unlock()
}

func (e *EventFd) unsubscribe(s *Subscription) {
	e.mu.Lock()
	e.watchers.remove(&s.in)
	e.watchers.remove(&s.out)
	e.mu.Unlock()
}

func (e *EventFd) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: e.ino, Mode: 0o600, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = nsecToTimespec(e.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (e *EventFd) flags() int {
	if e.nonblock.Load() {
		return O_RDWR | O_NONBLOCK
	}

	return O_RDWR
}

func (e *EventFd) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
	if n < 8 {
		return 0, EINVAL
	}

	e.mu.Lock()

	for e.count == 0 {
		if mode.nonblock || e.nonblock.Load() {
			e.mu.Unlock()

			return 0, EAGAIN
		}

		if _, err := p.watch(&e.mu, &e.watchers, false, time.Time{}, restarts); err != nil {
			e.mu.Unlock()

			return 0, err
		}
	}

	value := e.count

	if e.semaphore {
		value = 1
	}

	got, err := into.with(0, 8, true, func(b []byte) (int, error) {
		binary.LittleEndian.PutUint64(b, value)

		return 8, nil
	})

	if err == nil {
		e.count -= value
		e.watchers.tellOut()
	}

	e.mu.Unlock()

	return got, err
}

func (e *EventFd) write(p *Process, from *MemfdFile, n int, mode IoMode) (int, error) {
	if n != 8 {
		return 0, EINVAL
	}

	var value uint64

	if _, err := from.with(0, 8, false, func(b []byte) (int, error) {
		value = binary.LittleEndian.Uint64(b)

		return 8, nil
	}); err != nil {
		return 0, err
	}

	if value == ^uint64(0) {
		return 0, EINVAL
	}

	e.mu.Lock()

	for eventFdMax-e.count < value {
		if mode.nonblock || e.nonblock.Load() {
			e.mu.Unlock()

			return 0, EAGAIN
		}

		if _, err := p.watch(&e.mu, &e.watchers, true, time.Time{}, restarts); err != nil {
			e.mu.Unlock()

			return 0, err
		}
	}

	e.count += value

	if value > 0 {
		e.watchers.tellIn()
	}

	e.mu.Unlock()

	return 8, nil
}

func (e *EventFd) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, e.stat(p, r.st)
	case *FileGetfl:
		r.flags = e.flags()

		return false, nil
	case *FileSetfl:
		e.nonblock.Store(r.flags&O_NONBLOCK != 0)

		return false, nil
	case *FileRead:
		if r.explicit {
			return false, ESPIPE
		}

		var err error

		r.count, err = e.read(p, r.into, r.n, r.mode)

		return false, err
	case *FileWrite:
		if r.explicit {
			return false, ESPIPE
		}

		var err error

		r.count, err = e.write(p, r.from, r.n, r.mode)

		return false, err
	}

	return false, errUnserved
}
