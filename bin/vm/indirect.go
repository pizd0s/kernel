package main

import (
	"sync"
)

type Indirect struct {
	ObjectIdent

	mu     sync.Mutex
	target *Description
	subs   []Subscribed
}

func newIndirect(target *Description) *Indirect {
	return &Indirect{ObjectIdent: newObjectIdent(), target: target}
}

func (i *Indirect) closed(d *Description) {
	i.target.release()
}

func (i *Indirect) current() Object {
	i.mu.Lock()

	t := i.target.object()

	i.mu.Unlock()

	return t
}

func (i *Indirect) poll(d *Description, events int16, through []*Epoll) int16 {
	t := i.current()

	return t.poll(nil, events, through)
}

func (i *Indirect) subscribe(d *Description, s *Subscription, events int16) {
	i.mu.Lock()
	i.subs = append(i.subs, Subscribed{s, events})
	i.target.object().subscribe(nil, s, events)
	i.mu.Unlock()
}

func (i *Indirect) unsubscribe(d *Description, s *Subscription) {
	i.mu.Lock()

	for k := range i.subs {
		if i.subs[k].s == s {
			i.subs = append(i.subs[:k], i.subs[k+1:]...)

			break
		}
	}

	i.target.object().unsubscribe(nil, s)
	i.mu.Unlock()
}

func (i *Indirect) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
	t := i.current()
	cancelled, err := t.call(p, nil, r, group, waits)

	if to := movedOf(r); to != nil {
		i.retarget(to)
	}

	return cancelled, err
}

func (i *Indirect) retarget(to *Description) {
	i.mu.Lock()

	from := i.target

	for _, s := range i.subs {
		from.object().unsubscribe(nil, s.s)
		to.object().subscribe(nil, s.s, s.events)
	}

	i.target = to

	subs := append([]Subscribed(nil), i.subs...)

	i.mu.Unlock()

	from.release()

	for _, s := range subs {
		s.s.to.wake(&s.s.in)
		s.s.to.wake(&s.s.out)
	}
}

func movedOf(r Request) *Description {
	var to *Description

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
