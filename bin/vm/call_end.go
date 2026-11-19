//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

const (
	callNone = iota
	callSent
	callReceived
)

const (
	callCancel = 1 << iota
	callLeft
)

type Call struct {
	mu sync.Mutex

	phase atomic.Uint32
	seq   uint64

	request Packet
	answer  Packet
	there   atomic.Bool
	ended   atomic.Bool

	said     atomic.Uint32
	uid, gid uint32

	served atomic.Bool
	handed int

	asking  Waiters[Received]
	serving Waiters[Turn]
	servers Subscribers
	askers  Subscribers
	ino     atomic.Uint64

	small [96]byte
}

type CallEnd struct {
	c        *Call
	refs     atomic.Int32
	serves   bool
	passed   bool
	armed    bool
	seq      uint64
	nonblock atomic.Bool
}

func newCall(nonblock bool) (*CallEnd, *CallEnd) {
	c := &Call{}

	c.served.Store(true)

	serving, asking := &CallEnd{c: c, serves: true}, &CallEnd{c: c}

	for _, e := range []*CallEnd{serving, asking} {
		e.refs.Store(1)
		e.nonblock.Store(nonblock)
	}

	return serving, asking
}

func (e *CallEnd) ref() {
	e.refs.Add(1)
}

func (e *CallEnd) unref() {
	if e.refs.Add(-1) != 0 {
		return
	}

	c := e.c

	switch {
	case !e.serves:
		c.mu.Lock()
		c.said.Or(callLeft)

		files := append(c.request.files, c.answer.files...)

		c.request, c.answer = Packet{}, Packet{}
		c.there.Store(false)
		c.serving.wake(Turn{})
		c.servers.tell()
		c.mu.Unlock()
		dropFiles(files)
	case e.passed:
		c.mu.Lock()

		open := e.armed && e.seq == c.seq && c.phase.Load() == callReceived

		if e.armed && e.seq == c.seq {
			c.handed--
		}

		if open {
			c.end()
			c.askers.tell()
		}

		c.mu.Unlock()
	default:
		c.mu.Lock()
		c.served.Store(false)

		phase := c.phase.Load()
		open := phase == callSent || phase == callReceived && c.handed == 0

		if open {
			c.end()
		} else {
			c.give()
		}

		c.askers.tell()
		c.mu.Unlock()
	}
}

func (c *Call) end() {
	dropFiles(c.request.files)
	c.request = Packet{}
	c.phase.Store(callNone)
	c.said.And(^uint32(callCancel))
	c.ended.Store(true)
	c.give()
}

func (c *Call) give() {
	if c.asking.first == nil {
		return
	}

	if c.there.Load() || c.ended.Load() {
		var got Received
		_, got.eof = c.found(&got.pk, false)
		c.asking.pop().wake(got)
	}

	if !c.served.Load() && c.phase.Load() == callNone {
		c.asking.wake(Received{eof: true})
	}
}

func (c *Call) found(pk *Packet, peek bool) (ok, eof bool) {
	switch {
	case c.ended.Load():
		if !peek {
			c.ended.Store(false)
		}

		return true, true
	case c.there.Load():
		*pk = c.answer

		if !peek {
			c.answer = Packet{}
			c.there.Store(false)
		}

		return true, false
	case !c.served.Load() && c.phase.Load() == callNone:
		return true, true
	}

	return false, false
}

func (e *CallEnd) pass() *CallEnd {
	if !e.serves || e.passed {
		return e
	}

	e.c.mu.Lock()

	sent := &CallEnd{c: e.c, serves: true, passed: true, seq: e.c.seq}

	e.c.mu.Unlock()

	return sent
}

func (e *CallEnd) arm() {
	if !e.passed {
		return
	}

	c := e.c

	c.mu.Lock()

	if !e.armed {
		e.armed = true

		if e.seq == c.seq {
			c.handed++
		}
	}

	c.mu.Unlock()
}

func armFiles(files []Object) {
	for _, f := range files {
		if e := f.asCall(); e != nil {
			e.arm()
		}
	}
}

func (e *CallEnd) ask(w *Waiter[Received], data []byte, files []Object) error {
	c := e.c

	c.mu.Lock()

	if !c.served.Load() {
		c.mu.Unlock()

		return ePipe
	}

	if c.phase.Load() != callNone || c.there.Load() || c.ended.Load() {
		c.mu.Unlock()

		return eBusy
	}

	for _, f := range files {
		f.ref()
	}

	c.request = Packet{data: data, files: files}
	c.seq++
	c.handed = 0
	c.phase.Store(callSent)

	if w != nil {
		c.asking.push(w)
	}

	c.serving.wake(Turn{})
	c.servers.tell()
	c.mu.Unlock()

	return nil
}

func (e *CallEnd) idle() bool {
	c := e.c

	return c.served.Load() && c.phase.Load() == callNone && !c.there.Load() && !c.ended.Load()
}

func (e *CallEnd) forget(w *Waiter[Received]) bool {
	c := e.c

	c.mu.Lock()

	defer c.mu.Unlock()

	return c.asking.remove(w)
}

func (e *CallEnd) leave(p *Process, w *Waiter[Received]) {
	if !w.done.Load() {
		e.forget(w)
	}

	p.receives.put(w)
}

func (e *CallEnd) cancel(p *Process) {
	c := e.c

	c.mu.Lock()

	if c.phase.Load() != callNone {
		c.uid, c.gid = p.cred.euid(), p.cred.egid()
		c.said.Or(callCancel)
	}

	c.serving.wake(Turn{})
	c.servers.tell()
	c.mu.Unlock()
}

func (e *CallEnd) reply(gather func(small []byte) ([]byte, error), files []Object) (int, error) {
	c := e.c

	c.mu.Lock()

	if c.said.Load()&callLeft != 0 || c.phase.Load() != callReceived || e.passed && e.seq != c.seq {
		c.mu.Unlock()

		return 0, ePipe
	}

	data, err := gather(c.small[:])

	if err != nil {
		c.mu.Unlock()

		return 0, err
	}

	for _, f := range files {
		f.ref()
	}

	c.phase.Store(callNone)
	c.said.And(^uint32(callCancel))

	if w := c.asking.pop(); w != nil {
		w.wake(Received{pk: Packet{data: data, files: files}})
	} else {
		c.answer = Packet{data: data, files: files}
		c.there.Store(true)
	}

	c.askers.tell()
	c.mu.Unlock()

	return len(data), nil
}

func (e *CallEnd) has() bool {
	return !e.passed || e.seq == e.c.seq
}

func (e *CallEnd) hears() bool {
	if e.passed {
		return e.seq == e.c.seq
	}

	return e.c.handed == 0
}

func (e *CallEnd) heard(pk *Packet, peek bool) (ok, eof bool) {
	c := e.c

	if !e.passed && c.phase.Load() == callSent {
		if pk != nil {
			*pk = c.request
		}

		if !peek {
			c.request = Packet{}
			c.phase.Store(callReceived)
		}

		return true, false
	}

	if c.said.Load()&callCancel != 0 && c.phase.Load() == callReceived && e.hears() {
		if pk != nil {
			data := make([]byte, remoteHeader)

			binary.LittleEndian.PutUint32(data, rCancel)
			binary.LittleEndian.PutUint32(data[8:], c.uid)
			binary.LittleEndian.PutUint32(data[12:], c.gid)
			*pk = Packet{data: data}
		}

		if !peek {
			c.said.And(^uint32(callCancel))
		}

		return true, false
	}

	if c.said.Load()&callLeft != 0 || e.passed && (e.seq != c.seq || c.phase.Load() != callReceived) {
		return true, true
	}

	return false, false
}

func (e *CallEnd) asFile() File {
	return e
}

func (e *CallEnd) asTransfer() Transfer {
	return e
}

func (e *CallEnd) asSeeker() Seeker {
	return nil
}

func (e *CallEnd) asDir() Dir {
	return nil
}

func (e *CallEnd) asTerminal() Terminal {
	return nil
}

func (e *CallEnd) asWindowed() Windowed {
	return nil
}

func (e *CallEnd) asLink() Link {
	return nil
}

func (e *CallEnd) asAttrs() Attrs {
	return nil
}

func (e *CallEnd) asSocket() Socket {
	return e
}

func (e *CallEnd) asIoctl() Ioctl {
	return nil
}

func (e *CallEnd) asLocker() Locker {
	return nil
}

func (e *CallEnd) asEpoll() *Epoll {
	return nil
}

func (e *CallEnd) asMemfd() *MemfdFile {
	return nil
}

func (e *CallEnd) asPair() *PairEnd {
	return nil
}

func (e *CallEnd) asListener() *UnixListener {
	return nil
}

func (e *CallEnd) asConsole() *ConsoleEnd {
	return nil
}

func (e *CallEnd) asVFS() *RemoteDir {
	return nil
}

func (e *CallEnd) asCall() *CallEnd {
	return e
}

func (e *CallEnd) poll(events int16) int16 {
	c := e.c
	mask := int16(0)

	if !e.serves {
		if c.there.Load() || c.ended.Load() {
			mask |= pollIn | pollRdnorm
		}

		if !c.served.Load() {
			mask |= pollHup
		} else if e.idle() {
			mask |= pollOut | pollWrnorm
		}

		return mask & (events | pollErr | pollHup)
	}

	c.mu.Lock()

	ok, eof := e.heard(nil, true)

	c.mu.Unlock()

	if ok {
		mask |= pollIn | pollRdnorm

		if eof {
			mask |= pollHup
		}
	}

	if c.phase.Load() == callReceived && e.has() {
		mask |= pollOut | pollWrnorm
	}

	return mask & (events | pollErr | pollHup)
}

func (e *CallEnd) subscribe(s *Subscription, events int16) {
	c := e.c

	c.mu.Lock()

	if e.serves {
		c.servers.add(&s.in)
	} else {
		c.askers.add(&s.in)
	}

	c.mu.Unlock()
}

func (e *CallEnd) unsubscribe(s *Subscription) {
	c := e.c

	c.mu.Lock()
	c.servers.remove(&s.in)
	c.askers.remove(&s.in)
	c.mu.Unlock()
}

func (e *CallEnd) stat(p *Process, st *Stat) error {
	c := e.c

	if c.ino.Load() == 0 {
		c.ino.CompareAndSwap(0, pairInodes.Add(1))
	}

	*st = Stat{Ino: c.ino.Load(), Mode: sIfsock | 0o777, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (e *CallEnd) flags() (int, error) {
	fl := oRdwr

	if e.nonblock.Load() {
		fl |= oNonblock
	}

	return fl, nil
}

func (e *CallEnd) setFlags(fl int) error {
	e.nonblock.Store(fl&oNonblock != 0)

	return nil
}

func (e *CallEnd) put(gather func(small []byte) ([]byte, error), files []Object) (int, error) {
	if e.serves {
		return e.reply(gather, files)
	}

	data, err := gather(nil)

	if err != nil {
		return 0, err
	}

	return len(data), e.ask(nil, data, files)
}

func (e *CallEnd) called(p *Process, pk *Packet, peek, wait bool) (ok, eof bool, err error) {
	c := e.c

	c.mu.Lock()
	ok, eof = e.heard(pk, peek)

	if ok || !wait {
		c.mu.Unlock()

		return ok, eof, nil
	}

	w := p.turns.get(p)

	c.serving.push(w)
	c.mu.Unlock()

	defer func() {
		if !w.done.Load() {
			c.mu.Lock()
			c.serving.remove(w)
			c.mu.Unlock()
		}

		p.turns.put(w)
	}()

	_, err = p.await(&w.done, time.Time{}, restarts)

	return false, false, err
}

func (e *CallEnd) answered(p *Process, pk *Packet, peek, wait bool) (ok, eof bool, err error) {
	c := e.c

	c.mu.Lock()
	ok, eof = c.found(pk, peek)

	if ok || !wait {
		c.mu.Unlock()

		return ok, eof, nil
	}

	w := p.receives.get(p)

	c.asking.push(w)
	c.mu.Unlock()

	defer e.leave(p, w)

	if _, err := p.await(&w.done, time.Time{}, restarts); err != nil && e.forget(w) {
		return false, false, err
	}

	*pk = w.answer.pk

	return true, w.answer.eof, nil
}

func (e *CallEnd) receive(p *Process, bufs [][]byte, flags uint32, control, room uint32) (int, uint32, uint32, error) {
	nonblock := flags&msgDontwait != 0 || e.nonblock.Load()
	peek := flags&msgPeek != 0

	for {
		var pk Packet
		var ok, eof bool
		var err error

		if e.serves {
			ok, eof, err = e.called(p, &pk, peek, !nonblock && !peek)
		} else {
			ok, eof, err = e.answered(p, &pk, peek, !nonblock && !peek)
		}

		if err != nil {
			return 0, 0, 0, err
		}

		if ok && eof {
			return 0, 0, 0, nil
		}

		if ok {
			n, out, rest := 0, uint32(0), pk.data

			for _, b := range bufs {
				k := copy(b, rest)

				rest = rest[k:]
				n += k
			}

			if len(rest) > 0 {
				out |= msgTrunc
			}

			if flags&msgTrunc != 0 {
				n = len(pk.data)
			}

			if peek {
				return n, 0, out, nil
			}

			written, cut := p.putRights(pk.files, control, room, flags&msgCmsgCloexec != 0)

			return n, written, out | cut, nil
		}

		if nonblock {
			return 0, 0, 0, eAgain
		}

		if peek {
			if _, err := p.waitObject(e, pollIn, -1, true); err != nil {
				return 0, 0, 0, err
			}
		}
	}
}

func (e *CallEnd) read(p *Process, b []byte, mode IoMode) (int, error) {
	flags := uint32(0)

	if mode.nonblock {
		flags = msgDontwait
	}

	n, _, _, err := e.receive(p, [][]byte{b}, flags, 0, 0)

	return n, err
}

func (e *CallEnd) write(p *Process, b []byte, mode IoMode) (int, error) {
	if len(b) > packetMax {
		return 0, eMsgsize
	}

	return e.put(func(small []byte) ([]byte, error) {
		if len(b) <= len(small) {
			return small[:copy(small, b)], nil
		}

		return append([]byte(nil), b...), nil
	}, nil)
}

func (e *CallEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, eSpipe
}

func (e *CallEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, eSpipe
}

func (e *CallEnd) bind(p *Process, at, n uint32) error {
	return eInval
}

func (e *CallEnd) connect(p *Process, at, n uint32) error {
	return eIsconn
}

func (e *CallEnd) listen(backlog int) error {
	return eInval
}

func (e *CallEnd) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, eInval
}

func (e *CallEnd) sockname(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (e *CallEnd) peername(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (e *CallEnd) socketKind() int {
	return sockCall
}

func (e *CallEnd) shutdown(how int) error {
	return eOpnotsupp
}

func (e *CallEnd) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	return eNoprotoopt
}

func (e *CallEnd) setsockopt(p *Process, level, name, at, n uint32) error {
	return eNoprotoopt
}

func (e *CallEnd) sendmsg(p *Process, at, flags uint32) (int, error) {
	h := p.span(at, sizeofMsghdr)

	if binary.LittleEndian.Uint32(h) != 0 {
		return 0, eIsconn
	}

	files, _, err := p.rights(binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:]))

	if err != nil {
		return 0, err
	}

	n, err := e.put(func(small []byte) ([]byte, error) {
		return p.gatherIn(small, binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:]), packetMax)
	}, files)

	if err != nil {
		return 0, err
	}

	armFiles(files)

	return n, nil
}

func (e *CallEnd) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	if addr != 0 {
		return 0, eIsconn
	}

	return e.write(p, p.span(buf, n), IoMode{})
}

func (e *CallEnd) recvmsg(p *Process, at, flags uint32) (int, error) {
	var few [4][]byte

	spans, err := p.ioSpansIn(few[:0], p.u32(at+8), p.u32(at+12))

	if err != nil {
		return 0, err
	}

	n, written, out, err := e.receive(p, spans, flags, p.u32(at+16), p.u32(at+20))

	if err != nil {
		return 0, err
	}

	if p.u32(at) != 0 {
		p.put32(at+4, 0)
	}

	p.put32(at+20, written)
	p.put32(at+24, out|flags&msgCmsgCloexec)

	return n, nil
}

func (e *CallEnd) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	got, _, _, err := e.receive(p, [][]byte{p.span(buf, n)}, flags, 0, 0)

	return got, nil, err
}
