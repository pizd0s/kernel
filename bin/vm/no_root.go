//go:build linux && amd64

package main

import (
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// noRoot is the root of a process that has none, the first process's
// when `vm run` is given no root: an empty directory nothing is under.
type noRoot struct {
	refs atomic.Int32
	made time.Time
}

func newNoRoot() *noRoot {
	r := &noRoot{made: time.Now()}
	r.refs.Store(1)

	return r
}

func (r *noRoot) ref()   { r.refs.Add(1) }
func (r *noRoot) unref() { r.refs.Add(-1) }

func (r *noRoot) stat(p *Process, st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Ino: 1, Mode: syscall.S_IFDIR | 0o555, Nlink: 2, Blksize: 4096}
	st.Mtim = syscall.NsecToTimespec(r.made.UnixNano())
	st.Atim, st.Ctim = st.Mtim, st.Mtim

	return nil
}

// A noRoot is a File and a Dir.
func (r *noRoot) asFile() File              { return r }
func (r *noRoot) asTransfer() Transfer      { return nil }
func (r *noRoot) asSeeker() Seeker          { return nil }
func (r *noRoot) asDir() Dir                { return r }
func (r *noRoot) asTerminal() Terminal      { return nil }
func (r *noRoot) asWindowed() Windowed      { return nil }
func (r *noRoot) asLink() Link              { return nil }
func (r *noRoot) asAttrs() Attrs            { return nil }
func (r *noRoot) asSocket() Socket          { return nil }
func (r *noRoot) asIoctl() Ioctl            { return nil }
func (r *noRoot) asLocker() Locker          { return nil }
func (r *noRoot) asEpoll() *epoll           { return nil }
func (r *noRoot) asMemfd() *memfdFile       { return nil }
func (r *noRoot) asPair() *pairEnd          { return nil }
func (r *noRoot) asListener() *unixListener { return nil }
func (r *noRoot) asConsole() *consoleEnd    { return nil }
func (r *noRoot) asVFS() *remoteDir         { return nil }
func (r *noRoot) asCall() *callEnd          { return nil }

func (r *noRoot) poll(events int16) int16 {
	return events & (pollIn | pollOut | pollRdnorm | pollWrnorm)
}

func (r *noRoot) subscribe(s *subscription, events int16)   {}
func (r *noRoot) unsubscribe(s *subscription)               {}
func (r *noRoot) flags() (int, error)                       { return syscall.O_RDONLY, nil }
func (r *noRoot) setFlags(fl int) error                     { return nil }
func (r *noRoot) readdir(p *Process, b []byte) (int, error) { return 0, nil }
func (r *noRoot) path(p *Process) (string, error)           { return "/", nil }

// itself tells whether a path names the root itself: none but dots and
// slashes, ".", "..", "/". Anything else is not there.
func (r *noRoot) itself(path string) bool {
	for _, name := range strings.Split(path, "/") {
		if name != "" && name != "." && name != ".." {
			return false
		}
	}

	return true
}

// gone is the error of a call on a path: the root's own, or ENOENT for
// what is not there.
func (r *noRoot) gone(path string, own error) error {
	if r.itself(path) {
		return own
	}

	return syscall.ENOENT
}

func (r *noRoot) openPath(p *Process, path string, flags int, mode uint32) (Object, error) {
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

func (r *noRoot) statPath(p *Process, path string, follow bool, st *syscall.Stat_t) error {
	if !r.itself(path) {
		return syscall.ENOENT
	}

	return r.stat(p, st)
}

func (r *noRoot) readlinkPath(p *Process, path string) (string, error) {
	return "", r.gone(path, syscall.EINVAL)
}

// Nothing is made in the root: what names it is there, EEXIST.
func (r *noRoot) mkdirPath(p *Process, path string, mode uint32) error {
	return r.makes(path)
}

func (r *noRoot) mknodPath(p *Process, path string, mode uint32) error {
	return r.makes(path)
}

func (r *noRoot) symlinkPath(p *Process, target, path string) error {
	return r.makes(path)
}

func (r *noRoot) makes(path string) error {
	if r.itself(path) {
		return syscall.EEXIST
	}

	return syscall.EROFS
}

func (r *noRoot) unlinkPath(p *Process, path string, dir bool) error {
	if dir {
		return r.gone(path, syscall.EBUSY)
	}

	return r.gone(path, syscall.EISDIR)
}

func (r *noRoot) renamePath(p *Process, old string, to Dir, new string) error {
	return r.gone(old, syscall.EBUSY)
}

func (r *noRoot) linkPath(p *Process, old string, to Dir, new string, follow bool) error {
	return r.gone(old, syscall.EPERM)
}

func (r *noRoot) chmodPath(p *Process, path string, mode uint32, follow bool) error {
	return r.gone(path, syscall.EROFS)
}

func (r *noRoot) chownPath(p *Process, path string, uid, gid int, follow bool) error {
	return r.gone(path, syscall.EROFS)
}

func (r *noRoot) utimesPath(p *Process, path string, ts []syscall.Timespec, follow bool) error {
	return r.gone(path, syscall.EROFS)
}

func (r *noRoot) truncatePath(p *Process, path string, size int64) error {
	return r.gone(path, syscall.EISDIR)
}

func (r *noRoot) statfsPath(p *Process, path string, st *syscall.Statfs_t) error {
	if !r.itself(path) {
		return syscall.ENOENT
	}

	*st = syscall.Statfs_t{Bsize: 4096, Frsize: 4096, Namelen: 255}

	return nil
}

func (r *noRoot) realpathOf(p *Process, path string) (string, error) {
	return "/", r.gone(path, nil)
}

// No file system is mounted on the root, and no socket is in it.
func (r *noRoot) mountPath(p *Process, target, source string, conn Object) error {
	return syscall.EINVAL
}

func (r *noRoot) unmountPath(p *Process, path string, flags uint32) error {
	return syscall.EINVAL
}

func (r *noRoot) bindPath(p *Process, path string, mode uint32, held Object) error {
	return syscall.EOPNOTSUPP
}

func (r *noRoot) connectPath(p *Process, path string) (Object, error) {
	return nil, syscall.EOPNOTSUPP
}
