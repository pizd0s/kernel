//go:build linux && amd64

package main

import (
	"strings"
	"sync/atomic"
	"syscall"
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

func (r *NoRoot) stat(p *Process, st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Ino: 1, Mode: syscall.S_IFDIR | 0o555, Nlink: 2, Blksize: 4096}
	st.Mtim = syscall.NsecToTimespec(r.made.UnixNano())
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

func (r *NoRoot) asVFS() *RemoteDir {
	return nil
}

func (r *NoRoot) asCall() *CallEnd {
	return nil
}

func (r *NoRoot) poll(events int16) int16 {
	return events & (pollIn | pollOut | pollRdnorm | pollWrnorm)
}

func (r *NoRoot) subscribe(s *Subscription, events int16) {
}

func (r *NoRoot) unsubscribe(s *Subscription) {
}

func (r *NoRoot) flags() (int, error) {
	return syscall.O_RDONLY, nil
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

	return syscall.ENOENT
}

func (r *NoRoot) openPath(p *Process, path string, flags int, mode uint32) (Object, error) {
	switch {
	case !r.itself(path) && flags&syscall.O_CREAT != 0:
		return nil, syscall.EROFS
	case !r.itself(path):
		return nil, syscall.ENOENT
	case flags&syscall.O_CREAT != 0 || flags&3 != syscall.O_RDONLY:
		return nil, syscall.EISDIR
	}

	r.ref()

	return r, nil
}

func (r *NoRoot) statPath(p *Process, path string, follow bool, st *syscall.Stat_t) error {
	if !r.itself(path) {
		return syscall.ENOENT
	}

	return r.stat(p, st)
}

func (r *NoRoot) readlinkPath(p *Process, path string) (string, error) {
	return "", r.gone(path, syscall.EINVAL)
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
		return syscall.EEXIST
	}

	return syscall.EROFS
}

func (r *NoRoot) unlinkPath(p *Process, path string, dir bool) error {
	if dir {
		return r.gone(path, syscall.EBUSY)
	}

	return r.gone(path, syscall.EISDIR)
}

func (r *NoRoot) renamePath(p *Process, old string, to Dir, new string) error {
	return r.gone(old, syscall.EBUSY)
}

func (r *NoRoot) linkPath(p *Process, old string, to Dir, new string, follow bool) error {
	return r.gone(old, syscall.EPERM)
}

func (r *NoRoot) chmodPath(p *Process, path string, mode uint32, follow bool) error {
	return r.gone(path, syscall.EROFS)
}

func (r *NoRoot) chownPath(p *Process, path string, uid, gid int, follow bool) error {
	return r.gone(path, syscall.EROFS)
}

func (r *NoRoot) utimesPath(p *Process, path string, ts []syscall.Timespec, follow bool) error {
	return r.gone(path, syscall.EROFS)
}

func (r *NoRoot) truncatePath(p *Process, path string, size int64) error {
	return r.gone(path, syscall.EISDIR)
}

func (r *NoRoot) statfsPath(p *Process, path string, st *syscall.Statfs_t) error {
	if !r.itself(path) {
		return syscall.ENOENT
	}

	*st = syscall.Statfs_t{Bsize: 4096, Frsize: 4096, Namelen: 255}

	return nil
}

func (r *NoRoot) realpathOf(p *Process, path string) (string, error) {
	return "/", r.gone(path, nil)
}

func (r *NoRoot) mountPath(p *Process, target, source string, conn Object) error {
	return syscall.EINVAL
}

func (r *NoRoot) unmountPath(p *Process, path string, flags uint32) error {
	return syscall.EINVAL
}

func (r *NoRoot) bindPath(p *Process, path string, mode uint32, held Object) error {
	return syscall.EOPNOTSUPP
}

func (r *NoRoot) connectPath(p *Process, path string) (Object, error) {
	return nil, syscall.EOPNOTSUPP
}
