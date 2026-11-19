//go:build linux && amd64

package main

import (
	"math"
	"sync"
	"time"
)

const lockPast = math.MaxUint64

type FileLock struct {
	owner       *RemoteNode
	ranged      bool
	write       bool
	start, stop uint64
	granted     bool
	next        *FileLock
}

type LockedFile struct {
	held, waiting *FileLock
	given         Waiters[Turn]
}

type LockTable struct {
	mu    sync.Mutex
	files map[FileKey]*LockedFile
}

func (a *FileLock) crossing(b *FileLock) bool {
	if a.owner == b.owner || a.ranged != b.ranged {
		return false
	}

	if !a.ranged {
		return a.write || b.write
	}

	return a.start < b.stop && b.start < a.stop && (a.write || b.write)
}

func (f *LockedFile) inWay(want *FileLock) *FileLock {
	for l := f.held; l != nil; l = l.next {
		if l.crossing(want) {
			return l
		}
	}

	return nil
}

func (f *LockedFile) ready(want, before *FileLock) bool {
	if f.inWay(want) != nil {
		return false
	}

	for w := f.waiting; w != before; w = w.next {
		if w.crossing(want) {
			return false
		}
	}

	return true
}

func (f *LockedFile) drop(owner *RemoteNode, ranged bool, start, stop uint64) {
	for at := &f.held; *at != nil; {
		l := *at

		switch {
		case l.owner != owner || l.ranged != ranged || ranged && (l.stop <= start || l.start >= stop):
			at = &l.next
		case ranged && l.start < start && l.stop > stop:

			rest := new(FileLock)

			*rest = *l
			rest.start, l.stop, l.next = stop, start, rest

			at = &rest.next
		case ranged && l.start < start:
			l.stop = start
			at = &l.next
		case ranged && l.stop > stop:
			l.start = stop
			at = &l.next
		default:
			*at = l.next
		}
	}
}

func (f *LockedFile) hold(want *FileLock) {
	f.drop(want.owner, want.ranged, want.start, want.stop)
	want.next, f.held = f.held, want
}

func (f *LockedFile) leave(w *FileLock) {
	for at := &f.waiting; *at != nil; at = &(*at).next {
		if *at == w {
			*at = w.next

			return
		}
	}
}

func (f *LockedFile) give() bool {
	given := false

	for w := f.waiting; w != nil; {
		if !f.ready(w, w) {
			w = w.next

			continue
		}

		f.leave(w)
		f.hold(w)
		w.granted, given = true, true
		w = f.waiting
	}

	if given {
		f.given.wake(Turn{})
	}

	return given
}

func (t *LockTable) settle(key FileKey, f *LockedFile) {
	f.give()

	if f.held == nil && f.waiting == nil {
		delete(t.files, key)
	}
}

func (t *LockTable) lock(p *Process, want *FileLock, waits bool) error {
	n := want.owner

	n.locked.Store(true)
	t.mu.Lock()

	f := t.files[n.file]

	if f == nil {
		f = &LockedFile{}
		t.files[n.file] = f
	}

	if f.ready(want, nil) {
		f.hold(want)
		t.settle(n.file, f)
		t.mu.Unlock()

		return nil
	}

	if !waits {
		t.mu.Unlock()

		return EAGAIN
	}

	if !want.ranged {
		f.drop(n, false, 0, 0)
	}

	last := &f.waiting

	for *last != nil {
		last = &(*last).next
	}

	*last = want
	f.give()
	t.mu.Unlock()

	return t.wait(p, f, want)
}

func (t *LockTable) wait(p *Process, f *LockedFile, w *FileLock) (err error) {
	n := w.owner

	n.ref()

	defer func() {
		t.mu.Lock()

		if w.granted {
			err = nil
		} else {
			f.leave(w)
			t.settle(n.file, f)
		}

		t.mu.Unlock()
		n.unref()
	}()

	for {
		_, woken, _, err := attend(p, &p.turns, &t.mu, &f.given, func() bool { return w.granted }, time.Time{}, restarts)

		if err != nil || !woken {
			return err
		}
	}
}

func (t *LockTable) unlock(n *RemoteNode, ranged bool, start, stop uint64) {
	if !n.locked.Load() {
		return
	}

	t.mu.Lock()

	if f := t.files[n.file]; f != nil {
		f.drop(n, ranged, start, stop)
		t.settle(n.file, f)
	}

	t.mu.Unlock()
}

func (t *LockTable) way(want *FileLock) (FileLock, bool) {
	t.mu.Lock()

	defer t.mu.Unlock()

	if f := t.files[want.owner.file]; f != nil {
		if l := f.inWay(want); l != nil {
			return *l, true
		}
	}

	return FileLock{}, false
}

func (t *LockTable) forget(n *RemoteNode) {
	t.mu.Lock()

	if f := t.files[n.file]; f != nil {
		for at := &f.held; *at != nil; {
			if l := *at; l.owner == n {
				*at = l.next
			} else {
				at = &l.next
			}
		}

		t.settle(n.file, f)
	}

	t.mu.Unlock()
}
