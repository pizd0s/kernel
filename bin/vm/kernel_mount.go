//go:build linux && amd64

package main

import (
	"sync/atomic"
	"syscall"
)

const umountNofollow = 8

// mount mounts on the directory target names the directory source names,
// or the root of the file system whose socket source names, which the
// kernel connects to (kernel_unix.go): the VFS's call (kernel_vfs.go),
// both paths from the working directory or the root. fstype, flags and
// data are not used; a process of the host's mounts nothing.
func (p *Process) libcMount(source, target, fstype, flags, data uint32) uint32 {
	from, to := p.str(source), p.str(target)

	for _, path := range []string{from, to} {
		if err := checked(path); err != nil {
			return p.result(0, err)
		}
	}

	handled, err := p.onVFS(atFdcwd, ".", func(v *remoteDir) error {
		var st syscall.Stat_t

		if v.statPath(p, from, true, &st) != nil || st.Mode&syscall.S_IFMT != syscall.S_IFSOCK {
			return v.mountPath(p, to, from, nil)
		}

		conn, err := p.connectUnix(v, from, sockSeqpacket, false, new(atomic.Int64))

		if err != nil {
			return err
		}

		defer conn.unref()

		return v.mountPath(p, to, from, conn)
	})

	if !handled {
		err = syscall.EINVAL
	}

	return p.result(0, err)
}

// umount2 unmounts what is mounted on the directory target names; the
// flags but UMOUNT_NOFOLLOW are not used.
func (p *Process) libcUmount2(target, flags uint32) uint32 {
	handled, err := p.onVFS(atFdcwd, p.str(target), func(v *remoteDir) error {
		return v.unmountPath(p, p.str(target), flags&umountNofollow)
	})

	if !handled {
		err = syscall.EINVAL
	}

	return p.result(0, err)
}

func (p *Process) libcUmount(target uint32) uint32 {
	return p.libcUmount2(target, 0)
}
