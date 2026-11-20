//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"time"
)

var memfdInodes atomic.Uint64

type MemfdFile struct {
	inst *Instance
	mem  []byte
	mode int
	refs atomic.Int32

	mu   sync.Mutex
	off  int64
	ino  uint64
	made time.Time
}

func newMemfd(inst *Instance, mem []byte, mode int) *MemfdFile {
	f := &MemfdFile{inst: inst, mem: mem, mode: mode, ino: memfdInodes.Add(1), made: time.Now()}

	f.refs.Store(1)

	if inst != nil {
		inst.holds.Add(1)
	}

	return f
}

func (f *MemfdFile) ref() {
	f.refs.Add(1)
}

func (f *MemfdFile) unref() {
	if f.refs.Add(-1) == 0 && f.inst != nil {
		f.inst.unhold()
	}
}

func (f *MemfdFile) asFile() File {
	return f
}

func (f *MemfdFile) asTransfer() Transfer {
	return f
}

func (f *MemfdFile) asSeeker() Seeker {
	return f
}

func (f *MemfdFile) asDir() Dir {
	return nil
}

func (f *MemfdFile) asTerminal() Terminal {
	return nil
}

func (f *MemfdFile) asLink() Link {
	return nil
}

func (f *MemfdFile) asAttrs() Attrs {
	return nil
}

func (f *MemfdFile) asSocket() Socket {
	return nil
}

func (f *MemfdFile) asIoctl() Ioctl {
	return nil
}

func (f *MemfdFile) asLocker() Locker {
	return nil
}

func (f *MemfdFile) asEpoll() *Epoll {
	return nil
}

func (f *MemfdFile) asPair() *PairEnd {
	return nil
}

func (f *MemfdFile) asListener() *UnixListener {
	return nil
}

func (f *MemfdFile) asConsole() *ConsoleEnd {
	return nil
}

func (f *MemfdFile) asRemote() *RemoteFD {
	return nil
}

func (f *MemfdFile) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: f.ino, Mode: S_IFREG | 0o777, Nlink: 1, Size: int64(len(f.mem)),
		Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = nsecToTimespec(f.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (f *MemfdFile) poll(events int16, through []*Epoll) int16 {
	return events & (POLLIN | POLLRDNORM | POLLOUT | POLLWRNORM)
}

func (f *MemfdFile) subscribe(s *Subscription, events int16) {
}

func (f *MemfdFile) unsubscribe(s *Subscription) {
}

func (f *MemfdFile) flags() (int, error) {
	return f.mode, nil
}

func (f *MemfdFile) setFlags(fl int) error {
	return nil
}

func (f *MemfdFile) read(p *Process, b []byte, mode IoMode) (int, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	n, err := f.pread(p, b, f.off)

	f.off += int64(n)

	return n, err
}

func (f *MemfdFile) write(p *Process, b []byte, mode IoMode) (int, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	n, err := f.pwrite(p, b, f.off)

	f.off += int64(n)

	return n, err
}

func (f *MemfdFile) pread(p *Process, b []byte, off int64) (int, error) {
	switch {
	case f.mode == O_WRONLY:
		return 0, EBADF
	case off < 0:
		return 0, EINVAL
	case off >= int64(len(f.mem)):
		return 0, nil
	}

	n := min(len(b), len(f.mem)-int(off))

	moveMemory(b, f.mem[off:int(off)+n])

	return n, nil
}

func (f *MemfdFile) pwrite(p *Process, b []byte, off int64) (int, error) {
	switch {
	case f.mode == O_RDONLY:
		return 0, EBADF
	case off < 0:
		return 0, EINVAL
	case len(b) == 0:
		return 0, nil
	case off >= int64(len(f.mem)):
		return 0, ENOSPC
	}

	n := min(len(b), len(f.mem)-int(off))

	moveMemory(f.mem[off:], b[:n])

	return n, nil
}

func (f *MemfdFile) seek(p *Process, off int64, whence int) (int64, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	switch whence {
	case 0:
	case 1:
		off += f.off
	case 2:
		off += int64(len(f.mem))
	default:
		return 0, EINVAL
	}

	if off < 0 {
		return 0, EINVAL
	}

	f.off = off

	return off, nil
}

func (f *MemfdFile) copyFileRange(p *Process, in, out Transfer, from, to int64, n int) (int, error) {
	if in == out {
		return 0, EINVAL
	}

	size := int64(len(f.mem))

	if out == Transfer(f) {
		switch {
		case n == 0:
			return 0, nil
		case to >= size:
			return 0, ENOSPC
		}

		return in.pread(p, f.mem[to:to+min(int64(n), size-to)], from)
	}

	if n == 0 || from >= size {
		return 0, nil
	}

	return out.pwrite(p, f.mem[from:from+min(int64(n), size-from)], to)
}
