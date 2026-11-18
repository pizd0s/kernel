//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// A call pair is what a call and its answer go on: socketpair's SOCK_CALL
// (lib/c's sys/socket.h), a connection to a server as light as one
// exchange is. Its first end serves, its second asks. One call is in hand
// at a time and nothing is queued: the asking end sends the call, a
// packet, files and all, the serving end receives it and sends its
// answer, a packet too, and the asking end receives that. A call sent
// before the answer to the one before was received is EBUSY, and an
// answer to no call the serving end received EPIPE. A connection of the
// file protocol is such a pair, which a server makes for who asks to
// connect and whose asking end the connection's OK brings
// (kernel_remote.go).
//
// The asking end tells of itself what a signal makes its caller say, its
// CANCEL of the call in hand, which the serving end receives after the
// call, a request's header, and its going, the end of the data. The
// serving end's going with a call unanswered is the end of the data to
// the asking end, the server gone, and a call sent after it EPIPE.
//
// A serving end sent to another, with a packet, is the call in hand:
// who receives it may answer that call, once, and no call after it, and
// hears its CANCEL; if it lets go of it unanswered, the asking end reads
// the end of the data, whoever else holds the serving end. So a server
// hands a call on with its serving end and is out of the way, and one
// that dies with a call handed to it fails that call alone.
type call struct {
	mu sync.Mutex
	// phase is where the call in hand is: none, sent, or received and not
	// answered yet. seq counts the calls sent.
	phase atomic.Uint32
	seq   uint64
	// request is the call until the serving end receives it; answer the
	// answer until the asking end does, there says there is one, ended
	// that the call in hand ended with none.
	request packet
	answer  packet
	there   atomic.Bool
	ended   atomic.Bool
	// said is what the asking end said: callCancel while a CANCEL of the
	// call in hand is there to receive, callLeft once it is gone. uid and
	// gid are the CANCEL's, the caller's.
	said     atomic.Uint32
	uid, gid uint32
	// served is unset once the serving end the pair was made with is
	// gone; handed counts the ends the call in hand was sent on with that
	// are held still.
	served atomic.Bool
	handed int
	// to is the process a call of the kernel's waits in, note its event of
	// the answer; servers are the subscriptions of the serving ends, told
	// of a call, a CANCEL and the asking end's going, askers those of an
	// asking end a program holds.
	to      *Process
	note    event
	servers subscribers
	askers  subscribers
	ino     atomic.Uint64
	// small is where a short answer's bytes are kept: none is allocated
	// then.
	small [96]byte
}

const (
	callNone = iota
	callSent
	callReceived
)

const (
	callCancel = 1 << iota
	callLeft
)

// A callEnd is an end of a call pair: the serving one, the asking one, or
// a serving end sent on, which is the call numbered seq, armed once the
// packet it went with was queued.
type callEnd struct {
	c        *call
	refs     atomic.Int32
	serves   bool
	passed   bool
	armed    bool
	seq      uint64
	nonblock atomic.Bool
}

// newCall makes a call pair: the serving end and the asking end, a
// reference to each.
func newCall(nonblock bool) (*callEnd, *callEnd) {
	c := &call{}
	c.served.Store(true)
	c.note.kind = eventChanged
	serving, asking := &callEnd{c: c, serves: true}, &callEnd{c: c}

	for _, e := range []*callEnd{serving, asking} {
		e.refs.Store(1)
		e.nonblock.Store(nonblock)
	}

	return serving, asking
}

func (e *callEnd) ref() {
	e.refs.Add(1)
}

// unref is the end's going when its last reference goes.
func (e *callEnd) unref() {
	if e.refs.Add(-1) != 0 {
		return
	}

	c := e.c

	switch {
	case !e.serves:
		c.mu.Lock()
		c.said.Or(callLeft)
		files := append(c.request.files, c.answer.files...)
		c.request, c.answer = packet{}, packet{}
		c.there.Store(false)
		to := c.to
		c.to = nil
		c.mu.Unlock()
		dropFiles(files)

		if to != nil {
			to.events.drop(&c.note)
		}

		c.servers.tell()
	case e.passed:
		c.mu.Lock()
		open := e.armed && e.seq == c.seq && c.phase.Load() == callReceived

		if e.armed && e.seq == c.seq {
			c.handed--
		}

		if open {
			c.end()
		}

		c.mu.Unlock()

		if open {
			c.wake()
		}
	default:
		c.mu.Lock()
		c.served.Store(false)
		phase := c.phase.Load()
		open := phase == callSent || phase == callReceived && c.handed == 0

		if open {
			c.end()
		}

		c.mu.Unlock()
		c.wake()
	}
}

// end ends the call in hand with no answer, with the lock: the request
// nobody received goes, files and all.
func (c *call) end() {
	dropFiles(c.request.files)
	c.request = packet{}
	c.phase.Store(callNone)
	c.said.And(^uint32(callCancel))
	c.ended.Store(true)
}

// answered tells whether the call in hand has its answer to take, or
// ended with none.
func (e *callEnd) answered() bool {
	return e.c.there.Load() || e.c.ended.Load()
}

// wake wakes who waits for the answer: the answer came, or the call
// ended with none, or the server went.
func (c *call) wake() {
	c.mu.Lock()
	to := c.to
	c.mu.Unlock()

	if to != nil {
		to.post(&c.note)
	}

	c.askers.tell()
}

// pass is the end as it goes with a packet: a serving end is the call in
// hand, an end of its own with no reference yet, which the packet takes.
func (e *callEnd) pass() *callEnd {
	if !e.serves || e.passed {
		return e
	}

	e.c.mu.Lock()
	sent := &callEnd{c: e.c, serves: true, passed: true, seq: e.c.seq}
	e.c.mu.Unlock()

	return sent
}

// arm is the packet the end went with queued: from now on the end's
// going unanswered ends its call.
func (e *callEnd) arm() {
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

// armFiles arms the call ends among the files of a packet that was
// queued.
func armFiles(files []Object) {
	for _, f := range files {
		if e := f.asCall(); e != nil {
			e.arm()
		}
	}
}

// ask sends a call for p, a kernel's call to a server: data given away,
// with the files, which stay the caller's. EPIPE when the server is
// gone, EBUSY with a call in hand or its answer not taken.
func (e *callEnd) ask(p *Process, data []byte, files []Object) error {
	c := e.c
	c.mu.Lock()

	if !c.served.Load() {
		c.mu.Unlock()

		return syscall.EPIPE
	}

	if c.phase.Load() != callNone || c.there.Load() || c.ended.Load() {
		c.mu.Unlock()

		return syscall.EBUSY
	}

	for _, f := range files {
		f.ref()
	}

	c.request = packet{data: data, files: files}
	c.seq++
	c.handed = 0
	c.to = p
	c.phase.Store(callSent)
	c.mu.Unlock()
	c.servers.tell()

	return nil
}

// idle tells whether the pair takes a call now: none in hand, no answer
// left to take, the server there.
func (e *callEnd) idle() bool {
	c := e.c

	return c.served.Load() && c.phase.Load() == callNone && !c.there.Load() && !c.ended.Load()
}

// wait waits for the answer to the call in hand into pk, and tells of
// the end: eof when the call ended with none. The process takes its
// signals as it waits, and waits on.
func (e *callEnd) wait(p *Process, pk *packet) (eof bool) {
	c := e.c

	if !c.there.Load() && !c.ended.Load() {
		p.wait(time.Time{}, func(*event) (bool, error) {
			return c.there.Load() || c.ended.Load(), nil
		})
	}

	return e.take(pk)
}

// take is the answer that came, into pk: eof when the call ended with
// none, which is taken too.
func (e *callEnd) take(pk *packet) (eof bool) {
	c := e.c
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ended.Load() {
		c.ended.Store(false)

		return true
	}

	*pk = c.answer
	c.answer = packet{}
	c.there.Store(false)

	return false
}

// cancel is the caller's CANCEL of the call in hand, for who serves it to
// receive.
func (e *callEnd) cancel(p *Process) {
	c := e.c
	c.mu.Lock()

	if c.phase.Load() != callNone {
		c.uid, c.gid = p.cred.euid(), p.cred.egid()
		c.said.Or(callCancel)
	}

	c.mu.Unlock()
	c.servers.tell()
}

// reply is a serving end's answer to the call it has: the bytes gather
// gives, which may put a short answer's where the pair keeps them, small,
// and the files, with references of their own. It is the count of the
// bytes; EPIPE when the end has no call to answer: none received, one
// answered, another's, or the asker gone.
func (e *callEnd) reply(gather func(small []byte) ([]byte, error), files []Object) (int, error) {
	c := e.c
	c.mu.Lock()

	if c.said.Load()&callLeft != 0 || c.phase.Load() != callReceived || e.passed && e.seq != c.seq {
		c.mu.Unlock()

		return 0, syscall.EPIPE
	}

	data, err := gather(c.small[:])

	if err != nil {
		c.mu.Unlock()

		return 0, err
	}

	for _, f := range files {
		f.ref()
	}

	c.answer = packet{data: data, files: files}
	c.there.Store(true)
	c.phase.Store(callNone)
	c.said.And(^uint32(callCancel))
	c.mu.Unlock()
	c.wake()

	return len(data), nil
}

// has tells whether the serving end has the call in hand to answer: the
// pair's own any, one sent on the call it is.
func (e *callEnd) has() bool {
	return !e.passed || e.seq == e.c.seq
}

// hears tells whether the call's CANCEL is the serving end's to receive,
// with the lock: who has the call, the end it was sent on with, or the
// pair's own while it is sent on with none.
func (e *callEnd) hears() bool {
	if e.passed {
		return e.seq == e.c.seq
	}

	return e.c.handed == 0
}

// hear is what a serving end receives without waiting: the call, once; a
// CANCEL of the call it has, a request's header, once; or the end, eof,
// when the asker is gone or the call a sent end is is over. ok false when
// there is nothing yet. With peek nothing is taken.
func (e *callEnd) hear(peek bool) (pk packet, eof, ok bool) {
	c := e.c
	c.mu.Lock()
	defer c.mu.Unlock()

	if !e.passed && c.phase.Load() == callSent {
		pk = c.request

		if !peek {
			c.request = packet{}
			c.phase.Store(callReceived)
		}

		return pk, false, true
	}

	if c.said.Load()&callCancel != 0 && c.phase.Load() == callReceived && e.hears() {
		data := make([]byte, remoteHeader)
		binary.LittleEndian.PutUint32(data, rCancel)
		binary.LittleEndian.PutUint32(data[8:], c.uid)
		binary.LittleEndian.PutUint32(data[12:], c.gid)

		if !peek {
			c.said.And(^uint32(callCancel))
		}

		return packet{data: data}, false, true
	}

	if c.said.Load()&callLeft != 0 || e.passed && (e.seq != c.seq || c.phase.Load() != callReceived) {
		return packet{}, true, true
	}

	return packet{}, false, false
}

// A call end is a File, a Transfer and a Socket: a call and an answer are
// a send and a receive.
func (e *callEnd) asFile() File              { return e }
func (e *callEnd) asTransfer() Transfer      { return e }
func (e *callEnd) asSeeker() Seeker          { return nil }
func (e *callEnd) asDir() Dir                { return nil }
func (e *callEnd) asTerminal() Terminal      { return nil }
func (e *callEnd) asWindowed() Windowed      { return nil }
func (e *callEnd) asLink() Link              { return nil }
func (e *callEnd) asAttrs() Attrs            { return nil }
func (e *callEnd) asSocket() Socket          { return e }
func (e *callEnd) asIoctl() Ioctl            { return nil }
func (e *callEnd) asLocker() Locker          { return nil }
func (e *callEnd) asEpoll() *epoll           { return nil }
func (e *callEnd) asMemfd() *memfdFile       { return nil }
func (e *callEnd) asPair() *pairEnd          { return nil }
func (e *callEnd) asListener() *unixListener { return nil }
func (e *callEnd) asConsole() *consoleEnd    { return nil }
func (e *callEnd) asVFS() *remoteDir         { return nil }
func (e *callEnd) asCall() *callEnd          { return e }

// poll: a serving end reads a call, a CANCEL and the asker's going, a
// hangup too, and writes while it has a call to answer; an asking end
// reads the answer and the server's going, and writes while no call is
// in hand.
func (e *callEnd) poll(events int16) int16 {
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

	if _, eof, ok := e.hear(true); ok {
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

func (e *callEnd) subscribe(s *subscription, events int16) {
	if e.serves {
		e.c.servers.add(&s.in)
	} else {
		e.c.askers.add(&s.in)
	}
}

func (e *callEnd) unsubscribe(s *subscription) {
	if e.serves {
		e.c.servers.remove(&s.in)
	} else {
		e.c.askers.remove(&s.in)
	}
}

func (e *callEnd) stat(p *Process, st *syscall.Stat_t) error {
	c := e.c

	if c.ino.Load() == 0 {
		c.ino.CompareAndSwap(0, pairInodes.Add(1))
	}

	*st = syscall.Stat_t{Ino: c.ino.Load(), Mode: syscall.S_IFSOCK | 0o777, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (e *callEnd) flags() (int, error) {
	fl := syscall.O_RDWR

	if e.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (e *callEnd) setFlags(fl int) error {
	e.nonblock.Store(fl&syscall.O_NONBLOCK != 0)

	return nil
}

// put sends a packet of the end's, the bytes gather gives: a serving
// end's is its answer, an asking end's a call of the holder's own.
func (e *callEnd) put(gather func(small []byte) ([]byte, error), files []Object) (int, error) {
	if e.serves {
		return e.reply(gather, files)
	}

	data, err := gather(nil)

	if err != nil {
		return 0, err
	}

	return len(data), e.ask(nil, data, files)
}

// receive reads a packet into the holder's buffers, waiting for it unless
// the read does not block: a serving end's the call or what the asker
// said, an asking end's the answer. The files go to a control buffer of
// room bytes at control. It returns the count, the control bytes written
// and the flags of the message, MSG_TRUNC and MSG_CTRUNC; a count of 0
// with no error is the end.
func (e *callEnd) receive(p *Process, bufs [][]byte, flags uint32, control, room uint32) (int, uint32, uint32, error) {
	nonblock := flags&msgDontwait != 0 || e.nonblock.Load()
	peek := flags&msgPeek != 0

	for {
		var pk packet
		var eof, ok bool

		if e.serves {
			pk, eof, ok = e.hear(peek)
		} else if c := e.c; c.there.Load() || c.ended.Load() {
			if peek {
				c.mu.Lock()
				pk, eof, ok = c.answer, c.ended.Load(), true
				c.mu.Unlock()
			} else {
				eof, ok = e.take(&pk), true
			}
		} else if !c.served.Load() && c.phase.Load() == callNone {
			eof, ok = true, true
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
			return 0, 0, 0, syscall.EAGAIN
		}

		if _, err := p.waitObject(e, pollIn, -1, true); err != nil {
			return 0, 0, 0, err
		}
	}
}

func (e *callEnd) read(p *Process, b []byte, mode ioMode) (int, error) {
	flags := uint32(0)

	if mode.nonblock {
		flags = msgDontwait
	}

	n, _, _, err := e.receive(p, [][]byte{b}, flags, 0, 0)

	return n, err
}

func (e *callEnd) write(p *Process, b []byte, mode ioMode) (int, error) {
	if len(b) > packetMax {
		return 0, syscall.EMSGSIZE
	}

	return e.put(func(small []byte) ([]byte, error) {
		if len(b) <= len(small) {
			return small[:copy(small, b)], nil
		}

		return append([]byte(nil), b...), nil
	}, nil)
}

func (e *callEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (e *callEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

// The socket calls: connected and unnamed, as a pair's end, with no
// options.

func (e *callEnd) bind(p *Process, at, n uint32) error {
	return syscall.EINVAL
}

func (e *callEnd) connect(p *Process, at, n uint32) error {
	return syscall.EISCONN
}

func (e *callEnd) listen(backlog int) error {
	return syscall.EINVAL
}

func (e *callEnd) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, syscall.EINVAL
}

func (e *callEnd) sockname(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (e *callEnd) peername(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (e *callEnd) socketKind() int {
	return sockCall
}

func (e *callEnd) shutdown(how int) error {
	return syscall.EOPNOTSUPP
}

func (e *callEnd) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	return syscall.ENOPROTOOPT
}

func (e *callEnd) setsockopt(p *Process, level, name, at, n uint32) error {
	return syscall.ENOPROTOOPT
}

// sendmsg sends the guest's msghdr as the end's packet: the descriptors
// of its SCM_RIGHTS the packet's files, its iovecs gathered into its
// bytes, a short answer's where the pair keeps them.
func (e *callEnd) sendmsg(p *Process, at, flags uint32) (int, error) {
	h := p.span(at, sizeofMsghdr)

	if binary.LittleEndian.Uint32(h) != 0 {
		return 0, syscall.EISCONN
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

func (e *callEnd) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	if addr != 0 {
		return 0, syscall.EISCONN
	}

	return e.write(p, p.span(buf, n), ioMode{})
}

// recvmsg reads the end's packet into the guest's msghdr: its files into
// the control buffer, no address.
func (e *callEnd) recvmsg(p *Process, at, flags uint32) (int, error) {
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

func (e *callEnd) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	got, _, _, err := e.receive(p, [][]byte{p.span(buf, n)}, flags, 0, 0)

	return got, nil, err
}
