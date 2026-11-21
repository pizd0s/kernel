//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type Capture struct {
	refs atomic.Int32
	ino  uint64
	size func(first []byte) int

	mu  sync.Mutex
	out []byte
}

func newCapture(size func(first []byte) int) *Capture {
	c := &Capture{ino: pipeInodes.Add(1), size: size}

	c.refs.Store(1)

	return c
}

func (c *Capture) ref() {
	c.refs.Add(1)
}

func (c *Capture) unref() {
	c.refs.Add(-1)
}

func (c *Capture) bytes() []byte {
	c.mu.Lock()

	defer c.mu.Unlock()

	return c.out
}

func (c *Capture) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: c.ino, Mode: S_IFIFO | 0o200, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (c *Capture) poll(events int16, through []*Epoll) int16 {
	return events & (POLLOUT | POLLWRNORM)
}

func (c *Capture) subscribe(s *Subscription, events int16) {
}

func (c *Capture) unsubscribe(s *Subscription) {
}

func (c *Capture) flags() (int, error) {
	return O_WRONLY, nil
}

func (c *Capture) setFlags(fl int) error {
	return nil
}

func (c *Capture) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	return 0, EBADF
}

func (c *Capture) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	return from.with(0, n, false, func(b []byte) (int, error) {
		c.mu.Lock()

		defer c.mu.Unlock()

		if c.out == nil && c.size != nil {
			c.out = make([]byte, 0, max(c.size(b), len(b)))
		}

		c.out = append(c.out, b...)

		return len(b), nil
	})
}

func (c *Capture) pread(p *Process, into Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (c *Capture) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (c *Capture) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (c *Capture) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (c *Capture) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (c *Capture) seek(p *Process, off int64, whence int) (int64, error) {
	return 0, ESPIPE
}

func (c *Capture) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (c *Capture) truncate(p *Process, size int64) error {
	return EINVAL
}

func (c *Capture) sync(p *Process, data bool) error {
	return EINVAL
}

func (c *Capture) chmod(p *Process, mode uint32) error {
	return EINVAL
}

func (c *Capture) chown(p *Process, uid, gid int) error {
	return EINVAL
}

func (c *Capture) utimes(p *Process, ts []Timespec) error {
	return EINVAL
}

func (c *Capture) statfs(p *Process, st *Statfs) error {
	return EINVAL
}

func (c *Capture) flock(p *Process, op int) error {
	return EBADF
}

func (c *Capture) recordLock(p *Process, cmd int, lk *Flock) error {
	return EBADF
}

func (c *Capture) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	return false, errUnserved
}

func (c *Capture) token() (uint64, error) {
	return 0, EXDEV
}

func (c *Capture) device() FileKey {
	return FileKey{}
}

func (c *Capture) bind(p *Process, a Address) error {
	return ENOTSOCK
}

func (c *Capture) connect(p *Process, a Address) error {
	return ENOTSOCK
}

func (c *Capture) listen(backlog int) error {
	return ENOTSOCK
}

func (c *Capture) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, ENOTSOCK
}

func (c *Capture) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	return nil, ECONNREFUSED
}

func (c *Capture) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	return 0, ENOTSOCK
}

func (c *Capture) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	return 0, Packet{}, 0, ENOTSOCK
}

func (c *Capture) sockname(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (c *Capture) peername(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (c *Capture) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return nil, ENOTSOCK
}

func (c *Capture) setsockopt(level, name uint32, value []byte) error {
	return ENOTSOCK
}

func (c *Capture) shutdown(how int) error {
	return ENOTSOCK
}

func (c *Capture) consume(sink func(pk *Packet)) {
}

func (c *Capture) socketKind() int {
	return 0
}

func (c *Capture) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (c *Capture) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (c *Capture) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
