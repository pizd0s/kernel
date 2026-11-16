//go:build linux && amd64

package main

import (
	"encoding/binary"
	"syscall"
)

// The VFS's calls (lib/protocol/protocol.h). A process of the system has
// rootfs's handles for its root, its working directory and its directory
// descriptors, and a call with a path goes to rootfs whole, on the handle
// it starts at: the root for an absolute path, else the working directory
// or the directory of a descriptor. rootfs is the VFS: it walks the path,
// follows the links, crosses the mounts, stops ".." at the root and makes
// the last step as Linux makes it; the kernel walks nothing of the
// system's. The id of the process's root, the handle's, goes first in
// every call. What a call opens is the open file its server made of a
// served pair, as it is: a file system's file, which the kernel then
// reads and writes itself (kernel_remote.go), or another handle.
const (
	vOpen uint32 = iota + 1
	vStat
	vReadlink
	vMkdir
	vMknod
	vSymlink
	vUnlink
	vRename
	vLink
	vChmod
	vChown
	vUtimes
	vTruncate
	vStatfs
	vRealpath
	vMount
	vUnmount
	vPath
	vBind
	vConnect
)

// rootID is the id of the process's root, or of the handle a call starts
// at for a process whose root is no handle: rootfs's own children, before
// they chroot.
func (p *Process) rootID(d *remoteHandle) uint64 {
	if root, ok := p.root.(*remoteDir); ok {
		return root.id
	}

	return d.id
}

// vfsAt is the handle a path relative to dirfd starts at when it is one
// of rootfs's, with a reference of the caller's, or nil for a directory
// of the host's.
func (p *Process) vfsAt(dirfd int32, path string) (*remoteDir, error) {
	d, err := p.start(dirfd, path)

	if err != nil {
		return nil, err
	}

	if v, ok := d.(*remoteDir); ok {
		return v, nil
	}

	d.unref()

	return nil, nil
}

// vfs is a call of the VFS's on the handle: the root's id, then the body,
// the files with it; the reply's packet.
func (d *remoteHandle) vfs(p *Process, op, arg uint32, body []byte, files ...File) (packet, error) {
	b := make([]byte, 8, 8+len(body))
	binary.LittleEndian.PutUint64(b, p.rootID(d))

	return d.call(p, op, arg, append(b, body...), files)
}

func flagOf(follow bool) uint32 {
	if follow {
		return 0
	}

	return atSymlinkNofollow
}

// openPath opens what a path names, as open(2) does: the open file its
// server made of a served pair, which the OK brings and the kernel takes
// as it is, or a FIFO's pipe, the kernel's; /dev/tty is the caller's
// controlling terminal (kernel_terminal.go).
func (d *remoteHandle) openPath(p *Process, path string, flags int, mode uint32) (File, error) {
	if p.chaosFail("openat") {
		return nil, syscall.EIO
	}

	body := make([]byte, 4, 4+len(path))
	binary.LittleEndian.PutUint32(body, mode)
	pk, err := d.vfs(p, vOpen, uint32(flags), append(body, path...))

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

	f := pk.files[0]

	if _, dir := f.(Dir); flags&syscall.O_DIRECTORY != 0 && !dir {
		f.unref()

		return nil, syscall.ENOTDIR
	}

	if t, ok := f.(*remoteTerminal); ok && flags&syscall.O_NOCTTY == 0 {
		p.acquire(t)
	}

	return f, nil
}

// statPath is the metadata of what a path names, or of a last link
// itself.
func (d *remoteHandle) statPath(p *Process, path string, follow bool, st *syscall.Stat_t) error {
	if p.chaosFail("file-stat") {
		return syscall.EIO
	}

	arg := uint32(0)

	if follow {
		arg = 1
	}

	pk, err := d.vfs(p, vStat, arg, []byte(path))

	if err != nil {
		return err
	}

	return decodeStat(pk.data, st)
}

func (d *remoteHandle) readlinkPath(p *Process, path string) (string, error) {
	pk, err := d.vfs(p, vReadlink, 0, []byte(path))

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

// makePath makes the last name of a path: MKDIR and MKNOD of a mode,
// SYMLINK of a target.
func (d *remoteHandle) makePath(p *Process, op, mode uint32, target, path string) error {
	body := []byte(path)

	if op == vSymlink {
		body = append(append([]byte(target), 0), path...)
	}

	_, err := d.vfs(p, op, mode, body)

	return err
}

func (d *remoteHandle) unlinkPath(p *Process, path string, dir bool) error {
	flags := uint32(0)

	if dir {
		flags = atRemovedir
	}

	_, err := d.vfs(p, vUnlink, flags, []byte(path))

	return err
}

// twoPaths is the body of a call on two paths, the second from the
// handle to: to's id, the old path and the new.
func twoPaths(to *remoteDir, old, new string) []byte {
	body := make([]byte, 8, 8+len(old)+1+len(new))
	binary.LittleEndian.PutUint64(body, to.id)

	return append(append(append(body, old...), 0), new...)
}

func (d *remoteHandle) renamePath(p *Process, old string, to *remoteDir, new string) error {
	_, err := d.vfs(p, vRename, 0, twoPaths(to, old, new))

	return err
}

func (d *remoteHandle) linkPath(p *Process, old string, to *remoteDir, new string, follow bool) error {
	body := twoPaths(to, old, new)
	arg := uint32(0)

	if follow {
		arg = 1
	}

	_, err := d.vfs(p, vLink, arg, body)

	return err
}

func (d *remoteHandle) chmodPath(p *Process, path string, mode uint32, follow bool) error {
	body := make([]byte, 4, 4+len(path))
	binary.LittleEndian.PutUint32(body, flagOf(follow))
	_, err := d.vfs(p, vChmod, mode, append(body, path...))

	return err
}

func (d *remoteHandle) chownPath(p *Process, path string, uid, gid int, follow bool) error {
	body := make([]byte, 12, 12+len(path))
	binary.LittleEndian.PutUint32(body, flagOf(follow))
	binary.LittleEndian.PutUint32(body[4:], uint32(uid))
	binary.LittleEndian.PutUint32(body[8:], uint32(gid))
	_, err := d.vfs(p, vChown, 0, append(body, path...))

	return err
}

func (d *remoteHandle) utimesPath(p *Process, path string, ts []syscall.Timespec, follow bool) error {
	when, times := whens(ts)
	body := make([]byte, 4, 4+len(times)+len(path))
	binary.LittleEndian.PutUint32(body, flagOf(follow))
	_, err := d.vfs(p, vUtimes, when, append(append(body, times...), path...))

	return err
}

func (d *remoteHandle) truncatePath(p *Process, path string, size int64) error {
	body := make([]byte, 8, 8+len(path))
	binary.LittleEndian.PutUint64(body, uint64(size))
	_, err := d.vfs(p, vTruncate, 0, append(body, path...))

	return err
}

func (d *remoteHandle) statfsPath(p *Process, path string, st *syscall.Statfs_t) error {
	pk, err := d.vfs(p, vStatfs, 0, []byte(path))

	if err != nil {
		return err
	}

	return decodeStatfs(pk.data, st)
}

// realpathOf is the path from the root of what a path names, with no
// ".", ".." or link in it.
func (d *remoteHandle) realpathOf(p *Process, path string) (string, error) {
	pk, err := d.vfs(p, vRealpath, 0, []byte(path))

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

// path is the handle's path from the root of the process.
func (d *remoteHandle) path(p *Process) (string, error) {
	pk, err := d.vfs(p, vPath, 0, nil)

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

// mountPath mounts on the directory target names the directory source
// names, or, with a connection to the file system whose socket source
// names, its root: the connection is its channel from then on.
func (d *remoteHandle) mountPath(p *Process, target, source string, conn *pairEnd) error {
	body := append(append([]byte(target), 0), source...)

	if conn != nil {
		_, err := d.vfs(p, vMount, 0, body, conn)

		return err
	}

	_, err := d.vfs(p, vMount, 0, body)

	return err
}

// bindPath makes a socket node of the last name of a path, of a mode,
// which holds what the name of a socket stands for: a listener, or the
// mouth of a datagram socket (kernel_unix.go).
func (d *remoteHandle) bindPath(p *Process, path string, mode uint32, held File) error {
	_, err := d.vfs(p, vBind, mode, []byte(path), held)

	return err
}

// connectPath is what the socket node a path names holds.
func (d *remoteHandle) connectPath(p *Process, path string) (File, error) {
	pk, err := d.vfs(p, vConnect, 0, []byte(path))

	if err != nil {
		return nil, err
	}

	if len(pk.files) == 0 {
		return nil, syscall.ECONNREFUSED
	}

	return pk.files[0], nil
}

func (d *remoteHandle) unmountPath(p *Process, path string, flags uint32) error {
	_, err := d.vfs(p, vUnmount, flags, []byte(path))

	return err
}

// The Dir role of a handle: the kernel walks nothing of the VFS's, and a
// name of a handle is a path of one name from it, the VFS's to walk.

func (d *remoteDir) resolve(p *Process, path string, w *walk) (step, error) {
	return step{}, syscall.EIO // the VFS walks its paths, never the kernel
}

func (d *remoteDir) statName(p *Process, name string, st *syscall.Stat_t) error {
	return d.statPath(p, name, false, st)
}

func (d *remoteDir) readlinkName(p *Process, name string) (string, error) {
	return d.readlinkPath(p, name)
}

func (d *remoteDir) openName(p *Process, name string, flags int, mode uint32) (File, error) {
	return d.openPath(p, name, flags, mode)
}

func (d *remoteDir) mkdir(p *Process, name string, mode uint32) error {
	return d.makePath(p, vMkdir, mode, "", name)
}

func (d *remoteDir) mknod(p *Process, name string, mode uint32, dev uint64) error {
	return d.makePath(p, vMknod, mode, "", name)
}

func (d *remoteDir) symlink(p *Process, target, name string) error {
	return d.makePath(p, vSymlink, 0, target, name)
}

func (d *remoteDir) unlink(p *Process, name string, dir bool) error {
	return d.unlinkPath(p, name, dir)
}

func (d *remoteDir) rename(p *Process, name string, to Dir, toName string) error {
	other, ok := to.(*remoteDir)

	if !ok {
		return syscall.EXDEV
	}

	return d.renamePath(p, name, other, toName)
}

func (d *remoteDir) link(p *Process, name string, to Dir, toName string) error {
	other, ok := to.(*remoteDir)

	if !ok {
		return syscall.EXDEV
	}

	return d.linkPath(p, name, other, toName, false)
}

// accessName is what a name of the handle, "" the handle itself, lets the
// process do: the ids are vm's, real and effective alike.
func (d *remoteDir) accessName(p *Process, name string, mode, flags uint32) error {
	var st syscall.Stat_t

	if err := d.statPath(p, name, flags&atSymlinkNofollow == 0, &st); err != nil {
		return err
	}

	if !p.permits(&st, mode&7) {
		return syscall.EACCES
	}

	return nil
}

func (d *remoteDir) chmodName(p *Process, name string, mode uint32) error {
	return d.chmodPath(p, name, mode, false)
}

func (d *remoteDir) chownName(p *Process, name string, uid, gid int) error {
	return d.chownPath(p, name, uid, gid, false)
}

func (d *remoteDir) utimesName(p *Process, name string, ts []syscall.Timespec) error {
	return d.utimesPath(p, name, ts, false)
}

func (d *remoteDir) truncateName(p *Process, name string, size int64) error {
	return d.truncatePath(p, name, size)
}

func (d *remoteDir) statfsName(p *Process, name string, st *syscall.Statfs_t) error {
	return d.statfsPath(p, name, st)
}
