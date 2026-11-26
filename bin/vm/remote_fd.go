package main

import (
	"encoding/binary"
	"sync"
	"time"
)

type RemoteFD struct {
	end *Description

	mu       sync.Mutex
	watchers Subscribers
	key      FileKey
	ready    int16
}

func (f *RemoteFD) closed(d *Description) {
	f.end.release()
}

func (f *RemoteFD) subscribe(d *Description, s *Subscription, events int16) {
	in, out := ioSides(events)

	f.mu.Lock()

	if in {
		f.watchers.add(&s.in)
	}

	if out {
		f.watchers.add(&s.out)
	}

	f.mu.Unlock()
}

func (f *RemoteFD) unsubscribe(d *Description, s *Subscription) {
	f.mu.Lock()
	f.watchers.remove(&s.in)
	f.watchers.remove(&s.out)
	f.mu.Unlock()
}

func (f *RemoteFD) move(d, to *Description) error {
	if d == nil || !d.grab() {
		return EPIPE
	}

	defer d.release()

	if d.object() != Object(f) {
		return EINVAL
	}

	d.swap(to)
	f.mu.Lock()
	f.watchers.drop()
	f.mu.Unlock()

	return nil
}

func (f *RemoteFD) poll(d *Description, events int16, through []*Epoll) int16 {
	f.mu.Lock()

	ready := f.ready

	f.mu.Unlock()

	return ready & (events | POLLERR | POLLHUP)
}

func (f *RemoteFD) deliver(pk *Packet) {
	if pk.kind == packetEnd {
		f.mu.Lock()
		f.ready = POLLHUP | POLLERR
		f.watchers.tell()
		f.mu.Unlock()

		return
	}

	if len(pk.data) >= remoteHeader && binary.LittleEndian.Uint32(pk.data) == rReady {
		f.mu.Lock()
		f.ready = readiness(binary.LittleEndian.Uint32(pk.data[4:]))
		f.watchers.tell()
		f.mu.Unlock()
	}

	dropFiles(pk.files)
}

func (f *RemoteFD) exchange(p *Process, d *Description, data []byte, files []*Description, interruptible bool) (pk Packet, cancelled bool, err error) {
	if interruptible && p.interrupts(true) {
		return Packet{}, false, EINTR
	}

	a := &Answer{ObjectIdent: newObjectIdent(), asker: p, of: d, on: f}

	defer a.leave()

	end, _ := f.end.object().(*PairEnd)

	if end == nil || end.deliver(p, &Packet{data: data, files: append(files, newDescription(a, O_RDWR))}, IoMode{quiet: true, kernel: true}) != nil {
		return Packet{}, false, EIO
	}

	outer, was := p.ask(a)

	p.await(&a.done, time.Time{}, waitsOn)
	cancelled = p.cancelled
	p.unask(outer, was)

	return a.taken(), cancelled, nil
}

func (f *RemoteFD) call(p *Process, d *Description, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileIdentity:
		r.id, r.key = f.objectID(), f.key

		return false, nil
	case *FileRead:
		switch {
		case !r.explicit && p.chaosFail("read"):
			return false, EIO
		case r.n < 0 || r.explicit && r.at < 0:
			return false, EINVAL
		case r.n == 0 && !r.sock:
			return false, nil
		}
	case *FileWrite:
		switch {
		case !r.explicit && p.chaosFail("write"):
			return false, EIO
		case r.n < 0 || r.explicit && r.at < 0:
			return false, EINVAL
		case r.n == 0 && !r.sock:
			return false, nil
		}
	case *FileCopyTo:
		var err error

		r.count, err = p.fileCopyFrom(r.out, d, r.from, r.to, r.n)

		return false, err
	case *FileCopyFrom:
		var err error

		r.count, err = copyThrough(p, r.in, d, r.from, r.to, r.n)

		return false, err
	}

	op := r.op()

	if op == 0 {
		return false, errUnserved
	}

	if waits && p.interrupts(true) {
		return false, EINTR
	}

	arg, data, files := r.ser(p, make([]byte, remoteHeader, remoteHeader+64))

	p.head(data, op, arg, group)

	pk, cancelled, err := f.exchange(p, d, data, files, waits)

	if (err == EIO || err == nil && len(pk.data) < remoteHeader) && d != nil && d.object() != Object(f) {
		dropFiles(pk.files)

		return d.object().call(p, d, r, group, waits)
	}

	if err != nil {
		return false, err
	}

	if err := replyOf(&pk, false); err != nil {
		return cancelled, err
	}

	return cancelled, r.des(p, &pk)
}

func (f *RemoteFD) objectID() uint64 {
	return f.end.object().objectID()
}
