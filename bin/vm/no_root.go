//go:build linux && amd64

package main

import (
	"strings"
	"sync/atomic"
	"time"
)

type NoRoot struct {
	refs atomic.Int32
	made time.Time
}

func newNoRoot() *NoRoot {
	r := &NoRoot{made: time.Now()}

	r.refs.Store(1)

	return r
}

func (r *NoRoot) ref() {
	r.refs.Add(1)
}

func (r *NoRoot) unref() {
	r.refs.Add(-1)
}

func (r *NoRoot) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: 1, Mode: S_IFDIR | 0o555, Nlink: 2, Blksize: 4096}
	st.Mtim = nsecToTimespec(r.made.UnixNano())
	st.Atim, st.Ctim = st.Mtim, st.Mtim

	return nil
}

func (r *NoRoot) poll(events int16, through []*Epoll) int16 {
	return events & (POLLIN | POLLOUT | POLLRDNORM | POLLWRNORM)
}

func (r *NoRoot) subscribe(s *Subscription, events int16) {
}

func (r *NoRoot) unsubscribe(s *Subscription) {
}

func (r *NoRoot) flags(p *Process) (int, error) {
	return O_RDONLY, nil
}

func (r *NoRoot) setFlags(p *Process, fl int) error {
	return nil
}

func (r *NoRoot) readdir(p *Process, into *MemfdFile, n int) (int, error) {
	return 0, nil
}

func (r *NoRoot) itself(path string) bool {
	for _, name := range strings.Split(path, "/") {
		if name != "" && name != "." && name != ".." {
			return false
		}
	}

	return true
}

func (r *NoRoot) gone(path string, own error) error {
	if r.itself(path) {
		return own
	}

	return ENOENT
}

func (r *NoRoot) makes(path string) error {
	if r.itself(path) {
		return EEXIST
	}

	return EROFS
}

func (r *NoRoot) call(p *Process, q Request, group uint32, waits bool) (bool, error) {
	switch q := q.(type) {
	case *FileStat:
		return false, r.stat(p, q.st)
	case *VfsOpen:
		switch {
		case !r.itself(q.path) && q.flags&O_CREAT != 0:
			return false, EROFS
		case !r.itself(q.path):
			return false, ENOENT
		case q.flags&O_CREAT != 0 || q.flags&3 != O_RDONLY:
			return false, EISDIR
		}

		r.ref()
		q.file = r

		return false, nil
	case *VfsStat:
		if !r.itself(q.path) {
			return false, ENOENT
		}

		return false, r.stat(p, &q.st)
	case *VfsStatfs:
		if !r.itself(q.path) {
			return false, ENOENT
		}

		q.st = Statfs{Bsize: 4096, Frsize: 4096, Namelen: 255}

		return false, nil
	case *VfsRealpath:
		q.out = "/"

		return false, r.gone(q.path, nil)
	case *VfsPath:
		q.out = "/"

		return false, nil
	case *VfsReadlink:
		return false, r.gone(q.path, EINVAL)
	case *VfsMkdir:
		return false, r.makes(q.path)
	case *VfsMknod:
		return false, r.makes(q.path)
	case *VfsSymlink:
		return false, r.makes(q.path)
	case *VfsUnlink:
		if q.dir {
			return false, r.gone(q.path, EBUSY)
		}

		return false, r.gone(q.path, EISDIR)
	case *VfsRename:
		return false, r.gone(q.old, EBUSY)
	case *VfsLink:
		return false, r.gone(q.old, EPERM)
	case *VfsChmod:
		return false, r.gone(q.path, EROFS)
	case *VfsChown:
		return false, r.gone(q.path, EROFS)
	case *VfsUtimes:
		return false, r.gone(q.path, EROFS)
	case *VfsTruncate:
		return false, r.gone(q.path, EISDIR)
	case *VfsMount, *VfsUnmount:
		return false, EINVAL
	case *FileGetfl:
		var err error

		q.flags, err = r.flags(p)

		return false, err
	case *FileSetfl:
		return false, r.setFlags(p, q.flags)
	case *FileReaddir:
		var err error

		q.count, err = r.readdir(p, q.into, q.n)

		return false, err
	case *FileRead:
		return false, EISDIR
	case *FileWrite:
		return false, EBADF
	case *FileCopyTo, *FileCopyFrom:
		return false, EBADF
	}

	return false, errUnserved
}
