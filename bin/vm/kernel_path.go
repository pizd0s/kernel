//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"strings"
	"syscall"
)

// Paths are the guest's. A path starts at the process's root when it is
// absolute, else at the working directory or at the directory of a
// directory descriptor: directories the process holds, not names. The
// kernel walks none: a call goes to the directory it starts at with the
// path whole (Dir), and what serves the directory walks it, follows its
// links and makes the last step as Linux's namei and the call make it.

// checked is a path a call takes: none is ENOENT, one of PATH_MAX or more
// ENAMETOOLONG.
func checked(path string) error {
	switch {
	case path == "":
		return syscall.ENOENT
	case len(path) >= 4096:
		return syscall.ENAMETOOLONG
	}

	return nil
}

const (
	oPath             = 0o10000000
	atFdcwd           = -100
	atSymlinkNofollow = 0x100
	atRemovedir       = 0x200
	atEaccess         = 0x200
	atSymlinkFollow   = 0x400
	atEmptyPath       = 0x1000
	sIfifo            = 0o10000
)

// onPath runs call on the directory a path relative to dirfd starts at,
// which walks it: none, or one of PATH_MAX or more, is the path's error.
func (p *Process) onPath(dirfd int32, path string, call func(d Dir) error) error {
	if err := checked(path); err != nil {
		return err
	}

	d, err := p.start(dirfd, path)

	if err != nil {
		return err
	}

	defer d.unref()

	return call(d)
}

// onPaths runs call on the directories two paths start at.
func (p *Process) onPaths(olddir int32, old string, newdir int32, new string, call func(from, to Dir) error) error {
	return p.onPath(olddir, old, func(from Dir) error {
		return p.onPath(newdir, new, func(to Dir) error {
			return call(from, to)
		})
	})
}

// start is the directory a path starts at, which the caller holds a
// reference to: the root for an absolute one, else the working directory
// or the directory of dirfd.
func (p *Process) start(dirfd int32, path string) (Dir, error) {
	d := p.root

	if !strings.HasPrefix(path, "/") {
		d = p.cwd

		if dirfd != atFdcwd {
			var err error

			if d, err = p.dirAt(uint32(dirfd)); err != nil {
				return nil, err
			}
		}
	}

	d.ref()

	return d, nil
}

// onAt runs a call on what an *at call names: byPath on the path, a last
// link followed unless AT_SYMLINK_NOFOLLOW, or byFile on the descriptor's
// own file when the path is empty with AT_EMPTY_PATH.
func (p *Process) onAt(dirfd int32, path string, flags uint32, byFile func(f Object) error, byPath func(d Dir, follow bool) error) error {
	if path == "" && flags&atEmptyPath != 0 {
		if dirfd == atFdcwd {
			return byFile(p.cwd)
		}

		obj, err := p.fd(uint32(dirfd))

		if err != nil {
			return err
		}

		return byFile(obj)
	}

	return p.onPath(dirfd, path, func(d Dir) error {
		return byPath(d, flags&atSymlinkNofollow == 0)
	})
}

// attrsCall is a call on a descriptor's attributes; a file without any
// has no such call: EINVAL.
func attrsCall(call func(a Attrs) error) func(f Object) error {
	return func(f Object) error {
		if a := f.asAttrs(); a != nil {
			return call(a)
		}

		return syscall.EINVAL
	}
}

// openAt opens what a path relative to dirfd names, as open(2) does.
func (p *Process) openAt(dirfd int32, path string, flags int, mode uint32) (Object, error) {
	var f Object
	err := p.onPath(dirfd, path, func(d Dir) error {
		var err error
		f, err = d.openPath(p, path, flags, mode)

		return err
	})

	return f, err
}

// open opens a guest path relative to dirfd and installs the file at the
// lowest descriptor.
func (p *Process) open(dirfd int32, path string, flags, va uint32) (int, error) {
	mode := uint32(0)

	if flags&(syscall.O_CREAT|0o20000000) != 0 {
		mode = p.varArg(va, 0) & 0o7777 &^ p.umask
	}

	if err := p.descriptorFree(); err != nil {
		return 0, err
	}

	f, err := p.openAt(dirfd, path, int(flags&^syscall.O_CLOEXEC), mode)

	if err != nil {
		return 0, err
	}

	return p.install(f, 0, flags&syscall.O_CLOEXEC != 0)
}

// openPath opens a guest path.
func (p *Process) openPath(path string, flags int, mode uint32) (Object, error) {
	return p.openAt(atFdcwd, path, flags, mode)
}

// readPath reads a file of the root whole, up to max bytes: the kernel's
// own reads of the databases and tables under the root. The error is the
// open's; what cannot be read, a directory, is read as empty, as musl
// reads it through fopen.
func (p *Process) readPath(path string, max int) ([]byte, error) {
	f, err := p.openPath(path, syscall.O_RDONLY, 0)

	if err != nil {
		return nil, err
	}

	defer f.unref()
	t := f.asTransfer()

	if t == nil {
		return nil, nil
	}

	var data []byte
	buf := make([]byte, 65536)

	for len(data) < max {
		n, err := t.read(p, buf, ioMode{})

		if err != nil || n == 0 {
			break
		}

		data = append(data, buf[:n]...)
	}

	return data, nil
}

func (p *Process) mkdir(dirfd int32, path string, mode uint32) error {
	return p.onPath(dirfd, path, func(d Dir) error {
		return d.mkdirPath(p, path, mode&0o7777&^p.umask)
	})
}

// mknod makes a FIFO or a regular file; a device is none the system
// makes, EPERM.
func (p *Process) mknod(dirfd int32, path string, mode uint32, dev uint64) error {
	switch mode & syscall.S_IFMT {
	case 0, syscall.S_IFREG, syscall.S_IFIFO:
	default:
		return syscall.EPERM
	}

	return p.onPath(dirfd, path, func(d Dir) error {
		return d.mknodPath(p, path, mode&^(p.umask&0o777))
	})
}

func (p *Process) symlink(target string, dirfd int32, path string) error {
	if target == "" {
		return syscall.ENOENT
	}

	return p.onPath(dirfd, path, func(d Dir) error {
		return d.symlinkPath(p, target, path)
	})
}

// remove takes the last name of a path relative to dirfd away, a
// directory's with dir.
func (p *Process) remove(dirfd int32, path string, dir bool) error {
	return p.onPath(dirfd, path, func(d Dir) error {
		return d.unlinkPath(p, path, dir)
	})
}

// rename moves the last name of one path to the last name of another.
func (p *Process) rename(olddir int32, old string, newdir int32, new string) error {
	return p.onPaths(olddir, old, newdir, new, func(from, to Dir) error {
		return from.renamePath(p, old, to, new)
	})
}

// link makes the last name of a new path a name of what an old one
// names, a last link of it followed when follow says.
func (p *Process) link(olddir int32, old string, newdir int32, new string, follow bool) error {
	return p.onPaths(olddir, old, newdir, new, func(from, to Dir) error {
		return from.linkPath(p, old, to, new, follow)
	})
}

// statPath is the metadata of what a path names, or of a last symbolic
// link itself.
func (p *Process) statPath(path string, follow bool, st *syscall.Stat_t) error {
	return p.onPath(atFdcwd, path, func(d Dir) error {
		return d.statPath(p, path, follow, st)
	})
}

// accessAt checks what a path names for mode, as access does, or with
// AT_SYMLINK_NOFOLLOW a last link itself.
func (p *Process) accessAt(dirfd int32, path string, mode, flags uint32) error {
	return p.onPath(dirfd, path, func(d Dir) error {
		return p.access(d, path, mode, flags)
	})
}

// access is what a path from d lets the process do, by its stat: the ids
// are vm's, real and effective alike.
func (p *Process) access(d Dir, path string, mode, flags uint32) error {
	var st syscall.Stat_t

	if err := d.statPath(p, path, flags&atSymlinkNofollow == 0, &st); err != nil {
		return err
	}

	if !p.permits(&st, mode&7) {
		return syscall.EACCES
	}

	return nil
}

// putStatvfs writes a guest struct statvfs.
func (p *Process) putStatvfs(at uint32, st *syscall.Statfs_t) {
	b := p.span(at, 72)
	clear(b)
	le := binary.LittleEndian
	// Linux fs/statfs.c already substitutes f_bsize for a zero f_frsize.
	le.PutUint32(b, uint32(st.Bsize))
	le.PutUint32(b[4:], uint32(st.Frsize))
	le.PutUint64(b[8:], st.Blocks)
	le.PutUint64(b[16:], st.Bfree)
	le.PutUint64(b[24:], st.Bavail)
	le.PutUint64(b[32:], st.Files)
	le.PutUint64(b[40:], st.Ffree)
	le.PutUint64(b[48:], st.Ffree)
	le.PutUint32(b[56:], uint32(st.Fsid.X__val[0]))
	le.PutUint32(b[60:], uint32(st.Flags))
	le.PutUint32(b[64:], uint32(st.Namelen))
}

// putStatfsOf writes a guest struct statfs.
func (p *Process) putStatfsOf(at uint32, st *syscall.Statfs_t) {
	b := p.span(at, 88)
	clear(b)
	le := binary.LittleEndian
	le.PutUint32(b, uint32(st.Type))
	le.PutUint32(b[4:], uint32(st.Bsize))
	le.PutUint64(b[8:], st.Blocks)
	le.PutUint64(b[16:], st.Bfree)
	le.PutUint64(b[24:], st.Bavail)
	le.PutUint64(b[32:], st.Files)
	le.PutUint64(b[40:], st.Ffree)
	le.PutUint32(b[48:], uint32(st.Fsid.X__val[0]))
	le.PutUint32(b[52:], uint32(st.Fsid.X__val[1]))
	le.PutUint32(b[56:], uint32(st.Namelen))
	le.PutUint32(b[60:], uint32(st.Frsize))
	le.PutUint32(b[64:], uint32(st.Flags))
}

func (p *Process) statfsPath(path string, st *syscall.Statfs_t) error {
	return p.onPath(atFdcwd, path, func(d Dir) error {
		return d.statfsPath(p, path, st)
	})
}

func (p *Process) libcOpen(path, flags, va uint32) uint32 {
	return p.result(p.open(atFdcwd, p.str(path), flags, va))
}

func (p *Process) libcOpenat(dirfd, path, flags, va uint32) uint32 {
	return p.result(p.open(int32(dirfd), p.str(path), flags, va))
}

func (p *Process) libcFstatat(dirfd, path, at, flags uint32) uint32 {
	var st syscall.Stat_t
	err := p.statFlags(int32(dirfd), p.str(path), flags, &st)

	if err == nil {
		p.putStat(at, &st)
	}

	return p.result(0, err)
}

// statFlags is fstatat's stat of a path relative to dirfd, with its flags;
// statAt follows the last link with follow, as WASI's path_filestat_get
// does (kernel_wasi.go).
func (p *Process) statFlags(dirfd int32, path string, flags uint32, st *syscall.Stat_t) error {
	return p.onAt(dirfd, path, flags, func(f Object) error {
		return statFile(p, f, st)
	}, func(d Dir, follow bool) error {
		return d.statPath(p, path, follow, st)
	})
}

func (p *Process) statAt(dirfd int32, path string, follow bool, st *syscall.Stat_t) error {
	if follow {
		return p.statFlags(dirfd, path, 0, st)
	}

	return p.statFlags(dirfd, path, atSymlinkNofollow, st)
}

func (p *Process) libcAccess(path, mode uint32) uint32 {
	return p.result(0, p.accessAt(atFdcwd, p.str(path), mode, 0))
}

// faccessat of a descriptor's own file: a file of the kernel is the
// process's to use.
func (p *Process) libcFaccessat(dirfd, path, mode, flags uint32) uint32 {
	return p.result(0, p.onAt(int32(dirfd), p.str(path), flags, func(f Object) error {
		return nil
	}, func(d Dir, follow bool) error {
		return p.access(d, p.str(path), mode, flags)
	}))
}

func (p *Process) libcTruncate(path uint32, size uint64) uint32 {
	return p.result(0, p.onPath(atFdcwd, p.str(path), func(d Dir) error {
		return d.truncatePath(p, p.str(path), int64(size))
	}))
}

func (p *Process) libcStatvfs(path, at uint32) uint32 {
	var st syscall.Statfs_t
	err := p.statfsPath(p.str(path), &st)

	if err == nil {
		p.putStatvfs(at, &st)
	}

	return p.result(0, err)
}

func (p *Process) libcFstatvfs(fd, at uint32) uint32 {
	n, err := p.attrsAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	var st syscall.Statfs_t

	if err := n.statfs(p, &st); err != nil {
		return p.result(0, err)
	}

	p.putStatvfs(at, &st)

	return p.result(0, nil)
}

// setCwd makes d, whose reference it takes, the working directory.
func (p *Process) setCwd(d Dir) {
	old := p.cwd
	p.cwd = d

	if old != nil {
		old.unref()
	}
}

// directory is the directory a path names, one the process may search,
// held: for a working directory or a root.
func (p *Process) directory(path string) (Dir, error) {
	var nd Dir
	err := p.onPath(atFdcwd, path, func(d Dir) error {
		f, err := d.openPath(p, path, oPath|syscall.O_DIRECTORY, 0)

		if err != nil {
			return err
		}

		h := f.asDir()

		if h == nil {
			f.unref()

			return syscall.ENOTDIR
		}

		if err := p.access(h, "", 1, 0); err != nil {
			h.unref()

			return err
		}

		nd = h

		return nil
	})

	return nd, err
}

// chdir makes a directory the working directory: the directory itself,
// which a rename does not change and a removal leaves without a path.
func (p *Process) chdir(path string) error {
	d, err := p.directory(path)

	if err != nil {
		return err
	}

	p.setCwd(d)

	return nil
}

func (p *Process) libcChdir(path uint32) uint32 {
	return p.result(0, p.chdir(p.str(path)))
}

// fchdir makes a directory descriptor's directory the working directory:
// a Dir's, which chroot(".") may then take; anything else is ENOTDIR.
func (p *Process) libcFchdir(fd uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	d := obj.asDir()

	if d == nil {
		return p.result(0, syscall.ENOTDIR)
	}

	d.ref()

	if err := p.access(d, "", 1, 0); err != nil {
		d.unref()

		return p.result(0, err)
	}

	p.setCwd(d)

	return p.result(0, nil)
}

// chroot makes a directory the process's root: ".." stops there and
// absolute paths start there from then on. The working directory stays
// what it is, as Linux leaves it.
func (p *Process) libcChroot(path uint32) uint32 {
	d, err := p.directory(p.str(path))

	if err != nil {
		return p.result(0, err)
	}

	p.root.unref()
	p.root = d

	return p.result(0, nil)
}

// cwdPath is the working directory's path: none past PATH_MAX, as
// Linux's getcwd has none.
func (p *Process) cwdPath() (string, error) {
	cwd, err := p.cwd.path(p)

	if err == nil && len(cwd) >= 4096 {
		err = syscall.ENAMETOOLONG
	}

	return cwd, err
}

func (p *Process) libcGetcwd(buf, size uint32) uint32 {
	cwd, err := p.cwdPath()

	if err != nil {
		return p.pointer(0, err)
	}

	if buf == 0 {
		at := p.newString(cwd)

		if at == 0 {
			return p.pointer(0, syscall.ENOMEM)
		}

		return p.pointer(at, nil)
	}

	if uint32(len(cwd)) >= size {
		return p.pointer(0, syscall.ERANGE)
	}

	p.putString(buf, cwd)

	return p.pointer(buf, nil)
}

func (p *Process) libcMkdir(path, mode uint32) uint32 {
	return p.result(0, p.mkdir(atFdcwd, p.str(path), mode))
}

func (p *Process) libcMkdirat(dirfd, path, mode uint32) uint32 {
	return p.result(0, p.mkdir(int32(dirfd), p.str(path), mode))
}

func (p *Process) libcRmdir(path uint32) uint32 {
	return p.result(0, p.remove(atFdcwd, p.str(path), true))
}

func (p *Process) libcUnlink(path uint32) uint32 {
	return p.result(0, p.remove(atFdcwd, p.str(path), false))
}

func (p *Process) libcUnlinkat(dirfd, path, flags uint32) uint32 {
	if flags&^atRemovedir != 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(0, p.remove(int32(dirfd), p.str(path), flags&atRemovedir != 0))
}

// remove unlinks a file or empties a directory, whichever it is.
func (p *Process) libcRemove(path uint32) uint32 {
	err := p.remove(atFdcwd, p.str(path), false)

	if err == syscall.EISDIR || err == syscall.EPERM {
		err = p.remove(atFdcwd, p.str(path), true)
	}

	return p.result(0, err)
}

func (p *Process) libcRename(old, new uint32) uint32 {
	return p.result(0, p.rename(atFdcwd, p.str(old), atFdcwd, p.str(new)))
}

func (p *Process) libcRenameat(olddir, old, newdir, new uint32) uint32 {
	return p.result(0, p.rename(int32(olddir), p.str(old), int32(newdir), p.str(new)))
}

func (p *Process) libcLink(old, new uint32) uint32 {
	return p.result(0, p.link(atFdcwd, p.str(old), atFdcwd, p.str(new), false))
}

func (p *Process) libcLinkat(olddir, old, newdir, new, flags uint32) uint32 {
	if flags&^atSymlinkFollow != 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(0, p.link(int32(olddir), p.str(old), int32(newdir), p.str(new), flags&atSymlinkFollow != 0))
}

func (p *Process) libcSymlink(target, path uint32) uint32 {
	return p.result(0, p.symlink(p.str(target), atFdcwd, p.str(path)))
}

func (p *Process) libcSymlinkat(target, dirfd, path uint32) uint32 {
	return p.result(0, p.symlink(p.str(target), int32(dirfd), p.str(path)))
}

// readlinkAt writes a link's target, as much as the buffer holds.
func (p *Process) readlinkAt(dirfd int32, path string, buf, size uint32) (int, error) {
	if p.chaosFail("readlink") {
		return 0, syscall.EIO
	}

	var target string
	err := p.onPath(dirfd, path, func(d Dir) error {
		var err error
		target, err = d.readlinkPath(p, path)

		return err
	})

	if err != nil {
		return 0, err
	}

	return copy(p.span(buf, size), target), nil
}

func (p *Process) libcReadlink(path, buf, size uint32) uint32 {
	return p.result(p.readlinkAt(atFdcwd, p.str(path), buf, size))
}

// readlinkat with an empty path reads the link a descriptor is: what is
// no link is ENOENT then, as Linux has it, not the named path's EINVAL.
func (p *Process) libcReadlinkat(dirfd, path, buf, size uint32) uint32 {
	if p.u8(path) != 0 {
		return p.result(p.readlinkAt(int32(dirfd), p.str(path), buf, size))
	}

	obj, err := p.fd(dirfd)

	if dirfd == uint32(atFdcwd&0xffffffff) {
		obj, err = p.cwd, nil
	}

	if err != nil {
		return p.result(0, err)
	}

	l := obj.asLink()

	if l == nil {
		return p.result(0, syscall.ENOENT)
	}

	target, err := l.readlink(p)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(copy(p.span(buf, size), target), nil)
}

// realpath is the path from the root of what a path names, with no ".",
// ".." or link in it.
func (p *Process) realpath(path string) (string, error) {
	var out string
	err := p.onPath(atFdcwd, path, func(d Dir) error {
		var err error
		out, err = d.realpathOf(p, path)

		return err
	})

	if err == nil && len(out) >= 4096 {
		err = syscall.ENAMETOOLONG
	}

	return out, err
}

func (p *Process) libcCanonicalize_file_name(path uint32) uint32 {
	guest, err := p.realpath(p.str(path))

	if err != nil {
		return p.pointer(0, err)
	}

	at := p.newString(guest)

	if at == 0 {
		return p.pointer(0, syscall.ENOMEM)
	}

	return p.pointer(at, nil)
}

func (p *Process) libcGet_current_dir_name() uint32 {
	cwd, err := p.cwdPath()

	if err != nil {
		return p.pointer(0, err)
	}

	at := p.newString(cwd)

	if at == 0 {
		return p.pointer(0, syscall.ENOMEM)
	}

	return p.pointer(at, nil)
}

func (p *Process) libcCreat(path, mode uint32) uint32 {
	f, err := p.openPath(p.str(path), syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC, mode&0o7777&^p.umask)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(p.install(f, 0, false))
}

func (p *Process) libcEaccess(path, mode uint32) uint32 {
	return p.result(0, p.accessAt(atFdcwd, p.str(path), mode, atEaccess))
}

func (p *Process) libcEuidaccess(path, mode uint32) uint32 {
	return p.result(0, p.accessAt(atFdcwd, p.str(path), mode, atEaccess))
}

func (p *Process) libcStatfs(path, at uint32) uint32 {
	var st syscall.Statfs_t
	err := p.statfsPath(p.str(path), &st)

	if err == nil {
		p.putStatfsOf(at, &st)
	}

	return p.result(0, err)
}

func (p *Process) libcFstatfs(fd, at uint32) uint32 {
	n, err := p.attrsAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	var st syscall.Statfs_t

	if err := n.statfs(p, &st); err != nil {
		return p.result(0, err)
	}

	p.putStatfsOf(at, &st)

	return p.result(0, nil)
}

func (p *Process) libcRealpath(path, resolved uint32) uint32 {
	guest, err := p.realpath(p.str(path))

	if err != nil {
		return p.pointer(0, err)
	}

	if resolved == 0 {
		resolved = p.newString(guest)

		if resolved == 0 {
			return p.pointer(0, syscall.ENOMEM)
		}

		return p.pointer(resolved, nil)
	}

	p.putString(resolved, guest)

	return p.pointer(resolved, nil)
}

func (p *Process) libcChmod(path, mode uint32) uint32 {
	return p.result(0, p.onPath(atFdcwd, p.str(path), func(d Dir) error {
		return d.chmodPath(p, p.str(path), mode&0o7777, true)
	}))
}

func (p *Process) libcFchmodat(dirfd, path, mode, flags uint32) uint32 {
	return p.result(0, p.onAt(int32(dirfd), p.str(path), flags, attrsCall(func(a Attrs) error {
		return a.chmod(p, mode&0o7777)
	}), func(d Dir, follow bool) error {
		return d.chmodPath(p, p.str(path), mode&0o7777, follow)
	}))
}

func (p *Process) libcFchownat(dirfd, path, uid, gid, flags uint32) uint32 {
	return p.result(0, p.onAt(int32(dirfd), p.str(path), flags, attrsCall(func(a Attrs) error {
		return a.chown(p, int(int32(uid)), int(int32(gid)))
	}), func(d Dir, follow bool) error {
		return d.chownPath(p, p.str(path), int(int32(uid)), int(int32(gid)), follow)
	}))
}

func (p *Process) libcMknod(path, mode uint32, dev uint64) uint32 {
	return p.result(0, p.mknod(atFdcwd, p.str(path), mode, dev))
}

func (p *Process) libcMknodat(dirfd, path, mode uint32, dev uint64) uint32 {
	return p.result(0, p.mknod(int32(dirfd), p.str(path), mode, dev))
}

func (p *Process) libcMkfifo(path, mode uint32) uint32 {
	return p.result(0, p.mknod(atFdcwd, p.str(path), sIfifo|mode&0o7777, 0))
}

func (p *Process) libcMkfifoat(dirfd, path, mode uint32) uint32 {
	return p.result(0, p.mknod(int32(dirfd), p.str(path), sIfifo|mode&0o7777, 0))
}

func (p *Process) libcUtimes(path, times uint32) uint32 {
	return p.result(0, p.utimens(atFdcwd, p.str(path), p.timevals(times), true))
}

func (p *Process) libcLutimes(path, times uint32) uint32 {
	return p.result(0, p.utimens(atFdcwd, p.str(path), p.timevals(times), false))
}

func (p *Process) libcUtime(path, times uint32) uint32 {
	var ts []syscall.Timespec

	if times != 0 {
		ts = []syscall.Timespec{{Sec: int64(p.u64(times))}, {Sec: int64(p.u64(times + 8))}}
	}

	return p.result(0, p.utimens(atFdcwd, p.str(path), ts, true))
}

// utimensat of a null path sets the times of the descriptor's own file,
// as Linux's does.
func (p *Process) libcUtimensat(dirfd, path, times, flags uint32) uint32 {
	if path == 0 {
		n, err := p.attrsAt(dirfd)

		if err == nil {
			err = n.utimes(p, p.timespecs(times))
		}

		return p.result(0, err)
	}

	return p.result(0, p.utimens(int32(dirfd), p.str(path), p.timespecs(times), flags&atSymlinkNofollow == 0))
}

// readdir_r returns an error number.
func (p *Process) libcReaddir_r(dir, entry, result uint32) uint32 {
	e, err := p.readdir(dir)
	p.put32(result, 0)

	if err != nil {
		return uint32(errnoOf(err))
	}

	if e != 0 {
		n := 19 + uint32(len(p.cstr(e+19))) + 1
		copy(p.span(entry, n), p.span(e, n))
		p.put32(result, entry)
	}

	return 0
}

func (p *Process) libcSeekdir(dir, at uint32) {
	p.rewinddir(dir)

	for p.u32(dir+dirIndex) < at {
		if e, _ := p.readdir(dir); e == 0 {
			return
		}
	}
}

func (p *Process) libcMkdtemp(template uint32) uint32 {
	err := p.temporary(template, 0, func(path string) error {
		return p.mkdir(atFdcwd, path, 0o700)
	})

	return p.pointer(template, err)
}

func (p *Process) libcMktemp(template uint32) uint32 {
	err := p.temporary(template, 0, func(path string) error {
		var st syscall.Stat_t
		err := p.statPath(path, false, &st)

		if err == syscall.ENOENT {
			return nil
		}

		if err == nil {
			return syscall.EEXIST
		}

		return err
	})

	if err != nil {
		p.span(template, 1)[0] = 0
		p.setErrno(errnoOf(err))
	}

	return p.pointer(template, nil)
}

// timevals reads a guest struct timeval[2] as timespecs, or nil for a
// null pointer.
func (p *Process) timevals(at uint32) []syscall.Timespec {
	if at == 0 {
		return nil
	}

	b := p.span(at, 32)
	le := binary.LittleEndian
	var ts []syscall.Timespec

	for i := range 2 {
		ts = append(ts, syscall.Timespec{Sec: int64(le.Uint64(b[16*i:])), Nsec: 1000 * int64(le.Uint64(b[16*i+8:]))})
	}

	return ts
}

func (p *Process) stat(path, at uint32, follow bool) uint32 {
	var st syscall.Stat_t
	err := p.statPath(p.str(path), follow, &st)

	if err == nil {
		p.putStat(at, &st)
	}

	return p.result(0, err)
}

func (p *Process) chown(path, uid, gid uint32, follow bool) uint32 {
	return p.result(0, p.onPath(atFdcwd, p.str(path), func(d Dir) error {
		return d.chownPath(p, p.str(path), int(int32(uid)), int(int32(gid)), follow)
	}))
}

// utimens sets the times of what a path names, or of a last symbolic
// link itself.
func (p *Process) utimens(dirfd int32, path string, ts []syscall.Timespec, follow bool) error {
	return p.onPath(dirfd, path, func(d Dir) error {
		return d.utimesPath(p, path, ts, follow)
	})
}

const tempLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// temporary fills the XXXXXX that ends a template, or comes before a
// suffix of its last bytes, with random letters until create makes what
// the path names, EEXIST meaning another try.
func (p *Process) temporary(template, suffix uint32, create func(path string) error) error {
	t := p.cstr(template)
	end := len(t) - int(suffix)

	if int(suffix) > len(t) || end < 6 || string(t[end-6:end]) != "XXXXXX" {
		return syscall.EINVAL
	}

	for range 100 {
		for i := end - 6; i < end; i++ {
			t[i] = tempLetters[rand.IntN(len(tempLetters))]
		}

		if err := create(string(t)); err != syscall.EEXIST {
			return err
		}
	}

	return syscall.EEXIST
}

// mkstemp makes a file of a template, open with flags.
func (p *Process) mkstemp(template, suffix, flags uint32) uint32 {
	fd := -1
	err := p.temporary(template, suffix, func(path string) error {
		f, err := p.openPath(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL|int(flags&^syscall.O_CLOEXEC), 0o600)

		if err == nil {
			fd, err = p.install(f, 0, flags&syscall.O_CLOEXEC != 0)
		}

		return err
	})

	return p.result(fd, err)
}

// A DIR is a block of the heap: the descriptor, the position and end of
// the entries in the buffer, how many entries readdir returned since the
// start, then the buffer, which the directory fills with entries of the
// guest's struct dirent layout. telldir is that count, which seekdir reads
// again from the start: a host's offset may not fit a long.
const (
	dirFd     = 0
	dirPos    = 4
	dirEnd    = 8
	dirIndex  = 12
	dirBuf    = 16
	dirBufLen = 4096
)

func (p *Process) libcOpendir(path uint32) uint32 {
	f, err := p.openPath(p.str(path), syscall.O_RDONLY|syscall.O_DIRECTORY, 0)

	if err != nil {
		return p.pointer(0, err)
	}

	fd, err := p.install(f, 0, true)

	if err != nil {
		return p.pointer(0, err)
	}

	dir := p.newDir(fd)

	if dir == 0 {
		p.closeFd(fd)

		return p.pointer(0, syscall.ENOMEM)
	}

	return p.pointer(dir, nil)
}

// newDir makes the DIR of a descriptor, or returns 0.
func (p *Process) newDir(fd int) uint32 {
	dir := p.malloc(dirBuf + dirBufLen)

	if dir != 0 {
		p.put32(dir+dirFd, uint32(fd))
		p.put32(dir+dirPos, 0)
		p.put32(dir+dirEnd, 0)
		p.put32(dir+dirIndex, 0)
	}

	return dir
}

// fdopendir makes a DIR of a directory's descriptor, which becomes
// close-on-exec, as musl's.
func (p *Process) libcFdopendir(fd uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.pointer(0, err)
	}

	var st syscall.Stat_t

	if err := statFile(p, obj, &st); err != nil {
		return p.pointer(0, err)
	}

	if obj.asDir() == nil || st.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return p.pointer(0, syscall.ENOTDIR)
	}

	dir := p.newDir(int(fd))

	if dir == 0 {
		return p.pointer(0, syscall.ENOMEM)
	}

	p.files.slots[fd].cloexec = true

	return p.pointer(dir, nil)
}

func (p *Process) rewinddir(dir uint32) {
	if s, err := p.seekerAt(p.u32(dir + dirFd)); err == nil {
		s.seek(p, 0, 0)
	}

	p.put32(dir+dirPos, 0)
	p.put32(dir+dirEnd, 0)
	p.put32(dir+dirIndex, 0)
}

// direntCompare compares the names of two struct dirent** of scandir.
func (p *Process) direntCompare(a, b uint32, compare func(x, y []byte) int) uint32 {
	x, y := p.cstr(p.u32(a)+19), p.cstr(p.u32(b)+19)

	return uint32(int32(compare(x, y)))
}

func (p *Process) libcReaddir(dir uint32) uint32 {
	entry, err := p.readdir(dir)

	return p.pointer(entry, err)
}

// readdir returns the next entry of a DIR, or 0 at its end.
func (p *Process) readdir(dir uint32) (uint32, error) {
	pos, end := p.u32(dir+dirPos), p.u32(dir+dirEnd)

	if pos >= end {
		d, err := p.dirAt(p.u32(dir + dirFd))

		if err != nil {
			return 0, err
		}

		n, err := d.readdir(p, p.span(dir+dirBuf, dirBufLen))

		if err != nil || n == 0 {
			return 0, err
		}

		pos, end = 0, uint32(n)
		p.put32(dir+dirEnd, end)
	}

	entry := dir + dirBuf + pos
	p.put32(dir+dirPos, pos+uint32(p.u16(entry+16)))
	p.put32(dir+dirIndex, p.u32(dir+dirIndex)+1)

	return entry, nil
}

func (p *Process) libcClosedir(dir uint32) uint32 {
	fd := p.u32(dir + dirFd)
	p.free(dir)

	if !p.closeFd(int(fd)) {
		return p.result(0, syscall.EBADF)
	}

	return p.result(0, nil)
}

func (p *Process) libcStat(path, at uint32) uint32 {
	return p.stat(path, at, true)
}

func (p *Process) libcLstat(path, at uint32) uint32 {
	return p.stat(path, at, false)
}

// Process accounting needs privilege.
func (p *Process) libcAcct(path uint32) uint32 {
	return p.result(0, syscall.EPERM)
}

func (p *Process) libcChown(path, uid, gid uint32) uint32 {
	return p.chown(path, uid, gid, true)
}

func (p *Process) libcLchown(path, uid, gid uint32) uint32 {
	return p.chown(path, uid, gid, false)
}

func (p *Process) libcDirfd(dir uint32) uint32 {
	return p.u32(dir + dirFd)
}

func (p *Process) libcTelldir(dir uint32) uint32 {
	return p.u32(dir + dirIndex)
}

func (p *Process) libcAlphasort(a, b uint32) uint32 {
	return p.direntCompare(a, b, bytes.Compare)
}

func (p *Process) libcVersionsort(a, b uint32) uint32 {
	return p.direntCompare(a, b, strverscmp)
}

func (p *Process) libcMkstemp(template uint32) uint32 {
	return p.mkstemp(template, 0, 0)
}

func (p *Process) libcMkostemp(template, flags uint32) uint32 {
	return p.mkstemp(template, 0, flags)
}

func (p *Process) libcMkstemps(template, suffix uint32) uint32 {
	return p.mkstemp(template, suffix, 0)
}

func (p *Process) libcRewinddir(dir uint32) {
	p.rewinddir(dir)
}
