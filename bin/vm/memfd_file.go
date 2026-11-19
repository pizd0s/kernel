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

func window(p *Process, b []byte, mode int) *MemfdFile {
	var inst *Instance

	if p != nil {
		inst = p.inst
	}

	return newMemfd(inst, b, mode)
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

func (f *MemfdFile) asWindowed() Windowed {
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

func (f *MemfdFile) asMemfd() *MemfdFile {
	return f
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

func (f *MemfdFile) asVFS() *RemoteDir {
	return nil
}

func (f *MemfdFile) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: f.ino, Mode: S_IFREG | 0o777, Nlink: 1, Size: int64(len(f.mem)),
		Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = nsecToTimespec(f.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (f *MemfdFile) poll(events int16) int16 {
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

func (f *MemfdFile) fill(p *Process, src Object, from, to *int64, n int) (int, error) {
	if src == Object(f) {
		return 0, EINVAL
	}

	if f.mode == O_RDONLY {
		return 0, EBADF
	}

	at := f.position(to)

	switch {
	case at < 0:
		return 0, EINVAL
	case n == 0:
		return 0, nil
	case at >= int64(len(f.mem)):
		return 0, ENOSPC
	}

	n = min(n, len(f.mem)-int(at))
	var count int
	var err error

	if m := src.asMemfd(); m != nil {
		count, err = m.moveTo(f.mem[at:int(at)+n], from)
	} else if w := src.asWindowed(); w != nil {
		count, err = w.readWindow(p, f, uint32(at), n, from)
	} else {
		err = EINVAL
	}

	f.moved(to, count)

	return count, err
}

func (f *MemfdFile) drain(p *Process, dst Object, from, to *int64, n int) (int, error) {
	if f.mode == O_WRONLY {
		return 0, EBADF
	}

	at := f.position(from)

	switch {
	case at < 0:
		return 0, EINVAL
	case n == 0 || at >= int64(len(f.mem)):
		return 0, nil
	}

	n = min(n, len(f.mem)-int(at))
	var count int
	var err error

	if d := dst.asWindowed(); d != nil {
		count, err = d.writeWindow(p, f, uint32(at), n, to)
	} else {
		err = EINVAL
	}

	f.moved(from, count)

	return count, err
}

func (f *MemfdFile) moveTo(b []byte, from *int64) (int, error) {
	n, err := f.pread(nil, b, f.position(from))

	f.moved(from, n)

	return n, err
}

func (f *MemfdFile) position(at *int64) int64 {
	if at != nil {
		return *at
	}

	f.mu.Lock()

	defer f.mu.Unlock()

	return f.off
}

func (f *MemfdFile) moved(at *int64, count int) {
	if at != nil {
		*at += int64(count)

		return
	}

	f.mu.Lock()
	f.off += int64(count)
	f.mu.Unlock()
}
