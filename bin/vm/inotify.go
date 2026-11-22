//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

var inotifyInodes atomic.Uint64

const (
	inotifyQueueMax = 16384
	inotifyRecord   = 4096
)

type Inotify struct {
	mu       sync.Mutex
	watches  map[int32]*InotifyWatch
	next     int32
	queue    []byte
	queued   int
	last     int
	changed  bool
	watchers Subscribers
	refs     atomic.Int32
	nonblock atomic.Bool
	ino      uint64
	made     time.Time
}

func newInotify(nonblock bool) *Inotify {
	in := &Inotify{watches: map[int32]*InotifyWatch{}, next: 1, last: -1, ino: inotifyInodes.Add(1), made: time.Now()}

	in.refs.Store(1)
	in.nonblock.Store(nonblock)

	return in
}

func (in *Inotify) ref() {
	in.refs.Add(1)
}

func (in *Inotify) unref() {
	if in.refs.Add(-1) != 0 {
		return
	}

	in.mu.Lock()

	watches := in.watches

	in.watches = map[int32]*InotifyWatch{}
	in.mu.Unlock()

	for _, w := range watches {
		in.release(w)
	}
}

func (in *Inotify) release(w *InotifyWatch) {
	w.end.unsubscribe(&w.sub)
	w.end.unref()
	w.dir.unref()
}

func (in *Inotify) ring() {
	var buf [4]*Hook

	in.mu.Lock()

	in.changed = true

	hooks := in.watchers.hooks(buf[:0])

	in.mu.Unlock()

	for _, h := range hooks {
		h.sub.to.wake(h)
	}
}

func (in *Inotify) snapshot() []*InotifyWatch {
	watches := make([]*InotifyWatch, 0, len(in.watches))

	for _, w := range in.watches {
		watches = append(watches, w)
	}

	return watches
}

func (in *Inotify) poll(events int16, through []*Epoll) int16 {
	in.mu.Lock()

	ready := len(in.queue) > 0
	watches := in.snapshot()

	in.mu.Unlock()

	for _, w := range watches {
		if ready {
			break
		}

		ready = w.end.poll(POLLIN, through)&POLLIN != 0
	}

	if ready {
		return (POLLIN | POLLRDNORM) & events
	}

	return 0
}

func (in *Inotify) subscribe(s *Subscription, events int16) {
	if events&(POLLIN|POLLRDNORM) == 0 {
		return
	}

	in.mu.Lock()
	in.watchers.add(&s.in)
	in.mu.Unlock()
}

func (in *Inotify) unsubscribe(s *Subscription) {
	in.mu.Lock()
	in.watchers.remove(&s.in)
	in.mu.Unlock()
}

func (in *Inotify) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: in.ino, Mode: 0o600, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = nsecToTimespec(in.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (in *Inotify) put(wd int32, mask uint32, name string) {
	size := 0

	if name != "" {
		size = (len(name) + 1 + 15) &^ 15
	}

	if in.last >= 0 {
		e := in.queue[in.last:]

		if int32(binary.LittleEndian.Uint32(e)) == wd && binary.LittleEndian.Uint32(e[4:]) == mask &&
			int(binary.LittleEndian.Uint32(e[12:])) == size && cstring(e[16:16+size]) == name {
			return
		}
	}

	if in.queued >= inotifyQueueMax {
		return
	}

	in.queued++

	if in.queued == inotifyQueueMax {
		wd, mask, name, size = -1, IN_Q_OVERFLOW, "", 0
	}

	in.last = len(in.queue)
	in.queue = binary.LittleEndian.AppendUint32(in.queue, uint32(wd))
	in.queue = binary.LittleEndian.AppendUint32(in.queue, mask)
	in.queue = binary.LittleEndian.AppendUint32(in.queue, 0)
	in.queue = binary.LittleEndian.AppendUint32(in.queue, uint32(size))
	in.queue = append(in.queue, name...)
	in.queue = append(in.queue, make([]byte, size-len(name))...)
}

func cstring(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}

	return string(b)
}

func (in *Inotify) drain(p *Process, w *InotifyWatch) error {
	buf := make([]byte, inotifyRecord)

	for {
		n, err := p.fileRead(w.end, bytesFile(buf), len(buf), IoMode{nonblock: true, kernel: true})

		if err == EAGAIN {
			return nil
		}

		if err != nil {
			return err
		}

		if n == 0 {
			in.mu.Lock()

			if in.watches[w.wd] == w {
				delete(in.watches, w.wd)
				in.tell(w, IN_DELETE_SELF, "")
				in.put(w.wd, IN_IGNORED, "")
			}

			in.mu.Unlock()
			in.release(w)

			return nil
		}

		for at := 0; at < n; {
			kind := buf[at]
			name := cstring(buf[at+1 : n])

			at += len(name) + 2

			switch kind {
			case WATCH_ADDED:
				var st Stat

				dir := p.dirStat(w.dir, name, false, &st) == nil && st.Mode&S_IFMT == S_IFDIR

				in.mu.Lock()
				w.names[name] = dir
				in.tell(w, IN_CREATE|isdir(dir), name)
				in.mu.Unlock()
			case WATCH_REMOVED:
				in.mu.Lock()

				dir := w.names[name]

				delete(w.names, name)
				in.tell(w, IN_DELETE|isdir(dir), name)
				in.mu.Unlock()
			}
		}
	}
}

func isdir(dir bool) uint32 {
	if dir {
		return IN_ISDIR
	}

	return 0
}

func (in *Inotify) tell(w *InotifyWatch, mask uint32, name string) {
	if w.mask&mask&IN_ALL_EVENTS == 0 || in.watches[w.wd] != w && mask != IN_DELETE_SELF {
		return
	}

	in.put(w.wd, mask, name)

	if w.mask&IN_ONESHOT != 0 && mask != IN_DELETE_SELF {
		delete(in.watches, w.wd)
		in.put(w.wd, IN_IGNORED, "")

		go in.release(w)
	}
}

func (in *Inotify) add(p *Process, path string, mask uint32) (int32, error) {
	flags := O_RDONLY | O_DIRECTORY

	if mask&IN_DONT_FOLLOW != 0 {
		flags |= O_NOFOLLOW
	}

	dir, err := p.openPath(path, flags, 0)

	if err == ELOOP && mask&IN_DONT_FOLLOW != 0 {
		err = ENOTDIR
	}

	if err != nil {
		return 0, err
	}

	wd, err := in.watch(p, dir, mask)

	if err != nil || in.owner(wd) != dir {
		dir.unref()
	}

	return wd, err
}

func (in *Inotify) owner(wd int32) Object {
	in.mu.Lock()

	defer in.mu.Unlock()

	if w := in.watches[wd]; w != nil {
		return w.dir
	}

	return nil
}

func (in *Inotify) watch(p *Process, dir Object, mask uint32) (int32, error) {
	var st Stat

	if err := p.dirStat(dir, "", true, &st); err != nil {
		return 0, err
	}

	if wd, ok, err := in.change(st.Dev, st.Ino, mask); ok {
		return wd, err
	}

	r := DirWatch{flags: O_RDONLY | O_NONBLOCK}

	if err := p.request(dir, &r); err != nil {
		return 0, err
	}

	w := &InotifyWatch{in: in, mask: mask &^ (IN_MASK_ADD | IN_MASK_CREATE), dev: st.Dev, ino: st.Ino, dir: dir,
		end: r.file, names: map[string]bool{}}

	if err := in.existing(p, w); err != nil {
		w.end.unref()

		return 0, err
	}

	in.mu.Lock()

	if wd, ok, err := in.changeLocked(st.Dev, st.Ino, mask); ok {
		in.mu.Unlock()
		w.end.unref()

		return wd, err
	}

	w.wd = in.next
	in.next++
	in.watches[w.wd] = w
	in.mu.Unlock()

	w.sub.init(w, 0)
	w.end.subscribe(&w.sub, POLLIN)

	return w.wd, nil
}

func (in *Inotify) existing(p *Process, w *InotifyWatch) error {
	buf := make([]byte, inotifyRecord)

	for {
		n, err := p.fileRead(w.end, bytesFile(buf), len(buf), IoMode{nonblock: true, kernel: true})

		if err != nil {
			return err
		}

		if n == 0 {
			return ENOENT
		}

		for at := 0; at < n; {
			kind := buf[at]
			name := cstring(buf[at+1 : n])

			at += len(name) + 2

			switch kind {
			case WATCH_EXISTING:
				var st Stat

				w.names[name] = p.dirStat(w.dir, name, false, &st) == nil && st.Mode&S_IFMT == S_IFDIR
			case WATCH_IDLE:
				return nil
			}
		}
	}
}

func (in *Inotify) change(dev, ino uint64, mask uint32) (int32, bool, error) {
	in.mu.Lock()

	defer in.mu.Unlock()

	return in.changeLocked(dev, ino, mask)
}

func (in *Inotify) changeLocked(dev, ino uint64, mask uint32) (int32, bool, error) {
	for _, w := range in.watches {
		if w.dev != dev || w.ino != ino {
			continue
		}

		if mask&IN_MASK_CREATE != 0 {
			return 0, true, EEXIST
		}

		if mask&IN_MASK_ADD != 0 {
			w.mask |= mask &^ IN_MASK_ADD
		} else {
			w.mask = mask
		}

		return w.wd, true, nil
	}

	return 0, false, nil
}

func (in *Inotify) remove(wd int32) error {
	in.mu.Lock()

	w := in.watches[wd]

	if w == nil {
		in.mu.Unlock()

		return EINVAL
	}

	delete(in.watches, wd)
	in.put(wd, IN_IGNORED, "")

	hooks := in.watchers.hooks(nil)

	in.mu.Unlock()
	in.release(w)

	for _, h := range hooks {
		h.sub.to.wake(h)
	}

	return nil
}

func (in *Inotify) take(into *MemfdFile, n int) (int, error) {
	size := 0

	for size < len(in.queue) {
		next := size + 16 + int(binary.LittleEndian.Uint32(in.queue[size+12:]))

		if next > n {
			break
		}

		size = next
	}

	if size == 0 {
		return 0, EINVAL
	}

	got, err := into.with(0, size, true, func(b []byte) (int, error) {
		return copy(b, in.queue[:size]), nil
	})

	if err != nil {
		return 0, err
	}

	for at := 0; at < size; at += 16 + int(binary.LittleEndian.Uint32(in.queue[at+12:])) {
		in.queued--
	}

	in.queue = append(in.queue[:0], in.queue[size:]...)
	in.last = -1

	if len(in.queue) > 0 {
		in.last = 0

		for at := 0; at < len(in.queue); at += 16 + int(binary.LittleEndian.Uint32(in.queue[at+12:])) {
			in.last = at
		}
	}

	return got, nil
}

func (in *Inotify) look(p *Process) error {
	in.mu.Lock()

	in.changed = false

	watches := in.snapshot()

	in.mu.Unlock()

	for _, w := range watches {
		if err := in.drain(p, w); err != nil {
			return err
		}
	}

	return nil
}

func (in *Inotify) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
	for {
		if err := in.look(p); err != nil {
			return 0, err
		}

		in.mu.Lock()

		if len(in.queue) > 0 {
			got, err := in.take(into, n)

			in.mu.Unlock()

			return got, err
		}

		if in.changed {
			in.mu.Unlock()

			continue
		}

		if mode.nonblock || in.nonblock.Load() {
			in.mu.Unlock()

			return 0, EAGAIN
		}

		_, err := p.watch(&in.mu, &in.watchers, false, time.Time{}, restarts)

		in.mu.Unlock()

		if err != nil {
			return 0, err
		}
	}
}

func (in *Inotify) ioctl(p *Process, req uintptr, arg unsafe.Pointer) error {
	if req != FIONREAD {
		return ENOTTY
	}

	if err := in.look(p); err != nil {
		return err
	}

	in.mu.Lock()
	*(*int32)(arg) = int32(len(in.queue))
	in.mu.Unlock()

	return nil
}

func (in *Inotify) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, in.stat(p, r.st)
	case *FileGetfl:
		r.flags = O_RDONLY

		if in.nonblock.Load() {
			r.flags |= O_NONBLOCK
		}

		return false, nil
	case *FileSetfl:
		in.nonblock.Store(r.flags&O_NONBLOCK != 0)

		return false, nil
	case *FileRead:
		if r.explicit {
			return false, ESPIPE
		}

		var err error

		r.count, err = in.read(p, r.into, r.n, r.mode)

		return false, err
	case *FileWrite:
		return false, EBADF
	case *FileIoctl:
		return false, in.ioctl(p, r.req, r.arg)
	case *InotifyAdd:
		var err error

		r.wd, err = in.add(p, r.path, r.mask)

		return false, err
	case *InotifyRemove:
		return false, in.remove(r.wd)
	}

	return false, errUnserved
}
