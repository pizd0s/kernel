//go:build linux && amd64

package main

import (
	"encoding/binary"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// An epoll is a set of objects of the kernel, each a member subscribed to
// the set (subscription.go): a change of its object wakes the set with the
// member's id, which marks it, in the order of the marks; a member is
// marked as it is added, as it may be ready then. epoll_wait asks the
// marked members and no other, and while none of them is ready it waits
// among those that wait at the set (subscription.go), whom a mark wakes
// to ask again. A
// level-triggered member reported stays marked, asked again by the next
// wait while it is ready; an edge-triggered one is reported once a change
// of its object, a one-shot one once until EPOLL_CTL_MOD. Any object can
// be a member: one that never changes, a regular file, wakes nothing and
// is ready whenever asked, reported at every wait level-triggered, once
// edge-triggered. A set can be a member of a set: a change of a member
// wakes those subscribed to the set. The epoll keeps a reference to a
// member's object, so a member is removed before its last close, or stays
// until the epoll's.
//
// The guest's struct epoll_event is 16 bytes: events u32, data u64 at 8.

const (
	epollCtlAdd    = 1
	epollCtlDel    = 2
	epollCtlMod    = 3
	epollOneshot   = 1 << 30
	epollEt        = 1 << 31
	epollEventSize = 16
	epollMaxEvents = 1 << 20
)

type epoll struct {
	mu      sync.Mutex
	members map[epollKey]*epollMember
	// order is the members as added, byID by the ids of their
	// subscriptions; ready the marked ones, the oldest mark first, spare
	// the list a wait gave back.
	order  []*epollMember
	byID   map[uint64]*epollMember
	lastID uint64
	ready  []*epollMember
	spare  []*epollMember
	// subs are the subscriptions to the set: of the polls it is asked by,
	// of the sets it is a member of; waiting are its waits that found no
	// member ready, each woken by a mark to ask again, with mu.
	subs    subscribers
	waiting waiters[turn]
	refs    atomic.Int32
	ino     uint64
	made    time.Time
}

// An epollKey names a member as Linux does: by the descriptor it was
// added with and the file.
type epollKey struct {
	fd  uint32
	obj Object
}

// An epollMember is a file of a set: the events it asks for, the data a
// report of it gives. queued says it is marked; disabled that it is a
// one-shot member reported; removed that it is out of the set. asking
// says a wait asks the file, and raced that a mark came meanwhile: the
// file's poll can wake its subscriptions itself, so that mark is the
// wait's to keep, for a member not ready, or to drop, for one it reports
// edge-triggered: the report gives what the mark told.
type epollMember struct {
	key      epollKey
	sub      subscription
	id       uint64
	events   uint32
	data     uint64
	queued   bool
	disabled bool
	removed  bool
	asking   bool
	raced    bool
}

var (
	_ waker = (*epoll)(nil)
)

var epollInodes atomic.Uint64

func newEpoll() *epoll {
	ep := &epoll{members: map[epollKey]*epollMember{}, byID: map[uint64]*epollMember{}, ino: epollInodes.Add(1), made: time.Now()}
	ep.refs.Store(1)

	return ep
}

func (ep *epoll) ref() {
	ep.refs.Add(1)
}

// unref on the last reference lets every member's file go.
func (ep *epoll) unref() {
	if ep.refs.Add(-1) != 0 {
		return
	}

	ep.mu.Lock()
	members := ep.order
	ep.members, ep.byID, ep.order, ep.ready = map[epollKey]*epollMember{}, map[uint64]*epollMember{}, nil, nil

	for _, m := range members {
		m.removed = true
	}

	ep.mu.Unlock()

	for _, m := range members {
		m.key.obj.unsubscribe(&m.sub)
		m.key.obj.unref()
	}
}

// An epoll is a File and a Transfer, and itself to a call that wants one.
func (ep *epoll) asFile() File              { return ep }
func (ep *epoll) asTransfer() Transfer      { return ep }
func (ep *epoll) asSeeker() Seeker          { return nil }
func (ep *epoll) asDir() Dir                { return nil }
func (ep *epoll) asTerminal() Terminal      { return nil }
func (ep *epoll) asWindowed() Windowed      { return nil }
func (ep *epoll) asLink() Link              { return nil }
func (ep *epoll) asAttrs() Attrs            { return nil }
func (ep *epoll) asSocket() Socket          { return nil }
func (ep *epoll) asIoctl() Ioctl            { return nil }
func (ep *epoll) asLocker() Locker          { return nil }
func (ep *epoll) asEpoll() *epoll           { return ep }
func (ep *epoll) asMemfd() *memfdFile       { return nil }
func (ep *epoll) asPair() *pairEnd          { return nil }
func (ep *epoll) asListener() *unixListener { return nil }
func (ep *epoll) asConsole() *consoleEnd    { return nil }
func (ep *epoll) asVFS() *remoteDir         { return nil }
func (ep *epoll) asCall() *callEnd          { return nil }

// stat is an anonymous inode's: no type, as Linux has it.
func (ep *epoll) stat(p *Process, st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Ino: ep.ino, Mode: 0o600, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = syscall.NsecToTimespec(ep.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

// poll: the epoll is readable when a marked member is ready.
func (ep *epoll) poll(events int16) int16 {
	type asked struct {
		obj    Object
		events uint32
	}

	ep.mu.Lock()
	marked := make([]asked, 0, len(ep.ready))

	for _, m := range ep.ready {
		if !m.disabled && !m.removed {
			marked = append(marked, asked{m.key.obj, m.events})
		}
	}

	ep.mu.Unlock()

	for _, m := range marked {
		if m.obj.poll(int16(m.events)) != 0 {
			return (pollIn | pollRdnorm) & events
		}
	}

	return 0
}

// subscribe is to the changes of the members: a wait for the set, or a
// set the set is a member of.
func (ep *epoll) subscribe(s *subscription, events int16) {
	ep.mu.Lock()
	ep.subs.add(&s.in)
	ep.mu.Unlock()
}

func (ep *epoll) unsubscribe(s *subscription) {
	ep.mu.Lock()
	ep.subs.remove(&s.in)
	ep.mu.Unlock()
}

// wake marks the member of a subscription, a change of its object, and
// wakes those subscribed to the set.
func (ep *epoll) wake(l *hook) {
	ep.mu.Lock()

	if m := ep.byID[l.sub.id]; m != nil {
		ep.mark(m)
	}

	ep.subs.tell()
	ep.mu.Unlock()
}

// mark queues a member for the next wait, with the lock, unless it is
// marked, disabled or out, and wakes the waits asleep.
func (ep *epoll) mark(m *epollMember) {
	if m.queued || m.disabled || m.removed {
		return
	}

	if m.asking {
		m.raced = true

		return
	}

	m.queued = true
	ep.ready = append(ep.ready, m)
	ep.waiting.wake(turn{})
}

func (ep *epoll) flags() (int, error) {
	return syscall.O_RDWR, nil
}

func (ep *epoll) setFlags(fl int) error {
	return nil
}

// The Transfer role, to answer EINVAL as Linux does: an epoll has no data.
func (ep *epoll) read(p *Process, b []byte, mode ioMode) (int, error) {
	return 0, syscall.EINVAL
}

func (ep *epoll) write(p *Process, b []byte, mode ioMode) (int, error) {
	return 0, syscall.EINVAL
}

func (ep *epoll) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.EINVAL
}

func (ep *epoll) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.EINVAL
}

// contains tells whether an epoll is a member of the epoll, at any depth:
// a loop would wake forever.
func (ep *epoll) contains(inner *epoll) bool {
	ep.mu.Lock()
	members := slices.Clone(ep.order)
	ep.mu.Unlock()

	for _, m := range members {
		if e := m.key.obj.asEpoll(); e != nil && (e == inner || e.contains(inner)) {
			return true
		}
	}

	return false
}

// ctl changes the set: a member's subscription is taken and given back
// without the set's lock, which a change of the member's file takes.
func (ep *epoll) ctl(op uint32, key epollKey, events uint32, data uint64) error {
	switch op {
	case epollCtlAdd:
		return ep.add(key, events, data)
	case epollCtlMod:
		ep.mu.Lock()
		m := ep.members[key]

		if m == nil {
			ep.mu.Unlock()

			return syscall.ENOENT
		}

		m.events, m.data, m.disabled = events, data, false
		ep.mu.Unlock()
		key.obj.unsubscribe(&m.sub)
		key.obj.subscribe(&m.sub, int16(events))
		ep.wake(&m.sub.in)

		return nil
	case epollCtlDel:
		ep.mu.Lock()
		m := ep.members[key]

		if m == nil {
			ep.mu.Unlock()

			return syscall.ENOENT
		}

		ep.remove(m)
		ep.mu.Unlock()
		key.obj.unsubscribe(&m.sub)
		key.obj.unref()

		return nil
	}

	return syscall.EINVAL
}

// remove takes a member out of the set, with the lock.
func (ep *epoll) remove(m *epollMember) {
	m.removed = true
	delete(ep.members, m.key)
	delete(ep.byID, m.id)
	ep.order = slices.DeleteFunc(ep.order, func(o *epollMember) bool { return o == m })
}

// add makes a member of an object, asked by the next wait, as it may be
// ready now.
func (ep *epoll) add(key epollKey, events uint32, data uint64) error {
	if inner := key.obj.asEpoll(); inner != nil && (inner == ep || inner.contains(ep)) {
		return syscall.ELOOP
	}

	ep.mu.Lock()

	if _, dup := ep.members[key]; dup {
		ep.mu.Unlock()

		return syscall.EEXIST
	}

	ep.lastID++
	m := &epollMember{key: key, id: ep.lastID, events: events, data: data}
	m.sub.init(ep, m.id)
	ep.members[key], ep.byID[m.id] = m, m
	ep.order = append(ep.order, m)
	ep.mu.Unlock()

	key.obj.subscribe(&m.sub, int16(events))
	key.obj.ref()
	ep.wake(&m.sub.in)

	return nil
}

// collect reports up to max of the marked members that are ready into a
// guest's epoll_event array, and tells how many. The members are asked
// without the set's lock, as a file's change can come with a lock of the
// file's held, a pipe's, which its poll takes too. One not ready waits for
// its next change; a level-triggered one reported is marked again, after
// the others; those max left unasked stay first. who, when there is one,
// is the member reported last, for a collect of one.
func (ep *epoll) collect(buf []byte, max int, who **epollMember) int {
	ep.mu.Lock()

	if len(ep.ready) == 0 {
		ep.mu.Unlock()

		return 0
	}

	marked := ep.ready
	ep.ready, ep.spare = ep.spare[:0], nil

	for _, m := range marked {
		m.queued = false
	}

	n, i := 0, 0

	for ; i < len(marked) && n < max; i++ {
		m := marked[i]
		events := m.events

		if m.disabled || m.removed {
			continue
		}

		m.asking = true
		ep.mu.Unlock()
		revents := m.key.obj.poll(int16(events))
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

		binary.LittleEndian.PutUint32(buf[epollEventSize*n:], uint32(uint16(revents)))
		binary.LittleEndian.PutUint64(buf[epollEventSize*n+8:], m.data)
		n++

		if who != nil {
			*who = m
		}

		m.disabled = events&epollOneshot != 0

		if events&epollEt == 0 {
			ep.mark(m)
		}
	}

	if i < len(marked) {
		left := make([]*epollMember, 0, len(marked)-i+len(ep.ready))

		for _, m := range marked[i:] {
			if !m.queued && !m.disabled && !m.removed {
				m.queued = true
				left = append(left, m)
			}
		}

		ep.ready = append(left, ep.ready...)
	}

	clear(marked)
	ep.spare = marked[:0]
	ep.mu.Unlock()

	return n
}

// wait waits for up to max ready members until end, zero for never: with
// none ready it waits at the set for a mark, and asks again. A signal
// interrupts it.
func (ep *epoll) wait(p *Process, buf []byte, max int, end time.Time) (int, error) {
	for {
		if n := ep.collect(buf, max, nil); n > 0 {
			return n, nil
		}

		if timedOut, err := ep.marked(p, end); timedOut || err != nil {
			return 0, err
		}
	}
}

// marked waits at the set, for a wait of p's that found no member ready,
// until a member is marked: timedOut when end passed first, zero for
// never.
func (ep *epoll) marked(p *Process, end time.Time) (timedOut bool, err error) {
	if !end.IsZero() && !time.Now().Before(end) {
		return true, nil
	}

	_, _, timedOut, err = attend(p, &p.turns, &ep.mu, &ep.waiting, func() bool { return len(ep.ready) > 0 }, end, interrupts)

	return timedOut, err
}

// one waits as wait does for one ready member: the member, and its
// epoll_event in out, or none past the end.
func (ep *epoll) one(p *Process, out []byte, end time.Time) (*epollMember, error) {
	for {
		var m *epollMember

		if ep.collect(out, 1, &m) > 0 {
			return m, nil
		}

		if timedOut, err := ep.marked(p, end); timedOut || err != nil {
			return nil, err
		}
	}
}
