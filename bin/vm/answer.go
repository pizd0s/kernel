//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	answerCancel = 1 << iota
	answerLeft
)

type Answer struct {
	refs  atomic.Int32
	asker *Process
	done  atomic.Bool

	mu       sync.Mutex
	data     []byte
	files    []Object
	said     uint32
	uid, gid uint32
	watchers Subscribers
}

func (a *Answer) ref() {
	a.refs.Add(1)
}

func (a *Answer) unref() {
	if a.refs.Add(-1) == 0 {
		a.done.Store(true)
		a.asker.ring()
	}
}

func (a *Answer) cancel(p *Process) {
	a.mu.Lock()

	if a.said&answerLeft == 0 {
		a.said |= answerCancel
		a.uid, a.gid = p.cred.euid(), p.cred.egid()
		a.watchers.tell()
	}

	a.mu.Unlock()
}

func (a *Answer) leave() {
	a.mu.Lock()
	a.said = answerLeft

	files := a.files

	a.files = nil
	a.watchers.tell()
	a.mu.Unlock()
	dropFiles(files)
}

func (a *Answer) taken() Packet {
	a.mu.Lock()

	defer a.mu.Unlock()

	pk := Packet{data: a.data, files: a.files}

	a.files = nil

	return pk
}

func (a *Answer) heard(b []byte) (int, error) {
	a.mu.Lock()

	defer a.mu.Unlock()

	switch {
	case a.said&answerCancel != 0:
		var h [remoteHeader]byte

		a.said &^= answerCancel
		binary.LittleEndian.PutUint32(h[:], rCancel)
		binary.LittleEndian.PutUint32(h[8:], a.uid)
		binary.LittleEndian.PutUint32(h[12:], a.gid)

		return copy(b, h[:]), nil
	case a.said&answerLeft != 0:
		return 0, nil
	}

	return 0, EAGAIN
}

func (a *Answer) put(data []byte, files []Object) (int, error) {
	a.mu.Lock()

	defer a.mu.Unlock()

	if a.said&answerLeft != 0 {
		return 0, EPIPE
	}

	if len(a.data)+len(data) > packetMax {
		return 0, EMSGSIZE
	}

	for _, f := range files {
		f.ref()
	}

	if len(a.data) > 0 {
		a.data = append(a.data, data...)
	} else {
		a.data = data
	}

	a.files = append(a.files, files...)

	return len(data), nil
}

func (a *Answer) poll(events int16, through []*Epoll) int16 {
	a.mu.Lock()

	said := a.said

	a.mu.Unlock()

	mask := int16(POLLOUT | POLLWRNORM)

	if said != 0 {
		mask |= POLLIN | POLLRDNORM
	}

	if said&answerLeft != 0 {
		mask |= POLLHUP
	}

	return mask & (events | POLLERR | POLLHUP)
}

func (a *Answer) subscribe(s *Subscription, events int16) {
	a.mu.Lock()
	a.watchers.add(&s.in)
	a.mu.Unlock()
}

func (a *Answer) unsubscribe(s *Subscription) {
	a.mu.Lock()
	a.watchers.remove(&s.in)
	a.mu.Unlock()
}

func (a *Answer) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	return into.with(0, n, true, a.heard)
}

func (a *Answer) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	data := make([]byte, n)

	got, err := from.with(0, n, false, func(b []byte) (int, error) {
		return copy(data, b), nil
	})

	if err != nil {
		return 0, err
	}

	return a.put(data[:got], nil)
}

func (a *Answer) pread(p *Process, into Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (a *Answer) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (a *Answer) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (a *Answer) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (a *Answer) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (a *Answer) bind(p *Process, addr Address) error {
	return EINVAL
}

func (a *Answer) connect(p *Process, addr Address) error {
	return EISCONN
}

func (a *Answer) listen(backlog int) error {
	return EOPNOTSUPP
}

func (a *Answer) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, EOPNOTSUPP
}

func (a *Answer) sockname(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (a *Answer) peername(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (a *Answer) socketKind() int {
	return SOCK_SEQPACKET
}

func (a *Answer) shutdown(how int) error {
	return nil
}

func (a *Answer) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return nil, ENOPROTOOPT
}

func (a *Answer) setsockopt(level, name uint32, value []byte) error {
	return ENOPROTOOPT
}

func (a *Answer) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	if to.given {
		return 0, EISCONN
	}

	return a.put(pk.data, pk.files)
}

func (a *Answer) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	var h [remoteHeader]byte

	n, err := a.heard(h[:])

	if err != nil {
		return 0, Packet{}, 0, err
	}

	return into.put(0, h[:min(n, want)]), Packet{}, 0, nil
}

func (a *Answer) consume(sink func(pk *Packet)) {
}

func (a *Answer) stat(p *Process, st *Stat) error {
	return EBADF
}

func (a *Answer) flags() (int, error) {
	return 0, EBADF
}

func (a *Answer) setFlags(fl int) error {
	return EBADF
}

func (a *Answer) seek(p *Process, off int64, whence int) (int64, error) {
	return 0, ESPIPE
}

func (a *Answer) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (a *Answer) truncate(p *Process, size int64) error {
	return EINVAL
}

func (a *Answer) sync(p *Process, data bool) error {
	return EINVAL
}

func (a *Answer) chmod(p *Process, mode uint32) error {
	return EINVAL
}

func (a *Answer) chown(p *Process, uid, gid int) error {
	return EINVAL
}

func (a *Answer) utimes(p *Process, ts []Timespec) error {
	return EINVAL
}

func (a *Answer) statfs(p *Process, st *Statfs) error {
	return EINVAL
}

func (a *Answer) flock(p *Process, op int) error {
	return EBADF
}

func (a *Answer) recordLock(p *Process, cmd int, lk *Flock) error {
	return EBADF
}

func (a *Answer) call(p *Process, op, arg, group uint32, body []byte, files []Object, interruptible bool) (Packet, bool, error) {
	return Packet{}, false, errUnserved
}

func (a *Answer) token() (uint64, error) {
	return 0, EXDEV
}

func (a *Answer) device() FileKey {
	return FileKey{}
}

func (a *Answer) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	return nil, ECONNREFUSED
}

func (a *Answer) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (a *Answer) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (a *Answer) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
