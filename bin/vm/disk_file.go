//go:build linux && amd64

package main

import (
	"sync/atomic"
	"syscall"
)

// diskFile is a disk of vm's, a file of the host (`vm run --disk`): read
// and written at offsets as it is, its size the file's, a block device to
// the system, which vm's own calls move the bytes of. A memfd's copy to
// and from it goes straight between the file and the memfd's memory
// (memfd_file.go).
type diskFile struct {
	fd   int
	refs atomic.Int32
	pos  atomic.Int64
	ino  uint64
}

func newDiskFile(fd int) *diskFile {
	f := &diskFile{fd: fd, ino: pipeInodes.Add(1)}
	f.refs.Store(1)

	return f
}

func (f *diskFile) ref() {
	f.refs.Add(1)
}

func (f *diskFile) unref() {
	if f.refs.Add(-1) == 0 {
		syscall.Close(f.fd)
	}
}

// A diskFile is a File, a Transfer, a Seeker, a Windowed and an Attrs.
func (f *diskFile) asFile() File              { return f }
func (f *diskFile) asTransfer() Transfer      { return f }
func (f *diskFile) asSeeker() Seeker          { return f }
func (f *diskFile) asDir() Dir                { return nil }
func (f *diskFile) asTerminal() Terminal      { return nil }
func (f *diskFile) asWindowed() Windowed      { return f }
func (f *diskFile) asLink() Link              { return nil }
func (f *diskFile) asAttrs() Attrs            { return f }
func (f *diskFile) asSocket() Socket          { return nil }
func (f *diskFile) asIoctl() Ioctl            { return nil }
func (f *diskFile) asLocker() Locker          { return nil }
func (f *diskFile) asEpoll() *epoll           { return nil }
func (f *diskFile) asMemfd() *memfdFile       { return nil }
func (f *diskFile) asPair() *pairEnd          { return nil }
func (f *diskFile) asListener() *unixListener { return nil }
func (f *diskFile) asConsole() *consoleEnd    { return nil }
func (f *diskFile) asVFS() *remoteDir         { return nil }
func (f *diskFile) asCall() *callEnd          { return nil }

// size is the file's size now.
func (f *diskFile) size() (int64, error) {
	var st syscall.Stat_t

	if err := syscall.Fstat(f.fd, &st); err != nil {
		return 0, syscall.EIO
	}

	return st.Size, nil
}

func (f *diskFile) stat(p *Process, st *syscall.Stat_t) error {
	size, err := f.size()

	if err != nil {
		return err
	}

	*st = syscall.Stat_t{Ino: f.ino, Mode: syscall.S_IFBLK | 0o660, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Size: size,
		Blksize: 4096, Blocks: (size + 511) / 512}

	return nil
}

func (f *diskFile) poll(events int16) int16 {
	return events & (pollIn | pollRdnorm | pollOut | pollWrnorm)
}

func (f *diskFile) subscribe(s *subscription, events int16) {
}

func (f *diskFile) unsubscribe(s *subscription) {
}

func (f *diskFile) flags() (int, error) {
	return syscall.O_RDWR, nil
}

func (f *diskFile) setFlags(fl int) error {
	return nil
}

// at reads into b, or writes it with write, at off: what the host's file
// gives, a short read at its end.
func (f *diskFile) at(b []byte, off int64, write bool) (int, error) {
	if off < 0 {
		return 0, syscall.EINVAL
	}

	for {
		var n int
		var err error

		if write {
			n, err = syscall.Pwrite(f.fd, b, off)
		} else {
			n, err = syscall.Pread(f.fd, b, off)
		}

		if err == syscall.EINTR {
			continue
		}

		if err != nil {
			return 0, syscall.EIO
		}

		return n, nil
	}
}

func (f *diskFile) pread(p *Process, b []byte, off int64) (int, error) {
	return f.at(b, off, false)
}

func (f *diskFile) pwrite(p *Process, b []byte, off int64) (int, error) {
	return f.at(b, off, true)
}

func (f *diskFile) read(p *Process, b []byte, mode ioMode) (int, error) {
	n, err := f.at(b, f.pos.Load(), false)
	f.pos.Add(int64(n))

	return n, err
}

func (f *diskFile) write(p *Process, b []byte, mode ioMode) (int, error) {
	n, err := f.at(b, f.pos.Load(), true)
	f.pos.Add(int64(n))

	return n, err
}

func (f *diskFile) seek(p *Process, off int64, whence int) (int64, error) {
	base := int64(0)

	switch whence {
	case 0:
	case 1:
		base = f.pos.Load()
	case 2:
		size, err := f.size()

		if err != nil {
			return 0, err
		}

		base = size
	default:
		return 0, syscall.EINVAL
	}

	if base+off < 0 {
		return 0, syscall.EINVAL
	}

	f.pos.Store(base + off)

	return base + off, nil
}

func (f *diskFile) truncate(p *Process, size int64) error {
	return syscall.EINVAL
}

func (f *diskFile) sync(p *Process, data bool) error {
	sync := syscall.Fsync

	if data {
		sync = syscall.Fdatasync
	}

	if err := sync(f.fd); err != nil {
		return syscall.EIO
	}

	return nil
}

func (f *diskFile) chmod(p *Process, mode uint32) error {
	return syscall.EPERM
}

func (f *diskFile) chown(p *Process, uid, gid int) error {
	return syscall.EPERM
}

func (f *diskFile) utimes(p *Process, ts []syscall.Timespec) error {
	return syscall.EPERM
}

func (f *diskFile) statfs(p *Process, st *syscall.Statfs_t) error {
	*st = syscall.Statfs_t{Bsize: 4096, Frsize: 4096, Namelen: 255}

	return nil
}

// readWindow and writeWindow move a disk's bytes into a memfd's window or
// from it: vm's calls copy between the disk and the window's memory.
func (d *diskFile) readWindow(p *Process, w *memfdFile, woff uint32, n int, from *int64) (int, error) {
	return d.transfer(p, w.mem[woff:int(woff)+n], from, false)
}

func (d *diskFile) writeWindow(p *Process, w *memfdFile, woff uint32, n int, to *int64) (int, error) {
	return d.transfer(p, w.mem[woff:int(woff)+n], to, true)
}

// transfer reads the disk into b, or with write writes b into it, at off
// or at its position, which moves.
func (d *diskFile) transfer(p *Process, b []byte, off *int64, write bool) (int, error) {
	switch {
	case off == nil && write:
		return d.write(p, b, ioMode{})
	case off == nil:
		return d.read(p, b, ioMode{})
	}

	n, err := d.at(b, *off, write)
	*off += int64(n)

	return n, err
}
