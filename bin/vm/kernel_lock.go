//go:build linux && amd64

package main

import (
	"math"
	"sync"
	"syscall"
)

// The locks of flock and fcntl are the kernel's. A server knows one thing
// of them, which of its open files are one file; the kernel knows the
// rest: who holds a lock, who waits for one, when an open file closes.
//
// A file is named by its fileKey (kernel_remote.go), the dev and ino its
// server made the open file with. An open file made with no ino has no
// locks. A directory's handle is rootfs's, which gives it the directory's
// dev and ino: a directory has flock's lock so.
//
// The table is the kernel's and lives as long. A file is in it while a
// lock on it is held or waited for. A lock is an open file's, as Linux's
// open file description locks are: flock's on the whole file, shared or
// exclusive, fcntl's on a range of bytes, to read or to write. It goes
// when it is let go of, or with the last reference of its open file, so
// an ino a file system gives again starts with no locks. Those that wait
// have a lock in the order they asked: none is taken past one that waits
// for it, a shared one behind an exclusive one, nor by one that only
// tries. A process that waits sleeps as in any wait (waitFor): a signal it
// takes interrupts it, unless its handlers restart calls, and then it
// keeps its place.

// lockPast is the stop of a range to the end of a file and past it.
const lockPast = math.MaxUint64

// fileLock is a lock of an open file on a file, held or waited for:
// flock's, or with ranged fcntl's on the bytes [start, stop); write is
// flock's exclusive one and fcntl's to write. granted is set of one that
// waited once it is held.
type fileLock struct {
	owner       *remoteNode
	ranged      bool
	write       bool
	start, stop uint64
	granted     bool
	next        *fileLock
}

// lockedFile is the locks of a file: those held, those waited for, the
// first to come first, and the processes that wait, told when a lock is
// given.
type lockedFile struct {
	held, waiting *fileLock
	given         subscribers
}

type lockTable struct {
	mu    sync.Mutex
	files map[fileKey]*lockedFile
}

// crossing tells whether two locks are in each other's way: two open
// files', flock's with one exclusive, or fcntl's on bytes they share with
// one to write.
func (a *fileLock) crossing(b *fileLock) bool {
	if a.owner == b.owner || a.ranged != b.ranged {
		return false
	}

	if !a.ranged {
		return a.write || b.write
	}

	return a.start < b.stop && b.start < a.stop && (a.write || b.write)
}

// inWay is the lock held in the way of one.
func (f *lockedFile) inWay(want *fileLock) *fileLock {
	for l := f.held; l != nil; l = l.next {
		if l.crossing(want) {
			return l
		}
	}

	return nil
}

// ready tells whether a lock is its open file's to take now: none held is
// in its way, and none waited for before it in the queue, before, or
// anywhere in it for one that does not wait yet.
func (f *lockedFile) ready(want, before *fileLock) bool {
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

// drop lets go of what an open file holds: flock's lock, or with ranged
// fcntl's of the bytes [start, stop), a lock across an edge cut there.
func (f *lockedFile) drop(owner *remoteNode, ranged bool, start, stop uint64) {
	for at := &f.held; *at != nil; {
		l := *at

		switch {
		case l.owner != owner || l.ranged != ranged || ranged && (l.stop <= start || l.start >= stop):
			at = &l.next
		case ranged && l.start < start && l.stop > stop:
			// The middle goes: what is past it is a lock of its own.
			rest := new(fileLock)
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

// hold makes a lock its open file's, in place of what it held there.
func (f *lockedFile) hold(want *fileLock) {
	f.drop(want.owner, want.ranged, want.start, want.stop)
	want.next, f.held = f.held, want
}

// leave takes a lock out of the queue of those waited for.
func (f *lockedFile) leave(w *fileLock) {
	for at := &f.waiting; *at != nil; at = &(*at).next {
		if *at == w {
			*at = w.next

			return
		}
	}
}

// give gives the locks waited for to those they are free for now, the
// first to come first, and tells whether it gave one. One that takes a
// range in place of what it held may free an earlier one's: from the first
// again then.
func (f *lockedFile) give() bool {
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

	return given
}

// settle is what follows a change of a file's locks, under the table's
// lock: those that wait are given what is free now, and a file with no
// lock held or waited for leaves the table. It tells whether a lock was
// given: those that wait are told then (given.tell), the table's lock let
// go of.
func (t *lockTable) settle(key fileKey, f *lockedFile) bool {
	given := f.give()

	if f.held == nil && f.waiting == nil {
		delete(t.files, key)
	}

	return given
}

// lock takes a lock for its open file, the lock's owner: at once when it
// is the open file's to take; else EAGAIN, or with waits the process
// waits for it in the queue, and what the open file held of flock's lock
// goes meanwhile, as a change of flock's lock is no single step.
func (t *lockTable) lock(p *Process, want *fileLock, waits bool) error {
	n := want.owner
	n.locked.Store(true)
	t.mu.Lock()
	f := t.files[n.file]

	if f == nil {
		f = &lockedFile{}
		t.files[n.file] = f
	}

	if f.ready(want, nil) {
		f.hold(want)
		given := t.settle(n.file, f)
		t.mu.Unlock()

		if given {
			f.given.tell()
		}

		return nil
	}

	if !waits {
		t.mu.Unlock()

		return syscall.EAGAIN
	}

	if !want.ranged {
		f.drop(n, false, 0, 0)
	}

	last := &f.waiting

	for *last != nil {
		last = &(*last).next
	}

	*last = want
	given := f.give()
	t.mu.Unlock()

	if given {
		f.given.tell()
	}

	return t.wait(p, f, want)
}

// wait waits until a lock in the queue is given. The open file lives
// while its lock is waited for, though a handler closes its descriptor.
// However the wait ends, a lock not given leaves the queue, and those
// behind it may have theirs; one given as the wait was interrupted is
// held, and the call is done.
func (t *lockTable) wait(p *Process, f *lockedFile, w *fileLock) (err error) {
	n := w.owner
	n.ref()

	defer func() {
		t.mu.Lock()
		given := false

		if w.granted {
			err = nil
		} else {
			f.leave(w)
			given = t.settle(n.file, f)
		}

		t.mu.Unlock()

		if given {
			f.given.tell()
		}

		n.unref()
	}()

	return p.waitFor(func() bool {
		t.mu.Lock()
		granted := w.granted
		t.mu.Unlock()

		return granted
	}, &f.given, true)
}

// unlock lets go of what an open file holds: flock's lock, or with ranged
// fcntl's of the bytes [start, stop).
func (t *lockTable) unlock(n *remoteNode, ranged bool, start, stop uint64) {
	if !n.locked.Load() {
		return
	}

	t.mu.Lock()
	f := t.files[n.file]
	given := false

	if f != nil {
		f.drop(n, ranged, start, stop)
		given = t.settle(n.file, f)
	}

	t.mu.Unlock()

	if given {
		f.given.tell()
	}
}

// way is the lock another open file holds in the way of one, for
// F_GETLK.
func (t *lockTable) way(want *fileLock) (fileLock, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if f := t.files[want.owner.file]; f != nil {
		if l := f.inWay(want); l != nil {
			return *l, true
		}
	}

	return fileLock{}, false
}

// forget lets go of all an open file holds, as its last reference goes.
func (t *lockTable) forget(n *remoteNode) {
	t.mu.Lock()
	f := t.files[n.file]
	given := false

	if f != nil {
		for at := &f.held; *at != nil; {
			if l := *at; l.owner == n {
				*at = l.next
			} else {
				at = &l.next
			}
		}

		given = t.settle(n.file, f)
	}

	t.mu.Unlock()

	if given {
		f.given.tell()
	}
}
