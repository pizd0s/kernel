//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type RemoteFD struct {
	end  Object
	refs atomic.Int32

	mu       sync.Mutex
	watchers Subscribers
	id       uint64
	key      FileKey
	accmode  int
	opath    bool
	append   bool
	nonblock bool
	pos      int64
	ready    int16

	locks *LockTable
}

func (f *RemoteFD) ref() {
	f.refs.Add(1)
}

func (f *RemoteFD) unref() {
	if f.refs.Add(-1) == 0 {
		if f.locks != nil {
			f.locks.forget(f)
		}

		f.end.unref()
	}
}

func (f *RemoteFD) subscribe(s *Subscription, events int16) {
	f.mu.Lock()
	f.watchers.add(&s.in)
	f.mu.Unlock()
}

func (f *RemoteFD) unsubscribe(s *Subscription) {
	f.mu.Lock()
	f.watchers.remove(&s.in)
	f.mu.Unlock()
}

func (f *RemoteFD) poll(events int16, through []*Epoll) int16 {
	f.mu.Lock()

	ready := f.ready

	f.mu.Unlock()

	return ready & (events | POLLERR | POLLHUP)
}

func (f *RemoteFD) deliver(pk *Packet) {
	if pk.kind == packetEnd {
		f.mu.Lock()
		f.ready = POLLHUP | POLLERR
		f.watchers.tell()
		f.mu.Unlock()

		return
	}

	if len(pk.data) >= remoteHeader && binary.LittleEndian.Uint32(pk.data) == rReady {
		f.mu.Lock()
		f.ready = readiness(binary.LittleEndian.Uint32(pk.data[4:]))
		f.watchers.tell()
		f.mu.Unlock()
	}

	dropFiles(pk.files)
}

func (f *RemoteFD) flags() (int, error) {
	f.mu.Lock()

	defer f.mu.Unlock()

	fl := f.accmode

	if f.opath {
		fl |= O_PATH
	}

	if f.append {
		fl |= O_APPEND
	}

	if f.nonblock {
		fl |= O_NONBLOCK
	}

	return fl, nil
}

func (f *RemoteFD) setFlags(fl int) error {
	f.mu.Lock()
	f.nonblock, f.append = fl&O_NONBLOCK != 0, fl&O_APPEND != 0
	f.mu.Unlock()

	return nil
}

func (f *RemoteFD) position() int64 {
	f.mu.Lock()

	defer f.mu.Unlock()

	return f.pos
}

func (f *RemoteFD) advance(count int) {
	f.mu.Lock()
	f.pos += int64(count)
	f.mu.Unlock()
}

func (f *RemoteFD) exchange(p *Process, data []byte, files []Object, interruptible bool) (pk Packet, cancelled bool, err error) {
	if interruptible && p.interrupts(true) {
		return Packet{}, false, EINTR
	}

	a := &Answer{asker: p}

	defer a.leave()

	if _, err := p.sockSend(f.end, &Packet{data: data, files: append(files, a)}, Address{}, IoMode{quiet: true, kernel: true}); err != nil {
		return Packet{}, false, EIO
	}

	outer, was := p.ask(a)

	p.await(&a.done, time.Time{}, waitsOn)
	cancelled = p.cancelled
	p.unask(outer, was)

	return a.taken(), cancelled, nil
}

func (f *RemoteFD) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	if r, ok := r.(*FileIdentity); ok {
		r.id, r.key = f.id, f.key

		return false, nil
	}

	op := r.op()

	if op == 0 {
		return false, errUnserved
	}

	if waits && p.interrupts(true) {
		return false, EINTR
	}

	arg, data, files := r.ser(p, make([]byte, remoteHeader, remoteHeader+64))

	p.head(data, op, arg, group)

	pk, cancelled, err := f.exchange(p, data, files, waits)

	if err != nil {
		return false, err
	}

	if err := replyOf(&pk, false); err != nil {
		return cancelled, err
	}

	return cancelled, r.des(p, &pk)
}

func (f *RemoteFD) raw(p *Process, op, arg, group uint32, body []byte, files []Object, interruptible bool) (Packet, bool, error) {
	if interruptible && p.interrupts(true) {
		return Packet{}, false, EINTR
	}

	data := make([]byte, remoteHeader+len(body))

	p.head(data, op, arg, group)
	copy(data[remoteHeader:], body)

	pk, cancelled, err := f.exchange(p, data, files, interruptible)

	if err != nil {
		return Packet{}, false, err
	}

	restart := interruptible && cancelled && !p.interrupts(true)

	if err := replyOf(&pk, false); err != nil {
		return Packet{}, restart, err
	}

	dropFiles(pk.files)
	pk.files = nil

	return pk, restart, nil
}

func (f *RemoteFD) on(p *Process, op, arg uint32, body []byte) (Packet, error) {
	pk, _, err := f.raw(p, op, arg, p.group(), body, nil, false)

	return pk, err
}

func (f *RemoteFD) simple(p *Process, op, arg uint32, body []byte) error {
	_, err := f.on(p, op, arg, body)

	return err
}

func ioBody(off int64, flags uint32) [12]byte {
	var b [12]byte

	binary.LittleEndian.PutUint64(b[:], uint64(off))
	binary.LittleEndian.PutUint32(b[8:], flags)

	return b
}

func (f *RemoteFD) ioFlags(at bool) uint32 {
	flags := uint32(0)

	f.mu.Lock()

	if f.nonblock {
		flags |= ioNonblock
	}

	f.mu.Unlock()

	if at {
		flags |= ioAt
	}

	return flags
}

func (f *RemoteFD) readAt(p *Process, into Object, size int, off int64, at bool, group uint32) (int, error) {
	if off < 0 || size < 0 {
		return 0, EINVAL
	}

	if size == 0 {
		return 0, nil
	}

	body := ioBody(off, f.ioFlags(at))

	for {
		pk, cancelled, err := f.raw(p, rRead, uint32(size), group, body[:], []Object{into}, true)

		if err == EINTR && cancelled && p.interrupted() {
			continue
		}

		if err != nil {
			return 0, err
		}

		if len(pk.data) < 4 || int(binary.LittleEndian.Uint32(pk.data)) > size {
			return 0, EIO
		}

		return int(binary.LittleEndian.Uint32(pk.data)), nil
	}
}

func (f *RemoteFD) put(p *Process, from Object, size int, off int64, atEnd, at bool, group uint32) (int, int64, error) {
	if off < 0 || size < 0 {
		return 0, 0, EINVAL
	}

	if size == 0 {
		return 0, off, nil
	}

	flags := f.ioFlags(at)

	if atEnd {
		flags |= ioAppend
	}

	body := ioBody(off, flags)

	for {
		pk, cancelled, err := f.raw(p, rWrite, uint32(size), group, body[:], []Object{from}, true)

		if err == EINTR && cancelled && p.interrupted() {
			continue
		}

		if err != nil {
			return 0, 0, err
		}

		if len(pk.data) < 12 || int(binary.LittleEndian.Uint32(pk.data)) > size {
			return 0, 0, EIO
		}

		return int(binary.LittleEndian.Uint32(pk.data)), int64(binary.LittleEndian.Uint64(pk.data[4:])), nil
	}
}

func (f *RemoteFD) read(p *Process, into Object, n int, mode IoMode) (int, error) {
	if f.opath {
		return 0, EISDIR
	}

	if p.chaosFail("read") {
		return 0, EIO
	}

	count, err := p.job(func(group uint32) (int, error) {
		return f.readAt(p, into, n, f.position(), false, group)
	})

	f.advance(count)

	return count, err
}

func (f *RemoteFD) write(p *Process, from Object, n int, mode IoMode) (int, error) {
	if f.opath {
		return 0, EBADF
	}

	if p.chaosFail("write") {
		return 0, EIO
	}

	var off, end int64
	var atEnd bool

	count, err := p.job(func(group uint32) (int, error) {
		f.mu.Lock()

		off, atEnd = f.pos, f.append

		f.mu.Unlock()

		wrote, at, err := f.put(p, from, n, off, atEnd, false, group)

		end = at

		return wrote, err
	})

	if err == nil {
		if !atEnd {
			end = off
		}

		f.mu.Lock()
		f.pos = end + int64(count)
		f.mu.Unlock()
	}

	return count, err
}

func (f *RemoteFD) pread(p *Process, into Object, n int, off int64) (int, error) {
	if f.opath {
		return 0, EISDIR
	}

	return f.readAt(p, into, n, off, true, p.group())
}

func (f *RemoteFD) pwrite(p *Process, from Object, n int, off int64) (int, error) {
	if f.opath {
		return 0, EBADF
	}

	count, _, err := f.put(p, from, n, off, false, true, p.group())

	return count, err
}

func (f *RemoteFD) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (f *RemoteFD) seek(p *Process, off int64, whence int) (int64, error) {
	if f.opath {
		return 0, ESPIPE
	}

	base := int64(0)

	switch whence {
	case 0:
	case 1:
		base = f.position()
	case 2:
		var st Stat

		if err := p.fileStat(f, &st); err != nil {
			return 0, err
		}

		base = st.Size
	default:
		return 0, EINVAL
	}

	if base+off < 0 {
		return 0, EINVAL
	}

	f.mu.Lock()
	f.pos = base + off
	f.mu.Unlock()

	return base + off, nil
}

func (f *RemoteFD) copyTo(p *Process, out Object, from, to int64, n int) (int, error) {
	if f.opath {
		return 0, EBADF
	}

	return out.copyFrom(p, f, from, to, n)
}

func (f *RemoteFD) copyFrom(p *Process, in Object, from, to int64, n int) (int, error) {
	if f.opath {
		return 0, EBADF
	}

	return copyThrough(p, in, f, from, to, n)
}

func (f *RemoteFD) readdir(p *Process, into Object, size int) (int, error) {
	var after [8]byte

	binary.LittleEndian.PutUint64(after[:], uint64(f.position()))

	pk, err := f.on(p, rReaddir, 0, after[:])

	if err != nil {
		return 0, err
	}

	data := pk.data
	b := make([]byte, size)
	n := 0
	le := binary.LittleEndian

	for len(data) >= 17 {
		ino, pos := le.Uint64(data), le.Uint64(data[8:])
		kind := data[16]
		end := 17

		for end < len(data) && data[end] != 0 {
			end++
		}

		name := data[17:end]
		reclen := (19 + len(name) + 1 + 7) &^ 7

		if n+reclen > len(b) {
			break
		}

		clear(b[n : n+reclen])
		le.PutUint64(b[n:], ino)
		le.PutUint64(b[n+8:], pos)
		le.PutUint16(b[n+16:], uint16(reclen))
		b[n+18] = kind
		copy(b[n+19:], name)
		n += reclen

		f.mu.Lock()
		f.pos = int64(pos)
		f.mu.Unlock()

		data = data[min(end+1, len(data)):]
	}

	return into.with(0, n, true, func(dst []byte) (int, error) {
		return copy(dst, b[:n]), nil
	})
}

func (f *RemoteFD) ioctl(req uintptr, arg unsafe.Pointer) error {
	return ENOTTY
}

func (f *RemoteFD) epollCtl(op uint32, key EpollKey, events uint32, data uint64) error {
	return EINVAL
}

func (f *RemoteFD) epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error) {
	return 0, EINVAL
}
