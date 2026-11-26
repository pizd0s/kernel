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
	held bool

	mu  sync.Mutex
	ino uint64
}

func newMemfd(mem *LinearMemory, at, n int64) *MemfdFile {
	f := &MemfdFile{ObjectIdent: newObjectIdent(), mem: mem, at: at, n: n, ino: memfdInodes.Add(1)}

	if mem.owner != nil {
		mem.owner.holds.Add(1)
		f.held = true
	}

	return f
}

func bytesFile(b []byte) *Description {
	return opened(newMemfd(&LinearMemory{data: b}, 0, int64(len(b))), O_RDWR)
}

func (d *Description) memory() *MemfdFile {
	f, _ := d.object().(*MemfdFile)

	return f
}

func (f *MemfdFile) closed(d *Description) {
	if f.held {
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

func (f *MemfdFile) poll(d *Description, events int16, through []*Epoll) int16 {
	return events & (POLLIN | POLLRDNORM | POLLOUT | POLLWRNORM)
}

func (f *MemfdFile) subscribe(d *Description, s *Subscription, events int16) {
}

func (f *MemfdFile) unsubscribe(d *Description, s *Subscription) {
}

func (f *MemfdFile) read(p *Process, d *Description, into *MemfdFile, n int) (int, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	count, err := f.pread(p, into, n, d.offset)

	d.offset += int64(count)

	return count, err
}

func (f *MemfdFile) write(p *Process, d *Description, from *MemfdFile, n int) (int, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	count, err := f.pwrite(p, from, n, d.offset)

	d.offset += int64(count)

	return count, err
}

func (f *MemfdFile) pread(p *Process, into *MemfdFile, n int, off int64) (int, error) {
	enterBoth(f.mem, into.mem)

	defer leaveBoth(f.mem, into.mem)

	b := f.bytes()
	size := int64(len(b))

	switch {
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

func (f *MemfdFile) seek(d *Description, off int64, whence int) (int64, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	switch whence {
	case 0:
	case 1:
		off += d.offset
	case 2:
		off += f.size()
	default:
		return 0, EINVAL
	}

	if off < 0 {
		return 0, EINVAL
	}

	d.offset = off

	return off, nil
}

func (f *MemfdFile) splice(at int64, n int, mode int) *Description {
	size := f.size()

	at = min(max(at, 0), size)

	return opened(newMemfd(f.mem, f.at+at, min(int64(max(n, 0)), size-at)), mode)
}

func (f *MemfdFile) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	f.mem.enter()

	defer f.mem.leave()

	return f.within(at, n, writes, fn)
}

func (f *MemfdFile) within(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	b := f.bytes()
	size := int64(len(b))

	if at < 0 || n < 0 {
		return 0, EINVAL
	}

	at = min(at, size)

	return fn(b[at : at+min(int64(n), size-at)])
}

func (f *MemfdFile) put(at int, data []byte) int {
	n, _ := f.with(int64(at), len(data), true, func(b []byte) (int, error) {
		return copy(b, data), nil
	})

	return n
}

func (f *MemfdFile) take(at int, b []byte) int {
	n, _ := f.with(int64(at), len(b), false, func(src []byte) (int, error) {
		return copy(b, src), nil
	})

	return n
}

func (f *MemfdFile) copyTo(p *Process, out *Description, from, to int64, n int) (int, error) {
	size := f.size()

	switch {
	case out.object() == Object(f):
		return 0, EINVAL
	case n == 0 || from >= size:
		return 0, nil
	}

	n = int(min(int64(n), size-from))

	part := f.splice(from, n, O_RDONLY)
	count, err := p.filePwrite(out, part, n, to)

	part.release()

	return count, err
}

func (f *MemfdFile) copyFrom(p *Process, in *Description, from, to int64, n int) (int, error) {
	size := f.size()

	switch {
	case n == 0:
		return 0, nil
	case to >= size:
		return 0, ENOSPC
	}

	n = int(min(int64(n), size-to))

	part := f.splice(to, n, O_WRONLY)
	count, err := p.filePread(in, part, n, from)

	part.release()

	return count, err
}

func (f *MemfdFile) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, f.stat(p, r.st)
	case *FileRead:
		var err error

		switch {
		case !d.reads():
			err = EBADF
		case r.explicit:
			r.count, err = f.pread(p, r.into.memory(), r.n, r.at)
		default:
			r.count, err = f.read(p, d, r.into.memory(), r.n)
		}

		return false, err
	case *FileWrite:
		var err error

		switch {
		case !d.writes():
			err = EBADF
		case r.explicit:
			r.count, err = f.pwrite(p, r.from.memory(), r.n, r.at)
		default:
			r.count, err = f.write(p, d, r.from.memory(), r.n)
		}

		return false, err
	case *FileSeek:
		var err error

		r.pos, err = f.seek(d, r.off, r.whence)

		return false, err
	case *FileCopyTo:
		if !d.reads() {
			return false, EBADF
		}

		var err error

		r.count, err = f.copyTo(p, r.out, r.from, r.to, r.n)

		return false, err
	case *FileCopyFrom:
		if !d.writes() {
			return false, EBADF
		}

		var err error

		r.count, err = f.copyFrom(p, r.in, r.from, r.to, r.n)

		return false, err
	}

	return false, errUnserved
}
