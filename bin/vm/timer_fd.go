package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

var timerFdInodes atomic.Uint64

type TimerFd struct {
	ObjectIdent

	mu       sync.Mutex
	clock    uint32
	timer    *time.Timer
	epoch    uint64
	end      time.Time
	interval time.Duration
	ticks    uint64
	watchers Subscribers
	nonblock atomic.Bool
	ino      uint64
	made     time.Time
}

func newTimerFd(clock uint32) *TimerFd {
	t := &TimerFd{ObjectIdent: newObjectIdent(), clock: clock, ino: timerFdInodes.Add(1), made: time.Now()}

	return t
}

func (t *TimerFd) closed(d *Description) {
	t.mu.Lock()
	t.disarm()
	t.mu.Unlock()
}

func (t *TimerFd) disarm() {
	if t.timer != nil {
		t.timer.Stop()
	}

	t.epoch++
	t.timer, t.interval = nil, 0
}

func (t *TimerFd) left() time.Duration {
	if t.timer == nil {
		return 0
	}

	return max(time.Until(t.end), time.Nanosecond)
}

func (t *TimerFd) arm(end time.Time) {
	t.end = end

	epoch := t.epoch

	t.timer = time.AfterFunc(time.Until(end), func() {
		t.due(epoch)
	})
}

func (t *TimerFd) due(epoch uint64) {
	t.mu.Lock()

	defer t.mu.Unlock()

	if epoch != t.epoch {
		return
	}

	t.epoch++

	if t.interval > 0 {
		passed := 1 + uint64(max(time.Since(t.end), 0)/t.interval)

		t.ticks += passed
		t.arm(t.end.Add(time.Duration(passed) * t.interval))
	} else {
		t.ticks++
		t.timer = nil
	}

	t.watchers.tellIn()
}

func (t *TimerFd) set(value, interval time.Duration, absolute bool) (time.Duration, time.Duration) {
	t.mu.Lock()

	defer t.mu.Unlock()

	left, every := t.left(), t.interval

	t.disarm()
	t.ticks = 0

	if value > 0 {
		if host, ok := hostClock(t.clock); ok && absolute {
			value = max(value-clockGettime(host), time.Nanosecond)
		}

		t.interval = interval
		t.arm(time.Now().Add(value))
	}

	return left, every
}

func (t *TimerFd) get() (time.Duration, time.Duration) {
	t.mu.Lock()

	defer t.mu.Unlock()

	return t.left(), t.interval
}

func (t *TimerFd) poll(d *Description, events int16, through []*Epoll) int16 {
	t.mu.Lock()

	ticks := t.ticks

	t.mu.Unlock()

	if ticks > 0 {
		return (POLLIN | POLLRDNORM) & events
	}

	return 0
}

func (t *TimerFd) subscribe(d *Description, s *Subscription, events int16) {
	if events&(POLLIN|POLLRDNORM) == 0 {
		return
	}

	t.mu.Lock()
	t.watchers.add(&s.in)
	t.mu.Unlock()
}

func (t *TimerFd) unsubscribe(d *Description, s *Subscription) {
	t.mu.Lock()
	t.watchers.remove(&s.in)
	t.mu.Unlock()
}

func (t *TimerFd) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: t.ino, Mode: 0o600, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = nsecToTimespec(t.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (t *TimerFd) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
	if n < 8 {
		return 0, EINVAL
	}

	t.mu.Lock()

	for t.ticks == 0 {
		if mode.nonblock {
			t.mu.Unlock()

			return 0, EAGAIN
		}

		if _, err := p.watch(&t.mu, &t.watchers, false, time.Time{}, restarts); err != nil {
			t.mu.Unlock()

			return 0, err
		}
	}

	ticks := t.ticks

	got, err := into.with(0, 8, true, func(b []byte) (int, error) {
		binary.LittleEndian.PutUint64(b, ticks)

		return 8, nil
	})

	if err == nil {
		t.ticks = 0
	}

	t.mu.Unlock()

	return got, err
}

func (t *TimerFd) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, t.stat(p, r.st)
	case *FileRead:
		if r.explicit {
			return false, ESPIPE
		}

		var err error

		r.count, err = t.read(p, r.into.memory(), r.n, r.mode)

		return false, err
	case *FileWrite:
		return false, EINVAL
	case *TimerSet:
		r.left, r.every = t.set(r.value, r.interval, r.absolute)

		return false, nil
	case *TimerGet:
		r.left, r.every = t.get()

		return false, nil
	}

	return false, errUnserved
}
