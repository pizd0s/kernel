//go:build linux && amd64

package main

import (
	"encoding/binary"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// A pair is the kernel's, of AF_UNIX sockets of a kind, SOCK_SEQPACKET,
// SOCK_STREAM or SOCK_DGRAM: two channels of messages (kernel_queue.go),
// one each way, and an end of the pair reads one and writes the other; a
// wait for an end subscribes to the channel it reads, for input, and to
// the one it writes, for room. A packet is bytes, the files sent with
// them, SCM_RIGHTS, which the receiver's table takes, and the sender's
// credentials, which go to a receiver that asks for them (SO_PASSCRED).
// A read of a seqpacket or a datagram pair takes one packet, as much of
// it as fits, its files only when a control buffer asks for them; a read
// of a stream takes bytes, those of the packets queued as far as they
// fit, a packet with files only first, the read ending with it, and what
// of a packet does not fit is the next read's; a write of a stream goes
// in packets of at most packetMax bytes, its files with the first. The
// last close of an end ends the channel it wrote, EOF to the other end,
// but for a datagram pair, which has no end of data, and finishes the one
// it read, EPIPE to the other end's writes, ECONNREFUSED to a datagram
// pair's. Seqpacket pairs carry the file protocol (kernel_remote.go).
const (
	sockStream     = 1
	sockDgram      = 2
	sockSeqpacket  = 5
	packetMax      = 65536
	queueMax       = 64
	msgTrunc       = 0x20
	msgPeek        = 0x2
	msgWaitall     = 0x100
	soType         = 3
	soError        = 4
	soSndbuf       = 7
	soRcvbuf       = 8
	soPasscred     = 16
	soPeercred     = 17
	soRcvtimeo     = 20
	soSndtimeo     = 21
	scmCredentials = 2
	ucredSize      = 12
	shutRd         = 0
	shutWr         = 1
	shutRdwr       = 2
)

// A packet is bytes, files and the sender's credentials, and the address
// of a datagram's sender, the path it is bound to (kernel_unix.go).
type packet struct {
	data  []byte
	files []File
	cred  *ucred
	addr  []byte
}

// ucred is SCM_CREDENTIALS' struct ucred: the pid, uid and gid of a
// sender.
type ucred struct {
	pid, uid, gid uint32
}

// ucred is the process's own: its pid and real ids.
func (p *Process) ucred() *ucred {
	return &ucred{uint32(p.pid), p.cred.uid(), p.cred.gid()}
}

// dropFiles lets go of the files of packets nobody takes.
func dropFiles(files []File) {
	for _, f := range files {
		f.unref()
	}
}

// pairState is what both ends share: the inode and the kind.
type pairState struct {
	ino  uint64
	made time.Time
	kind int
}

type pairEnd struct {
	shared *pairState
	peer   *pairEnd
	// to is what the end writes, from what it reads.
	to, from *msgChannel
	refs     atomic.Int32
	nonblock atomic.Bool
	// dead is set once the end closed its channels; noWrite and noRead
	// are shutdown's.
	dead    atomic.Bool
	noWrite atomic.Bool
	noRead  atomic.Bool
	// front is a packet MSG_PEEK took out of the channel, or what of a
	// packet a read of a stream left, the next read's; peeked says there is
	// one, for a poll to ask without mu.
	mu     sync.Mutex
	front  *packet
	peeked atomic.Bool
	sockOptions
}

// sockOptions are the options of the socket level a socket of the
// kernel's keeps: SO_RCVTIMEO's and SO_SNDTIMEO's nanoseconds, zero for
// none, SO_PASSCRED, and the options of soInts set, ints, with intsMu.
type sockOptions struct {
	rcvTimeout, sndTimeout atomic.Int64
	passcred               atomic.Bool
	intsMu                 sync.Mutex
	ints                   map[uint32]uint32
}

// copyOptions gives o the options of from, as a connection gets those of
// its socket.
func (o *sockOptions) copyOptions(from *sockOptions) {
	o.rcvTimeout.Store(from.rcvTimeout.Load())
	o.sndTimeout.Store(from.sndTimeout.Load())
	o.passcred.Store(from.passcred.Load())
	from.intsMu.Lock()
	ints := maps.Clone(from.ints)
	from.intsMu.Unlock()
	o.intsMu.Lock()
	o.ints = ints
	o.intsMu.Unlock()
}

// soInts are the options of the socket level that are ints a socket keeps
// as they were set, those true Linux's booleans, 0 or 1: SO_DEBUG,
// SO_REUSEADDR, SO_DONTROUTE, SO_BROADCAST, SO_KEEPALIVE, SO_OOBINLINE,
// SO_PRIORITY, SO_REUSEPORT and SO_RCVLOWAT, at least 1.
var soInts = map[uint32]bool{1: true, 2: true, 5: true, 6: true, 9: true, 10: true, 12: false, 15: true, soRcvlowat: false}

const soRcvlowat = 18

// intOption is the value of an option of soInts.
func (o *sockOptions) intOption(name uint32) uint32 {
	o.intsMu.Lock()
	defer o.intsMu.Unlock()

	if v, ok := o.ints[name]; ok {
		return v
	}

	return bit(name == soRcvlowat)
}

var _ Socket = (*pairEnd)(nil)

var pairInodes atomic.Uint64

// newPair makes a seqpacket pair, newPairOf one of a kind, each end with
// one reference.
func newPair(nonblock bool) (*pairEnd, *pairEnd) {
	return newPairOf(sockSeqpacket, nonblock)
}

func newPairOf(kind int, nonblock bool) (*pairEnd, *pairEnd) {
	s := &pairState{ino: pairInodes.Add(1), made: time.Now(), kind: kind}
	ab, ba := newMsgChannel(queueMax), newMsgChannel(queueMax)
	a, b := &pairEnd{shared: s, to: ab, from: ba}, &pairEnd{shared: s, to: ba, from: ab}
	a.peer, b.peer = b, a

	for _, e := range []*pairEnd{a, b} {
		e.refs.Store(1)
		e.nonblock.Store(nonblock)
	}

	return a, b
}

func (e *pairEnd) ref() {
	e.refs.Add(1)
}

// endpoint is the end itself: what a directory of a server is made of
// (kernel_remote.go asRoot).
func (e *pairEnd) endpoint() *pairEnd {
	return e
}

// unref closes the end when its last reference goes: what it read is
// finished, what was queued for it dropped, files and all; what it wrote
// ends, which wakes the other end's queue: the end of its data, and EPIPE
// to its writes.
func (e *pairEnd) unref() {
	if e.refs.Add(-1) != 0 {
		return
	}

	e.from.finish()
	e.mu.Lock()

	if e.front != nil {
		dropFiles(e.front.files)
		e.front = nil
		e.peeked.Store(false)
	}

	e.mu.Unlock()
	e.dead.Store(true)
	e.to.end()
}

// subscribe makes to learn of what may make the end readable, for input,
// and writable, for output; of both when neither is asked.
func (e *pairEnd) subscribe(to waker, id uint64, events int16) bool {
	in, out := ioSides(events)

	if in {
		e.from.readers.add(to, id)
	}

	if out {
		e.to.writers.add(to, id)
	}

	return true
}

func (e *pairEnd) unsubscribe(to waker, id uint64) {
	e.from.readers.remove(to, id)
	e.to.writers.remove(to, id)
}

// ioSides tells whether events ask for input, for output; both when they
// ask for neither, for a wait that is for the hangup.
func ioSides(events int16) (in, out bool) {
	in, out = events&(pollIn|pollRdnorm|pollPri) != 0, events&(pollOut|pollWrnorm) != 0

	if !in && !out {
		in, out = true, true
	}

	return in, out
}

func (e *pairEnd) stat(p *Process, st *syscall.Stat_t) error {
	s := e.shared
	*st = syscall.Stat_t{Ino: s.ino, Mode: syscall.S_IFSOCK | 0o777, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = syscall.NsecToTimespec(s.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

// poll: the other end's going is the end of the data, a hangup, but for
// a datagram pair, which writes to it fail at once.
func (e *pairEnd) poll(events int16) int16 {
	mask := int16(0)
	dgram := e.shared.kind == sockDgram

	if e.peeked.Load() || e.from.count() > 0 || e.noRead.Load() || !dgram && (e.peer.dead.Load() || e.peer.noWrite.Load()) {
		mask |= pollIn | pollRdnorm
	}

	switch {
	case e.peer.dead.Load() || e.to.gone():
		mask |= pollOut | pollWrnorm

		if !dgram {
			mask |= pollHup
		}
	case e.to.room():
		mask |= pollOut | pollWrnorm
	}

	return mask & (events | pollErr | pollHup)
}

// wait waits for events of the end as its blocking call does, until the
// timeout of to, SO_RCVTIMEO's or SO_SNDTIMEO's: false when it passed. A
// signal is EINTR, after the handlers, which restart the call only
// without a timeout.
func (e *pairEnd) wait(p *Process, events int16, to *atomic.Int64) (bool, error) {
	timeout := -1

	if ns := to.Load(); ns > 0 {
		timeout = waitMs(time.Duration(ns))
	}

	ready, err := p.waitObject(e, events, timeout, timeout < 0)

	return ready > 0, err
}

func (e *pairEnd) flags() (int, error) {
	fl := syscall.O_RDWR

	if e.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (e *pairEnd) setFlags(fl int) error {
	e.nonblock.Store(fl&syscall.O_NONBLOCK != 0)

	return nil
}

// send queues a packet for the other end, blocking for room unless
// nonblock. The packet takes references of its own to the files before
// the other end can see it, and gives them back when it does not go; the
// sender keeps its own. EPIPE comes without SIGPIPE, as Linux has it for
// these.
func (e *pairEnd) send(p *Process, data []byte, files []File, mode ioMode) error {
	if len(data) > packetMax {
		return syscall.EMSGSIZE
	}

	return e.sendOwned(p, append([]byte(nil), data...), files, mode)
}

// sendOwned is send of data the caller gives away: the packet keeps it as
// it is.
func (e *pairEnd) sendOwned(p *Process, data []byte, files []File, mode ioMode) error {
	return e.sendCred(p, data, files, nil, mode)
}

// sendCred is sendOwned with the credentials the sender gave, SCM_CREDENTIALS',
// or none: a receiver that asks for them gets the sender's own then.
func (e *pairEnd) sendCred(p *Process, data []byte, files []File, cred *ucred, mode ioMode) error {
	if len(data) > packetMax {
		return syscall.EMSGSIZE
	}

	if cred == nil && p != nil && e.peer.passcred.Load() {
		cred = p.ucred()
	}

	for _, f := range files {
		f.ref()
	}

	err := e.queue(p, packet{data: data, files: files, cred: cred}, mode)

	if err != nil {
		dropFiles(files)
	}

	return err
}

// sendStream writes spans to a stream pair, in packets of at most
// packetMax bytes, the files and the credentials with the first, waiting
// for room between them: what was queued before EAGAIN, a signal or the
// timeout is the count. EPIPE is SIGPIPE too, unless quiet.
func (e *pairEnd) sendStream(p *Process, spans [][]byte, files []File, cred *ucred, mode ioMode) (int, error) {
	sent := 0

	for len(spans) > 0 {
		chunk := make([]byte, 0, min(totalLength(spans), packetMax))

		for len(spans) > 0 && len(chunk) < cap(chunk) {
			c := min(len(spans[0]), cap(chunk)-len(chunk))
			chunk = append(chunk, spans[0][:c]...)

			if spans[0] = spans[0][c:]; len(spans[0]) == 0 {
				spans = spans[1:]
			}
		}

		if len(chunk) == 0 {
			continue
		}

		if err := e.sendCred(p, chunk, files, cred, mode); err != nil {
			if err == syscall.EPIPE && !mode.quiet {
				p.sendSelf(int(syscall.SIGPIPE))
			}

			if sent > 0 {
				return sent, nil
			}

			return 0, err
		}

		sent += len(chunk)
		files, cred = nil, nil
	}

	return sent, nil
}

// totalLength is the bytes of spans.
func totalLength(spans [][]byte) int {
	n := 0

	for _, b := range spans {
		n += len(b)
	}

	return n
}

// kernelSend is a send of the kernel's own for p, a request to a server:
// data given away, with files, quiet, and no signal of p's ends its wait
// for room, which sleeps on p's mail. A nonblocking end is EAGAIN when
// full.
func (e *pairEnd) kernelSend(p *Process, data []byte, files []File) error {
	if len(data) > packetMax {
		return syscall.EMSGSIZE
	}

	for _, f := range files {
		f.ref()
	}

	err := e.kernelPut(p, packet{data: data, files: files})

	if err != nil {
		dropFiles(files)
	}

	return err
}

func (e *pairEnd) kernelPut(p *Process, pk packet) error {
	waiting := false

	defer func() {
		if waiting {
			e.to.writers.remove(p, 0)
		}
	}()

	for {
		p.takeMail()

		if e.noWrite.Load() {
			return syscall.EPIPE
		}

		full, err := e.to.put(pk)

		if err != nil || !full {
			return err
		}

		if e.nonblock.Load() {
			return syscall.EAGAIN
		}

		if !waiting {
			// A take from the full channel wakes its writers.
			e.to.writers.add(p, 0)
			waiting = true

			continue
		}

		p.waitMail(time.Time{})
	}
}

func (e *pairEnd) queue(p *Process, pk packet, mode ioMode) error {
	return e.queueAs(p, pk, mode.nonblock || e.nonblock.Load(), &e.sndTimeout)
}

// queueAs is queue for a sender of the end's that is not the end itself,
// a datagram socket of a name's (kernel_unix.go), with its own nonblock
// and timeout.
func (e *pairEnd) queueAs(p *Process, pk packet, nonblock bool, timeout *atomic.Int64) error {
	for {
		if e.noWrite.Load() {
			return syscall.EPIPE
		}

		full, err := e.to.put(pk)

		if err == syscall.EPIPE && e.shared.kind == sockDgram {
			return syscall.ECONNREFUSED
		}

		if err != nil || !full {
			return err
		}

		if nonblock {
			return syscall.EAGAIN
		}

		if ready, err := e.wait(p, pollOut, timeout); err != nil {
			return err
		} else if !ready {
			return syscall.EAGAIN
		}
	}
}

// take is the next packet without waiting: the one peeked, else the
// channel's; eof when the writer is gone, ok false when there is none
// yet; a datagram pair's end of data is only its own shutdown. A take
// that makes room in a full channel wakes the writer's queue.
func (e *pairEnd) take(peek bool) (pk packet, eof, ok bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.takeLocked(peek)
}

func (e *pairEnd) takeLocked(peek bool) (pk packet, eof, ok bool) {
	if e.front != nil {
		pk = *e.front

		if !peek {
			e.front = nil
			e.peeked.Store(false)
		}

		return pk, false, true
	}

	if e.noRead.Load() {
		return packet{}, true, true
	}

	// The other end's shutdown before the channel, as the channel's end
	// before its messages: what it wrote before it shut its writing down
	// is queued by then.
	dgram := e.shared.kind == sockDgram
	shut := !dgram && e.peer.noWrite.Load()
	pk, eof, ok, _ = e.from.take()

	if eof && dgram {
		return packet{}, false, false
	}

	if ok && !eof && peek {
		e.front = &pk
		e.peeked.Store(true)
	}

	if ok {
		return pk, eof, true
	}

	// Nothing queued: the end of the data when the other end shut its
	// writing down.
	if shut {
		return packet{}, true, true
	}

	return packet{}, false, false
}

// keepFront makes what of a packet a read of a stream did not take the
// next read's, with the lock.
func (e *pairEnd) keepFront(pk packet) {
	e.front = &pk
	e.peeked.Store(true)
}

// recv takes the next packet, or looks at it with peek: EOF is an empty
// packet with eof set.
func (e *pairEnd) recv(p *Process, mode ioMode, peek bool) (packet, bool, error) {
	nonblock := mode.nonblock || e.nonblock.Load()

	for {
		pk, eof, ok := e.take(peek)

		if ok {
			return pk, eof, nil
		}

		if nonblock {
			return packet{}, false, syscall.EAGAIN
		}

		if ready, err := e.wait(p, pollIn, &e.rcvTimeout); err != nil {
			return packet{}, false, err
		} else if !ready {
			return packet{}, false, syscall.EAGAIN
		}
	}
}

// kernelRecv takes the next packet as the kernel for p, the reply of a
// server: its wait sleeps on p's mail, which no signal of p's ends.
func (e *pairEnd) kernelRecv(p *Process) (packet, bool, error) {
	if pk, eof, ok := e.take(false); ok {
		return pk, eof, nil
	}

	e.from.readers.add(p, 0)
	defer e.from.readers.remove(p, 0)

	for {
		p.takeMail()

		if pk, eof, ok := e.take(false); ok {
			return pk, eof, nil
		}

		p.waitMail(time.Time{})
	}
}

// receive reads into the guest's buffers: a packet, of a seqpacket or a
// datagram pair, whose whole length is the count with MSG_TRUNC, or the
// bytes of a stream (receiveStream). The files and the credentials it
// came with go to a control buffer of room bytes at control when there
// is one, else they are dropped. It returns the count, the control bytes
// written, the flags of the message, MSG_TRUNC and MSG_CTRUNC, and the
// sender's address: the path a datagram's sender is bound to, none of a
// pair's.
func (e *pairEnd) receive(p *Process, bufs [][]byte, flags uint32, control, room uint32) (int, uint32, uint32, []byte, error) {
	if e.shared.kind == sockStream {
		n, written, out, err := e.receiveStream(p, bufs, flags, control, room)

		return n, written, out, nil, err
	}

	mode := ioMode{nonblock: flags&msgDontwait != 0}
	pk, eof, err := e.recv(p, mode, flags&msgPeek != 0)

	if err != nil || eof {
		return 0, 0, 0, nil, err
	}

	n, out := 0, uint32(0)
	data := pk.data

	for _, b := range bufs {
		c := copy(b, data)
		data = data[c:]
		n += c
	}

	if len(data) > 0 {
		out |= msgTrunc
	}

	if flags&msgTrunc != 0 {
		n = len(pk.data)
	}

	if flags&msgPeek != 0 {
		return n, 0, out, pk.addr, nil
	}

	written, cut := e.putControl(p, pk, control, room, flags&msgCmsgCloexec != 0)

	return n, written, out | cut, pk.addr, nil
}

// receiveStream reads a stream's bytes into bufs: what is queued, as
// described at the pair, waiting only while there is none; with MSG_PEEK
// they stay, the first packet's, and with MSG_WAITALL it goes on until
// bufs are full or the data ends, a signal or the timeout ending it with
// what it has.
func (e *pairEnd) receiveStream(p *Process, bufs [][]byte, flags uint32, control, room uint32) (int, uint32, uint32, error) {
	nonblock := flags&msgDontwait != 0 || e.nonblock.Load()
	want := totalLength(bufs)
	got := 0
	var written, cut uint32

	if want == 0 {
		return 0, 0, 0, nil
	}

	for {
		n, first, eof, ended := e.streamTake(advance(bufs, got), flags&msgPeek != 0)

		if got == 0 && n > 0 && flags&msgPeek == 0 {
			written, cut = e.putControl(p, first, control, room, flags&msgCmsgCloexec != 0)
		} else {
			dropFiles(first.files)
		}

		got += n

		if eof || ended || got >= want || got > 0 && flags&msgWaitall == 0 || flags&msgPeek != 0 && got > 0 {
			return got, written, cut, nil
		}

		if nonblock {
			if got > 0 {
				return got, written, cut, nil
			}

			return 0, 0, 0, syscall.EAGAIN
		}

		ready, err := e.wait(p, pollIn, &e.rcvTimeout)

		if err != nil || !ready {
			if got > 0 {
				return got, written, cut, nil
			}

			if err == nil {
				err = syscall.EAGAIN
			}

			return 0, 0, 0, err
		}
	}
}

// streamTake copies into bufs what the stream has queued, without
// waiting: the bytes of the packets in turn, as far as they fit, but for
// a packet with files, which only a read with nothing yet takes, ending
// with it; what of a packet does not fit stays first, all of it with
// peek, which looks at the first packet alone. It returns the bytes, the
// first packet with its files and credentials, the read's, whether the
// data ended, and whether the read ends there, after files.
func (e *pairEnd) streamTake(bufs [][]byte, peek bool) (n int, first packet, eof, ended bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	room := totalLength(bufs)

	for n < room {
		pk, end, ok := e.takeLocked(peek)

		if !ok {
			return n, first, false, false
		}

		if end {
			return n, first, n == 0, false
		}

		if n > 0 && len(pk.files) > 0 {
			e.keepFront(pk)

			return n, first, false, true
		}

		c := scatterInto(advance(bufs, n), pk.data)

		if n == 0 {
			first = packet{files: pk.files, cred: pk.cred}
		}

		n += c

		if peek {
			return n, packet{}, false, true
		}

		if c < len(pk.data) {
			e.keepFront(packet{data: pk.data[c:], cred: pk.cred})
		}

		if len(pk.files) > 0 {
			return n, first, false, true
		}
	}

	return n, first, false, false
}

// advance is bufs without their first n bytes.
func advance(bufs [][]byte, n int) [][]byte {
	for len(bufs) > 0 && n >= len(bufs[0]) {
		n -= len(bufs[0])
		bufs = bufs[1:]
	}

	if n == 0 {
		return bufs
	}

	return append([][]byte{bufs[0][n:]}, bufs[1:]...)
}

// scatterInto copies data into bufs in turn, as far as it fits.
func scatterInto(bufs [][]byte, data []byte) int {
	n := 0

	for _, b := range bufs {
		c := copy(b, data)
		data = data[c:]
		n += c

		if len(data) == 0 {
			break
		}
	}

	return n
}

// putControl writes the control messages of a packet into the guest's
// control buffer of room bytes at control: its files installed as the
// process's descriptors, one SCM_RIGHTS, and then, for an end that asks
// for them, its credentials, SCM_CREDENTIALS. What does not fit is dropped
// with MSG_CTRUNC. It returns the bytes written and the flag.
func (e *pairEnd) putControl(p *Process, pk packet, control, room uint32, cloexec bool) (uint32, uint32) {
	written, cut := p.putRights(pk.files, control, room, cloexec)

	if pk.cred == nil || !e.passcred.Load() {
		return written, cut
	}

	size := guestCmsgHeader + uint32(ucredSize)

	if room-written < size {
		return written, cut | msgCtrunc
	}

	at := control + written
	p.put32(at, size)
	p.put32(at+4, solSocket)
	p.put32(at+8, scmCredentials)
	p.put32(at+12, pk.cred.pid)
	p.put32(at+16, pk.cred.uid)
	p.put32(at+20, pk.cred.gid)

	return written + min(guestAlign(size), room-written), cut
}

func (e *pairEnd) read(p *Process, b []byte, mode ioMode) (int, error) {
	if p.chaosFail("read") {
		return 0, syscall.EIO
	}

	n, _, _, _, err := e.receive(p, [][]byte{b}, 0, 0, 0)

	return n, err
}

func (e *pairEnd) write(p *Process, b []byte, mode ioMode) (int, error) {
	if p.chaosFail("write") {
		return 0, syscall.EIO
	}

	if e.shared.kind == sockStream {
		return e.sendStream(p, [][]byte{b}, nil, nil, mode)
	}

	if err := e.send(p, b, nil, mode); err != nil {
		return 0, err
	}

	return len(b), nil
}

func (e *pairEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (e *pairEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

// The socket calls: a pair is connected and unnamed.

func (e *pairEnd) bind(p *Process, at, n uint32) error {
	return syscall.EINVAL
}

func (e *pairEnd) connect(p *Process, at, n uint32) error {
	return syscall.EISCONN
}

func (e *pairEnd) listen(backlog int) error {
	return syscall.EINVAL
}

func (e *pairEnd) accept(p *Process, flags uint32) (File, []byte, error) {
	return nil, nil, syscall.EINVAL
}

func (e *pairEnd) sockname(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (e *pairEnd) peername(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (e *pairEnd) shutdown(how int) error {
	if how < shutRd || how > shutRdwr {
		return syscall.EINVAL
	}

	// No more reading ends the data for this end and the other end's
	// writes; no more writing ends the data for the other end and this
	// end's writes. The queues of both wake.
	if how != shutWr {
		e.noRead.Store(true)
		e.from.edge(true, true)
	}

	if how != shutRd {
		e.noWrite.Store(true)
		e.to.edge(true, true)
	}

	return nil
}

func (e *pairEnd) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	return e.sockOptions.get(p, e.shared.kind, level, name, at, lenAt)
}

// get is getsockopt of a socket of a kind with the options o.
func (o *sockOptions) get(p *Process, kind int, level, name, at, lenAt uint32) error {
	if level != solSocket {
		return syscall.ENOPROTOOPT
	}

	var value uint32

	switch name {
	case soType:
		value = uint32(kind)
	case soError, soAcceptconn:
		value = 0
	case soPasscred:
		value = bit(o.passcred.Load())
	case soRcvtimeo, soSndtimeo:
		to := &o.rcvTimeout

		if name == soSndtimeo {
			to = &o.sndTimeout
		}

		var b [16]byte
		d := time.Duration(to.Load())
		binary.LittleEndian.PutUint64(b[:], uint64(d/time.Second))
		binary.LittleEndian.PutUint64(b[8:], uint64(d%time.Second/time.Microsecond))
		length := min(p.u32(lenAt), 16)
		copy(p.span(at, length), b[:])
		p.put32(lenAt, length)

		return nil
	case soSndbuf, soRcvbuf:
		value = queueMax * packetMax
	case 1, 2, 5, 6, 9, 10, 12, 15, soRcvlowat:
		value = o.intOption(name)
	case soPeercred:
		// The peer is in this kernel: the ids are the process's own.
		b := make([]byte, 12)
		binary.LittleEndian.PutUint32(b, uint32(p.pid))
		binary.LittleEndian.PutUint32(b[4:], p.cred.uid())
		binary.LittleEndian.PutUint32(b[8:], p.cred.gid())
		length := min(p.u32(lenAt), 12)
		copy(p.span(at, length), b)
		p.put32(lenAt, length)

		return nil
	default:
		return syscall.ENOPROTOOPT
	}

	length := min(p.u32(lenAt), 4)
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], value)
	copy(p.span(at, length), b[:])
	p.put32(lenAt, length)

	return nil
}

// setsockopt keeps SO_RCVTIMEO, SO_SNDTIMEO, SO_PASSCRED and the options of
// soInts, as Linux checks them, and takes the other options of the socket
// level as done.
func (e *pairEnd) setsockopt(p *Process, level, name, at, n uint32) error {
	return e.sockOptions.set(p, level, name, at, n)
}

// set is setsockopt with the options o.
func (o *sockOptions) set(p *Process, level, name, at, n uint32) error {
	if level != solSocket {
		return syscall.ENOPROTOOPT
	}

	switch name {
	case soRcvtimeo, soSndtimeo:
		if n < 16 {
			return syscall.EINVAL
		}

		sec, usec := int64(p.u64(at)), int64(p.u64(at+8))

		if usec < 0 || usec >= 1e6 {
			return syscall.EDOM
		}

		d := time.Duration(0)

		if sec >= 0 {
			d = time.Duration(min(sec, 1<<33))*time.Second + time.Duration(usec)*time.Microsecond
		}

		if name == soRcvtimeo {
			o.rcvTimeout.Store(int64(d))
		} else {
			o.sndTimeout.Store(int64(d))
		}
	case soPasscred:
		if n < 4 {
			return syscall.EINVAL
		}

		o.passcred.Store(p.u32(at) != 0)
	case 1, 2, 5, 6, 9, 10, 12, 15, soRcvlowat:
		if n < 4 {
			return syscall.EINVAL
		}

		v := p.u32(at)

		switch {
		case soInts[name]:
			v = bit(v != 0)
		case name == soRcvlowat:
			v = uint32(max(int32(v), 1))
		}

		o.intsMu.Lock()

		if o.ints == nil {
			o.ints = map[uint32]uint32{}
		}

		o.ints[name] = v
		o.intsMu.Unlock()
	}

	return nil
}

func (e *pairEnd) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	if addr != 0 {
		return 0, syscall.EISCONN
	}

	mode := ioMode{nonblock: flags&msgDontwait != 0, quiet: flags&msgNosignal != 0}

	if e.shared.kind == sockStream {
		return e.sendStream(p, [][]byte{p.span(buf, n)}, nil, nil, mode)
	}

	if err := e.send(p, p.span(buf, n), nil, mode); err != nil {
		return 0, err
	}

	return int(n), nil
}

func (e *pairEnd) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	got, _, _, from, err := e.receive(p, [][]byte{p.span(buf, n)}, flags, 0, 0)

	return got, senderAddress(from), err
}

// senderAddress is the address recvfrom and recvmsg give of a sender bound
// to path, and none, no byte of it, of one that is not, as Linux's.
func senderAddress(path []byte) []byte {
	if len(path) == 0 {
		return nil
	}

	return unixAddress(path)
}

// sendmsg sends the guest's msghdr, read at once: the descriptors of its
// SCM_RIGHTS as the packet's files, its iovecs gathered into the packet's
// bytes. The descriptors are Linux's first: a bad one is EBADF even in a
// message too long.
func (e *pairEnd) sendmsg(p *Process, at, flags uint32) (int, error) {
	h := p.span(at, sizeofMsghdr)

	if binary.LittleEndian.Uint32(h) != 0 {
		return 0, syscall.EISCONN
	}

	files, cred, err := p.rights(binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:]))

	if err != nil {
		return 0, err
	}

	mode := ioMode{nonblock: flags&msgDontwait != 0, quiet: flags&msgNosignal != 0}

	if e.shared.kind == sockStream {
		spans, err := p.ioSpans(binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:]))

		if err != nil {
			return 0, err
		}

		return e.sendStream(p, spans, files, cred, mode)
	}

	data, err := p.gather(binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:]), packetMax)

	if err != nil {
		return 0, err
	}

	if err := e.sendCred(p, data, files, cred, mode); err != nil {
		return 0, err
	}

	return len(data), nil
}

// recvmsg reads a packet into the guest's msghdr: the iovecs scattered,
// the files into its control buffer, the flags into msg_flags, the
// sender's address into msg_name.
func (e *pairEnd) recvmsg(p *Process, at, flags uint32) (int, error) {
	spans, err := p.ioSpans(p.u32(at+8), p.u32(at+12))

	if err != nil {
		return 0, err
	}

	n, written, out, from, err := e.receive(p, spans, flags, p.u32(at+16), p.u32(at+20))

	if err != nil {
		return 0, err
	}

	if name := p.u32(at); name != 0 {
		addr := senderAddress(from)
		copy(p.span(name, min(p.u32(at+4), uint32(len(addr)))), addr)
		p.put32(at+4, uint32(len(addr)))
	}

	p.put32(at+20, written)
	p.put32(at+24, out|flags&msgCmsgCloexec)

	return n, nil
}

// rights is the files of the SCM_RIGHTS of a guest's control buffer, the
// process's own, which send takes references to, and the credentials of
// its SCM_CREDENTIALS, the process's own, as Linux has a process without
// privileges give: EPERM else. No other control message is known.
func (p *Process) rights(at, n uint32) ([]File, *ucred, error) {
	var files []File
	var cred *ucred

	for off := uint32(0); off+guestCmsgHeader <= n; {
		length := p.u32(at + off)

		if length < guestCmsgHeader || off+length > n {
			return nil, nil, syscall.EINVAL
		}

		level, kind := p.u32(at+off+4), p.u32(at+off+8)
		data := p.span(at+off+guestCmsgHeader, length-guestCmsgHeader)

		switch {
		case level == solSocket && kind == scmRights:
			for i := 0; i+4 <= len(data); i += 4 {
				f, err := p.fd(binary.LittleEndian.Uint32(data[i:]))

				if err != nil {
					return nil, nil, err
				}

				files = append(files, f)
			}
		case level == solSocket && kind == scmCredentials && len(data) == ucredSize:
			c := ucred{binary.LittleEndian.Uint32(data), binary.LittleEndian.Uint32(data[4:]), binary.LittleEndian.Uint32(data[8:])}

			if !p.cred.privileged() && (c.pid != uint32(p.pid) || !slices.Contains(p.cred.uids[:], c.uid) || !slices.Contains(p.cred.gids[:], c.gid)) {
				return nil, nil, syscall.EPERM
			}

			cred = &c
		default:
			return nil, nil, syscall.EINVAL
		}

		off += guestAlign(length)
	}

	return files, cred, nil
}

// putRights installs a packet's files as the process's descriptors and
// writes them as one SCM_RIGHTS to the guest's control buffer of room
// bytes; those that do not fit, or all of them without a buffer, are
// dropped with MSG_CTRUNC. It returns the control bytes written and the
// flag.
func (p *Process) putRights(files []File, at, room uint32, cloexec bool) (uint32, uint32) {
	if len(files) == 0 {
		return 0, 0
	}

	var fds []byte
	flags := uint32(0)

	for _, f := range files {
		if guestCmsgHeader+uint32(len(fds))+4 > room {
			f.unref()
			flags |= msgCtrunc

			continue
		}

		fd, err := p.install(f, 0, cloexec)

		if err != nil {
			flags |= msgCtrunc

			continue
		}

		fds = binary.LittleEndian.AppendUint32(fds, uint32(fd))
	}

	if len(fds) == 0 {
		return 0, flags
	}

	size := guestCmsgHeader + uint32(len(fds))
	p.put32(at, size)
	p.put32(at+4, solSocket)
	p.put32(at+8, scmRights)
	copy(p.span(at+guestCmsgHeader, uint32(len(fds))), fds)

	return min(guestAlign(size), room), flags
}
