package main

import (
	"sync"
	"sync/atomic"
)

type Description struct {
	holders  atomic.Int32
	obj      atomic.Pointer[Object]
	locks    atomic.Pointer[LockTable]
	nonblock atomic.Bool

	mu sync.Mutex
}

func newDescription(obj Object) *Description {
	d := &Description{}

	d.obj.Store(&obj)

	return d
}

func held(obj Object) *Description {
	d := newDescription(obj)

	d.holders.Store(1)

	return d
}

func opened(obj Object, nonblock bool) *Description {
	d := held(obj)

	d.nonblock.Store(nonblock)

	return d
}

func (d *Description) object() Object {
	return *d.obj.Load()
}

func (d *Description) hold() {
	d.holders.Add(1)
}

func (d *Description) release() {
	if d.holders.Add(-1) != 0 {
		return
	}

	if t := d.locks.Load(); t != nil {
		t.forget(d)
	}

	d.object().closed()
}

func (d *Description) mode() IoMode {
	return IoMode{nonblock: d.nonblock.Load()}
}

func (d *Description) dontwait() uint32 {
	return bit(d.nonblock.Load()) * MSG_DONTWAIT
}

func (d *Description) subscribe(s *Subscription, events int16) {
	d.mu.Lock()
	d.object().subscribe(s, events)
	d.mu.Unlock()
}

func (d *Description) unsubscribe(s *Subscription) {
	d.mu.Lock()
	d.object().unsubscribe(s)
	d.mu.Unlock()
}

func (d *Description) swap(with *Description) {
	with.mu.Lock()

	a, b := d.object(), with.object()

	d.obj.Store(&b)
	with.obj.Store(&a)
	with.mu.Unlock()
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
