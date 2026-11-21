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

func (f *DiskFile) flags(p *Process) (int, error) {
	return O_RDWR, nil
}

func (f *DiskFile) setFlags(p *Process, fl int) error {
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

func (f *DiskFile) pread(p *Process, into *MemfdFile, n int, off int64) (int, error) {
	return into.with(0, n, true, func(b []byte) (int, error) {
		return f.at(b, off, false)
	})
}

func (f *DiskFile) pwrite(p *Process, from *MemfdFile, n int, off int64) (int, error) {
	return from.with(0, n, false, func(b []byte) (int, error) {
		return f.at(b, off, true)
	})
}

func (f *DiskFile) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
	count, err := f.pread(p, into, n, f.pos.Load())

	f.pos.Add(int64(count))

	return count, err
}

func (f *DiskFile) write(p *Process, from *MemfdFile, n int, mode IoMode) (int, error) {
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
	return p.fileCopyFrom(out, f, from, to, n)
}

func (f *DiskFile) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return copyThrough(p, in, f, from, to, n)
}

func (f *DiskFile) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, f.stat(p, r.st)
	case *FileSync:
		return false, f.sync(p, r.data)
	case *FileChmod:
		return false, f.chmod(p, r.mode)
	case *FileChown:
		return false, f.chown(p, r.uid, r.gid)
	case *FileUtimes:
		return false, f.utimes(p, r.ts)
	case *FileStatfs:
		return false, f.statfs(p, r.st)
	case *FileGetfl:
		var err error

		r.flags, err = f.flags(p)

		return false, err
	case *FileSetfl:
		return false, f.setFlags(p, r.flags)
	case *FileRead:
		var err error

		if r.explicit {
			r.count, err = f.pread(p, r.into, r.n, r.at)
		} else {
			r.count, err = f.read(p, r.into, r.n, r.mode)
		}

		return false, err
	case *FileWrite:
		var err error

		if r.explicit {
			r.count, err = f.pwrite(p, r.from, r.n, r.at)
		} else {
			r.count, err = f.write(p, r.from, r.n, r.mode)
		}

		return false, err
	case *FileSeek:
		var err error

		r.pos, err = f.seek(p, r.off, r.whence)

		return false, err
	case *FileCopyTo:
		var err error

		r.count, err = f.copyTo(p, r.out, r.from, r.to, r.n)

		return false, err
	case *FileCopyFrom:
		var err error

		r.count, err = f.copyFrom(p, r.in, r.from, r.to, r.n)

		return false, err
	}

	return false, errUnserved
}
