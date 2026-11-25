package main

import (
	"sync"
	"sync/atomic"
)

var memfdInodes atomic.Uint64

type MemfdFile struct {
	ObjectIdent

	mem  *LinearMemory
	at   int64
	n    int64
	mode int
	held bool
	refs atomic.Int32

	mu  sync.Mutex
	off int64
	ino uint64
}

func newMemfd(mem *LinearMemory, at, n int64, mode int) *MemfdFile {
	f := &MemfdFile{ObjectIdent: newObjectIdent(), mem: mem, at: at, n: n, mode: mode, ino: memfdInodes.Add(1)}

	f.refs.Store(1)

	if mem.owner != nil {
		mem.owner.holds.Add(1)
		f.held = true
	}

	return f
}

func bytesFile(b []byte) *MemfdFile {
	return newMemfd(&LinearMemory{data: b}, 0, int64(len(b)), O_RDWR)
}

func grants(mode int, writes bool) bool {
	if writes {
		return mode != O_RDONLY
	}

	return mode != O_WRONLY
}

func narrowed(mode, to int) int {
	switch {
	case to == O_RDWR || mode == to:
		return mode
	case mode == O_RDWR:
		return to
	}

	return mode
}

func (f *MemfdFile) ref() {
	f.refs.Add(1)
}

func (f *MemfdFile) unref() {
	if f.refs.Add(-1) == 0 && f.held {
		f.mem.owner.unhold()
	}
}

func (f *MemfdFile) bytes() []byte {
	b := f.mem.bytes()
	start := min(f.at, int64(len(b)))
	end := int64(len(b))

	if f.n >= 0 {
		end = min(f.at+f.n, end)
	}

	return b[start:end]
}

func (f *MemfdFile) size() int64 {
	f.mem.enter()

	defer f.mem.leave()

	return int64(len(f.bytes()))
}

func (f *MemfdFile) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: f.ino, Mode: S_IFREG | 0o777, Nlink: 1, Size: f.size(),
		Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (f *MemfdFile) poll(events int16, through []*Epoll) int16 {
	return events & (POLLIN | POLLRDNORM | POLLOUT | POLLWRNORM)
}

func (f *MemfdFile) subscribe(s *Subscription, events int16) {
}

func (f *MemfdFile) unsubscribe(s *Subscription) {
}

func (f *MemfdFile) flags(p *Process) (int, error) {
	return f.mode, nil
}

func (f *MemfdFile) setFlags(p *Process, fl int) error {
	return nil
}

func (f *MemfdFile) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	count, err := f.pread(p, into, n, f.off)

	f.off += int64(count)

	return count, err
}

func (f *MemfdFile) write(p *Process, from *MemfdFile, n int, mode IoMode) (int, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	count, err := f.pwrite(p, from, n, f.off)

	f.off += int64(count)

	return count, err
}

func (f *MemfdFile) pread(p *Process, into *MemfdFile, n int, off int64) (int, error) {
	enterBoth(f.mem, into.mem)

	defer leaveBoth(f.mem, into.mem)

	b := f.bytes()
	size := int64(len(b))

	switch {
	case !grants(f.mode, false):
		return 0, EBADF
	case off < 0 || n < 0:
		return 0, EINVAL
	case n == 0 || off >= size:
		return 0, nil
	}

	src := b[off : off+min(int64(n), size-off)]

	return into.within(0, len(src), true, func(dst []byte) (int, error) {
		moveMemory(dst, src[:len(dst)])

		return len(dst), nil
	})
}

func (f *MemfdFile) pwrite(p *Process, from *MemfdFile, n int, off int64) (int, error) {
	enterBoth(f.mem, from.mem)

	defer leaveBoth(f.mem, from.mem)

	b := f.bytes()
	size := int64(len(b))

	switch {
	case !grants(f.mode, true):
		return 0, EBADF
	case off < 0 || n < 0:
		return 0, EINVAL
	case n == 0:
		return 0, nil
	case off >= size:
		return 0, ENOSPC
	}

	dst := b[off : off+min(int64(n), size-off)]

	return from.within(0, len(dst), false, func(src []byte) (int, error) {
		moveMemory(dst, src)

		return len(src), nil
	})
}

func (f *MemfdFile) seek(p *Process, off int64, whence int) (int64, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	switch whence {
	case 0:
	case 1:
		off += f.off
	case 2:
		off += f.size()
	default:
		return 0, EINVAL
	}

	if off < 0 {
		return 0, EINVAL
	}

	f.off = off

	return off, nil
}

func (f *MemfdFile) splice(at int64, n int, mode int) *MemfdFile {
	size := f.size()

	at = min(max(at, 0), size)

	return newMemfd(f.mem, f.at+at, min(int64(max(n, 0)), size-at), narrowed(f.mode, mode))
}

func (f *MemfdFile) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	f.mem.enter()

	defer f.mem.leave()

	return f.within(at, n, writes, fn)
}

func (f *MemfdFile) within(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	b := f.bytes()
	size := int64(len(b))

	switch {
	case !grants(f.mode, writes):
		return 0, EBADF
	case at < 0 || n < 0:
		return 0, EINVAL
	}

	at = min(at, size)

	return fn(b[at : at+min(int64(n), size-at)])
}

func (f *MemfdFile) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	size := f.size()

	switch {
	case out == Object(f):
		return 0, EINVAL
	case n == 0 || from >= size:
		return 0, nil
	case !grants(f.mode, false):
		return 0, EBADF
	}

	n = int(min(int64(n), size-from))

	part := f.splice(from, n, O_RDONLY)
	count, err := p.filePwrite(out, part, n, to)

	part.unref()

	return count, err
}

func (f *MemfdFile) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	size := f.size()

	switch {
	case n == 0:
		return 0, nil
	case to >= size:
		return 0, ENOSPC
	case !grants(f.mode, true):
		return 0, EBADF
	}

	n = int(min(int64(n), size-to))

	part := f.splice(to, n, O_WRONLY)
	count, err := p.filePread(in, part, n, from)

	part.unref()

	return count, err
}

func (f *MemfdFile) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, f.stat(p, r.st)
	case *FileGetfl:
		var err error

		r.flags, err = f.flags(p)

		return false, err
	case *FileSetfl:
		return false, f.setFlags(p, r.flags)
	case *FileRead:
		var err error

		if r.explicit {
			r.count, err = f.pread(p, r.into, r.n, r.at)
		} else {
			r.count, err = f.read(p, r.into, r.n, r.mode)
		}

		return false, err
	case *FileWrite:
		var err error

		if r.explicit {
			r.count, err = f.pwrite(p, r.from, r.n, r.at)
		} else {
			r.count, err = f.write(p, r.from, r.n, r.mode)
		}

		return false, err
	case *FileSeek:
		var err error

		r.pos, err = f.seek(p, r.off, r.whence)

		return false, err
	case *FileCopyTo:
		var err error

		r.count, err = f.copyTo(p, r.out, r.from, r.to, r.n)

		return false, err
	case *FileCopyFrom:
		var err error

		r.count, err = f.copyFrom(p, r.in, r.from, r.to, r.n)

		return false, err
	}

	return false, errUnserved
}
