package main

import (
	"time"
)

type NoRoot struct {
	ObjectIdent

	made time.Time
}

func newNoRoot() *NoRoot {
	r := &NoRoot{ObjectIdent: newObjectIdent(), made: time.Now()}

	return r
}

func (r *NoRoot) closed(d *Description) {
}

func (r *NoRoot) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: 1, Mode: S_IFDIR | 0o555, Nlink: 2, Blksize: 4096}
	st.Mtim = nsecToTimespec(r.made.UnixNano())
	st.Atim, st.Ctim = st.Mtim, st.Mtim

	return nil
}

func (r *NoRoot) poll(d *Description, events int16, through []*Epoll) int16 {
	return events & (POLLIN | POLLOUT | POLLRDNORM | POLLWRNORM)
}

func (r *NoRoot) subscribe(d *Description, s *Subscription, events int16) {
}

func (r *NoRoot) unsubscribe(d *Description, s *Subscription) {
}

func (r *NoRoot) readdir(p *Process, into *MemfdFile, n int) (int, error) {
	return 0, nil
}

func (r *NoRoot) call(p *Process, d *Description, q Request, group uint32, waits bool) (bool, error) {
	switch q := q.(type) {
	case *FileStat:
		return false, r.stat(p, q.st)
	case *Ctl:
		return false, ENOENT
	case *FileReaddir:
		var err error

		q.count, err = r.readdir(p, q.into.memory(), q.n)

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
