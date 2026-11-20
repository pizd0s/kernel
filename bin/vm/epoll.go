//go:build linux && amd64

package main

import (
	"encoding/binary"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

var (
	_           Waker = (*Epoll)(nil)
	epollInodes atomic.Uint64
)

const epollMaxEvents = 1 << 20

type Epoll struct {
	mu      sync.Mutex
	members map[EpollKey]*EpollMember

	order  []*EpollMember
	byID   map[uint64]*EpollMember
	lastID uint64
	ready  MsgChannel[*EpollMember]

	subs    Subscribers
	told    bool
	self    []*Epoll
	waiting Waiters[Turn]
	refs    atomic.Int32
	ino     uint64
	made    time.Time
}

type EpollKey struct {
	fd  uint32
	obj Object
}

type EpollMember struct {
	key      EpollKey
	sub      Subscription
	id       uint64
	events   uint32
	data     uint64
	queued   bool
	disabled bool
	removed  bool
	asking   bool
	raced    bool
}

func newEpoll() *Epoll {
	ep := &Epoll{members: map[EpollKey]*EpollMember{}, byID: map[uint64]*EpollMember{}, ino: epollInodes.Add(1), made: time.Now()}

	ep.self = []*Epoll{ep}
	ep.refs.Store(1)

	return ep
}

func (ep *Epoll) ref() {
	ep.refs.Add(1)
}

func (ep *Epoll) unref() {
	if ep.refs.Add(-1) != 0 {
		return
	}

	ep.mu.Lock()

	members := ep.order

	ep.members, ep.byID, ep.order, ep.ready = map[EpollKey]*EpollMember{}, map[uint64]*EpollMember{}, nil, MsgChannel[*EpollMember]{}

	for _, m := range members {
		m.removed = true
	}

	ep.mu.Unlock()

	for _, m := range members {
		m.key.obj.unsubscribe(&m.sub)
		m.key.obj.unref()
	}
}

func (ep *Epoll) asFile() File {
	return ep
}

func (ep *Epoll) asTransfer() Transfer {
	return ep
}

func (ep *Epoll) asSeeker() Seeker {
	return nil
}

func (ep *Epoll) asDir() Dir {
	return nil
}

func (ep *Epoll) asTerminal() Terminal {
	return nil
}

func (ep *Epoll) asLink() Link {
	return nil
}

func (ep *Epoll) asAttrs() Attrs {
	return nil
}

func (ep *Epoll) asSocket() Socket {
	return nil
}

func (ep *Epoll) asIoctl() Ioctl {
	return nil
}

func (ep *Epoll) asLocker() Locker {
	return nil
}

func (ep *Epoll) asEpoll() *Epoll {
	return ep
}

func (ep *Epoll) asPair() *PairEnd {
	return nil
}

func (ep *Epoll) asListener() *UnixListener {
	return nil
}

func (ep *Epoll) asConsole() *ConsoleEnd {
	return nil
}

func (ep *Epoll) asRemote() *RemoteFD {
	return nil
}

func (ep *Epoll) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: ep.ino, Mode: 0o600, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = nsecToTimespec(ep.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (ep *Epoll) poll(events int16, through []*Epoll) int16 {
	if slices.Contains(through, ep) {
		return 0
	}

	inner := ep.self

	if len(through) > 0 {
		inner = append(slices.Clip(through), ep)
	}

	type asked struct {
		obj    Object
		events uint32
	}

	ep.mu.Lock()

	ep.told = false

	marked := make([]asked, 0, ep.ready.len())

	for left := ep.ready.len(); left > 0; left-- {
		m, _ := ep.ready.take()

		ep.ready.put(m)

		if !m.disabled && !m.removed {
			marked = append(marked, asked{m.key.obj, m.events})
		}
	}

	ep.mu.Unlock()

	for _, m := range marked {
		if m.obj.poll(int16(m.events), inner) != 0 {
			return (POLLIN | POLLRDNORM) & events
		}
	}

	return 0
}

func (ep *Epoll) subscribe(s *Subscription, events int16) {
	ep.mu.Lock()
	ep.subs.add(&s.in)
	ep.mu.Unlock()
}

func (ep *Epoll) unsubscribe(s *Subscription) {
	ep.mu.Lock()
	ep.subs.remove(&s.in)
	ep.mu.Unlock()
}

func (ep *Epoll) wake(l *Hook) {
	var buf [4]*Hook

	hooks := buf[:0]

	ep.mu.Lock()

	if m := ep.byID[l.sub.id]; m != nil && !m.disabled && !m.removed {
		ep.mark(m)

		if !ep.told {
			ep.told = true
			hooks = ep.subs.hooks(hooks)
		}
	}

	ep.mu.Unlock()

	for _, h := range hooks {
		h.sub.to.wake(h)
	}
}

func (ep *Epoll) mark(m *EpollMember) {
	if m.queued || m.disabled || m.removed {
		return
	}

	if m.asking {
		m.raced = true

		return
	}

	m.queued = true
	ep.ready.put(m)
	ep.waiting.wake(Turn{})
}

func (ep *Epoll) flags() (int, error) {
	return O_RDWR, nil
}

func (ep *Epoll) setFlags(fl int) error {
	return nil
}

func (ep *Epoll) read(p *Process, into Transfer, n int, mode IoMode) (int, error) {
	return 0, EINVAL
}

func (ep *Epoll) write(p *Process, from Transfer, n int, mode IoMode) (int, error) {
	return 0, EINVAL
}

func (ep *Epoll) pread(p *Process, into Transfer, n int, off int64) (int, error) {
	return 0, EINVAL
}

func (ep *Epoll) pwrite(p *Process, from Transfer, n int, off int64) (int, error) {
	return 0, EINVAL
}

func (ep *Epoll) splice(at int64, n int, mode int) Transfer {
	return newSpliced(ep, at, n, mode)
}

func (ep *Epoll) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (ep *Epoll) copyTo(p *Process, out Transfer, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (ep *Epoll) copyFrom(p *Process, in Transfer, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (ep *Epoll) ctl(op uint32, key EpollKey, events uint32, data uint64) error {
	switch op {
	case EPOLL_CTL_ADD:
		return ep.add(key, events, data)
	case EPOLL_CTL_MOD:
		ep.mu.Lock()

		m := ep.members[key]

		if m == nil {
			ep.mu.Unlock()

			return ENOENT
		}

		m.events, m.data, m.disabled = events, data, false
		ep.mu.Unlock()
		key.obj.unsubscribe(&m.sub)
		key.obj.subscribe(&m.sub, int16(events))
		ep.wake(&m.sub.in)

		return nil
	case EPOLL_CTL_DEL:
		ep.mu.Lock()

		m := ep.members[key]

		if m == nil {
			ep.mu.Unlock()

			return ENOENT
		}

		ep.remove(m)
		ep.mu.Unlock()
		key.obj.unsubscribe(&m.sub)
		key.obj.unref()

		return nil
	}

	return EINVAL
}

func (ep *Epoll) remove(m *EpollMember) {
	m.removed = true
	delete(ep.members, m.key)
	delete(ep.byID, m.id)
	ep.order = slices.DeleteFunc(ep.order, func(o *EpollMember) bool { return o == m })
}

func (ep *Epoll) add(key EpollKey, events uint32, data uint64) error {
	ep.mu.Lock()

	if _, dup := ep.members[key]; dup {
		ep.mu.Unlock()

		return EEXIST
	}

	ep.lastID++

	m := &EpollMember{key: key, id: ep.lastID, events: events, data: data}

	m.sub.init(ep, m.id)
	ep.members[key], ep.byID[m.id] = m, m
	ep.order = append(ep.order, m)
	ep.mu.Unlock()

	key.obj.subscribe(&m.sub, int16(events))
	key.obj.ref()
	ep.wake(&m.sub.in)

	return nil
}

func (ep *Epoll) collect(buf []byte, max int, who **EpollMember) int {
	n := 0

	ep.mu.Lock()

	for left := ep.ready.len(); left > 0 && n < max; left-- {
		m, _ := ep.ready.take()
		events := m.events

		m.queued = false

		if m.disabled || m.removed {
			continue
		}

		m.asking = true
		ep.mu.Unlock()

		revents := m.key.obj.poll(int16(events), ep.self)

		ep.mu.Lock()
		m.asking = false

		raced := m.raced

		m.raced = false

		if revents == 0 {
			if raced {
				ep.mark(m)
			}

			continue
		}

		binary.LittleEndian.PutUint32(buf[sizeofEpollEvent*n:], uint32(uint16(revents)))
		binary.LittleEndian.PutUint64(buf[sizeofEpollEvent*n+8:], m.data)
		n++

		if who != nil {
			*who = m
		}

		m.disabled = events&EPOLLONESHOT != 0

		if events&EPOLLET == 0 {
			ep.mark(m)
		}
	}

	ep.mu.Unlock()

	return n
}

func (ep *Epoll) wait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	for {
		if n := ep.collect(buf(), max, nil); n > 0 {
			return n, nil
		}

		if timedOut, err := ep.marked(p, end); timedOut || err != nil {
			return 0, err
		}
	}
}

func (ep *Epoll) marked(p *Process, end time.Time) (timedOut bool, err error) {
	if !end.IsZero() && !time.Now().Before(end) {
		return true, nil
	}

	_, _, timedOut, err = attend(p, &p.turns, &ep.mu, &ep.waiting, func() bool { return ep.ready.len() > 0 }, end, interrupts)

	return timedOut, err
}

func (ep *Epoll) one(p *Process, out []byte, end time.Time) (*EpollMember, error) {
	for {
		var m *EpollMember

		if ep.collect(out, 1, &m) > 0 {
			return m, nil
		}

		if timedOut, err := ep.marked(p, end); timedOut || err != nil {
			return nil, err
		}
	}
}
