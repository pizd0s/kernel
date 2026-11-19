//go:build linux && amd64

package main

// A waker is what an object wakes as it changes: by the hook of the
// subscription, which says by which of its lists.
type waker interface {
	wake(l *hook)
}

// A hook is a subscription's place in one list of an object's
// (subscribers). A subscription has two: in, for the changes of what the
// object reads, and out, for those of what it writes; an object of one
// kind of change takes in.
type hook struct {
	prev, next *hook
	list       *subscribers
	sub        *subscription
}

// A subscription is a subscriber's: what it wakes, its id there, its two
// hooks and its event, for a process it wakes. Its subscriber makes it
// before it subscribes and keeps it until it took it back; spare is the
// next of a process's spare ones.
type subscription struct {
	to      waker
	id      uint64
	in, out hook
	event   event
	spare   *subscription
}

// init makes s a subscription that wakes to with id.
func (s *subscription) init(to waker, id uint64) {
	s.to, s.id = to, id
	s.in.sub, s.out.sub = s, s
	s.event.kind, s.event.id = eventChanged, id
}

// subscribers are the subscriptions an object wakes for one kind of its
// changes: a list of their hooks, under the object's lock, as all of the
// object is: who subscribes, who takes a subscription back and who tells
// holds it. So a tell is in the turn of the lock that made the change,
// and what it wakes, a process's queue or an epoll set, is locked under
// the object: neither ever asks an object with its own lock held.
type subscribers struct {
	first *hook
}

// add puts a hook that is in no list in the list.
func (s *subscribers) add(l *hook) {
	l.list, l.prev, l.next = s, nil, s.first

	if s.first != nil {
		s.first.prev = l
	}

	s.first = l
}

// remove takes a hook out of the list, when it is there.
func (s *subscribers) remove(l *hook) {
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

// tell wakes every subscription of the list.
func (s *subscribers) tell() {
	for l := s.first; l != nil; l = l.next {
		l.sub.to.wake(l)
	}
}

// takeOver gives the hooks of from to s, a channel's in place of another
// one's, with from's object's lock, s's object being no one's yet.
func (s *subscribers) takeOver(from *subscribers) {
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
