package main

type Barrier struct {
	ObjectIdent

	count uint32
}

func newBarrier(count uint32) *Barrier {
	b := &Barrier{ObjectIdent: newObjectIdent(), count: count}

	return b
}

func (b *Barrier) closed(d *Description) {
}

func (b *Barrier) poll(d *Description, events int16, through []*Epoll) int16 {
	return 0
}

func (b *Barrier) subscribe(d *Description, s *Subscription, events int16) {
}

func (b *Barrier) unsubscribe(d *Description, s *Subscription) {
}

func (b *Barrier) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
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
