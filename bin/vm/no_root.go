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

func (r *NoRoot) asFile() File {
	return r
}

func (r *NoRoot) asTransfer() Transfer {
	return nil
}

func (r *NoRoot) asSeeker() Seeker {
	return nil
}

func (r *NoRoot) asDir() Dir {
	return r
}

func (r *NoRoot) asTerminal() Terminal {
	return nil
}

func (r *NoRoot) asWindowed() Windowed {
	return nil
}

func (r *NoRoot) asLink() Link {
	return nil
}

func (r *NoRoot) asAttrs() Attrs {
	return nil
}

func (r *NoRoot) asSocket() Socket {
	return nil
}

func (r *NoRoot) asIoctl() Ioctl {
	return nil
}

func (r *NoRoot) asLocker() Locker {
	return nil
}

func (r *NoRoot) asEpoll() *Epoll {
	return nil
}

func (r *NoRoot) asMemfd() *MemfdFile {
	return nil
}

func (r *NoRoot) asPair() *PairEnd {
	return nil
}

func (r *NoRoot) asListener() *UnixListener {
	return nil
}

func (r *NoRoot) asConsole() *ConsoleEnd {
	return nil
}

func (r *NoRoot) asRemote() *RemoteFD {
	return nil
}

func (r *NoRoot) poll(events int16, through []*Epoll) int16 {
	return events & (POLLIN | POLLOUT | POLLRDNORM | POLLWRNORM)
}

func (r *NoRoot) subscribe(s *Subscription, events int16) {
}

func (r *NoRoot) unsubscribe(s *Subscription) {
}

func (r *NoRoot) flags() (int, error) {
	return O_RDONLY, nil
}

func (r *NoRoot) setFlags(fl int) error {
	return nil
}

func (r *NoRoot) readdir(p *Process, b []byte) (int, error) {
	return 0, nil
}

func (r *NoRoot) path(p *Process) (string, error) {
	return "/", nil
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

func (r *NoRoot) openPath(p *Process, path string, flags int, mode uint32) (Object, error) {
	switch {
	case !r.itself(path) && flags&O_CREAT != 0:
		return nil, EROFS
	case !r.itself(path):
		return nil, ENOENT
	case flags&O_CREAT != 0 || flags&3 != O_RDONLY:
		return nil, EISDIR
	}

	r.ref()

	return r, nil
}

func (r *NoRoot) statPath(p *Process, path string, follow bool, st *Stat) error {
	if !r.itself(path) {
		return ENOENT
	}

	return r.stat(p, st)
}

func (r *NoRoot) readlinkPath(p *Process, path string) (string, error) {
	return "", r.gone(path, EINVAL)
}

func (r *NoRoot) mkdirPath(p *Process, path string, mode uint32) error {
	return r.makes(path)
}

func (r *NoRoot) mknodPath(p *Process, path string, mode uint32) error {
	return r.makes(path)
}

func (r *NoRoot) symlinkPath(p *Process, target, path string) error {
	return r.makes(path)
}

func (r *NoRoot) makes(path string) error {
	if r.itself(path) {
		return EEXIST
	}

	return EROFS
}

func (r *NoRoot) unlinkPath(p *Process, path string, dir bool) error {
	if dir {
		return r.gone(path, EBUSY)
	}

	return r.gone(path, EISDIR)
}

func (r *NoRoot) renamePath(p *Process, old string, to Dir, new string) error {
	return r.gone(old, EBUSY)
}

func (r *NoRoot) linkPath(p *Process, old string, to Dir, new string, follow bool) error {
	return r.gone(old, EPERM)
}

func (r *NoRoot) chmodPath(p *Process, path string, mode uint32, follow bool) error {
	return r.gone(path, EROFS)
}

func (r *NoRoot) chownPath(p *Process, path string, uid, gid int, follow bool) error {
	return r.gone(path, EROFS)
}

func (r *NoRoot) utimesPath(p *Process, path string, ts []Timespec, follow bool) error {
	return r.gone(path, EROFS)
}

func (r *NoRoot) truncatePath(p *Process, path string, size int64) error {
	return r.gone(path, EISDIR)
}

func (r *NoRoot) statfsPath(p *Process, path string, st *Statfs) error {
	if !r.itself(path) {
		return ENOENT
	}

	*st = Statfs{Bsize: 4096, Frsize: 4096, Namelen: 255}

	return nil
}

func (r *NoRoot) realpathOf(p *Process, path string) (string, error) {
	return "/", r.gone(path, nil)
}

func (r *NoRoot) mountPath(p *Process, target, source string, conn Object) error {
	return EINVAL
}

func (r *NoRoot) unmountPath(p *Process, path string, flags uint32) error {
	return EINVAL
}

func (r *NoRoot) bindPath(p *Process, path string, mode uint32, held Object) error {
	return EOPNOTSUPP
}

func (r *NoRoot) connectPath(p *Process, path string) (Object, error) {
	return nil, EOPNOTSUPP
}
