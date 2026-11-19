//go:build linux && amd64

package main

import (
	"encoding/binary"
	"syscall"
)

// remoteDir is a handle of a directory of rootfs's, the VFS's, which the
// calls with a path go to whole; id is its, which a call
// names the root by.
type remoteDir struct {
	node remoteNode
	id   uint64
	// opath is set of one O_PATH opened: it takes no locks, EBADF.
	opath bool
}

func (d *remoteDir) ref()   { d.node.ref() }
func (d *remoteDir) unref() { d.node.unref() }

// A remoteDir is a File, a Seeker, a Dir, an Attrs and a Locker, and
// itself to a call that wants the VFS's handle: its node answers but
// the Dir's calls, readdir and seek.
func (d *remoteDir) asFile() File         { return d }
func (d *remoteDir) asTransfer() Transfer { return nil }
func (d *remoteDir) asSeeker() Seeker     { return d }
func (d *remoteDir) asDir() Dir           { return d }
func (d *remoteDir) asTerminal() Terminal { return nil }
func (d *remoteDir) asWindowed() Windowed { return nil }
func (d *remoteDir) asLink() Link         { return nil }
func (d *remoteDir) asAttrs() Attrs       { return d }
func (d *remoteDir) asSocket() Socket     { return nil }
func (d *remoteDir) asIoctl() Ioctl       { return nil }
func (d *remoteDir) asLocker() Locker {
	if d.opath {
		return nil
	}

	return d
}
func (d *remoteDir) asEpoll() *epoll           { return nil }
func (d *remoteDir) asMemfd() *memfdFile       { return nil }
func (d *remoteDir) asPair() *pairEnd          { return nil }
func (d *remoteDir) asListener() *unixListener { return nil }
func (d *remoteDir) asConsole() *consoleEnd    { return nil }
func (d *remoteDir) asVFS() *remoteDir         { return d }
func (d *remoteDir) asCall() *callEnd          { return nil }

func (d *remoteDir) stat(p *Process, st *syscall.Stat_t) error { return d.node.stat(p, st) }
func (d *remoteDir) poll(events int16) int16                   { return d.node.poll(events) }
func (d *remoteDir) subscribe(s *subscription, events int16)   { d.node.subscribe(s, events) }
func (d *remoteDir) unsubscribe(s *subscription)               { d.node.unsubscribe(s) }
func (d *remoteDir) flags() (int, error)                       { return d.node.flags() }
func (d *remoteDir) setFlags(fl int) error                     { return d.node.setFlags(fl) }

func (d *remoteDir) truncate(p *Process, size int64) error          { return d.node.truncate(p, size) }
func (d *remoteDir) sync(p *Process, data bool) error               { return d.node.sync(p, data) }
func (d *remoteDir) chmod(p *Process, mode uint32) error            { return d.node.chmod(p, mode) }
func (d *remoteDir) chown(p *Process, uid, gid int) error           { return d.node.chown(p, uid, gid) }
func (d *remoteDir) utimes(p *Process, ts []syscall.Timespec) error { return d.node.utimes(p, ts) }
func (d *remoteDir) statfs(p *Process, st *syscall.Statfs_t) error  { return d.node.statfs(p, st) }

func (d *remoteDir) flock(p *Process, op int) error { return d.node.flock(p, op) }
func (d *remoteDir) recordLock(p *Process, cmd int, lk *syscall.Flock_t) error {
	return d.node.recordLock(p, cmd, lk)
}

// A directory's readdir and seek; its other calls are the VFS's.

// readdir fills the entries after the last it gave, as many as fit, in
// the guest's struct dirent layout: ino, off, reclen, type, name; an
// entry's off is its position.
func (d *remoteDir) readdir(p *Process, b []byte) (int, error) {
	var after [8]byte
	binary.LittleEndian.PutUint64(after[:], d.node.after)
	pk, err := d.node.on(p, rReaddir, 0, after[:])

	if err != nil {
		return 0, err
	}

	data := pk.data
	n := 0
	le := binary.LittleEndian

	for len(data) >= 17 {
		ino, pos := le.Uint64(data), le.Uint64(data[8:])
		kind := data[16]
		end := 17

		for end < len(data) && data[end] != 0 {
			end++
		}

		name := data[17:end]
		reclen := (19 + len(name) + 1 + 7) &^ 7

		if n+reclen > len(b) {
			break
		}

		clear(b[n : n+reclen])
		le.PutUint64(b[n:], ino)
		le.PutUint64(b[n+8:], pos)
		le.PutUint16(b[n+16:], uint16(reclen))
		b[n+18] = kind
		copy(b[n+19:], name)
		n += reclen
		d.node.after = pos
		data = data[min(end+1, len(data)):]
	}

	return n, nil
}

// seek of a directory is rewinddir.
func (d *remoteDir) seek(p *Process, off int64, whence int) (int64, error) {
	if off != 0 || whence != 0 {
		return 0, syscall.EINVAL
	}

	d.node.after = 0

	return 0, nil
}

func flagOf(follow bool) uint32 {
	if follow {
		return 0
	}

	return atSymlinkNofollow
}

// openPath opens what a path names, as open(2) does: the open file the
// OK brings, an end of a pair its server made and what the OK says it is
// (opened), or a FIFO's pipe, the kernel's; /dev/tty is the caller's
// controlling terminal (process.go).
func (d *remoteDir) openPath(p *Process, path string, flags int, mode uint32) (Object, error) {
	if p.chaosFail("openat") {
		return nil, syscall.EIO
	}

	body := make([]byte, 4, 4+len(path))
	binary.LittleEndian.PutUint32(body, mode)
	pk, err := d.node.vfs(p, d.id, vOpen, uint32(flags), append(body, path...))

	if err != nil {
		return nil, err
	}

	if pk.arg&openControlling != 0 {
		dropFiles(pk.files)

		return p.openControlling(flags)
	}

	if len(pk.files) == 0 {
		// A FIFO, whose stat the OK is: the VFS said who may open it, and
		// its pipe is the kernel's.
		var st syscall.Stat_t

		if err := decodeStat(pk.data, &st); err != nil || st.Mode&syscall.S_IFMT != syscall.S_IFIFO {
			return nil, syscall.EIO
		}

		return p.openFifo(&st, flags)
	}

	f, err := p.opened(&pk)

	if err != nil {
		return nil, err
	}

	if flags&syscall.O_DIRECTORY != 0 && f.asDir() == nil {
		f.unref()

		return nil, syscall.ENOTDIR
	}

	if t := f.asTerminal(); t != nil && flags&syscall.O_NOCTTY == 0 {
		p.acquire(t)
	}

	return f, nil
}

// statPath is the metadata of what a path names, or of a last link
// itself.
func (d *remoteDir) statPath(p *Process, path string, follow bool, st *syscall.Stat_t) error {
	if p.chaosFail("file-stat") {
		return syscall.EIO
	}

	arg := uint32(0)

	if follow {
		arg = 1
	}

	pk, err := d.node.vfs(p, d.id, vStat, arg, []byte(path))

	if err != nil {
		return err
	}

	return decodeStat(pk.data, st)
}

func (d *remoteDir) readlinkPath(p *Process, path string) (string, error) {
	pk, err := d.node.vfs(p, d.id, vReadlink, 0, []byte(path))

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

func (d *remoteDir) mkdirPath(p *Process, path string, mode uint32) error {
	_, err := d.node.vfs(p, d.id, vMkdir, mode, []byte(path))

	return err
}

func (d *remoteDir) mknodPath(p *Process, path string, mode uint32) error {
	_, err := d.node.vfs(p, d.id, vMknod, mode, []byte(path))

	return err
}

func (d *remoteDir) symlinkPath(p *Process, target, path string) error {
	_, err := d.node.vfs(p, d.id, vSymlink, 0, append(append([]byte(target), 0), path...))

	return err
}

func (d *remoteDir) unlinkPath(p *Process, path string, dir bool) error {
	flags := uint32(0)

	if dir {
		flags = atRemovedir
	}

	_, err := d.node.vfs(p, d.id, vUnlink, flags, []byte(path))

	return err
}

// twoPaths is the body of a call on two paths, the second from the
// handle to, rootfs's too: to's id, the old path and the new. Another
// directory's is another server's: EXDEV.
func twoPaths(to Dir, old, new string) ([]byte, error) {
	h := to.asVFS()

	if h == nil {
		return nil, syscall.EXDEV
	}

	body := make([]byte, 8, 8+len(old)+1+len(new))
	binary.LittleEndian.PutUint64(body, h.id)

	return append(append(append(body, old...), 0), new...), nil
}

func (d *remoteDir) renamePath(p *Process, old string, to Dir, new string) error {
	body, err := twoPaths(to, old, new)

	if err == nil {
		_, err = d.node.vfs(p, d.id, vRename, 0, body)
	}

	return err
}

func (d *remoteDir) linkPath(p *Process, old string, to Dir, new string, follow bool) error {
	body, err := twoPaths(to, old, new)
	arg := uint32(0)

	if follow {
		arg = 1
	}

	if err == nil {
		_, err = d.node.vfs(p, d.id, vLink, arg, body)
	}

	return err
}

func (d *remoteDir) chmodPath(p *Process, path string, mode uint32, follow bool) error {
	body := make([]byte, 4, 4+len(path))
	binary.LittleEndian.PutUint32(body, flagOf(follow))
	_, err := d.node.vfs(p, d.id, vChmod, mode, append(body, path...))

	return err
}

func (d *remoteDir) chownPath(p *Process, path string, uid, gid int, follow bool) error {
	body := make([]byte, 12, 12+len(path))
	binary.LittleEndian.PutUint32(body, flagOf(follow))
	binary.LittleEndian.PutUint32(body[4:], uint32(uid))
	binary.LittleEndian.PutUint32(body[8:], uint32(gid))
	_, err := d.node.vfs(p, d.id, vChown, 0, append(body, path...))

	return err
}

func (d *remoteDir) utimesPath(p *Process, path string, ts []syscall.Timespec, follow bool) error {
	when, times := whens(ts)
	body := make([]byte, 4, 4+len(times)+len(path))
	binary.LittleEndian.PutUint32(body, flagOf(follow))
	_, err := d.node.vfs(p, d.id, vUtimes, when, append(append(body, times...), path...))

	return err
}

func (d *remoteDir) truncatePath(p *Process, path string, size int64) error {
	body := make([]byte, 8, 8+len(path))
	binary.LittleEndian.PutUint64(body, uint64(size))
	_, err := d.node.vfs(p, d.id, vTruncate, 0, append(body, path...))

	return err
}

func (d *remoteDir) statfsPath(p *Process, path string, st *syscall.Statfs_t) error {
	pk, err := d.node.vfs(p, d.id, vStatfs, 0, []byte(path))

	if err != nil {
		return err
	}

	return decodeStatfs(pk.data, st)
}

// realpathOf is the path from the root of what a path names, with no
// ".", ".." or link in it.
func (d *remoteDir) realpathOf(p *Process, path string) (string, error) {
	pk, err := d.node.vfs(p, d.id, vRealpath, 0, []byte(path))

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

// path is the handle's path from the root of the process.
func (d *remoteDir) path(p *Process) (string, error) {
	pk, err := d.node.vfs(p, d.id, vPath, 0, nil)

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

// mountPath mounts on the directory target names the directory source
// names, or, with a connection to the file system whose socket source
// names, its root: the connection is its channel from then on.
func (d *remoteDir) mountPath(p *Process, target, source string, conn Object) error {
	body := append(append([]byte(target), 0), source...)

	if conn != nil {
		_, err := d.node.vfs(p, d.id, vMount, 0, body, conn)

		return err
	}

	_, err := d.node.vfs(p, d.id, vMount, 0, body)

	return err
}

// bindPath makes a socket node of the last name of a path, of a mode,
// which holds what the name of a socket stands for: a listener, or the
// mouth of a datagram socket (unix_dgram.go).
func (d *remoteDir) bindPath(p *Process, path string, mode uint32, held Object) error {
	_, err := d.node.vfs(p, d.id, vBind, mode, []byte(path), held)

	return err
}

// connectPath is what the socket node a path names holds.
func (d *remoteDir) connectPath(p *Process, path string) (Object, error) {
	pk, err := d.node.vfs(p, d.id, vConnect, 0, []byte(path))

	if err != nil {
		return nil, err
	}

	if len(pk.files) == 0 {
		return nil, syscall.ECONNREFUSED
	}

	return pk.files[0], nil
}

func (d *remoteDir) unmountPath(p *Process, path string, flags uint32) error {
	_, err := d.node.vfs(p, d.id, vUnmount, flags, []byte(path))

	return err
}

// An OPEN's OK's arg: the open is of the opener's controlling terminal
// (/dev/tty), which the kernel opens.
const openControlling = 4
