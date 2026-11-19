//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
)

const answerSmall = 96

const (
	answerCancel = 1 << iota
	answerLeft
)

type Answer struct {
	refs  atomic.Int32
	asker *Process
	done  atomic.Bool

	mu       sync.Mutex
	data     []byte
	small    [answerSmall]byte
	files    []Object
	said     uint32
	uid, gid uint32
	watchers Subscribers
}

func (a *Answer) ref() {
	a.refs.Add(1)
}

func (a *Answer) unref() {
	if a.refs.Add(-1) == 0 {
		a.done.Store(true)
		a.asker.ring()
	}
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

func (a *Answer) put(more func(small []byte) ([]byte, error), files []Object) (int, error) {
	a.mu.Lock()

	defer a.mu.Unlock()

	if a.said&answerLeft != 0 {
		return 0, EPIPE
	}

	small := a.small[:]

	if len(a.data) > 0 {
		small = nil
	}

	data, err := more(small)

	if err != nil {
		return 0, err
	}

	if len(a.data)+len(data) > packetMax {
		return 0, EMSGSIZE
	}

	for _, f := range files {
		f.ref()
	}

	if len(a.data) > 0 {
		a.data = append(a.data, data...)
	} else {
		a.data = data
	}

	a.files = append(a.files, files...)

	return len(data), nil
}

func (a *Answer) asFile() File {
	return nil
}

func (a *Answer) asTransfer() Transfer {
	return a
}

func (a *Answer) asSeeker() Seeker {
	return nil
}

func (a *Answer) asDir() Dir {
	return nil
}

func (a *Answer) asTerminal() Terminal {
	return nil
}

func (a *Answer) asWindowed() Windowed {
	return nil
}

func (a *Answer) asLink() Link {
	return nil
}

func (a *Answer) asAttrs() Attrs {
	return nil
}

func (a *Answer) asSocket() Socket {
	return a
}

func (a *Answer) asIoctl() Ioctl {
	return nil
}

func (a *Answer) asLocker() Locker {
	return nil
}

func (a *Answer) asEpoll() *Epoll {
	return nil
}

func (a *Answer) asMemfd() *MemfdFile {
	return nil
}

func (a *Answer) asPair() *PairEnd {
	return nil
}

func (a *Answer) asListener() *UnixListener {
	return nil
}

func (a *Answer) asConsole() *ConsoleEnd {
	return nil
}

func (a *Answer) asRemote() *RemoteFD {
	return nil
}

func (a *Answer) poll(events int16) int16 {
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

func (a *Answer) subscribe(s *Subscription, events int16) {
	a.mu.Lock()
	a.watchers.add(&s.in)
	a.mu.Unlock()
}

func (a *Answer) unsubscribe(s *Subscription) {
	a.mu.Lock()
	a.watchers.remove(&s.in)
	a.mu.Unlock()
}

func (a *Answer) read(p *Process, b []byte, mode IoMode) (int, error) {
	return a.heard(b)
}

func (a *Answer) write(p *Process, b []byte, mode IoMode) (int, error) {
	return a.put(func(small []byte) ([]byte, error) {
		if len(b) <= len(small) {
			return small[:copy(small, b)], nil
		}

		return append([]byte(nil), b...), nil
	}, nil)
}

func (a *Answer) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (a *Answer) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (a *Answer) bind(p *Process, at, n uint32) error {
	return EINVAL
}

func (a *Answer) connect(p *Process, at, n uint32) error {
	return EISCONN
}

func (a *Answer) listen(backlog int) error {
	return EOPNOTSUPP
}

func (a *Answer) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, EOPNOTSUPP
}

func (a *Answer) sockname(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (a *Answer) peername(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (a *Answer) socketKind() int {
	return SOCK_SEQPACKET
}

func (a *Answer) shutdown(how int) error {
	return nil
}

func (a *Answer) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	return ENOPROTOOPT
}

func (a *Answer) setsockopt(p *Process, level, name, at, n uint32) error {
	return ENOPROTOOPT
}

func (a *Answer) sendmsg(p *Process, at, flags uint32) (int, error) {
	h := p.span(at, sizeofMsghdr)

	if binary.LittleEndian.Uint32(h) != 0 {
		return 0, EISCONN
	}

	files, _, err := p.rights(binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:]))

	if err != nil {
		return 0, err
	}

	return a.put(func(small []byte) ([]byte, error) {
		return p.gatherIn(small, binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:]), packetMax)
	}, files)
}

func (a *Answer) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	if addr != 0 {
		return 0, EISCONN
	}

	return a.write(p, p.span(buf, n), IoMode{})
}

func (a *Answer) recvmsg(p *Process, at, flags uint32) (int, error) {
	var h [remoteHeader]byte
	var few [4][]byte

	n, err := a.heard(h[:])

	if err != nil {
		return 0, err
	}

	spans, err := p.ioSpansIn(few[:0], p.u32(at+8), p.u32(at+12))

	if err != nil {
		return 0, err
	}

	if p.u32(at) != 0 {
		p.put32(at+4, 0)
	}

	p.put32(at+20, 0)
	p.put32(at+24, flags&MSG_CMSG_CLOEXEC)

	return scatterInto(spans, h[:n]), nil
}

func (a *Answer) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	got, err := a.heard(p.span(buf, n))

	return got, nil, err
}
