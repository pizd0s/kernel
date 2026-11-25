package main

import (
	"sync/atomic"
	"time"
)

type NoRoot struct {
	ObjectIdent

	refs atomic.Int32
	made time.Time
}

func newNoRoot() *NoRoot {
	r := &NoRoot{ObjectIdent: newObjectIdent(), made: time.Now()}

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

func (r *NoRoot) call(p *Process, q Request, group uint32, waits bool) (bool, error) {
	switch q := q.(type) {
	case *FileStat:
		return false, r.stat(p, q.st)
	case *Ctl:
		return false, ENOENT
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
