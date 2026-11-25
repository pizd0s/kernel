package main

type Barrier struct {
	ObjectIdent

	count uint32
}

func newBarrier(count uint32) *Barrier {
	b := &Barrier{ObjectIdent: newObjectIdent(), count: count}

	return b
}

func (b *Barrier) closed() {
}

func (b *Barrier) poll(events int16, through []*Epoll) int16 {
	return 0
}

func (b *Barrier) subscribe(s *Subscription, events int16) {
}

func (b *Barrier) unsubscribe(s *Subscription) {
}

func (b *Barrier) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *SyncArrive:
		if b.count != 1 {
			return false, EDEADLK
		}

		r.serial = true

		return false, nil
	case *SyncCopy:
		r.copy = newBarrier(b.count)

		return false, nil
	}

	return false, errUnserved
}
