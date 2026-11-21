//go:build linux && amd64

package main

import (
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

type DiskFile struct {
	fd   int
	refs atomic.Int32
	pos  atomic.Int64
	ino  uint64
}

func newDiskFile(fd int) *DiskFile {
	f := &DiskFile{fd: fd, ino: pipeInodes.Add(1)}

	f.refs.Store(1)

	return f
}

func (f *DiskFile) ref() {
	f.refs.Add(1)
}

func (f *DiskFile) unref() {
	if f.refs.Add(-1) == 0 {
		syscall.Close(f.fd)
	}
}

func (f *DiskFile) size() (int64, error) {
	var st syscall.Stat_t

	if err := syscall.Fstat(f.fd, &st); err != nil {
		return 0, EIO
	}

	return st.Size, nil
}

func (f *DiskFile) stat(p *Process, st *Stat) error {
	size, err := f.size()

	if err != nil {
		return err
	}

	*st = Stat{Ino: f.ino, Mode: S_IFBLK | 0o660, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Size: size,
		Blksize: 4096, Blocks: (size + 511) / 512}

	return nil
}

func (f *DiskFile) poll(events int16, through []*Epoll) int16 {
	return events & (POLLIN | POLLRDNORM | POLLOUT | POLLWRNORM)
}

func (f *DiskFile) subscribe(s *Subscription, events int16) {
}

func (f *DiskFile) unsubscribe(s *Subscription) {
}

func (f *DiskFile) flags() (int, error) {
	return O_RDWR, nil
}

func (f *DiskFile) setFlags(fl int) error {
	return nil
}

func (f *DiskFile) at(b []byte, off int64, write bool) (int, error) {
	if off < 0 {
		return 0, EINVAL
	}

	for {
		var n int
		var err error

		if write {
			n, err = syscall.Pwrite(f.fd, b, off)
		} else {
			n, err = syscall.Pread(f.fd, b, off)
		}

		if err == syscall.EINTR {
			continue
		}

		if err != nil {
			return 0, EIO
		}

		return n, nil
	}
}

func (f *DiskFile) pread(p *Process, into Object, n int, off int64) (int, error) {
	return into.with(0, n, true, func(b []byte) (int, error) {
		return f.at(b, off, false)
	})
}

func (f *DiskFile) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	return from.with(0, n, false, func(b []byte) (int, error) {
		return f.at(b, off, true)
	})
}

func (f *DiskFile) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (f *DiskFile) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	count, err := f.pread(p, into, n, f.pos.Load())

	f.pos.Add(int64(count))

	return count, err
}

func (f *DiskFile) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	count, err := f.pwrite(p, from, n, f.pos.Load())

	f.pos.Add(int64(count))

	return count, err
}

func (f *DiskFile) seek(p *Process, off int64, whence int) (int64, error) {
	base := int64(0)

	switch whence {
	case 0:
	case 1:
		base = f.pos.Load()
	case 2:
		size, err := f.size()

		if err != nil {
			return 0, err
		}

		base = size
	default:
		return 0, EINVAL
	}

	if base+off < 0 {
		return 0, EINVAL
	}

	f.pos.Store(base + off)

	return base + off, nil
}

func (f *DiskFile) truncate(p *Process, size int64) error {
	return EINVAL
}

func (f *DiskFile) sync(p *Process, data bool) error {
	sync := syscall.Fsync

	if data {
		sync = syscall.Fdatasync
	}

	if err := sync(f.fd); err != nil {
		return EIO
	}

	return nil
}

func (f *DiskFile) chmod(p *Process, mode uint32) error {
	return EPERM
}

func (f *DiskFile) chown(p *Process, uid, gid int) error {
	return EPERM
}

func (f *DiskFile) utimes(p *Process, ts []Timespec) error {
	return EPERM
}

func (f *DiskFile) statfs(p *Process, st *Statfs) error {
	*st = Statfs{Bsize: 4096, Frsize: 4096, Namelen: 255}

	return nil
}

func (f *DiskFile) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	return out.copyFrom(p, f, from, to, n)
}

func (f *DiskFile) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return copyThrough(p, in, f, from, to, n)
}

func (f *DiskFile) readdir(p *Process, into Object, n int) (int, error) {
	return 0, ENOTDIR
}

func (f *DiskFile) flock(p *Process, op int) error {
	return EBADF
}

func (f *DiskFile) recordLock(p *Process, cmd int, lk *Flock) error {
	return EBADF
}

func (f *DiskFile) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	return false, errUnserved
}

func (f *DiskFile) token() (uint64, error) {
	return 0, EXDEV
}

func (f *DiskFile) device() FileKey {
	return FileKey{}
}

func (f *DiskFile) bind(p *Process, a Address) error {
	return ENOTSOCK
}

func (f *DiskFile) connect(p *Process, a Address) error {
	return ENOTSOCK
}

func (f *DiskFile) listen(backlog int) error {
	return ENOTSOCK
}

func (f *DiskFile) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, ENOTSOCK
}

func (f *DiskFile) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	return nil, ECONNREFUSED
}

func (f *DiskFile) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	return 0, ENOTSOCK
}

func (f *DiskFile) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	return 0, Packet{}, 0, ENOTSOCK
}

func (f *DiskFile) sockname(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (f *DiskFile) peername(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (f *DiskFile) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return nil, ENOTSOCK
}

func (f *DiskFile) setsockopt(level, name uint32, value []byte) error {
	return ENOTSOCK
}

func (f *DiskFile) shutdown(how int) error {
	return ENOTSOCK
}

func (f *DiskFile) consume(sink func(pk *Packet)) {
}

func (f *DiskFile) socketKind() int {
	return 0
}

func (f *DiskFile) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (f *DiskFile) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (f *DiskFile) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
