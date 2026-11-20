//go:build linux && amd64

package main

import (
	"sync/atomic"
	"syscall"
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

func (f *DiskFile) asFile() File {
	return f
}

func (f *DiskFile) asTransfer() Transfer {
	return f
}

func (f *DiskFile) asSeeker() Seeker {
	return f
}

func (f *DiskFile) asDir() Dir {
	return nil
}

func (f *DiskFile) asTerminal() Terminal {
	return nil
}

func (f *DiskFile) asLink() Link {
	return nil
}

func (f *DiskFile) asAttrs() Attrs {
	return f
}

func (f *DiskFile) asSocket() Socket {
	return nil
}

func (f *DiskFile) asIoctl() Ioctl {
	return nil
}

func (f *DiskFile) asLocker() Locker {
	return nil
}

func (f *DiskFile) asEpoll() *Epoll {
	return nil
}

func (f *DiskFile) asPair() *PairEnd {
	return nil
}

func (f *DiskFile) asListener() *UnixListener {
	return nil
}

func (f *DiskFile) asConsole() *ConsoleEnd {
	return nil
}

func (f *DiskFile) asRemote() *RemoteFD {
	return nil
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

func (f *DiskFile) pread(p *Process, b []byte, off int64) (int, error) {
	return f.at(b, off, false)
}

func (f *DiskFile) pwrite(p *Process, b []byte, off int64) (int, error) {
	return f.at(b, off, true)
}

func (f *DiskFile) read(p *Process, b []byte, mode IoMode) (int, error) {
	n, err := f.at(b, f.pos.Load(), false)

	f.pos.Add(int64(n))

	return n, err
}

func (f *DiskFile) write(p *Process, b []byte, mode IoMode) (int, error) {
	n, err := f.at(b, f.pos.Load(), true)

	f.pos.Add(int64(n))

	return n, err
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

func (f *DiskFile) copyFileRange(p *Process, in, out Transfer, from, to int64, n int) (int, error) {
	if out == Transfer(f) && in != out {
		return in.copyFileRange(p, in, out, from, to, n)
	}

	return copyThrough(p, in, out, from, to, n)
}
