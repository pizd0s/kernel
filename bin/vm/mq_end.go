package main

import "sync/atomic"

type MqEnd struct {
	refs     atomic.Int32
	queue    *MessageQueue
	access   int
	nonblock atomic.Bool
}

func newMqEnd(q *MessageQueue, access int, nonblock bool) *MqEnd {
	e := &MqEnd{queue: q, access: access}

	e.refs.Store(1)
	e.nonblock.Store(nonblock)

	return e
}

func (e *MqEnd) ref() {
	e.refs.Add(1)
}

func (e *MqEnd) unref() {
	if e.refs.Add(-1) == 0 {
		e.queue.forget(e)
	}
}

func (e *MqEnd) poll(events int16, through []*Epoll) int16 {
	return e.queue.poll(events)
}

func (e *MqEnd) subscribe(s *Subscription, events int16) {
	e.queue.subscribe(s, events)
}

func (e *MqEnd) unsubscribe(s *Subscription) {
	e.queue.unsubscribe(s)
}

func (e *MqEnd) flags() int {
	if e.nonblock.Load() {
		return e.access | O_NONBLOCK
	}

	return e.access
}

func (e *MqEnd) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		e.queue.stat(r.st)

		return false, nil
	case *FileGetfl:
		r.flags = e.flags()

		return false, nil
	case *FileSetfl:
		e.nonblock.Store(r.flags&O_NONBLOCK != 0)

		return false, nil
	case *MqSend:
		if e.access == O_RDONLY {
			return false, EBADF
		}

		return false, e.queue.send(p, r, e.nonblock.Load())
	case *MqReceive:
		if e.access == O_WRONLY {
			return false, EBADF
		}

		return false, e.queue.receive(p, r, e.nonblock.Load())
	case *MqAttr:
		e.queue.attributes(r)
		r.nonblock = e.nonblock.Load()

		if r.set {
			e.nonblock.Store(r.want)
		}

		return false, nil
	case *MqNotify:
		return false, e.queue.notify(p, e, r)
	}

	return false, errUnserved
}
