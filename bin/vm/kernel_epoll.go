//go:build linux && amd64

package main

import (
	"encoding/binary"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// An epoll is a list of files of the kernel. epoll_wait asks each member
// whether it is ready and, when none is, selects on the changes of them
// all, as every wait of the kernel does: a member is a file like any
// other, with no subscription of its own. A level-triggered member is
// reported while it is ready, an edge-triggered one once a change of its
// file, a one-shot one once until EPOLL_CTL_MOD; the next epoll_wait goes
// on from where the last stopped, so every member gets its turn. A file
// that is ready whenever asked and never changes, a regular file, cannot
// be a member: EPERM, as on Linux. A set can be a member of a set: its
// changes are its members'. The epoll keeps a reference to a member's
// file, so a member is removed before its last close, or stays until the
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
	scan(buf []byte, max int, armed bool, all []<-chan struct{}) (int, []<-chan struct{})
}

type epoll struct {
	mu      sync.Mutex
	members map[epollKey]*epollMember
	// order is the members as added, next where the next epoll_wait
	// starts among them.
	order []*epollMember
	next  int
	refs  atomic.Int32
	ino   uint64
	made  time.Time
}

// An epollKey names a member as Linux does: by the descriptor it was
// added with and the file.
type epollKey struct {
	fd  uint32
	obj File
}

type epollMember struct {
	key    epollKey
	events uint32
	data   uint64
	// last is the changes of the file when it was last reported: an
	// edge-triggered member with the same ones, unclosed, has not changed
	// and is not reported again. A file that counts its edges (an edger)
	// is compared by its count instead, edges, once reported. disabled is
	// a one-shot member reported.
	last     []<-chan struct{}
	edges    uint64
	reported bool
	disabled bool
}

// An edger counts the changes of a file whose changes are shared with
// other files, a channel to a process's (kernel_queue.go): an
// edge-triggered member of a set is new when its count is.
type edger interface {
	edges(events int16) uint64
}

var (
	_ Epoll    = (*epoll)(nil)
	_ Transfer = (*epoll)(nil)
)

var epollInodes atomic.Uint64

func newEpoll() *epoll {
	ep := &epoll{members: map[epollKey]*epollMember{}, ino: epollInodes.Add(1), made: time.Now()}
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
	ep.members, ep.order = map[epollKey]*epollMember{}, nil
	ep.mu.Unlock()

	for _, m := range members {
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

// poll: the epoll is readable when a member would be reported.
func (ep *epoll) poll(events int16) int16 {
	if n, _ := ep.scan(nil, 1, false, nil); n > 0 {
		return (pollIn | pollRdnorm) & events
	}

	return 0
}

// changes are the members', each once: the ends a process reads share
// its queue's.
func (ep *epoll) changes(events int16) []<-chan struct{} {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	var all []<-chan struct{}

	for _, m := range ep.order {
		if !m.disabled {
			all = memberChanges(all, m)
		}
	}

	return all
}

// A changeAppender adds its changes to a list, each once, without a list
// of its own: a wait that asks many files takes them so.
type changeAppender interface {
	appendChanges(events int16, all []<-chan struct{}) []<-chan struct{}
}

// memberChanges adds a member's changes to all.
func memberChanges(all []<-chan struct{}, m *epollMember) []<-chan struct{} {
	if a, ok := m.key.obj.(changeAppender); ok {
		return a.appendChanges(int16(m.events), all)
	}

	return appendChanges(all, m.key.obj.changes(int16(m.events)))
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

// snapshot is the members in order, starting from next.
func (ep *epoll) snapshot() []*epollMember {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	if len(ep.order) == 0 {
		return nil
	}

	at := ep.next % len(ep.order)

	return append(append([]*epollMember(nil), ep.order[at:]...), ep.order[:at]...)
}

// contains tells whether an epoll is a member of the epoll, at any depth:
// a loop would wake forever.
func (ep *epoll) contains(inner *epoll) bool {
	for _, m := range ep.snapshot() {
		if e, ok := m.key.obj.(*epoll); ok && (e == inner || e.contains(inner)) {
			return true
		}
	}

	return false
}

func (ep *epoll) ctl(op uint32, key epollKey, events uint32, data uint64) error {
	switch op {
	case epollCtlAdd:
		return ep.add(key, events, data)
	case epollCtlMod:
		ep.mu.Lock()
		defer ep.mu.Unlock()
		m := ep.members[key]

		if m == nil {
			return syscall.ENOENT
		}

		m.events, m.data, m.disabled, m.last, m.reported = events, data, false, nil, false

		return nil
	case epollCtlDel:
		ep.mu.Lock()
		m := ep.members[key]

		if m == nil {
			ep.mu.Unlock()

			return syscall.ENOENT
		}

		delete(ep.members, key)

		for i, o := range ep.order {
			if o == m {
				ep.order = append(ep.order[:i], ep.order[i+1:]...)

				break
			}
		}

		ep.mu.Unlock()
		key.obj.unref()

		return nil
	}

	return syscall.EINVAL
}

func (ep *epoll) add(key epollKey, events uint32, data uint64) error {
	if inner, ok := key.obj.(*epoll); ok && (inner == ep || inner.contains(ep)) {
		return syscall.ELOOP
	}

	if len(key.obj.changes(int16(events))) == 0 {
		return syscall.EPERM
	}

	ep.mu.Lock()
	defer ep.mu.Unlock()

	if _, dup := ep.members[key]; dup {
		return syscall.EEXIST
	}

	key.obj.ref()
	m := &epollMember{key: key, events: events, data: data}
	ep.members[key] = m
	ep.order = append(ep.order, m)

	return nil
}

// changedSince tells whether a file changed since it was reported with
// its changes last: other channels, or a closed one.
func changedSince(last, now []<-chan struct{}) bool {
	if len(last) != len(now) {
		return true
	}

	for i := range now {
		if now[i] != last[i] || closed(now[i]) {
			return true
		}
	}

	return false
}

// scan reports up to max members that are ready into a guest's
// epoll_event array, or counts one without an array, and tells how many;
// the next scan starts after the last reported. Armed, it adds each
// member's changes to all before it asks the member, so a change after
// the ask closes one of them: a wait that finds none ready sleeps on
// what it returns. The members are asked under the lock, in one pass.
func (ep *epoll) scan(buf []byte, max int, armed bool, all []<-chan struct{}) (int, []<-chan struct{}) {
	ep.mu.Lock()
	defer ep.mu.Unlock()

	count := len(ep.order)

	if count == 0 {
		return 0, all
	}

	at := ep.next % count
	n := 0

	for i := range count {
		m := ep.order[(at+i)%count]

		if m.disabled {
			continue
		}

		if armed {
			all = memberChanges(all, m)
		}

		events := m.events
		revents := m.key.obj.poll(int16(events))

		if revents == 0 {
			continue
		}

		var now []<-chan struct{}
		var edges uint64

		if events&epollEt != 0 {
			if counted, ok := m.key.obj.(edger); ok {
				edges = counted.edges(int16(events))

				if m.reported && edges == m.edges {
					continue
				}
			} else if now = m.key.obj.changes(int16(events)); !changedSince(m.last, now) {
				continue
			}
		}

		if buf == nil {
			return 1, all
		}

		binary.LittleEndian.PutUint32(buf[epollEventSize*n:], uint32(uint16(revents)))
		binary.LittleEndian.PutUint64(buf[epollEventSize*n+8:], m.data)
		n++
		m.last, m.edges, m.reported = now, edges, true
		m.disabled = events&epollOneshot != 0

		if n == max {
			ep.next += i + 1

			break
		}
	}

	return n, all
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

	buf := p.span(at, max*epollEventSize)
	end := deadline(timeout)

	for {
		if n, _ := ep.scan(buf, int(max), false, nil); n > 0 {
			return n, nil
		}

		if !end.IsZero() && !time.Now().Before(end) {
			return 0, nil
		}

		if p.wakeSignal() {
			return 0, syscall.EINTR
		}

		n, all := ep.scan(buf, int(max), true, p.waitChans[:0])

		if n > 0 {
			p.waitChans = all[:0]

			return n, nil
		}

		var timer *time.Timer
		var expired <-chan time.Time

		if !end.IsZero() {
			timer = time.NewTimer(time.Until(end))
			expired = timer.C
		}

		fired := p.sleepOn(all, expired)
		p.waitChans = all[:0]

		if timer != nil {
			timer.Stop()
		}

		if fired {
			return 0, nil
		}
	}
}

// sleepOn sleeps until the process is woken, a change of all closes or
// expired fires, and tells whether it fired. A few changes, as many as the
// files a process reads have, are a plain select.
func (p *Process) sleepOn(all []<-chan struct{}, expired <-chan time.Time) bool {
	switch len(all) {
	case 0:
		select {
		case <-p.wake:
		case <-expired:
			return true
		}

		return false
	case 1:
		select {
		case <-p.wake:
		case <-all[0]:
		case <-expired:
			return true
		}

		return false
	case 2:
		select {
		case <-p.wake:
		case <-all[0]:
		case <-all[1]:
		case <-expired:
			return true
		}

		return false
	}

	cases := make([]reflect.SelectCase, 0, len(all)+2)
	cases = append(cases, recvCase(p.wake), recvCase(expired))

	for _, ch := range all {
		cases = append(cases, recvCase(ch))
	}

	chosen, _, _ := reflect.Select(cases)

	return chosen == 1
}

func (p *Process) libcEpoll_wait(epfd, at, max, timeout uint32) uint32 {
	return p.result(p.epollWait(epfd, at, max, int(int32(timeout))))
}

func (p *Process) libcEpoll_pwait(epfd, at, max, timeout, mask uint32) uint32 {
	return p.result(p.withMask(mask, func() (int, error) {
		return p.epollWait(epfd, at, max, int(int32(timeout)))
	}))
}
