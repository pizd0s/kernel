package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
)

const (
	answerCancel = 1 << iota
	answerLeft
)

type Answer struct {
	ObjectIdent

	asker *Process
	of    *Description
	on    *RemoteFD
	done  atomic.Bool

	mu       sync.Mutex
	data     []byte
	files    []*Description
	said     uint32
	uid, gid uint32
	watchers Subscribers
}

func (a *Answer) closed(d *Description) {
	a.done.Store(true)
	a.asker.ring()
}

func (a *Answer) cancel(p *Process) {
	a.mu.Lock()

	if a.said&answerLeft == 0 {
		a.said |= answerCancel
		a.uid, a.gid = p.cred.euid(), p.cred.egid()
		a.watchers.tell()
	}

	a.mu.Unlock()
}

func (a *Answer) leave() {
	a.mu.Lock()
	a.said = answerLeft

	files := a.files

	a.files = nil
	a.watchers.tell()
	a.mu.Unlock()
	dropFiles(files)
}

func (a *Answer) taken() Packet {
	a.mu.Lock()

	defer a.mu.Unlock()

	pk := Packet{data: a.data, files: a.files}

	a.files = nil

	return pk
}

func (a *Answer) heard(b []byte) (int, error) {
	a.mu.Lock()

	defer a.mu.Unlock()

	switch {
	case a.said&answerCancel != 0:
		var h [remoteHeader]byte

		a.said &^= answerCancel
		binary.LittleEndian.PutUint32(h[:], rCancel)
		binary.LittleEndian.PutUint32(h[8:], a.uid)
		binary.LittleEndian.PutUint32(h[12:], a.gid)

		return copy(b, h[:]), nil
	case a.said&answerLeft != 0:
		return 0, nil
	}

	return 0, EAGAIN
}

func (a *Answer) put(data []byte, files []*Description) (int, error) {
	a.mu.Lock()

	defer a.mu.Unlock()

	if a.said&answerLeft != 0 {
		return 0, EPIPE
	}

	if len(a.data)+len(data) > packetMax {
		return 0, EMSGSIZE
	}

	if len(a.data) == 0 && len(data) >= remoteHeader && binary.LittleEndian.Uint32(data) == rMove {
		if a.on == nil || len(files) != 1 {
			return 0, EINVAL
		}

		if err := a.on.move(a.of, files[0]); err != nil {
			return 0, err
		}
	}

	holdFiles(files)

	if len(a.data) > 0 {
		a.data = append(a.data, data...)
	} else {
		a.data = data
	}

	a.files = append(a.files, files...)

	return len(data), nil
}

func (a *Answer) poll(d *Description, events int16, through []*Epoll) int16 {
	a.mu.Lock()

	said := a.said

	a.mu.Unlock()

	mask := int16(POLLOUT | POLLWRNORM)

	if said != 0 {
		mask |= POLLIN | POLLRDNORM
	}

	if said&answerLeft != 0 {
		mask |= POLLHUP
	}

	return mask & (events | POLLERR | POLLHUP)
}

func (a *Answer) subscribe(d *Description, s *Subscription, events int16) {
	a.mu.Lock()
	a.watchers.add(&s.in)
	a.mu.Unlock()
}

func (a *Answer) unsubscribe(d *Description, s *Subscription) {
	a.mu.Lock()
	a.watchers.remove(&s.in)
	a.mu.Unlock()
}

func (a *Answer) read(p *Process, into *MemfdFile, n int, mode IoMode) (int, error) {
	return into.with(0, n, true, a.heard)
}

func (a *Answer) write(p *Process, from *MemfdFile, n int, mode IoMode) (int, error) {
	data := make([]byte, n)

	got, err := from.with(0, n, false, func(b []byte) (int, error) {
		return copy(data, b), nil
	})

	if err != nil {
		return 0, err
	}

	return a.put(data[:got], nil)
}

func (a *Answer) socketKind() int {
	return SOCK_SEQPACKET
}

func (a *Answer) send(p *Process, r *SockSend) error {
	if r.to.given {
		return EISCONN
	}

	var err error

	r.n, err = a.put(r.pk.data, r.pk.files)

	return err
}

func (a *Answer) recv(p *Process, r *SockRecv) error {
	var h [remoteHeader]byte

	n, err := a.heard(h[:])

	if err != nil {
		return err
	}

	r.n = r.into.put(0, h[:min(n, r.want)])

	return nil
}

func (a *Answer) consume(sink func(pk *Packet)) {
}

func (a *Answer) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *SockSend:
		return false, a.send(p, r)
	case *SockRecv:
		return false, a.recv(p, r)
	case *SockType:
		r.kind = a.socketKind()

		return false, nil
	case *SockConsume:
		a.consume(r.sink)

		return false, nil
	case *FileRead:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = a.read(p, r.into.memory(), r.n, r.mode)

		return false, err
	case *FileWrite:
		if r.explicit {
			return false, errUnserved
		}

		var err error

		r.count, err = a.write(p, r.from.memory(), r.n, r.mode)

		return false, err
	}

	return false, errUnserved
}
