//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"
)

type PairState struct {
	ino  uint64
	made time.Time
	kind int
}

type PairEnd struct {
	shared *PairState
	peer   *PairEnd

	to, from *MsgChannel
	refs     atomic.Int32
	nonblock atomic.Bool

	dead    atomic.Bool
	noWrite atomic.Bool
	noRead  atomic.Bool

	mu     sync.Mutex
	front  *Packet
	peeked atomic.Bool

	options SockOptions

	dir *RemoteDir
}

func newPair(nonblock bool) (*PairEnd, *PairEnd) {
	return newPairOf(SOCK_SEQPACKET, nonblock)
}

func newPairOf(kind int, nonblock bool) (*PairEnd, *PairEnd) {
	s := &PairState{ino: pairInodes.Add(1), made: time.Now(), kind: kind}
	ab, ba := newMsgChannel(queueMax), newMsgChannel(queueMax)
	a, b := &PairEnd{shared: s, to: ab, from: ba}, &PairEnd{shared: s, to: ba, from: ab}

	a.peer, b.peer = b, a

	for _, e := range []*PairEnd{a, b} {
		e.refs.Store(1)
		e.nonblock.Store(nonblock)
	}

	return a, b
}

func (e *PairEnd) ref() {
	e.refs.Add(1)
}

func (e *PairEnd) unref() {
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

func (e *PairEnd) asFile() File {
	return e
}

func (e *PairEnd) asTransfer() Transfer {
	return e
}

func (e *PairEnd) asSeeker() Seeker {
	return nil
}

func (e *PairEnd) asDir() Dir {
	return nil
}

func (e *PairEnd) asTerminal() Terminal {
	return nil
}

func (e *PairEnd) asWindowed() Windowed {
	return nil
}

func (e *PairEnd) asLink() Link {
	return nil
}

func (e *PairEnd) asAttrs() Attrs {
	return nil
}

func (e *PairEnd) asSocket() Socket {
	return e
}

func (e *PairEnd) asIoctl() Ioctl {
	return nil
}

func (e *PairEnd) asLocker() Locker {
	return nil
}

func (e *PairEnd) asEpoll() *Epoll {
	return nil
}

func (e *PairEnd) asMemfd() *MemfdFile {
	return nil
}

func (e *PairEnd) asPair() *PairEnd {
	return e
}

func (e *PairEnd) asListener() *UnixListener {
	return nil
}

func (e *PairEnd) asConsole() *ConsoleEnd {
	return nil
}

func (e *PairEnd) asVFS() *RemoteDir {
	return nil
}

func (e *PairEnd) asCall() *CallEnd {
	return nil
}

func (e *PairEnd) subscribe(s *Subscription, events int16) {
	in, out := ioSides(events)

	if in {
		e.from.subscribe(&s.in, false)
	}

	if out {
		e.to.subscribe(&s.out, true)
	}
}

func (e *PairEnd) unsubscribe(s *Subscription) {
	e.from.unsubscribe(&s.in)
	e.to.unsubscribe(&s.out)
}

func (e *PairEnd) stat(p *Process, st *Stat) error {
	s := e.shared

	*st = Stat{Ino: s.ino, Mode: S_IFSOCK | 0o777, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}
	st.Atim = nsecToTimespec(s.made.UnixNano())
	st.Mtim, st.Ctim = st.Atim, st.Atim

	return nil
}

func (e *PairEnd) poll(events int16) int16 {
	mask := int16(0)
	dgram := e.shared.kind == SOCK_DGRAM

	if e.peeked.Load() || e.from.count() > 0 || e.noRead.Load() || !dgram && (e.peer.dead.Load() || e.peer.noWrite.Load()) {
		mask |= POLLIN | POLLRDNORM
	}

	switch {
	case e.peer.dead.Load() || e.to.gone():
		mask |= POLLOUT | POLLWRNORM

		if !dgram {
			mask |= POLLHUP
		}
	case e.to.room():
		mask |= POLLOUT | POLLWRNORM
	}

	return mask & (events | POLLERR | POLLHUP)
}

func (e *PairEnd) wait(p *Process, events int16, to *atomic.Int64) (bool, error) {
	timeout := -1

	if ns := to.Load(); ns > 0 {
		timeout = waitMs(time.Duration(ns))
	}

	ready, err := p.waitObject(e, events, timeout, timeout < 0)

	return ready > 0, err
}

func (e *PairEnd) flags() (int, error) {
	fl := O_RDWR

	if e.nonblock.Load() {
		fl |= O_NONBLOCK
	}

	return fl, nil
}

func (e *PairEnd) setFlags(fl int) error {
	e.nonblock.Store(fl&O_NONBLOCK != 0)

	return nil
}

func (e *PairEnd) send(p *Process, data []byte, files []Object, mode IoMode) error {
	if len(data) > packetMax {
		return EMSGSIZE
	}

	return e.sendOwned(p, append([]byte(nil), data...), files, mode)
}

func (e *PairEnd) sendOwned(p *Process, data []byte, files []Object, mode IoMode) error {
	return e.sendCred(p, data, files, nil, mode)
}

func (e *PairEnd) sendCred(p *Process, data []byte, files []Object, cred *Ucred, mode IoMode) error {
	if len(data) > packetMax {
		return EMSGSIZE
	}

	if cred == nil && p != nil && e.peer.options.passcred.Load() {
		cred = p.ucred()
	}

	for _, f := range files {
		f.ref()
	}

	err := e.queue(p, Packet{data: data, files: files, cred: cred}, mode)

	if err != nil {
		dropFiles(files)
	} else {
		armFiles(files)
	}

	return err
}

func (e *PairEnd) sendStream(p *Process, spans [][]byte, files []Object, cred *Ucred, mode IoMode) (int, error) {
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
			if err == EPIPE && !mode.quiet {
				p.sendSelf(SIGPIPE)
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

func totalLength(spans [][]byte) int {
	n := 0

	for _, b := range spans {
		n += len(b)
	}

	return n
}

func (e *PairEnd) kernelSend(p *Process, data []byte, files []Object) error {
	if len(data) > packetMax {
		return EMSGSIZE
	}

	for _, f := range files {
		f.ref()
	}

	pk := Packet{data: data, files: files}
	err := e.kernelPut(p, &pk)

	if err != nil {
		dropFiles(files)
	}

	return err
}

func (e *PairEnd) kernelPut(p *Process, pk *Packet) error {
	for {
		if e.noWrite.Load() {
			return EPIPE
		}

		if full, err := e.to.put(pk); err != nil || !full {
			return err
		}

		if e.nonblock.Load() {
			return EAGAIN
		}

		e.to.writable(p, func() bool { return e.noWrite.Load() }, time.Time{}, waitsOn)
	}
}

func (e *PairEnd) queue(p *Process, pk Packet, mode IoMode) error {
	return e.queueAs(p, pk, mode.nonblock || e.nonblock.Load(), &e.options.sndTimeout)
}

func (e *PairEnd) queueAs(p *Process, pk Packet, nonblock bool, timeout *atomic.Int64) error {
	for {
		if e.noWrite.Load() {
			return EPIPE
		}

		full, err := e.to.put(&pk)

		if err == EPIPE && e.shared.kind == SOCK_DGRAM {
			return ECONNREFUSED
		}

		if err != nil || !full {
			return err
		}

		if nonblock {
			return EAGAIN
		}

		end, how := until(timeout)

		if timedOut, err := e.to.writable(p, func() bool { return e.noWrite.Load() }, end, how); err != nil {
			return err
		} else if timedOut {
			return EAGAIN
		}
	}
}

func (e *PairEnd) seen() bool {
	return e.peeked.Load() || e.noRead.Load() || e.shared.kind != SOCK_DGRAM && (e.from.ended.Load() || e.peer.noWrite.Load())
}

func (e *PairEnd) take(peek bool) (pk Packet, eof, ok bool) {
	e.mu.Lock()

	defer e.mu.Unlock()

	return e.takeLocked(peek)
}

func (e *PairEnd) takeLocked(peek bool) (pk Packet, eof, ok bool) {
	if e.front != nil {
		pk = *e.front

		if !peek {
			e.front = nil
			e.peeked.Store(false)
		}

		return pk, false, true
	}

	if e.noRead.Load() {
		return Packet{}, true, true
	}

	dgram := e.shared.kind == SOCK_DGRAM
	shut := !dgram && e.peer.noWrite.Load()

	pk, eof, ok, _ = e.from.take()

	if eof && dgram {
		return Packet{}, false, false
	}

	if ok && !eof && peek {
		kept := pk

		e.front = &kept
		e.peeked.Store(true)
	}

	if ok {
		return pk, eof, true
	}

	if shut {
		return Packet{}, true, true
	}

	return Packet{}, false, false
}

func (e *PairEnd) keepFront(pk Packet) {
	e.front = &pk
	e.peeked.Store(true)
}

func (e *PairEnd) recv(p *Process, mode IoMode, peek bool) (Packet, bool, error) {
	nonblock := mode.nonblock || e.nonblock.Load()

	for {
		pk, eof, ok := e.take(peek)

		if ok {
			return pk, eof, nil
		}

		if nonblock {
			return Packet{}, false, EAGAIN
		}

		if peek {
			if ready, err := e.wait(p, POLLIN, &e.options.rcvTimeout); err != nil {
				return Packet{}, false, err
			} else if !ready {
				return Packet{}, false, EAGAIN
			}

			continue
		}

		end, how := until(&e.options.rcvTimeout)
		pk, given, timedOut, err := e.from.receive(p, func() bool { return e.seen() }, end, how)

		switch {
		case err != nil:
			return Packet{}, false, err
		case timedOut:
			return Packet{}, false, EAGAIN
		case given:
			return pk, false, nil
		}
	}
}

func (e *PairEnd) receive(p *Process, bufs [][]byte, flags uint32, control, room uint32) (int, uint32, uint32, []byte, error) {
	if e.shared.kind == SOCK_STREAM {
		n, written, out, err := e.receiveStream(p, bufs, flags, control, room)

		return n, written, out, nil, err
	}

	mode := IoMode{nonblock: flags&MSG_DONTWAIT != 0}
	pk, eof, err := e.recv(p, mode, flags&MSG_PEEK != 0)

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
		out |= MSG_TRUNC
	}

	if flags&MSG_TRUNC != 0 {
		n = len(pk.data)
	}

	if flags&MSG_PEEK != 0 {
		return n, 0, out, pk.addr, nil
	}

	written, cut := e.putControl(p, pk, control, room, flags&MSG_CMSG_CLOEXEC != 0)

	return n, written, out | cut, pk.addr, nil
}

func (e *PairEnd) receiveStream(p *Process, bufs [][]byte, flags uint32, control, room uint32) (int, uint32, uint32, error) {
	nonblock := flags&MSG_DONTWAIT != 0 || e.nonblock.Load()
	want := totalLength(bufs)
	got := 0

	var written, cut uint32

	if want == 0 {
		return 0, 0, 0, nil
	}

	for {
		n, first, eof, ended := e.streamTake(advance(bufs, got), flags&MSG_PEEK != 0)

		if got == 0 && n > 0 && flags&MSG_PEEK == 0 {
			written, cut = e.putControl(p, first, control, room, flags&MSG_CMSG_CLOEXEC != 0)
		} else {
			dropFiles(first.files)
		}

		got += n

		if eof || ended || got >= want || got > 0 && flags&MSG_WAITALL == 0 || flags&MSG_PEEK != 0 && got > 0 {
			return got, written, cut, nil
		}

		if nonblock {
			if got > 0 {
				return got, written, cut, nil
			}

			return 0, 0, 0, EAGAIN
		}

		end, how := until(&e.options.rcvTimeout)
		timedOut, err := e.from.readable(p, func() bool { return e.seen() }, end, how)

		if err != nil || timedOut {
			if got > 0 {
				return got, written, cut, nil
			}

			if err == nil {
				err = EAGAIN
			}

			return 0, 0, 0, err
		}
	}
}

func (e *PairEnd) streamTake(bufs [][]byte, peek bool) (n int, first Packet, eof, ended bool) {
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
			first = Packet{files: pk.files, cred: pk.cred}
		}

		n += c

		if peek {
			return n, Packet{}, false, true
		}

		if c < len(pk.data) {
			e.keepFront(Packet{data: pk.data[c:], cred: pk.cred})
		}

		if len(pk.files) > 0 {
			return n, first, false, true
		}
	}

	return n, first, false, false
}

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

func (e *PairEnd) putControl(p *Process, pk Packet, control, room uint32, cloexec bool) (uint32, uint32) {
	written, cut := p.putRights(pk.files, control, room, cloexec)

	if pk.cred == nil || !e.options.passcred.Load() {
		return written, cut
	}

	size := sizeofCmsghdr + uint32(sizeofUcred)

	if room-written < size {
		return written, cut | MSG_CTRUNC
	}

	at := control + written

	p.put32(at, size)
	p.put32(at+4, SOL_SOCKET)
	p.put32(at+8, SCM_CREDENTIALS)
	p.put32(at+12, pk.cred.pid)
	p.put32(at+16, pk.cred.uid)
	p.put32(at+20, pk.cred.gid)

	return written + min(guestAlign(size), room-written), cut
}

func (e *PairEnd) read(p *Process, b []byte, mode IoMode) (int, error) {
	if p.chaosFail("read") {
		return 0, EIO
	}

	n, _, _, _, err := e.receive(p, [][]byte{b}, 0, 0, 0)

	return n, err
}

func (e *PairEnd) write(p *Process, b []byte, mode IoMode) (int, error) {
	if p.chaosFail("write") {
		return 0, EIO
	}

	if e.shared.kind == SOCK_STREAM {
		return e.sendStream(p, [][]byte{b}, nil, nil, mode)
	}

	if err := e.send(p, b, nil, mode); err != nil {
		return 0, err
	}

	return len(b), nil
}

func (e *PairEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (e *PairEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (e *PairEnd) bind(p *Process, at, n uint32) error {
	return EINVAL
}

func (e *PairEnd) connect(p *Process, at, n uint32) error {
	return EISCONN
}

func (e *PairEnd) listen(backlog int) error {
	return EINVAL
}

func (e *PairEnd) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, EINVAL
}

func (e *PairEnd) sockname(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (e *PairEnd) peername(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (e *PairEnd) socketKind() int {
	return e.shared.kind
}

func (e *PairEnd) shutdown(how int) error {
	if how < SHUT_RD || how > SHUT_RDWR {
		return EINVAL
	}

	if how != SHUT_WR {
		e.noRead.Store(true)
		e.from.edge(true, true)
	}

	if how != SHUT_RD {
		e.noWrite.Store(true)
		e.to.edge(true, true)
	}

	return nil
}

func (e *PairEnd) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	return e.options.get(p, e.shared.kind, level, name, at, lenAt)
}

func (e *PairEnd) setsockopt(p *Process, level, name, at, n uint32) error {
	return e.options.set(p, level, name, at, n)
}

func (e *PairEnd) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	if addr != 0 {
		return 0, EISCONN
	}

	mode := IoMode{nonblock: flags&MSG_DONTWAIT != 0, quiet: flags&MSG_NOSIGNAL != 0}

	if e.shared.kind == SOCK_STREAM {
		return e.sendStream(p, [][]byte{p.span(buf, n)}, nil, nil, mode)
	}

	if err := e.send(p, p.span(buf, n), nil, mode); err != nil {
		return 0, err
	}

	return int(n), nil
}

func (e *PairEnd) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	got, _, _, from, err := e.receive(p, [][]byte{p.span(buf, n)}, flags, 0, 0)

	return got, senderAddress(from), err
}

func senderAddress(path []byte) []byte {
	if len(path) == 0 {
		return nil
	}

	return unixAddress(path)
}

func (e *PairEnd) sendmsg(p *Process, at, flags uint32) (int, error) {
	h := p.span(at, sizeofMsghdr)

	if binary.LittleEndian.Uint32(h) != 0 {
		return 0, EISCONN
	}

	files, cred, err := p.rights(binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:]))

	if err != nil {
		return 0, err
	}

	mode := IoMode{nonblock: flags&MSG_DONTWAIT != 0, quiet: flags&MSG_NOSIGNAL != 0}

	if e.shared.kind == SOCK_STREAM {
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

func (e *PairEnd) recvmsg(p *Process, at, flags uint32) (int, error) {
	var few [4][]byte

	spans, err := p.ioSpansIn(few[:0], p.u32(at+8), p.u32(at+12))

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
	p.put32(at+24, out|flags&MSG_CMSG_CLOEXEC)

	return n, nil
}

func (e *PairEnd) directory(p *Process) *RemoteDir {
	e.mu.Lock()

	defer e.mu.Unlock()

	if d := e.dir; d != nil && d.node.revive() {
		return d
	}

	e.ref()

	f, _ := served(p, openedDir, O_RDONLY, 0, 0, 0, e)

	e.dir = f.asVFS()

	return e.dir
}
