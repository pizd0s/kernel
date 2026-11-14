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
// every call. What a call opens is the file system's endpoint, which the
// kernel then reads and writes itself (kernel_remote.go), or another
// handle.
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

// handleID is the handle's id, asked once: an open or a chdir gave it,
// else the handle's FSTAT says it, which rootfs answers itself for a
// handle of its own trees.
func (d *remoteDir) handleID() (uint64, error) {
	if id := d.id.Load(); id != 0 {
		return id, nil
	}

	pk, err := d.on(rFstat, 0, nil)

	if err != nil {
		return 0, err
	}

	if len(pk.data) < remoteStat+8 {
		return 0, syscall.EIO
	}

	id := binary.LittleEndian.Uint64(pk.data[remoteStat:])
	d.id.Store(id)

	return id, nil
}

// rootID is the id of the process's root, or of the handle a call starts
// at for a process whose root is no handle: rootfs's own children, before
// they chroot.
func (p *Process) rootID(d *remoteDir) (uint64, error) {
	if root, ok := p.root.(*remoteDir); ok {
		return root.handleID()
	}

	return d.handleID()
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
// the files with it. The reply's packet and its endpoint come back, which
// an open keeps.
func (d *remoteDir) vfs(p *Process, op, arg uint32, body []byte, files ...File) (packet, *pairEnd, error) {
	root, err := p.rootID(d)

	if err != nil {
		return packet{}, nil, err
	}

	b := make([]byte, 8, 8+len(body))
	binary.LittleEndian.PutUint64(b, root)

	return d.call(op, arg, append(b, body...), files)
}

// vfsDone is a call of the VFS's that opens nothing: its reply's packet.
func (d *remoteDir) vfsDone(p *Process, op, arg uint32, body []byte, files ...File) (packet, error) {
	pk, mine, err := d.vfs(p, op, arg, body, files...)

	if err != nil {
		return packet{}, err
	}

	mine.unref()

	return pk, nil
}

func flagOf(follow bool) uint32 {
	if follow {
		return 0
	}

	return atSymlinkNofollow
}

// openPath opens what a path names, as open(2) does: a file system's open
// file, an object of the kernel, a FIFO's pipe, or a handle.
func (d *remoteDir) openPath(p *Process, path string, flags int, mode uint32) (File, error) {
	if chaosFail("openat") {
		return nil, syscall.EIO
	}

	body := make([]byte, 4, 4+len(path))
	binary.LittleEndian.PutUint32(body, mode)
	pk, mine, err := d.vfs(p, vOpen, uint32(flags), append(body, path...))

	if err != nil {
		return nil, err
	}

	if len(pk.files) > 0 {
		// The file itself: an object of the kernel the server hands out.
		mine.unref()

		return pk.files[0], nil
	}

	var st syscall.Stat_t

	if err := decodeStat(pk.data, &st); err != nil {
		mine.unref()

		return nil, err
	}

	node := remoteNode{end: mine, accmode: flags & 3, append: flags&syscall.O_APPEND != 0}
	node.refs.Store(1)

	switch {
	case len(pk.data) >= remoteStat+8:
		dir := &remoteDir{remoteNode: node, mode: st.Mode}
		dir.dev.Store(st.Dev)
		dir.ino.Store(st.Ino)
		dir.known.Store(true)
		dir.id.Store(binary.LittleEndian.Uint64(pk.data[remoteStat:]))

		return dir, nil
	case flags&syscall.O_DIRECTORY != 0:
		mine.unref()

		return nil, syscall.ENOTDIR
	case st.Mode&syscall.S_IFMT == syscall.S_IFIFO && flags&oPath == 0:
		// The VFS said who may open it; its pipe is the kernel's.
		mine.unref()

		return p.openFifo(&st, flags)
	}

	return &remoteFile{node}, nil
}

// statPath is the metadata of what a path names, or of a last link
// itself.
func (d *remoteDir) statPath(p *Process, path string, follow bool, st *syscall.Stat_t) error {
	arg := uint32(0)

	if follow {
		arg = 1
	}

	pk, err := d.vfsDone(p, vStat, arg, []byte(path))

	if err != nil {
		return err
	}

	return decodeStat(pk.data, st)
}

func (d *remoteDir) readlinkPath(p *Process, path string) (string, error) {
	pk, err := d.vfsDone(p, vReadlink, 0, []byte(path))

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

// makePath makes the last name of a path: MKDIR and MKNOD of a mode,
// SYMLINK of a target.
func (d *remoteDir) makePath(p *Process, op, mode uint32, target, path string) error {
	body := []byte(path)

	if op == vSymlink {
		body = append(append([]byte(target), 0), path...)
	}

	_, err := d.vfsDone(p, op, mode, body)

	return err
}

func (d *remoteDir) unlinkPath(p *Process, path string, dir bool) error {
	flags := uint32(0)

	if dir {
		flags = atRemovedir
	}

	_, err := d.vfsDone(p, vUnlink, flags, []byte(path))

	return err
}

// twoPaths is the body of a call on two paths, the second from the
// handle to: to's id, the old path and the new.
func twoPaths(to *remoteDir, old, new string) ([]byte, error) {
	id, err := to.handleID()

	if err != nil {
		return nil, err
	}

	body := make([]byte, 8, 8+len(old)+1+len(new))
	binary.LittleEndian.PutUint64(body, id)

	return append(append(append(body, old...), 0), new...), nil
}

func (d *remoteDir) renamePath(p *Process, old string, to *remoteDir, new string) error {
	body, err := twoPaths(to, old, new)

	if err != nil {
		return err
	}

	_, err = d.vfsDone(p, vRename, 0, body)

	return err
}

func (d *remoteDir) linkPath(p *Process, old string, to *remoteDir, new string, follow bool) error {
	body, err := twoPaths(to, old, new)

	if err != nil {
		return err
	}

	arg := uint32(0)

	if follow {
		arg = 1
	}

	_, err = d.vfsDone(p, vLink, arg, body)

	return err
}

func (d *remoteDir) chmodPath(p *Process, path string, mode uint32, follow bool) error {
	body := make([]byte, 4, 4+len(path))
	binary.LittleEndian.PutUint32(body, flagOf(follow))
	_, err := d.vfsDone(p, vChmod, mode, append(body, path...))

	return err
}

func (d *remoteDir) chownPath(p *Process, path string, uid, gid int, follow bool) error {
	body := make([]byte, 12, 12+len(path))
	binary.LittleEndian.PutUint32(body, flagOf(follow))
	binary.LittleEndian.PutUint32(body[4:], uint32(uid))
	binary.LittleEndian.PutUint32(body[8:], uint32(gid))
	_, err := d.vfsDone(p, vChown, 0, append(body, path...))

	return err
}

func (d *remoteDir) utimesPath(p *Process, path string, ts []syscall.Timespec, follow bool) error {
	when, times := whens(ts)
	body := make([]byte, 4, 4+len(times)+len(path))
	binary.LittleEndian.PutUint32(body, flagOf(follow))
	_, err := d.vfsDone(p, vUtimes, when, append(append(body, times...), path...))

	return err
}

func (d *remoteDir) truncatePath(p *Process, path string, size int64) error {
	body := make([]byte, 8, 8+len(path))
	binary.LittleEndian.PutUint64(body, uint64(size))
	_, err := d.vfsDone(p, vTruncate, 0, append(body, path...))

	return err
}

func (d *remoteDir) statfsPath(p *Process, path string, st *syscall.Statfs_t) error {
	pk, err := d.vfsDone(p, vStatfs, 0, []byte(path))

	if err != nil {
		return err
	}

	return decodeStatfs(pk.data, st)
}

// realpathOf is the path from the root of what a path names, with no
// ".", ".." or link in it.
func (d *remoteDir) realpathOf(p *Process, path string) (string, error) {
	pk, err := d.vfsDone(p, vRealpath, 0, []byte(path))

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

// path is the handle's path from the root of the process.
func (d *remoteDir) path(p *Process) (string, error) {
	pk, err := d.vfsDone(p, vPath, 0, nil)

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

// mountPath mounts on the directory target names the directory source
// names, or, with a connection to the file system whose socket source
// names, its root: the connection is its channel from then on.
func (d *remoteDir) mountPath(p *Process, target, source string, conn *pairEnd) error {
	body := append(append([]byte(target), 0), source...)

	if conn != nil {
		_, err := d.vfsDone(p, vMount, 0, body, conn)

		return err
	}

	_, err := d.vfsDone(p, vMount, 0, body)

	return err
}

// bindPath makes a socket node of the last name of a path, of a mode,
// which holds a listener.
func (d *remoteDir) bindPath(p *Process, path string, mode uint32, l *unixListener) error {
	_, err := d.vfsDone(p, vBind, mode, []byte(path), l)

	return err
}

// connectPath is the listener the socket node a path names holds.
func (d *remoteDir) connectPath(p *Process, path string) (File, error) {
	pk, mine, err := d.vfs(p, vConnect, 0, []byte(path))

	if err != nil {
		return nil, err
	}

	mine.unref()

	if len(pk.files) == 0 {
		return nil, syscall.ECONNREFUSED
	}

	return pk.files[0], nil
}

func (d *remoteDir) unmountPath(p *Process, path string, flags uint32) error {
	_, err := d.vfsDone(p, vUnmount, flags, []byte(path))

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

	if !permits(&st, mode&7) {
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
