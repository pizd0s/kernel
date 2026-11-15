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

// An epoll is a set of files of the kernel, each a member subscribed to
// the set (kernel_queue.go): a change of its file wakes the set with the
// member's id, which marks it, in the order of the marks. epoll_wait asks
// the marked members and no other, and sleeps on the set's one channel
// while none of them is ready. A level-triggered member reported stays
// marked, asked again by the next wait while it is ready; an
// edge-triggered one is reported once a change of its file, a one-shot
// one once until EPOLL_CTL_MOD. A file that is ready whenever asked and
// never changes, a regular file, cannot be a member: EPERM, as on Linux.
// A set can be a member of a set: a change of a member wakes those
// subscribed to the set. The epoll keeps a reference to a member's file,
// so a member is removed before its last close, or stays until the
// epoll's.
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

// An Epoll is the role epoll_ctl and epoll_wait ask of their descriptor.
type Epoll interface {
	File
	ctl(op uint32, key epollKey, events uint32, data uint64) error
	wait(p *Process, buf []byte, max int, end time.Time) (int, error)
}

type epoll struct {
	mu      sync.Mutex
	members map[epollKey]*epollMember
	// order is the members as added, byID by the ids of their
	// subscriptions; ready the marked ones, the oldest mark first, spare
	// the list a wait gave back; ch the channel a mark closes for the
	// waits asleep.
	order  []*epollMember
	byID   map[uint64]*epollMember
	lastID uint64
	ready  []*epollMember
	spare  []*epollMember
	ch     chan struct{}
	// subs are the subscriptions to the set: of the waits it is asked by,
	// of the sets it is a member of.
	subs subscribers
	refs atomic.Int32
	ino  uint64
	made time.Time
}

// An epollKey names a member as Linux does: by the descriptor it was
// added with and the file.
type epollKey struct {
	fd  uint32
	obj File
}

// An epollMember is a file of a set: the events it asks for, the data a
// report of it gives. queued says it is marked; disabled that it is a
// one-shot member reported; removed that it is out of the set. asking
// says a wait asks the file, and raced that a mark came meanwhile: the
// file's poll can wake its subscriptions itself, a host's that reads what
// the host has into its inbox (kernel_host.go), so that mark is the
// wait's to keep, for a member not ready, or to drop, for one it reports
// edge-triggered: the report gives what the mark told.
type epollMember struct {
	key      epollKey
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
	_ Epoll    = (*epoll)(nil)
	_ Transfer = (*epoll)(nil)
	_ waker    = (*epoll)(nil)
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
		m.key.obj.unsubscribe(ep, m.id)
		m.key.obj.unref()
	}
}

// stat is an anonymous inode's: no type, as Linux has it.
func (ep *epoll) stat(st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Ino: ep.ino, Mode: 0o600, Nlink: 1, Uid: uint32(syscall.Getuid()), Gid: uint32(syscall.Getgid()), Blksize: 4096}
	st.Atim = syscall.NsecToTimespec(ep.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

// poll: the epoll is readable when a marked member is ready.
func (ep *epoll) poll(events int16) int16 {
	type asked struct {
		obj    File
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
func (ep *epoll) subscribe(to waker, id uint64, events int16) bool {
	ep.subs.add(to, id)

	return true
}

func (ep *epoll) unsubscribe(to waker, id uint64) {
	ep.subs.remove(to, id)
}

// wake marks the member of an id, a change of its file, and wakes those
// subscribed to the set.
func (ep *epoll) wake(id uint64) {
	ep.mu.Lock()

	if m := ep.byID[id]; m != nil {
		ep.mark(m)
	}

	ep.mu.Unlock()
	ep.subs.tell()
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

	if ep.ch != nil {
		close(ep.ch)
		ep.ch = nil
	}
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
		if e, ok := m.key.obj.(*epoll); ok && (e == inner || e.contains(inner)) {
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
		key.obj.unsubscribe(ep, m.id)
		key.obj.subscribe(ep, m.id, int16(events))
		ep.wake(m.id)

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
		key.obj.unsubscribe(ep, m.id)
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

// add makes a member of a file, asked by the next wait, as it may be ready
// now; a file that never changes cannot be one.
func (ep *epoll) add(key epollKey, events uint32, data uint64) error {
	if inner, ok := key.obj.(*epoll); ok && (inner == ep || inner.contains(ep)) {
		return syscall.ELOOP
	}

	ep.mu.Lock()

	if _, dup := ep.members[key]; dup {
		ep.mu.Unlock()

		return syscall.EEXIST
	}

	ep.lastID++
	m := &epollMember{key: key, id: ep.lastID, events: events, data: data}
	ep.members[key], ep.byID[m.id] = m, m
	ep.order = append(ep.order, m)
	ep.mu.Unlock()

	if !key.obj.subscribe(ep, m.id, int16(events)) {
		ep.mu.Lock()
		ep.remove(m)
		ep.mu.Unlock()

		return syscall.EPERM
	}

	key.obj.ref()
	ep.wake(m.id)

	return nil
}

// collect reports up to max of the marked members that are ready into a
// guest's epoll_event array, and tells how many. The members are asked
// without the set's lock, as a file's change can come with a lock of the
// file's held, a pipe's, which its poll takes too. One not ready waits for
// its next change; a level-triggered one reported is marked again, after
// the others; those max left unasked stay first.
func (ep *epoll) collect(buf []byte, max int) int {
	ep.mu.Lock()
	marked := ep.ready
	ep.ready, ep.spare = ep.spare[:0], nil

	for _, m := range marked {
		m.queued = false
	}

	ep.mu.Unlock()
	n, i := 0, 0

	for ; i < len(marked) && n < max; i++ {
		m := marked[i]
		ep.mu.Lock()
		events, skip := m.events, m.disabled || m.removed
		m.asking = !skip
		ep.mu.Unlock()

		if skip {
			continue
		}

		revents := m.key.obj.poll(int16(events))
		ep.mu.Lock()
		m.asking = false
		raced := m.raced
		m.raced = false

		if revents == 0 {
			if raced {
				ep.mark(m)
			}

			ep.mu.Unlock()

			continue
		}

		binary.LittleEndian.PutUint32(buf[epollEventSize*n:], uint32(uint16(revents)))
		binary.LittleEndian.PutUint64(buf[epollEventSize*n+8:], m.data)
		n++
		m.disabled = events&epollOneshot != 0

		if events&epollEt == 0 {
			ep.mark(m)
		}

		ep.mu.Unlock()
	}

	ep.mu.Lock()

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

// wait waits for up to max ready members until end, zero for never: on
// the set's channel, which a mark closes, and the process's wake, a
// signal's, which interrupts it.
func (ep *epoll) wait(p *Process, buf []byte, max int, end time.Time) (int, error) {
	for {
		if n := ep.collect(buf, max); n > 0 {
			return n, nil
		}

		if !end.IsZero() && !time.Now().Before(end) {
			return 0, nil
		}

		if p.wakeSignal() {
			return 0, syscall.EINTR
		}

		ep.mu.Lock()

		if len(ep.ready) > 0 {
			ep.mu.Unlock()

			continue
		}

		if ep.ch == nil {
			ep.ch = make(chan struct{})
		}

		marked := ep.ch
		ep.mu.Unlock()
		var timer *time.Timer
		var expired <-chan time.Time

		if !end.IsZero() {
			timer = time.NewTimer(time.Until(end))
			expired = timer.C
		}

		fired := false

		select {
		case <-p.wake:
		case <-marked:
		case <-expired:
			fired = true
		}

		if timer != nil {
			timer.Stop()
		}

		if fired {
			return 0, nil
		}
	}
}

// epollAt is the Epoll of a descriptor: another file is EINVAL.
func (p *Process) epollAt(fd uint32) (Epoll, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if ep, ok := obj.(Epoll); ok {
		return ep, nil
	}

	return nil, syscall.EINVAL
}

func (p *Process) libcEpoll_create(size uint32) uint32 {
	if int32(size) <= 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(p.install(newEpoll(), 0, false))
}

func (p *Process) libcEpoll_create1(flags uint32) uint32 {
	if flags&^syscall.O_CLOEXEC != 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(p.install(newEpoll(), 0, flags&syscall.O_CLOEXEC != 0))
}

func (p *Process) libcEpoll_ctl(epfd, op, fd, at uint32) uint32 {
	ep, err := p.epollAt(epfd)

	if err != nil {
		return p.result(0, err)
	}

	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	if fd == epfd {
		return p.result(0, syscall.EINVAL)
	}

	var events uint32
	var data uint64

	if op != epollCtlDel {
		if at == 0 {
			return p.result(0, syscall.EFAULT)
		}

		b := p.span(at, epollEventSize)
		events, data = binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint64(b[8:])
	}

	return p.result(0, ep.ctl(op, epollKey{fd, obj}, events, data))
}

// epollWait waits for up to max ready members, for timeout milliseconds,
// forever when negative; a signal interrupts it, restarted or not.
func (p *Process) epollWait(epfd, at, max uint32, timeout int) (int, error) {
	if int32(max) <= 0 || max > epollMaxEvents {
		return 0, syscall.EINVAL
	}

	ep, err := p.epollAt(epfd)

	if err != nil {
		return 0, err
	}

	return ep.wait(p, p.span(at, max*epollEventSize), int(max), deadline(timeout))
}

func (p *Process) libcEpoll_wait(epfd, at, max, timeout uint32) uint32 {
	return p.result(p.epollWait(epfd, at, max, int(int32(timeout))))
}

func (p *Process) libcEpoll_pwait(epfd, at, max, timeout, mask uint32) uint32 {
	return p.result(p.withMask(mask, func() (int, error) {
		return p.epollWait(epfd, at, max, int(int32(timeout)))
	}))
}
