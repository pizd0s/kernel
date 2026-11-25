package main

import (
	"slices"
	"sync"
	"sync/atomic"
)

type Description struct {
	holders atomic.Int32
	obj     atomic.Pointer[Object]
	locks   atomic.Pointer[LockTable]

	mu    sync.Mutex
	subs  []Subscribed
	under *Description
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

	d.mu.Lock()

	under := d.under

	d.mu.Unlock()

	if under != nil {
		under.release()

		return
	}

	d.object().closed()
}

func (d *Description) subscribe(s *Subscription, events int16) {
	d.mu.Lock()
	d.subs = append(d.subs, Subscribed{s: s, events: events})
	d.object().subscribe(s, events)
	d.mu.Unlock()
}

func (d *Description) unsubscribe(s *Subscription) {
	d.mu.Lock()

	d.subs = slices.DeleteFunc(d.subs, func(x Subscribed) bool {
		return x.s == s
	})

	d.object().unsubscribe(s)
	d.mu.Unlock()
}

func (d *Description) adopt(to *Description) {
	d.mu.Lock()

	if d.under != nil {
		d.mu.Unlock()
		to.release()

		return
	}

	old, obj := d.object(), to.object()

	for _, s := range d.subs {
		old.unsubscribe(s.s)
		obj.subscribe(s.s, s.events)
	}

	d.obj.Store(&obj)
	d.under = to

	subs := slices.Clone(d.subs)

	d.mu.Unlock()
	old.closed()

	for _, s := range subs {
		s.s.to.wake(&s.s.in)
		s.s.to.wake(&s.s.out)
	}
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
