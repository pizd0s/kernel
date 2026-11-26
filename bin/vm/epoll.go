package main

import (
	"encoding/binary"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

var (
	_           Waker = (*EpollMember)(nil)
	epollInodes atomic.Uint64
)

const epollMaxEvents = 1 << 20

type Epoll struct {
	ObjectIdent

	mu      sync.Mutex
	members map[EpollKey]*EpollMember
	ready   MsgChannel[*EpollMember]

	subs    Subscribers
	told    bool
	self    []*Epoll
	waiting Waiters[Turn]
	ino     uint64
	made    time.Time
}

type EpollKey struct {
	fd   uint32
	desc *Description
}

type EpollMember struct {
	key      EpollKey
	sub      Subscription
	ep       *Epoll
	events   uint32
	data     uint64
	queued   bool
	disabled bool
}

func (m *EpollMember) again(events uint32) {
	if m.sub.dropped.Load() && m.sub.dropped.Swap(false) {
		m.key.desc.subscribe(&m.sub, int16(events))
	}
}

func newEpoll() *Epoll {
	ep := &Epoll{ObjectIdent: newObjectIdent(), members: map[EpollKey]*EpollMember{}, ino: epollInodes.Add(1), made: time.Now()}

	ep.self = []*Epoll{ep}

	return ep
}

func (ep *Epoll) closed(d *Description) {
	ep.mu.Lock()

	members := ep.members

	ep.members, ep.ready = map[EpollKey]*EpollMember{}, MsgChannel[*EpollMember]{}
	ep.mu.Unlock()

	for _, m := range members {
		m.key.desc.unsubscribe(&m.sub)
		m.key.desc.release()
	}
}

func (ep *Epoll) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: ep.ino, Mode: 0o600, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = nsecToTimespec(ep.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (ep *Epoll) poll(d *Description, events int16, through []*Epoll) int16 {
	if slices.Contains(through, ep) {
		return 0
	}

	inner := ep.self

	if len(through) > 0 {
		inner = append(slices.Clip(through), ep)
	}

	type asked struct {
		m      *EpollMember
		events uint32
	}

	ep.mu.Lock()

	ep.told = false

	marked := make([]asked, 0, ep.ready.len())

	for left := ep.ready.len(); left > 0; left-- {
		m, _ := ep.ready.take()

		ep.ready.put(m)

		if !m.disabled && ep.members[m.key] == m {
			marked = append(marked, asked{m, m.events})
		}
	}

	ep.mu.Unlock()

	for _, a := range marked {
		a.m.again(a.events)

		if a.m.key.desc.poll(int16(a.events), inner) != 0 {
			return (POLLIN | POLLRDNORM) & events
		}
	}

	return 0
}

func (ep *Epoll) subscribe(d *Description, s *Subscription, events int16) {
	ep.mu.Lock()
	ep.subs.add(&s.in)
	ep.mu.Unlock()
}

func (ep *Epoll) unsubscribe(d *Description, s *Subscription) {
	ep.mu.Lock()
	ep.subs.remove(&s.in)
	ep.mu.Unlock()
}

func (m *EpollMember) wake(l *Hook) {
	m.ep.ring(m)
}

func (ep *Epoll) ring(m *EpollMember) {
	var buf [4]*Hook

	hooks := buf[:0]

	ep.mu.Lock()

	if !m.disabled && ep.members[m.key] == m {
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
	if m.queued || m.disabled {
		return
	}

	m.queued = true
	ep.ready.put(m)
	ep.waiting.wake(Turn{})
}

func (ep *Epoll) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
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
		key.desc.unsubscribe(&m.sub)
		key.desc.subscribe(&m.sub, int16(events))
		ep.ring(m)

		return nil
	case EPOLL_CTL_DEL:
		ep.mu.Lock()

		m := ep.members[key]

		if m == nil {
			ep.mu.Unlock()

			return ENOENT
		}

		delete(ep.members, key)
		ep.mu.Unlock()
		key.desc.unsubscribe(&m.sub)
		key.desc.release()

		return nil
	}

	return EINVAL
}

func (ep *Epoll) add(key EpollKey, events uint32, data uint64) error {
	ep.mu.Lock()

	if _, dup := ep.members[key]; dup {
		ep.mu.Unlock()

		return EEXIST
	}

	m := &EpollMember{key: key, ep: ep, events: events, data: data}

	m.sub.init(m, 0)
	ep.members[key] = m
	ep.mu.Unlock()

	key.desc.subscribe(&m.sub, int16(events))
	key.desc.hold()
	ep.ring(m)

	return nil
}

func (ep *Epoll) collect(buf []byte, max int) int {
	n := 0

	ep.mu.Lock()

	for left := ep.ready.len(); left > 0 && n < max; left-- {
		m, _ := ep.ready.take()
		events := m.events

		m.queued = false

		if m.disabled || ep.members[m.key] != m {
			continue
		}

		ep.mu.Unlock()
		m.again(events)

		revents := m.key.desc.poll(int16(events), ep.self)

		ep.mu.Lock()

		if revents == 0 || ep.members[m.key] != m {
			continue
		}

		binary.LittleEndian.PutUint32(buf[sizeofEpollEvent*n:], uint32(uint16(revents)))
		binary.LittleEndian.PutUint64(buf[sizeofEpollEvent*n+8:], m.data)
		n++

		m.disabled = events&EPOLLONESHOT != 0

		if events&EPOLLET == 0 {
			ep.mark(m)
		}
	}

	ep.mu.Unlock()

	return n
}

func (ep *Epoll) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	for {
		if n := ep.collect(buf(), max); n > 0 {
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

func (ep *Epoll) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, ep.stat(p, r.st)
	case *EpollCtl:
		return false, ep.epollCtl(r.ctl, r.key, r.events, r.data)
	case *EpollWait:
		var err error

		r.count, err = ep.epollWait(p, r.buf, r.max, r.end)

		return false, err
	case *FileRead:
		return false, EINVAL
	case *FileWrite:
		return false, EINVAL
	}

	return false, errUnserved
}
