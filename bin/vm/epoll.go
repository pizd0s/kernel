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
	mu      sync.Mutex
	members map[EpollKey]*EpollMember
	ready   MsgChannel[*EpollMember]

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
	ep       *Epoll
	events   uint32
	data     uint64
	queued   bool
	disabled bool
}

func newEpoll() *Epoll {
	ep := &Epoll{members: map[EpollKey]*EpollMember{}, ino: epollInodes.Add(1), made: time.Now()}

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

	members := ep.members

	ep.members, ep.ready = map[EpollKey]*EpollMember{}, MsgChannel[*EpollMember]{}
	ep.mu.Unlock()

	for _, m := range members {
		m.key.obj.unsubscribe(&m.sub)
		m.key.obj.unref()
	}
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

		if !m.disabled && ep.members[m.key] == m {
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

func (ep *Epoll) flags(p *Process) (int, error) {
	return O_RDWR, nil
}

func (ep *Epoll) setFlags(p *Process, fl int) error {
	return nil
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
		key.obj.unsubscribe(&m.sub)
		key.obj.subscribe(&m.sub, int16(events))
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
		key.obj.unsubscribe(&m.sub)
		key.obj.unref()

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

	key.obj.subscribe(&m.sub, int16(events))
	key.obj.ref()
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

		revents := m.key.obj.poll(int16(events), ep.self)

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

func (ep *Epoll) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, ep.stat(p, r.st)
	case *FileGetfl:
		var err error

		r.flags, err = ep.flags(p)

		return false, err
	case *FileSetfl:
		return false, ep.setFlags(p, r.flags)
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
