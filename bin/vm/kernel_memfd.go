//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// memfdFile is the file memfd_create makes of a range of a process's
// memory: its bytes are that memory's. Whatever process the descriptor
// reaches reads and writes them, a read straight into its own memory and
// a write straight from it, with no copy between. The access mode is the
// creator's grant. The file holds its instance's region, which stays
// mapped while the file lives (Instance.unhold).
type memfdFile struct {
	inst *Instance
	mem  []byte
	mode int
	refs atomic.Int32
	// mu guards off, the position read, write and seek share.
	mu   sync.Mutex
	off  int64
	ino  uint64
	made time.Time
}

var memfdInodes atomic.Uint64

// memfd_create makes a file of the n bytes of the process's memory at at,
// open in the access mode of flags, close-on-exec with O_CLOEXEC.
func (p *Process) libcMemfd_create(at, n, flags uint32) uint32 {
	mode := int(flags & syscall.O_ACCMODE)

	if flags&^(syscall.O_ACCMODE|syscall.O_CLOEXEC) != 0 || mode == syscall.O_ACCMODE {
		return p.result(0, syscall.EINVAL)
	}

	i := p.inst

	if i.memory == nil || uint64(at)+uint64(n) > i.memory.pages*pageSize {
		return p.result(0, syscall.EFAULT)
	}

	f := newMemfd(i, i.memory.data[at:at+n:at+n], mode)
	fd, err := p.install(f, 0, flags&syscall.O_CLOEXEC != 0)

	if err != nil {
		f.unref()

		return p.result(0, err)
	}

	return p.result(fd, nil)
}

// newMemfd is the memfd of mem in the access mode mode: of a range of
// inst's memory, which it holds, or with inst nil of the kernel's.
func newMemfd(inst *Instance, mem []byte, mode int) *memfdFile {
	f := &memfdFile{inst: inst, mem: mem, mode: mode, ino: memfdInodes.Add(1), made: time.Now()}
	f.refs.Store(1)

	if inst != nil {
		inst.holds.Add(1)
	}

	return f
}

// window is the memfd a READ or WRITE of the file protocol brings
// (kernel_remote.go): b, where the process's read puts the bytes or its
// write takes them, for the server to write or read itself.
func window(p *Process, b []byte, mode int) *memfdFile {
	var inst *Instance

	if p != nil {
		inst = p.inst
	}

	return newMemfd(inst, b, mode)
}

func (f *memfdFile) ref() {
	f.refs.Add(1)
}

func (f *memfdFile) unref() {
	if f.refs.Add(-1) == 0 && f.inst != nil {
		f.inst.unhold()
	}
}

// A memfdFile is a File, a Transfer and a Seeker, and itself to a call
// that wants one.
func (f *memfdFile) asFile() File              { return f }
func (f *memfdFile) asTransfer() Transfer      { return f }
func (f *memfdFile) asSeeker() Seeker          { return f }
func (f *memfdFile) asDir() Dir                { return nil }
func (f *memfdFile) asTerminal() Terminal      { return nil }
func (f *memfdFile) asWindowed() Windowed      { return nil }
func (f *memfdFile) asLink() Link              { return nil }
func (f *memfdFile) asAttrs() Attrs            { return nil }
func (f *memfdFile) asSocket() Socket          { return nil }
func (f *memfdFile) asIoctl() Ioctl            { return nil }
func (f *memfdFile) asLocker() Locker          { return nil }
func (f *memfdFile) asEpoll() *epoll           { return nil }
func (f *memfdFile) asMemfd() *memfdFile       { return f }
func (f *memfdFile) asPair() *pairEnd          { return nil }
func (f *memfdFile) asListener() *unixListener { return nil }
func (f *memfdFile) asConsole() *consoleEnd    { return nil }
func (f *memfdFile) asVFS() *remoteDir         { return nil }

func (f *memfdFile) stat(p *Process, st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Ino: f.ino, Mode: syscall.S_IFREG | 0o777, Nlink: 1, Size: int64(len(f.mem)),
		Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = syscall.NsecToTimespec(f.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

// poll: memory is always ready, as a regular file is.
func (f *memfdFile) poll(events int16) int16 {
	return events & (pollIn | pollRdnorm | pollOut | pollWrnorm)
}

func (f *memfdFile) subscribe(s *subscription, events int16) {
}

func (f *memfdFile) unsubscribe(s *subscription) {
}

func (f *memfdFile) flags() (int, error) {
	return f.mode, nil
}

func (f *memfdFile) setFlags(fl int) error {
	return nil
}

func (f *memfdFile) read(p *Process, b []byte, mode ioMode) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	n, err := f.pread(p, b, f.off)
	f.off += int64(n)

	return n, err
}

func (f *memfdFile) write(p *Process, b []byte, mode ioMode) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	n, err := f.pwrite(p, b, f.off)
	f.off += int64(n)

	return n, err
}

// pread copies the range's bytes at off into b, the reader's memory, with
// the memmove a program's memcpy runs.
func (f *memfdFile) pread(p *Process, b []byte, off int64) (int, error) {
	switch {
	case f.mode == syscall.O_WRONLY:
		return 0, syscall.EBADF
	case off < 0:
		return 0, syscall.EINVAL
	case off >= int64(len(f.mem)):
		return 0, nil
	}

	n := min(len(b), len(f.mem)-int(off))
	moveMemory(b, f.mem[off:int(off)+n])

	return n, nil
}

// pwrite copies b, the writer's memory, into the range at off, as pread
// does; the range does not grow.
func (f *memfdFile) pwrite(p *Process, b []byte, off int64) (int, error) {
	switch {
	case f.mode == syscall.O_RDONLY:
		return 0, syscall.EBADF
	case off < 0:
		return 0, syscall.EINVAL
	case len(b) == 0:
		return 0, nil
	case off >= int64(len(f.mem)):
		return 0, syscall.ENOSPC
	}

	n := min(len(b), len(f.mem)-int(off))
	moveMemory(f.mem[off:], b[:n])

	return n, nil
}

func (f *memfdFile) seek(p *Process, off int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch whence {
	case 0:
	case 1:
		off += f.off
	case 2:
		off += int64(len(f.mem))
	default:
		return 0, syscall.EINVAL
	}

	if off < 0 {
		return 0, syscall.EINVAL
	}

	f.off = off

	return off, nil
}

// copyWindow is copy_file_range between a memfd and another file: the
// memfd's bytes are the memory it is of, so the other file's go straight
// there or come straight from there, one copy, a Windowed file's; another
// memfd's bytes are moved. from and to are the offsets given, nil for the
// files' positions, which move.
func copyWindow(p *Process, src, dst Object, from, to *int64, n int) (int, error) {
	if w := dst.asMemfd(); w != nil {
		return w.fill(p, src, from, to, n)
	}

	return src.asMemfd().drain(p, dst, from, to, n)
}

// fill copies up to n bytes of src, at from or its position, into the
// range at to or its position: the range does not grow, past its end the
// copy is ENOSPC. No lock is held while the bytes go: a server that
// writes to the range as it answers waits for nothing.
func (f *memfdFile) fill(p *Process, src Object, from, to *int64, n int) (int, error) {
	if src == Object(f) {
		return 0, syscall.EINVAL
	}

	if f.mode == syscall.O_RDONLY {
		return 0, syscall.EBADF
	}

	at := f.position(to)

	switch {
	case at < 0:
		return 0, syscall.EINVAL
	case n == 0:
		return 0, nil
	case at >= int64(len(f.mem)):
		return 0, syscall.ENOSPC
	}

	n = min(n, len(f.mem)-int(at))
	var count int
	var err error

	if m := src.asMemfd(); m != nil {
		count, err = m.moveTo(f.mem[at:int(at)+n], from)
	} else if w := src.asWindowed(); w != nil {
		count, err = w.readWindow(p, f, uint32(at), n, from)
	} else {
		err = syscall.EINVAL // as copy_file_range has it
	}

	f.moved(to, count)

	return count, err
}

// drain copies up to n bytes of the range, at from or its position, into
// dst at to or its position.
func (f *memfdFile) drain(p *Process, dst Object, from, to *int64, n int) (int, error) {
	if f.mode == syscall.O_WRONLY {
		return 0, syscall.EBADF
	}

	at := f.position(from)

	switch {
	case at < 0:
		return 0, syscall.EINVAL
	case n == 0 || at >= int64(len(f.mem)):
		return 0, nil
	}

	n = min(n, len(f.mem)-int(at))
	var count int
	var err error

	if d := dst.asWindowed(); d != nil {
		count, err = d.writeWindow(p, f, uint32(at), n, to)
	} else {
		err = syscall.EINVAL // as copy_file_range has it
	}

	f.moved(from, count)

	return count, err
}

// moveTo moves the range's bytes at from or its position, which moves,
// into b, another memfd's range.
func (f *memfdFile) moveTo(b []byte, from *int64) (int, error) {
	n, err := f.pread(nil, b, f.position(from))
	f.moved(from, n)

	return n, err
}

// position is where a copy of the range starts: at the offset given, or
// at the range's position.
func (f *memfdFile) position(at *int64) int64 {
	if at != nil {
		return *at
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.off
}

// moved moves the offset given, or the range's position, past count bytes.
func (f *memfdFile) moved(at *int64, count int) {
	if at != nil {
		*at += int64(count)

		return
	}

	f.mu.Lock()
	f.off += int64(count)
	f.mu.Unlock()
}

// readWindow reads size bytes of the server's file at from, or at its
// position, which moves, into the memfd w from its byte woff on.
func (n *remoteNode) readWindow(p *Process, w *memfdFile, woff uint32, size int, from *int64) (int, error) {
	if from != nil {
		count, err := n.readInto(p, w, woff, size, *from)
		*from += int64(count)

		return count, err
	}

	count, err := n.readInto(p, w, woff, size, atomic.LoadInt64(&n.pos))
	atomic.AddInt64(&n.pos, int64(count))

	return count, err
}

// writeWindow writes size bytes of the memfd w, from its byte woff on,
// into the server's file at to, or at its position, which moves. A file
// open to append is EBADF, as Linux has it.
func (n *remoteNode) writeWindow(p *Process, w *memfdFile, woff uint32, size int, to *int64) (int, error) {
	if n.append.Load() {
		return 0, syscall.EBADF
	}

	if to != nil {
		count, _, err := n.writeFrom(p, w, woff, size, *to, false)
		*to += int64(count)

		return count, err
	}

	count, _, err := n.writeFrom(p, w, woff, size, atomic.LoadInt64(&n.pos), false)
	atomic.AddInt64(&n.pos, int64(count))

	return count, err
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
