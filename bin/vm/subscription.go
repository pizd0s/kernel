//go:build linux && amd64

package main

type Waker interface {
	wake(l *Hook)
}

type Hook struct {
	prev, next *Hook
	list       *Subscribers
	sub        *Subscription
}

type Subscription struct {
	to      Waker
	id      uint64
	in, out Hook
	event   Event
	spare   *Subscription
}

func (s *Subscription) init(to Waker, id uint64) {
	s.to, s.id = to, id
	s.in.sub, s.out.sub = s, s
	s.event.kind, s.event.id = eventChanged, id
}

type Subscribers struct {
	first *Hook
}

func (s *Subscribers) add(l *Hook) {
	l.list, l.prev, l.next = s, nil, s.first

	if s.first != nil {
		s.first.prev = l
	}

	s.first = l
}

func (s *Subscribers) remove(l *Hook) {
	if l.list != s {
		return
	}

	if l.prev != nil {
		l.prev.next = l.next
	} else {
		s.first = l.next
	}

	if l.next != nil {
		l.next.prev = l.prev
	}

	l.list, l.prev, l.next = nil, nil, nil
}

func (s *Subscribers) tell() {
	for l := s.first; l != nil; l = l.next {
		l.sub.to.wake(l)
	}
}

func (s *Subscribers) hooks(into []*Hook) []*Hook {
	for l := s.first; l != nil; l = l.next {
		into = append(into, l)
	}

	return into
}

func (s *Subscribers) tellIn() {
	for l := s.first; l != nil; l = l.next {
		if l == &l.sub.in {
			l.sub.to.wake(l)
		}
	}
}

func (s *Subscribers) tellOut() {
	for l := s.first; l != nil; l = l.next {
		if l == &l.sub.out {
			l.sub.to.wake(l)
		}
	}
}

func (s *Subscribers) takeOver(from *Subscribers) {
	for l := from.first; l != nil; {
		next := l.next

		l.list, l.prev, l.next = s, nil, s.first

		if s.first != nil {
			s.first.prev = l
		}

		s.first, l = l, next
	}

	from.first = nil
}
