//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
)

type Indirect struct {
	refs atomic.Int32

	mu     sync.Mutex
	target Object
	subs   []Subscribed
}

type Subscribed struct {
	s      *Subscription
	events int16
}

func newIndirect(target Object) *Indirect {
	i := &Indirect{target: target}

	i.refs.Store(1)

	return i
}

func (i *Indirect) ref() {
	i.refs.Add(1)
}

func (i *Indirect) unref() {
	if i.refs.Add(-1) == 0 {
		i.target.unref()
	}
}

func (i *Indirect) current() Object {
	i.mu.Lock()

	t := i.target

	t.ref()
	i.mu.Unlock()

	return t
}

func (i *Indirect) poll(events int16, through []*Epoll) int16 {
	t := i.current()
	ready := t.poll(events, through)

	t.unref()

	return ready
}

func (i *Indirect) subscribe(s *Subscription, events int16) {
	i.mu.Lock()
	i.subs = append(i.subs, Subscribed{s, events})
	i.target.subscribe(s, events)
	i.mu.Unlock()
}

func (i *Indirect) unsubscribe(s *Subscription) {
	i.mu.Lock()

	for k := range i.subs {
		if i.subs[k].s == s {
			i.subs = append(i.subs[:k], i.subs[k+1:]...)

			break
		}
	}

	i.target.unsubscribe(s)
	i.mu.Unlock()
}

func (i *Indirect) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	t := i.current()
	cancelled, err := t.call(p, r, group, waits)

	t.unref()

	if to := movedOf(r); to != nil {
		i.retarget(to)
	}

	return cancelled, err
}

func (i *Indirect) retarget(to Object) {
	i.mu.Lock()

	from := i.target

	for _, s := range i.subs {
		from.unsubscribe(s.s)
		to.subscribe(s.s, s.events)
	}

	i.target = to

	subs := append([]Subscribed(nil), i.subs...)

	i.mu.Unlock()

	from.unref()

	for _, s := range subs {
		s.s.to.wake(&s.s.in)
		s.s.to.wake(&s.s.out)
	}
}

func movedOf(r Request) Object {
	var to Object

	switch r := r.(type) {
	case *SockBind:
		to, r.moved = r.moved, nil
	case *SockConnect:
		to, r.moved = r.moved, nil
	case *SockSend:
		to, r.moved = r.moved, nil
	}

	return to
}
