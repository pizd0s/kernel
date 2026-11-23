//go:build linux && amd64

package main

var _ Waker = (*InotifyWatch)(nil)

type InotifyWatch struct {
	in    *Inotify
	wd    int32
	mask  uint32
	dev   uint64
	ino   uint64
	dir   Object
	end   Object
	names map[string]bool
	sub   Subscription

	users   int
	retired bool
}

func (w *InotifyWatch) wake(l *Hook) {
	w.in.ring()
}
