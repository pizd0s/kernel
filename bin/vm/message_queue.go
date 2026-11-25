package main

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"
)

var messageQueueInodes atomic.Uint64

const (
	mqDefaultMaxmsg  = 10
	mqDefaultMsgsize = 8192
	mqMaxmsg         = 65536
	mqMsgsize        = 1 << 20
	mqBytes          = 1 << 26
)

type MessageQueue struct {
	mu       sync.Mutex
	name     string
	mode     uint32
	uid, gid uint32
	maxmsg   int
	msgsize  int
	messages []QueuedMessage
	watchers Subscribers
	waiting  int
	ino      uint64
	made     time.Time

	notifier *Process
	notified *MqEnd
	event    SigEvent
}

func newMessageQueue(name string, mode, uid, gid uint32, maxmsg, msgsize int) *MessageQueue {
	return &MessageQueue{name: name, mode: mode, uid: uid, gid: gid, maxmsg: maxmsg, msgsize: msgsize, ino: messageQueueInodes.Add(1),
		made: time.Now()}
}

func (q *MessageQueue) poll(events int16) int16 {
	q.mu.Lock()

	n := len(q.messages)

	q.mu.Unlock()

	mask := int16(0)

	if n > 0 {
		mask |= POLLIN | POLLRDNORM
	}

	if n < q.maxmsg {
		mask |= POLLOUT | POLLWRNORM
	}

	return mask & events
}

func (q *MessageQueue) subscribe(s *Subscription, events int16) {
	q.mu.Lock()

	if events&(POLLIN|POLLRDNORM) != 0 {
		q.watchers.add(&s.in)
	}

	if events&(POLLOUT|POLLWRNORM) != 0 {
		q.watchers.add(&s.out)
	}

	q.mu.Unlock()
}

func (q *MessageQueue) unsubscribe(s *Subscription) {
	q.mu.Lock()
	q.watchers.remove(&s.in)
	q.watchers.remove(&s.out)
	q.mu.Unlock()
}

func (q *MessageQueue) stat(st *Stat) {
	*st = Stat{Ino: q.ino, Mode: q.mode, Nlink: 1, Uid: q.uid, Gid: q.gid, Blksize: 4096}
	st.Atim = nsecToTimespec(q.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim
}

func (q *MessageQueue) deadline(p *Process, at uint32, timed bool) (time.Time, error) {
	if !timed {
		return time.Time{}, nil
	}

	return p.deadlineOf(at, CLOCK_REALTIME)
}

func (q *MessageQueue) send(p *Process, r *MqSend, nonblock bool) error {
	if int(r.n) > q.msgsize {
		return EMSGSIZE
	}

	data := bytes.Clone(p.span(r.from, r.n))

	q.mu.Lock()

	for len(q.messages) >= q.maxmsg {
		if nonblock {
			q.mu.Unlock()

			return EAGAIN
		}

		end, err := q.deadline(p, r.at, r.timed)

		if err != nil {
			q.mu.Unlock()

			return err
		}

		timedOut, err := p.watch(&q.mu, &q.watchers, true, end, restarts)

		switch {
		case err != nil:
			q.mu.Unlock()

			return err
		case timedOut:
			q.mu.Unlock()

			return ETIMEDOUT
		}
	}

	i := 0

	for i < len(q.messages) && q.messages[i].prio >= r.prio {
		i++
	}

	q.messages = append(q.messages, QueuedMessage{})
	copy(q.messages[i+1:], q.messages[i:])
	q.messages[i] = QueuedMessage{data: data, prio: r.prio}

	var target *Process

	var sig int
	var info SigInfo

	if len(q.messages) == 1 && q.waiting == 0 && q.notifier != nil {
		if q.event.kind == SIGEV_SIGNAL {
			target, sig = q.notifier, q.event.signal
			info = SigInfo{code: SI_MESGQ, pid: p.pid, uid: p.cred.uid(), value: q.event.value}
		}

		q.notifier, q.notified = nil, nil
	}

	q.watchers.tellIn()
	q.mu.Unlock()

	if target != nil {
		target.sendInfo(sig, info)
	}

	return nil
}

func (q *MessageQueue) receive(p *Process, r *MqReceive, nonblock bool) error {
	if r.room < q.msgsize {
		return EMSGSIZE
	}

	q.mu.Lock()

	defer q.mu.Unlock()

	for len(q.messages) == 0 {
		if nonblock {
			return EAGAIN
		}

		end, err := q.deadline(p, r.at, r.timed)

		if err != nil {
			return err
		}

		q.waiting++

		timedOut, err := p.watch(&q.mu, &q.watchers, false, end, restarts)

		q.waiting--

		switch {
		case err != nil:
			return err
		case timedOut:
			return ETIMEDOUT
		}
	}

	r.message = q.messages[0]

	q.messages[0] = QueuedMessage{}
	q.messages = q.messages[1:]
	q.watchers.tellOut()

	return nil
}

func (q *MessageQueue) attributes(r *MqAttr) {
	q.mu.Lock()
	r.maxmsg, r.msgsize, r.curmsgs = q.maxmsg, q.msgsize, len(q.messages)
	q.mu.Unlock()
}

func (q *MessageQueue) notify(p *Process, e *MqEnd, r *MqNotify) error {
	q.mu.Lock()

	defer q.mu.Unlock()

	if !r.set {
		if q.notifier == p {
			q.notifier, q.notified = nil, nil
		}

		return nil
	}

	if q.notifier != nil && !q.notifier.ended() {
		return EBUSY
	}

	q.notifier, q.notified, q.event = p, e, r.event

	return nil
}

func (q *MessageQueue) forget(e *MqEnd) {
	q.mu.Lock()

	if q.notified == e {
		q.notifier, q.notified = nil, nil
	}

	q.mu.Unlock()
}
