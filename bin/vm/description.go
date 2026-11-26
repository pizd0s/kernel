package main

import (
	"sync/atomic"
)

const statusFlags = O_APPEND | O_NONBLOCK | O_DSYNC | O_SYNC

type Description struct {
	holders atomic.Int32
	obj     atomic.Pointer[Object]
	locks   atomic.Pointer[LockTable]
	flags   atomic.Int32
	offset  int64
}

func newDescription(obj Object, flags int) *Description {
	d := &Description{}

	d.obj.Store(&obj)
	d.flags.Store(int32(flags))

	return d
}

func opened(obj Object, flags int) *Description {
	d := newDescription(obj, flags)

	d.holders.Store(1)

	return d
}

func held(obj Object) *Description {
	return opened(obj, O_RDWR)
}

func (d *Description) object() Object {
	return *d.obj.Load()
}

func (d *Description) hold() {
	d.holders.Add(1)
}

func (d *Description) grab() bool {
	for {
		n := d.holders.Load()

		if n <= 0 {
			return false
		}

		if d.holders.CompareAndSwap(n, n+1) {
			return true
		}
	}
}

func (d *Description) release() {
	if d.holders.Add(-1) != 0 {
		return
	}

	if t := d.locks.Load(); t != nil {
		t.forget(d)
	}

	d.object().closed(d)
}

func (d *Description) reads() bool {
	return d.flags.Load()&O_ACCMODE != O_WRONLY
}

func (d *Description) writes() bool {
	return d.flags.Load()&O_ACCMODE != O_RDONLY
}

func (d *Description) nonblock() bool {
	return d.flags.Load()&O_NONBLOCK != 0
}

func (d *Description) status() int {
	return int(d.flags.Load())
}

func rdwr(nonblock bool) int {
	return O_RDWR | int(bit(nonblock))*O_NONBLOCK
}

func (d *Description) setStatus(fl int) {
	for {
		was := d.flags.Load()

		if d.flags.CompareAndSwap(was, was&^statusFlags|int32(fl&statusFlags)) {
			return
		}
	}
}

func (d *Description) addStatus(fl int) {
	for {
		was := d.flags.Load()

		if d.flags.CompareAndSwap(was, was|int32(fl&statusFlags)) {
			return
		}
	}
}

func (d *Description) mode() IoMode {
	return IoMode{nonblock: d.nonblock()}
}

func (d *Description) dontwait() uint32 {
	return bit(d.nonblock()) * MSG_DONTWAIT
}

func (d *Description) poll(events int16, through []*Epoll) int16 {
	return d.object().poll(d, events, through)
}

func (d *Description) subscribe(s *Subscription, events int16) {
	for {
		at := d.obj.Load()

		(*at).subscribe(d, s, events)

		if d.obj.Load() == at {
			return
		}

		(*at).unsubscribe(d, s)
	}
}

func (d *Description) unsubscribe(s *Subscription) {
	d.object().unsubscribe(d, s)
}

func (d *Description) swap(with *Description) {
	a, b := d.obj.Load(), with.obj.Load()

	d.obj.Store(b)
	with.obj.Store(a)
}

func dropFiles(files []*Description) {
	for _, d := range files {
		d.release()
	}
}

func holdFiles(files []*Description) {
	for _, d := range files {
		d.hold()
	}
}
