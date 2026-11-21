//go:build linux && amd64

package main

import (
	"bytes"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"
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

func (r *NoRoot) flags() (int, error) {
	return O_RDONLY, nil
}

func (r *NoRoot) setFlags(fl int) error {
	return nil
}

func (r *NoRoot) readdir(p *Process, into Object, n int) (int, error) {
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

func named(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}

	return string(b)
}

func after(b []byte, n int) []byte {
	return b[min(n, len(b)):]
}

func (r *NoRoot) call(p *Process, op, arg, group uint32, body []byte, files []Object, interruptible bool) (Packet, bool, error) {
	if op < vOpen || op > vConnect {
		return Packet{}, false, errUnserved
	}

	b := after(body, 8)

	switch op {
	case vOpen:
		path := string(after(b, 4))

		switch flags := int(arg); {
		case !r.itself(path) && flags&O_CREAT != 0:
			return Packet{}, false, EROFS
		case !r.itself(path):
			return Packet{}, false, ENOENT
		case flags&O_CREAT != 0 || flags&3 != O_RDONLY:
			return Packet{}, false, EISDIR
		}

		r.ref()

		return Packet{files: []Object{r}}, false, nil
	case vStat:
		var st Stat

		if !r.itself(string(b)) {
			return Packet{}, false, ENOENT
		}

		r.stat(p, &st)

		return Packet{data: encodeStat(&st)}, false, nil
	case vStatfs:
		if !r.itself(string(b)) {
			return Packet{}, false, ENOENT
		}

		return Packet{data: encodeStatfs(&Statfs{Bsize: 4096, Frsize: 4096, Namelen: 255})}, false, nil
	case vRealpath:
		return Packet{data: []byte("/")}, false, r.gone(string(b), nil)
	case vPath:
		return Packet{data: []byte("/")}, false, nil
	}

	return Packet{}, false, r.refuses(op, arg, b)
}

func (r *NoRoot) refuses(op, arg uint32, b []byte) error {
	switch op {
	case vReadlink:
		return r.gone(string(b), EINVAL)
	case vMkdir, vMknod:
		return r.makes(string(b))
	case vSymlink:
		return r.makes(string(after(b, len(named(b))+1)))
	case vUnlink:
		if arg&AT_REMOVEDIR != 0 {
			return r.gone(string(b), EBUSY)
		}

		return r.gone(string(b), EISDIR)
	case vRename:
		return r.gone(named(after(b, 8)), EBUSY)
	case vLink:
		return r.gone(named(after(b, 8)), EPERM)
	case vChmod:
		return r.gone(string(after(b, 4)), EROFS)
	case vChown:
		return r.gone(string(after(b, 12)), EROFS)
	case vUtimes:
		return r.gone(string(after(b, 20)), EROFS)
	case vTruncate:
		return r.gone(string(after(b, 8)), EISDIR)
	case vMount, vUnmount:
		return EINVAL
	}

	return EOPNOTSUPP
}

func (r *NoRoot) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	return 0, EISDIR
}

func (r *NoRoot) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	return 0, EBADF
}

func (r *NoRoot) pread(p *Process, into Object, n int, off int64) (int, error) {
	return 0, EISDIR
}

func (r *NoRoot) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	return 0, EBADF
}

func (r *NoRoot) seek(p *Process, off int64, whence int) (int64, error) {
	return 0, ESPIPE
}

func (r *NoRoot) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (r *NoRoot) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	return 0, EBADF
}

func (r *NoRoot) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	return 0, EBADF
}

func (r *NoRoot) truncate(p *Process, size int64) error {
	return EINVAL
}

func (r *NoRoot) sync(p *Process, data bool) error {
	return EINVAL
}

func (r *NoRoot) chmod(p *Process, mode uint32) error {
	return EINVAL
}

func (r *NoRoot) chown(p *Process, uid, gid int) error {
	return EINVAL
}

func (r *NoRoot) utimes(p *Process, ts []Timespec) error {
	return EINVAL
}

func (r *NoRoot) statfs(p *Process, st *Statfs) error {
	return EINVAL
}

func (r *NoRoot) flock(p *Process, op int) error {
	return EBADF
}

func (r *NoRoot) recordLock(p *Process, cmd int, lk *Flock) error {
	return EBADF
}

func (r *NoRoot) token() (uint64, error) {
	return 0, EXDEV
}

func (r *NoRoot) device() FileKey {
	return FileKey{}
}

func (r *NoRoot) bind(p *Process, a Address) error {
	return ENOTSOCK
}

func (r *NoRoot) connect(p *Process, a Address) error {
	return ENOTSOCK
}

func (r *NoRoot) listen(backlog int) error {
	return ENOTSOCK
}

func (r *NoRoot) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, ENOTSOCK
}

func (r *NoRoot) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	return nil, ECONNREFUSED
}

func (r *NoRoot) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	return 0, ENOTSOCK
}

func (r *NoRoot) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	return 0, Packet{}, 0, ENOTSOCK
}

func (r *NoRoot) sockname(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (r *NoRoot) peername(p *Process) ([]byte, error) {
	return nil, ENOTSOCK
}

func (r *NoRoot) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return nil, ENOTSOCK
}

func (r *NoRoot) setsockopt(level, name uint32, value []byte) error {
	return ENOTSOCK
}

func (r *NoRoot) shutdown(how int) error {
	return ENOTSOCK
}

func (r *NoRoot) consume(sink func(pk *Packet)) {
}

func (r *NoRoot) socketKind() int {
	return 0
}

func (r *NoRoot) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (r *NoRoot) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (r *NoRoot) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
