//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

type RemoteFD struct {
	end  Object
	refs atomic.Int32

	mu       sync.Mutex
	watchers Subscribers
	id       uint64
	key      FileKey
	access   int
	ready    int16

	locks *LockTable
}

func (f *RemoteFD) ref() {
	f.refs.Add(1)
}

func (f *RemoteFD) unref() {
	if f.refs.Add(-1) == 0 {
		if f.locks != nil {
			f.locks.forget(f)
		}

		f.end.unref()
	}
}

func (f *RemoteFD) subscribe(s *Subscription, events int16) {
	f.mu.Lock()
	f.watchers.add(&s.in)
	f.mu.Unlock()
}

func (f *RemoteFD) unsubscribe(s *Subscription) {
	f.mu.Lock()
	f.watchers.remove(&s.in)
	f.mu.Unlock()
}

func (f *RemoteFD) poll(events int16, through []*Epoll) int16 {
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

func (f *RemoteFD) exchange(p *Process, data []byte, files []Object, interruptible bool) (pk Packet, cancelled bool, err error) {
	if interruptible && p.interrupts(true) {
		return Packet{}, false, EINTR
	}

	a := &Answer{asker: p}

	defer a.leave()

	if _, err := p.sockSend(f.end, &Packet{data: data, files: append(files, a)}, Address{}, IoMode{quiet: true, kernel: true}); err != nil {
		return Packet{}, false, EIO
	}

	outer, was := p.ask(a)

	p.await(&a.done, time.Time{}, waitsOn)
	cancelled = p.cancelled
	p.unask(outer, was)

	return a.taken(), cancelled, nil
}

func (f *RemoteFD) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileIdentity:
		r.id, r.key, r.access = f.id, f.key, f.access

		return false, nil
	case *FileRead:
		switch {
		case !r.explicit && p.chaosFail("read"):
			return false, EIO
		case r.n < 0 || r.explicit && r.at < 0:
			return false, EINVAL
		case r.n == 0:
			return false, nil
		}
	case *FileWrite:
		switch {
		case !r.explicit && p.chaosFail("write"):
			return false, EIO
		case r.n < 0 || r.explicit && r.at < 0:
			return false, EINVAL
		case r.n == 0:
			return false, nil
		}
	case *SockSend:
		if room := packetMax - remoteHeader - 4 - len(r.to.raw); len(r.pk.data) > room {
			kind := SockType{}

			if err := p.request(f, &kind); err != nil {
				return false, err
			}

			if kind.kind != SOCK_STREAM {
				return false, EMSGSIZE
			}

			r.pk.data = r.pk.data[:room]
		}
	case *FileCopyTo:
		var err error

		r.count, err = p.fileCopyFrom(r.out, f, r.from, r.to, r.n)

		return false, err
	case *FileCopyFrom:
		var err error

		r.count, err = copyThrough(p, r.in, f, r.from, r.to, r.n)

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

	pk, cancelled, err := f.exchange(p, data, files, waits)

	if err != nil {
		return false, err
	}

	if err := replyOf(&pk, false); err != nil {
		return cancelled, err
	}

	return cancelled, r.des(p, &pk)
}
