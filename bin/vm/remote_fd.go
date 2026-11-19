//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

type RemoteFD struct {
	end  *PairEnd
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
	group    int32
	tostop   bool

	locks  *LockTable
	locked atomic.Bool
}

func (f *RemoteFD) revive() bool {
	for {
		r := f.refs.Load()

		if r == 0 {
			return false
		}

		if f.refs.CompareAndSwap(r, r+1) {
			return true
		}
	}
}

func (f *RemoteFD) ref() {
	f.refs.Add(1)
}

func (f *RemoteFD) unref() {
	if f.refs.Add(-1) == 0 {
		if f.locked.Load() {
			f.locks.forget(f)
		}

		f.end.unref()
	}
}

func (f *RemoteFD) asFile() File {
	return f
}

func (f *RemoteFD) asTransfer() Transfer {
	if f.opath {
		return nil
	}

	return f
}

func (f *RemoteFD) asSeeker() Seeker {
	if f.opath {
		return nil
	}

	return f
}

func (f *RemoteFD) asDir() Dir {
	return f
}

func (f *RemoteFD) asTerminal() Terminal {
	return f
}

func (f *RemoteFD) asWindowed() Windowed {
	if f.opath {
		return nil
	}

	return f
}

func (f *RemoteFD) asLink() Link {
	if !f.opath {
		return nil
	}

	return f
}

func (f *RemoteFD) asAttrs() Attrs {
	return f
}

func (f *RemoteFD) asSocket() Socket {
	return nil
}

func (f *RemoteFD) asIoctl() Ioctl {
	return nil
}

func (f *RemoteFD) asLocker() Locker {
	if f.opath {
		return nil
	}

	return f
}

func (f *RemoteFD) asEpoll() *Epoll {
	return nil
}

func (f *RemoteFD) asMemfd() *MemfdFile {
	return nil
}

func (f *RemoteFD) asPair() *PairEnd {
	return nil
}

func (f *RemoteFD) asListener() *UnixListener {
	return nil
}

func (f *RemoteFD) asConsole() *ConsoleEnd {
	return nil
}

func (f *RemoteFD) asRemote() *RemoteFD {
	return f
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

func (f *RemoteFD) poll(events int16) int16 {
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

	if len(pk.data) >= remoteHeader+8 && binary.LittleEndian.Uint32(pk.data) == rReady {
		f.mu.Lock()
		f.group = int32(binary.LittleEndian.Uint32(pk.data[remoteHeader:]))
		f.tostop = binary.LittleEndian.Uint32(pk.data[remoteHeader+4:])&readyTostop != 0
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

	if f.end.kernelSend(p, &Packet{data: data, files: append(files, a)}) != nil {
		return Packet{}, false, EIO
	}

	outer, was := p.ask(a)

	p.await(&a.done, time.Time{}, waitsOn)
	cancelled = p.cancelled
	p.unask(outer, was)

	return a.taken(), cancelled, nil
}

func (f *RemoteFD) waiting(p *Process, op, arg uint32, body []byte, files []Object) (pk Packet, restart bool, err error) {
	if p.interrupts(true) {
		return Packet{}, false, EINTR
	}

	data := make([]byte, remoteHeader+len(body))

	p.head(data, op, arg)
	copy(data[remoteHeader:], body)

	pk, cancelled, err := f.exchange(p, data, files, true)

	if err != nil {
		return Packet{}, false, err
	}

	err = replyOf(&pk, false)

	dropFiles(pk.files)
	pk.files = nil

	return pk, cancelled && !p.interrupts(true), err
}

func (f *RemoteFD) call(p *Process, op, arg uint32, body []byte, files []Object) (Packet, error) {
	data := make([]byte, remoteHeader+len(body))

	p.head(data, op, arg)
	copy(data[remoteHeader:], body)

	return f.asked(p, op, data, files)
}

func (f *RemoteFD) asked(p *Process, op uint32, data []byte, files []Object) (Packet, error) {
	pk, _, err := f.exchange(p, data, files, false)

	if err != nil {
		return Packet{}, err
	}

	if err := replyOf(&pk, false); err != nil {
		return Packet{}, err
	}

	if (op == vOpen || op == vConnect || op == rOpen) && len(pk.files) > 0 {
		dropFiles(pk.files[1:])
		pk.files = pk.files[:1]
	} else {
		dropFiles(pk.files)
		pk.files = nil
	}

	return pk, nil
}

func (f *RemoteFD) on(p *Process, op, arg uint32, body []byte) (Packet, error) {
	return f.call(p, op, arg, body, nil)
}

func (f *RemoteFD) simple(p *Process, op, arg uint32, body []byte) error {
	_, err := f.on(p, op, arg, body)

	return err
}

func (f *RemoteFD) waits(p *Process, op, arg uint32, body []byte) error {
	for {
		_, restart, err := f.waiting(p, op, arg, body, nil)

		if err == EINTR && restart {
			continue
		}

		return err
	}
}

func (f *RemoteFD) vfs(p *Process, id uint64, op, arg uint32, body []byte, files ...Object) (Packet, error) {
	data := make([]byte, remoteHeader+8+len(body))

	p.head(data, op, arg)
	binary.LittleEndian.PutUint64(data[remoteHeader:], p.rootID(id))
	copy(data[remoteHeader+8:], body)

	return f.asked(p, op, data, files)
}

func (f *RemoteFD) stat(p *Process, st *Stat) error {
	pk, err := f.on(p, rFstat, 0, nil)

	if err != nil {
		return err
	}

	return decodeStat(pk.data, st)
}

func (f *RemoteFD) statfs(p *Process, st *Statfs) error {
	pk, err := f.on(p, rFstatfs, 0, nil)

	if err != nil {
		return err
	}

	return decodeStatfs(pk.data, st)
}

func (f *RemoteFD) truncate(p *Process, size int64) error {
	var body [8]byte

	binary.LittleEndian.PutUint64(body[:], uint64(size))

	return f.simple(p, rFtruncate, 0, body[:])
}

func (f *RemoteFD) sync(p *Process, data bool) error {
	arg := uint32(0)

	if data {
		arg = 1
	}

	return f.simple(p, rFsync, arg, nil)
}

func (f *RemoteFD) chmod(p *Process, mode uint32) error {
	return f.simple(p, rFchmod, mode, nil)
}

func (f *RemoteFD) chown(p *Process, uid, gid int) error {
	var body [8]byte

	binary.LittleEndian.PutUint32(body[:], uint32(uid))
	binary.LittleEndian.PutUint32(body[4:], uint32(gid))

	return f.simple(p, rFchown, 0, body[:])
}

func (f *RemoteFD) utimes(p *Process, ts []Timespec) error {
	when, body := whens(ts)

	return f.simple(p, rFutimes, when, body)
}

func (f *RemoteFD) flock(p *Process, op int) error {
	if f.locks == nil {
		return EINVAL
	}

	switch kind := op &^ LOCK_NB; kind {
	case LOCK_UN:
		f.locks.unlock(f, false, 0, 0)

		return nil
	case LOCK_SH, LOCK_EX:
		return f.locks.lock(p, &FileLock{owner: f, write: kind == LOCK_EX}, op&LOCK_NB == 0)
	}

	return EINVAL
}

func (f *RemoteFD) recordLock(p *Process, cmd int, lk *Flock) error {
	if f.locks == nil {
		return EINVAL
	}

	start, length := lk.Start, lk.Len

	switch lk.Whence {
	case 0:
	case 1:
		start += f.position()
	case 2:
		var st Stat

		if err := f.stat(p, &st); err != nil {
			return err
		}

		start += st.Size
	default:
		return EINVAL
	}

	if length < 0 {
		start, length = start+length, -length
	}

	if start < 0 || lk.Type != F_RDLCK && lk.Type != F_WRLCK && lk.Type != F_UNLCK {
		return EINVAL
	}

	want := &FileLock{owner: f, ranged: true, write: lk.Type == F_WRLCK, start: uint64(start), stop: lockPast}

	if length != 0 && want.start <= lockPast-uint64(length) {
		want.stop = want.start + uint64(length)
	}

	switch {
	case cmd != fOfdGetlk && (lk.Type == F_RDLCK && f.accmode == O_WRONLY || lk.Type == F_WRLCK && f.accmode == O_RDONLY):
		return EBADF
	case cmd == fOfdGetlk:
		lk.Whence, lk.Pid = 0, -1

		way, found := f.locks.way(want)

		if !found {
			lk.Type, lk.Start, lk.Len = F_UNLCK, start, length

			return nil
		}

		lk.Type, lk.Start, lk.Len = F_RDLCK, int64(way.start), 0

		if way.write {
			lk.Type = F_WRLCK
		}

		if way.stop != lockPast {
			lk.Len = int64(way.stop - way.start)
		}

		return nil
	case lk.Type == F_UNLCK:
		f.locks.unlock(f, true, want.start, want.stop)

		return nil
	}

	return f.locks.lock(p, want, cmd == fOfdSetlkw)
}

func ioBody(off int64, woff, flags uint32) [16]byte {
	var b [16]byte

	binary.LittleEndian.PutUint64(b[:], uint64(off))
	binary.LittleEndian.PutUint32(b[8:], woff)
	binary.LittleEndian.PutUint32(b[12:], flags)

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

func (f *RemoteFD) readAt(p *Process, b []byte, off int64, at bool) (int, error) {
	if off < 0 || len(b) == 0 {
		return f.readInto(p, nil, 0, 0, off, at)
	}

	w := window(p, b, O_WRONLY)
	count, err := f.readInto(p, w, 0, len(b), off, at)

	w.unref()

	return count, err
}

func (f *RemoteFD) readInto(p *Process, w *MemfdFile, woff uint32, size int, off int64, at bool) (int, error) {
	if off < 0 {
		return 0, EINVAL
	}

	if size == 0 {
		return 0, nil
	}

	body := ioBody(off, woff, f.ioFlags(at))

	for {
		pk, cancelled, err := f.waiting(p, rRead, uint32(size), body[:], []Object{w})

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

func (f *RemoteFD) writeAt(p *Process, b []byte, off int64) (int, error) {
	count, _, err := f.put(p, b, off, false, true)

	return count, err
}

func (f *RemoteFD) put(p *Process, b []byte, off int64, atEnd, at bool) (int, int64, error) {
	if off < 0 || len(b) == 0 {
		return f.writeFrom(p, nil, 0, 0, off, atEnd, at)
	}

	w := window(p, b, O_RDONLY)
	count, end, err := f.writeFrom(p, w, 0, len(b), off, atEnd, at)

	w.unref()

	return count, end, err
}

func (f *RemoteFD) writeFrom(p *Process, w *MemfdFile, woff uint32, size int, off int64, atEnd, at bool) (int, int64, error) {
	if off < 0 {
		return 0, 0, EINVAL
	}

	if size == 0 {
		return 0, off, nil
	}

	flags := f.ioFlags(at)

	if atEnd {
		flags |= ioAppend
	}

	body := ioBody(off, woff, flags)

	for {
		pk, cancelled, err := f.waiting(p, rWrite, uint32(size), body[:], []Object{w})

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

func (f *RemoteFD) read(p *Process, b []byte, mode IoMode) (int, error) {
	if p.chaosFail("read") {
		return 0, EIO
	}

	if err := p.background(f, SIGTTIN); err != nil {
		return 0, err
	}

	count, err := f.readAt(p, b, f.position(), false)

	f.advance(count)

	return count, err
}

func (f *RemoteFD) write(p *Process, b []byte, mode IoMode) (int, error) {
	if p.chaosFail("write") {
		return 0, EIO
	}

	if _, tostop, _ := f.jobs(); tostop {
		if err := p.background(f, SIGTTOU); err != nil {
			return 0, err
		}
	}

	f.mu.Lock()

	off, atEnd := f.pos, f.append

	f.mu.Unlock()

	count, end, err := f.put(p, b, off, atEnd, false)

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

func (f *RemoteFD) pread(p *Process, b []byte, off int64) (int, error) {
	return f.readAt(p, b, off, true)
}

func (f *RemoteFD) pwrite(p *Process, b []byte, off int64) (int, error) {
	return f.writeAt(p, b, off)
}

func (f *RemoteFD) seek(p *Process, off int64, whence int) (int64, error) {
	base := int64(0)

	switch whence {
	case 0:
	case 1:
		base = f.position()
	case 2:
		var st Stat

		if err := f.stat(p, &st); err != nil {
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

func (f *RemoteFD) readWindow(p *Process, w *MemfdFile, woff uint32, size int, from *int64) (int, error) {
	if from != nil {
		count, err := f.readInto(p, w, woff, size, *from, true)

		*from += int64(count)

		return count, err
	}

	count, err := f.readInto(p, w, woff, size, f.position(), false)

	f.advance(count)

	return count, err
}

func (f *RemoteFD) writeWindow(p *Process, w *MemfdFile, woff uint32, size int, to *int64) (int, error) {
	if fl, _ := f.flags(); fl&O_APPEND != 0 {
		return 0, EBADF
	}

	if to != nil {
		count, _, err := f.writeFrom(p, w, woff, size, *to, false, true)

		*to += int64(count)

		return count, err
	}

	count, _, err := f.writeFrom(p, w, woff, size, f.position(), false, false)

	f.advance(count)

	return count, err
}

func (f *RemoteFD) readdir(p *Process, b []byte) (int, error) {
	var after [8]byte

	binary.LittleEndian.PutUint64(after[:], uint64(f.position()))

	pk, err := f.on(p, rReaddir, 0, after[:])

	if err != nil {
		return 0, err
	}

	data := pk.data
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

	return n, nil
}

func (f *RemoteFD) readlink(p *Process) (string, error) {
	pk, err := f.vfs(p, f.id, vReadlink, 0, nil)

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

func flagOf(follow bool) uint32 {
	if follow {
		return 0
	}

	return AT_SYMLINK_NOFOLLOW
}

func (f *RemoteFD) openPath(p *Process, path string, flags int, mode uint32) (Object, error) {
	if p.chaosFail("openat") {
		return nil, EIO
	}

	body := make([]byte, 4, 4+len(path))

	binary.LittleEndian.PutUint32(body, mode)

	pk, err := f.vfs(p, f.id, vOpen, uint32(flags), append(body, path...))

	if err != nil {
		return nil, err
	}

	if pk.arg&openControlling != 0 {
		dropFiles(pk.files)

		return p.openControlling(flags)
	}

	if len(pk.files) == 0 {
		var st Stat

		if err := decodeStat(pk.data, &st); err != nil || st.Mode&S_IFMT != S_IFIFO {
			return nil, EIO
		}

		return p.openFifo(&st, flags)
	}

	obj, terminal, err := p.opened(&pk)

	if err != nil {
		return nil, err
	}

	if flags&O_DIRECTORY != 0 && obj.asDir() == nil {
		obj.unref()

		return nil, ENOTDIR
	}

	if terminal && flags&O_NOCTTY == 0 {
		p.acquire(obj.asTerminal())
	}

	return obj, nil
}

func (f *RemoteFD) statPath(p *Process, path string, follow bool, st *Stat) error {
	if p.chaosFail("file-stat") {
		return EIO
	}

	arg := uint32(0)

	if follow {
		arg = 1
	}

	pk, err := f.vfs(p, f.id, vStat, arg, []byte(path))

	if err != nil {
		return err
	}

	return decodeStat(pk.data, st)
}

func (f *RemoteFD) readlinkPath(p *Process, path string) (string, error) {
	pk, err := f.vfs(p, f.id, vReadlink, 0, []byte(path))

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

func (f *RemoteFD) mkdirPath(p *Process, path string, mode uint32) error {
	_, err := f.vfs(p, f.id, vMkdir, mode, []byte(path))

	return err
}

func (f *RemoteFD) mknodPath(p *Process, path string, mode uint32) error {
	_, err := f.vfs(p, f.id, vMknod, mode, []byte(path))

	return err
}

func (f *RemoteFD) symlinkPath(p *Process, target, path string) error {
	_, err := f.vfs(p, f.id, vSymlink, 0, append(append([]byte(target), 0), path...))

	return err
}

func (f *RemoteFD) unlinkPath(p *Process, path string, dir bool) error {
	flags := uint32(0)

	if dir {
		flags = AT_REMOVEDIR
	}

	_, err := f.vfs(p, f.id, vUnlink, flags, []byte(path))

	return err
}

func twoPaths(to Dir, old, new string) ([]byte, error) {
	h := to.asRemote()

	if h == nil {
		return nil, EXDEV
	}

	body := make([]byte, 8, 8+len(old)+1+len(new))

	binary.LittleEndian.PutUint64(body, h.id)

	return append(append(append(body, old...), 0), new...), nil
}

func (f *RemoteFD) renamePath(p *Process, old string, to Dir, new string) error {
	body, err := twoPaths(to, old, new)

	if err == nil {
		_, err = f.vfs(p, f.id, vRename, 0, body)
	}

	return err
}

func (f *RemoteFD) linkPath(p *Process, old string, to Dir, new string, follow bool) error {
	body, err := twoPaths(to, old, new)
	arg := uint32(0)

	if follow {
		arg = 1
	}

	if err == nil {
		_, err = f.vfs(p, f.id, vLink, arg, body)
	}

	return err
}

func (f *RemoteFD) chmodPath(p *Process, path string, mode uint32, follow bool) error {
	body := make([]byte, 4, 4+len(path))

	binary.LittleEndian.PutUint32(body, flagOf(follow))

	_, err := f.vfs(p, f.id, vChmod, mode, append(body, path...))

	return err
}

func (f *RemoteFD) chownPath(p *Process, path string, uid, gid int, follow bool) error {
	body := make([]byte, 12, 12+len(path))

	binary.LittleEndian.PutUint32(body, flagOf(follow))
	binary.LittleEndian.PutUint32(body[4:], uint32(uid))
	binary.LittleEndian.PutUint32(body[8:], uint32(gid))

	_, err := f.vfs(p, f.id, vChown, 0, append(body, path...))

	return err
}

func (f *RemoteFD) utimesPath(p *Process, path string, ts []Timespec, follow bool) error {
	when, times := whens(ts)
	body := make([]byte, 4, 4+len(times)+len(path))

	binary.LittleEndian.PutUint32(body, flagOf(follow))

	_, err := f.vfs(p, f.id, vUtimes, when, append(append(body, times...), path...))

	return err
}

func (f *RemoteFD) truncatePath(p *Process, path string, size int64) error {
	body := make([]byte, 8, 8+len(path))

	binary.LittleEndian.PutUint64(body, uint64(size))

	_, err := f.vfs(p, f.id, vTruncate, 0, append(body, path...))

	return err
}

func (f *RemoteFD) statfsPath(p *Process, path string, st *Statfs) error {
	pk, err := f.vfs(p, f.id, vStatfs, 0, []byte(path))

	if err != nil {
		return err
	}

	return decodeStatfs(pk.data, st)
}

func (f *RemoteFD) realpathOf(p *Process, path string) (string, error) {
	pk, err := f.vfs(p, f.id, vRealpath, 0, []byte(path))

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

func (f *RemoteFD) path(p *Process) (string, error) {
	pk, err := f.vfs(p, f.id, vPath, 0, nil)

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

func (f *RemoteFD) mountPath(p *Process, target, source string, conn Object) error {
	body := append(append([]byte(target), 0), source...)

	if conn != nil {
		_, err := f.vfs(p, f.id, vMount, 0, body, conn)

		return err
	}

	_, err := f.vfs(p, f.id, vMount, 0, body)

	return err
}

func (f *RemoteFD) bindPath(p *Process, path string, mode uint32, held Object) error {
	_, err := f.vfs(p, f.id, vBind, mode, []byte(path), held)

	return err
}

func (f *RemoteFD) connectPath(p *Process, path string) (Object, error) {
	pk, err := f.vfs(p, f.id, vConnect, 0, []byte(path))

	if err != nil {
		return nil, err
	}

	if len(pk.files) == 0 {
		return nil, ECONNREFUSED
	}

	return pk.files[0], nil
}

func (f *RemoteFD) unmountPath(p *Process, path string, flags uint32) error {
	_, err := f.vfs(p, f.id, vUnmount, flags, []byte(path))

	return err
}

func (f *RemoteFD) count(p *Process, op uint32) (uint32, error) {
	pk, err := f.on(p, op, 0, nil)

	if err == nil && len(pk.data) != 4 {
		err = EIO
	}

	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint32(pk.data), nil
}

func (f *RemoteFD) device() FileKey {
	return f.key
}

func (f *RemoteFD) jobs() (int32, bool, bool) {
	f.mu.Lock()

	defer f.mu.Unlock()

	return f.group, f.tostop, f.ready&POLLHUP != 0
}

func (f *RemoteFD) getattr(p *Process) ([]byte, error) {
	pk, err := f.on(p, rTcgetattr, 0, nil)

	if err == nil && len(pk.data) != sizeofTermios {
		err = EIO
	}

	return bytes.Clone(pk.data), err
}

func (f *RemoteFD) setattr(p *Process, when uint32, termios []byte) error {
	if when == 0 {
		return f.simple(p, rTcsetattr, when, termios)
	}

	return f.waits(p, rTcsetattr, when, termios)
}

func (f *RemoteFD) flush(p *Process, queue uint32) error {
	return f.simple(p, rTcflush, queue, nil)
}

func (f *RemoteFD) flow(p *Process, action uint32) error {
	return f.simple(p, rTcflow, action, nil)
}

func (f *RemoteFD) drain(p *Process) error {
	return f.waits(p, rTcdrain, 0, nil)
}

func (f *RemoteFD) window(p *Process) ([]byte, error) {
	pk, err := f.on(p, rGetWinsize, 0, nil)

	if err == nil && len(pk.data) != sizeofWinsize {
		err = EIO
	}

	return bytes.Clone(pk.data), err
}

func (f *RemoteFD) setWindow(p *Process, size []byte) error {
	return f.simple(p, rSetWinsize, 0, size)
}

func (f *RemoteFD) pending(p *Process) (uint32, error) {
	return f.count(p, rInq)
}

func (f *RemoteFD) unsent(p *Process) (uint32, error) {
	return f.count(p, rOutq)
}

func (f *RemoteFD) foreground(p *Process) (uint32, error) {
	return f.count(p, rGetPgrp)
}

func (f *RemoteFD) setForeground(p *Process, pgrp uint32) error {
	return f.simple(p, rSetPgrp, pgrp, nil)
}

func (f *RemoteFD) control(p *Process, sid, pgrp int32) error {
	var body [4]byte

	binary.LittleEndian.PutUint32(body[:], uint32(pgrp))

	return f.simple(p, rSetCtty, uint32(sid), body[:])
}

func (f *RemoteFD) reopen(p *Process, flags int) (Terminal, error) {
	pk, err := f.call(p, rOpen, uint32(flags), nil, nil)

	if err != nil {
		return nil, err
	}

	if len(pk.files) != 1 {
		dropFiles(pk.files)

		return nil, EIO
	}

	obj, _, err := p.opened(&pk)

	if err != nil {
		return nil, err
	}

	if r := obj.asTerminal(); r != nil {
		return r, nil
	}

	obj.unref()

	return nil, EIO
}

func (f *RemoteFD) name(p *Process, peer bool) (string, error) {
	arg := uint32(0)

	if peer {
		arg = 1
	}

	pk, err := f.on(p, rName, arg, nil)

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}
