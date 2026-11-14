//go:build linux && amd64

package main

import "syscall"

const umountNofollow = 8

// mount mounts on the directory target names the directory source names,
// or the root of the file system whose socket of /dev source names: the
// VFS's call (kernel_vfs.go), both paths from the working directory or
// the root. fstype, flags and data are not used; a process of the host's
// mounts nothing.
func (p *Process) libcMount(source, target, fstype, flags, data uint32) uint32 {
	from, to := p.str(source), p.str(target)

	for _, path := range []string{from, to} {
		if err := checked(path); err != nil {
			return p.result(0, err)
		}
	}

	handled, err := p.onVFS(atFdcwd, ".", func(v *remoteDir) error {
		return v.mountPath(p, to, from)
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
