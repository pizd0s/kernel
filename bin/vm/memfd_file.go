//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

var memfdInodes atomic.Uint64

type MemfdFile struct {
	mem  *LinearMemory
	at   int64
	n    int64
	mode int
	held bool
	refs atomic.Int32

	mu   sync.Mutex
	off  int64
	ino  uint64
	made time.Time
}

func newMemfd(mem *LinearMemory, at, n int64, mode int) *MemfdFile {
	f := &MemfdFile{mem: mem, at: at, n: n, mode: mode, ino: memfdInodes.Add(1), made: time.Now()}

	f.refs.Store(1)

	if mem.owner != nil {
		mem.owner.holds.Add(1)
		f.held = true
	}

	return f
}

func bytesFile(b []byte) *MemfdFile {
	return newMemfd(&LinearMemory{data: b}, 0, int64(len(b)), O_RDWR)
}

func grants(mode int, writes bool) bool {
	if writes {
		return mode != O_RDONLY
	}

	return mode != O_WRONLY
}

func narrowed(mode, to int) int {
	switch {
	case to == O_RDWR || mode == to:
		return mode
	case mode == O_RDWR:
		return to
	}

	return mode
}

func (f *MemfdFile) ref() {
	f.refs.Add(1)
}

func (f *MemfdFile) unref() {
	if f.refs.Add(-1) == 0 && f.held {
		f.mem.owner.unhold()
	}
}

func (f *MemfdFile) bytes() []byte {
	b := f.mem.bytes()
	start := min(f.at, int64(len(b)))
	end := int64(len(b))

	if f.n >= 0 {
		end = min(f.at+f.n, end)
	}

	return b[start:end]
}

func (f *MemfdFile) size() int64 {
	f.mem.enter()

	defer f.mem.leave()

	return int64(len(f.bytes()))
}

func (f *MemfdFile) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: f.ino, Mode: S_IFREG | 0o777, Nlink: 1, Size: f.size(),
		Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	st.Atim = nsecToTimespec(f.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (f *MemfdFile) poll(events int16, through []*Epoll) int16 {
	return events & (POLLIN | POLLRDNORM | POLLOUT | POLLWRNORM)
}

func (f *MemfdFile) subscribe(s *Subscription, events int16) {
}

func (f *MemfdFile) unsubscribe(s *Subscription) {
}

func (f *MemfdFile) flags() (int, error) {
	return f.mode, nil
}

func (f *MemfdFile) setFlags(fl int) error {
	return nil
}

func (f *MemfdFile) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	count, err := f.pread(p, into, n, f.off)

	f.off += int64(count)

	return count, err
}

func (f *MemfdFile) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	count, err := f.pwrite(p, from, n, f.off)

	f.off += int64(count)

	return count, err
}

func (f *MemfdFile) pread(p *Process, into Object, n int, off int64) (int, error) {
	f.mem.enter()

	defer f.mem.leave()

	b := f.bytes()
	size := int64(len(b))

	switch {
	case !grants(f.mode, false):
		return 0, EBADF
	case off < 0 || n < 0:
		return 0, EINVAL
	case n == 0 || off >= size:
		return 0, nil
	}

	src := b[off : off+min(int64(n), size-off)]

	return into.with(0, len(src), true, func(dst []byte) (int, error) {
		moveMemory(dst, src[:len(dst)])

		return len(dst), nil
	})
}

func (f *MemfdFile) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	f.mem.enter()

	defer f.mem.leave()

	b := f.bytes()
	size := int64(len(b))

	switch {
	case !grants(f.mode, true):
		return 0, EBADF
	case off < 0 || n < 0:
		return 0, EINVAL
	case n == 0:
		return 0, nil
	case off >= size:
		return 0, ENOSPC
	}

	dst := b[off : off+min(int64(n), size-off)]

	return from.with(0, len(dst), false, func(src []byte) (int, error) {
		moveMemory(dst, src)

		return len(src), nil
	})
}

func (f *MemfdFile) seek(p *Process, off int64, whence int) (int64, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	switch whence {
	case 0:
	case 1:
		off += f.off
	case 2:
		off += f.size()
	default:
		return 0, EINVAL
	}

	if off < 0 {
		return 0, EINVAL
	}

	f.off = off

	return off, nil
}

func (f *MemfdFile) splice(at int64, n int, mode int) Object {
	size := f.size()

	at = min(max(at, 0), size)

	return newMemfd(f.mem, f.at+at, min(int64(max(n, 0)), size-at), narrowed(f.mode, mode))
}

func (f *MemfdFile) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	f.mem.enter()

	defer f.mem.leave()

	b := f.bytes()
	size := int64(len(b))

	switch {
	case !grants(f.mode, writes):
		return 0, EBADF
	case at < 0 || n < 0:
		return 0, EINVAL
	}

	at = min(at, size)

	return fn(b[at : at+min(int64(n), size-at)])
}

func (f *MemfdFile) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	size := f.size()

	switch {
	case out == Object(f):
		return 0, EINVAL
	case n == 0 || from >= size:
		return 0, nil
	case !grants(f.mode, false):
		return 0, EBADF
	}

	n = int(min(int64(n), size-from))

	part := f.splice(from, n, O_RDONLY)
	count, err := out.pwrite(p, part, n, to)

	part.unref()

	return count, err
}

func (f *MemfdFile) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	size := f.size()

	switch {
	case n == 0:
		return 0, nil
	case to >= size:
		return 0, ENOSPC
	case !grants(f.mode, true):
		return 0, EBADF
	}

	n = int(min(int64(n), size-to))

	part := f.splice(to, n, O_WRONLY)
	count, err := in.pread(p, part, n, from)

	part.unref()

	return count, err
}

func (f *MemfdFile) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (f *MemfdFile) truncate(p *Process, size int64) error {
	return EINVAL
}

func (f *MemfdFile) sync(p *Process, data bool) error {
	return EINVAL
}

func (f *MemfdFile) chmod(p *Process, mode uint32) error {
	return EINVAL
}

func (f *MemfdFile) chown(p *Process, uid, gid int) error {
	return EINVAL
}

func (f *MemfdFile) utimes(p *Process, ts []Timespec) error {
	return EINVAL
}

func (f *MemfdFile) statfs(p *Process, st *Statfs) error {
	return EINVAL
}

func (f *MemfdFile) flock(p *Process, op int) error {
	return EBADF
}

func (f *MemfdFile) recordLock(p *Process, cmd int, lk *Flock) error {
	return EBADF
}

func (f *MemfdFile) call(p *Process, op, arg, group uint32, body []byte, files []Object, interruptible bool) (Packet, bool, error) {
	return Packet{}, false, errUnserved
}

func (f *MemfdFile) token() (uint64, error) {
	return 0, EXDEV
}

func (f *MemfdFile) device() FileKey {
	return FileKey{}
}

func (f *MemfdFile) bind(p *Process, a Address) error {
	return ENOTSOCK
}

func (f *MemfdFile) connect(p *Process, a Address) error {
	return ENOTSOCK
}

func (f *MemfdFile) listen(backlog int) error {
	return ENOTSOCK
}

func (f *MemfdFile) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, ENOTSOCK
}

func (f *MemfdFile) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	return nil, ECONNREFUSED
}

func (f *MemfdFile) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	return 0, ENOTSOCK
}

func (f *MemfdFile) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	return 0, Packet{}, 0, ENOTSOCK
}

func (f *MemfdFile) sockname(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (f *MemfdFile) peername(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (f *MemfdFile) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return nil, ENOTSOCK
}

func (f *MemfdFile) setsockopt(level, name uint32, value []byte) error {
	return ENOTSOCK
}

func (f *MemfdFile) shutdown(how int) error {
	return ENOTSOCK
}

func (f *MemfdFile) consume(sink func(pk *Packet)) {
}

func (f *MemfdFile) socketKind() int {
	return 0
}

func (f *MemfdFile) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (f *MemfdFile) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (f *MemfdFile) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
