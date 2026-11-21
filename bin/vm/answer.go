//go:build linux && amd64

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
	refs  atomic.Int32
	asker *Process
	done  atomic.Bool

	mu       sync.Mutex
	data     []byte
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

func (a *Answer) put(data []byte, files []Object) (int, error) {
	a.mu.Lock()

	defer a.mu.Unlock()

	if a.said&answerLeft != 0 {
		return 0, EPIPE
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

func (a *Answer) poll(events int16, through []*Epoll) int16 {
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

func (a *Answer) read(p *Process, into Transfer, n int, mode IoMode) (int, error) {
	return into.with(0, n, true, a.heard)
}

func (a *Answer) write(p *Process, from Transfer, n int, mode IoMode) (int, error) {
	data := make([]byte, n)

	got, err := from.with(0, n, false, func(b []byte) (int, error) {
		return copy(data, b), nil
	})

	if err != nil {
		return 0, err
	}

	return a.put(data[:got], nil)
}

func (a *Answer) pread(p *Process, into Transfer, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (a *Answer) pwrite(p *Process, from Transfer, n int, off int64) (int, error) {
	return 0, ESPIPE
}

func (a *Answer) splice(at int64, n int, mode int) Transfer {
	return newSpliced(a, at, n, mode)
}

func (a *Answer) with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error) {
	return 0, EINVAL
}

func (a *Answer) copyTo(p *Process, out Transfer, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (a *Answer) copyFrom(p *Process, in Transfer, from, to int64, n int) (int, error) {
	return 0, EINVAL
}

func (a *Answer) bind(p *Process, addr Address) error {
	return EINVAL
}

func (a *Answer) connect(p *Process, addr Address) error {
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

func (a *Answer) getsockopt(p *Process, level, name uint32) ([]byte, error) {
	return nil, ENOPROTOOPT
}

func (a *Answer) setsockopt(level, name uint32, value []byte) error {
	return ENOPROTOOPT
}

func (a *Answer) send(p *Process, pk *Packet, to Address, mode IoMode) (int, error) {
	if to.given {
		return 0, EISCONN
	}

	return a.put(pk.data, pk.files)
}

func (a *Answer) recv(p *Process, into Into, want int, flags uint32) (int, Packet, uint32, error) {
	var h [remoteHeader]byte

	n, err := a.heard(h[:])

	if err != nil {
		return 0, Packet{}, 0, err
	}

	return into.put(0, h[:min(n, want)]), Packet{}, 0, nil
}

func (a *Answer) consume(sink func(pk *Packet)) {
}
