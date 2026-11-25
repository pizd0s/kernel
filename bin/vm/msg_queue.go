package main

import (
	"sync"
	"time"
)

type MsgQueue struct {
	mu           sync.Mutex
	perm         IpcPerm
	messages     []SysvMessage
	bytes        int
	qbytes       int
	lspid, lrpid int32
	stime, rtime int64
	ctime        int64
	watchers     Subscribers
	removed      bool
}

func newMsgQueue(perm IpcPerm) *MsgQueue {
	return &MsgQueue{perm: perm, qbytes: MSGMNB, ctime: time.Now().Unix()}
}

func (q *MsgQueue) send(p *Process, m SysvMessage, nowait bool) error {
	q.mu.Lock()

	defer q.mu.Unlock()

	for {
		switch {
		case q.removed:
			return EIDRM
		case !q.perm.grants(&p.cred, 2):
			return EACCES
		case q.bytes+len(m.text) <= q.qbytes && len(q.messages) < q.qbytes:
			q.messages = append(q.messages, m)
			q.bytes += len(m.text)
			q.lspid, q.stime = p.pid, time.Now().Unix()
			q.watchers.tell()

			return nil
		case nowait:
			return EAGAIN
		}

		if _, err := p.watch(&q.mu, &q.watchers, true, time.Time{}, interrupts); err != nil {
			return err
		}
	}
}

func (q *MsgQueue) find(mtype int32) int {
	at := -1

	for i, m := range q.messages {
		switch {
		case mtype == 0:
			return i
		case mtype > 0 && m.mtype == mtype:
			return i
		case mtype < 0 && m.mtype <= -mtype && (at < 0 || m.mtype < q.messages[at].mtype):
			at = i
		}
	}

	return at
}

func (q *MsgQueue) receive(p *Process, room int, mtype int32, flags uint32) (SysvMessage, error) {
	q.mu.Lock()

	defer q.mu.Unlock()

	for {
		if q.removed {
			return SysvMessage{}, EIDRM
		}

		if !q.perm.grants(&p.cred, 4) {
			return SysvMessage{}, EACCES
		}

		if i := q.find(mtype); i >= 0 {
			m := q.messages[i]

			if len(m.text) > room {
				if flags&MSG_NOERROR == 0 {
					return SysvMessage{}, E2BIG
				}

				m.text = m.text[:room]
			}

			q.bytes -= len(q.messages[i].text)
			q.messages = append(q.messages[:i], q.messages[i+1:]...)
			q.lrpid, q.rtime = p.pid, time.Now().Unix()
			q.watchers.tell()

			return m, nil
		}

		if flags&IPC_NOWAIT != 0 {
			return SysvMessage{}, ENOMSG
		}

		if _, err := p.watch(&q.mu, &q.watchers, false, time.Time{}, interrupts); err != nil {
			return SysvMessage{}, err
		}
	}
}

func (q *MsgQueue) remove() {
	q.mu.Lock()
	q.removed = true
	q.messages, q.bytes = nil, 0
	q.watchers.tell()
	q.mu.Unlock()
}

func (q *MsgQueue) permission() IpcPerm {
	q.mu.Lock()

	defer q.mu.Unlock()

	return q.perm
}
