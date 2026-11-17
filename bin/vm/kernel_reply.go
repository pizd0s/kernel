//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// A replyEnd is where the answer to one request comes: the endpoint a
// request brings, last among its files. It is no end of a pair, but a
// small object with what one answer takes. Who holds it may answer, once,
// by sending a packet on it, a server by the descriptor that came with the
// request; and it passes the request on by sending the endpoint to
// another, as any file. The other side is no object at all: the process
// that waits, told by an event of its own in the process's queue.
//
// The waiter learns the answer, or that the last holder went with none,
// the end of the data: a server that is gone is EIO. A holder reads there
// what the waiter has to say: its CANCEL, once, which a signal it takes
// sends, and after it, or with none, the end of the data once the waiter
// waits no more. A holder that watches the endpoint, a server that keeps
// a request in its epoll set, is told of both.
type replyEnd struct {
	refs atomic.Int32
	// to is the process that waits, note its event of the answer.
	to   *Process
	note event
	// state is replyWaits until a holder answered or the last one went;
	// answer is the packet until the waiter takes it, under mu.
	state  atomic.Uint32
	mu     sync.Mutex
	answer packet
	// said is what the waiter said: replyCancel while a CANCEL is there
	// to read, replyLeft once it waits no more. uid and gid are the
	// CANCEL's, the waiter's.
	said     atomic.Uint32
	uid, gid uint32
	watchers subscribers
	nonblock atomic.Bool
	ino      atomic.Uint64
	// self is the endpoint as the files of a request that brings no
	// other, and small where a short answer's bytes are kept: neither is
	// allocated then.
	self  [1]Object
	small [96]byte
}

const (
	replyWaits = iota
	replyAnswered
	replyDropped
)

const (
	replyCancel = 1 << iota
	replyLeft
)

// newReply is where p waits for an answer, with the reference of its
// maker, which goes once the request is sent.
func newReply(p *Process) *replyEnd {
	r := &replyEnd{to: p}
	r.refs.Store(1)
	r.note.kind = eventChanged
	r.self[0] = r

	return r
}

// with is the files of a request with the endpoint last, as a request
// brings it.
func (r *replyEnd) with(files []Object) []Object {
	if len(files) == 0 {
		return r.self[:]
	}

	return append(files, r)
}

func (r *replyEnd) ref() {
	r.refs.Add(1)
}

// unref is a holder's going: the last one's with no answer is the end for
// the waiter.
func (r *replyEnd) unref() {
	if r.refs.Add(-1) == 0 && r.state.CompareAndSwap(replyWaits, replyDropped) && r.said.Load()&replyLeft == 0 {
		r.to.post(&r.note)
	}
}

// put is a holder's answer: the packet of data is the waiter's, its files
// with references of their own. EPIPE when the waiter waits no more, or
// has its answer.
func (r *replyEnd) put(data []byte, files []Object) error {
	for _, f := range files {
		f.ref()
	}

	r.mu.Lock()

	if r.said.Load()&replyLeft != 0 || r.state.Load() != replyWaits {
		r.mu.Unlock()
		dropFiles(files)

		return syscall.EPIPE
	}

	r.answer.data, r.answer.files = data, files
	r.state.Store(replyAnswered)
	r.mu.Unlock()
	r.to.post(&r.note)

	return nil
}

// wait waits for the answer into pk, and tells of the end: eof when the
// last holder went with none. The process takes its signals as it waits,
// and waits on.
func (r *replyEnd) wait(p *Process, pk *packet) (eof bool) {
	if r.state.Load() == replyWaits {
		p.wait(time.Time{}, func(*event) (bool, error) {
			return r.state.Load() != replyWaits, nil
		})
	}

	return r.take(pk)
}

// take is the answer that came, into pk, for the waiter: eof when there
// is none to come.
func (r *replyEnd) take(pk *packet) (eof bool) {
	if r.state.Load() == replyDropped {
		return true
	}

	r.mu.Lock()
	pk.data, pk.files = r.answer.data, r.answer.files
	r.answer.data, r.answer.files = nil, nil
	r.mu.Unlock()

	return false
}

// cancel is the waiter's CANCEL, for a holder to read.
func (r *replyEnd) cancel(p *Process) {
	r.mu.Lock()
	r.uid, r.gid = p.cred.euid(), p.cred.egid()
	r.said.Or(replyCancel)
	r.mu.Unlock()
	r.watchers.tell()
}

// leave is the waiter's going: it waits no more. An answer that came and
// was not taken goes, files and all, and a holder reads the end.
func (r *replyEnd) leave() {
	r.mu.Lock()
	r.said.Or(replyLeft)
	files := r.answer.files
	r.answer.data, r.answer.files = nil, nil
	r.mu.Unlock()
	dropFiles(files)
	r.to.events.drop(&r.note)
	r.watchers.tell()
}

// hear is what the waiter said, for a holder that reads: its CANCEL, a
// request's header, once, unless it is only looked at; else the end, eof,
// when it waits no more; ok false when it said nothing yet.
func (r *replyEnd) hear(peek bool) (data []byte, eof, ok bool) {
	said := r.said.Load()

	if said == 0 {
		return nil, false, false
	}

	if said&replyCancel == 0 {
		return nil, true, true
	}

	r.mu.Lock()
	data = make([]byte, remoteHeader)
	binary.LittleEndian.PutUint32(data, rCancel)
	binary.LittleEndian.PutUint32(data[8:], r.uid)
	binary.LittleEndian.PutUint32(data[12:], r.gid)

	if !peek {
		r.said.And(^uint32(replyCancel))
	}

	r.mu.Unlock()

	return data, false, true
}

// receive reads what the waiter said into the holder's buffers, waiting
// for it unless the read does not block: the count, and MSG_TRUNC for a
// packet cut short.
func (r *replyEnd) receive(p *Process, bufs [][]byte, flags uint32) (int, uint32, error) {
	nonblock := flags&msgDontwait != 0 || r.nonblock.Load()

	for {
		data, eof, ok := r.hear(flags&msgPeek != 0)

		if ok && eof {
			return 0, 0, nil
		}

		if ok {
			n, out, rest := 0, uint32(0), data

			for _, b := range bufs {
				c := copy(b, rest)
				rest = rest[c:]
				n += c
			}

			if len(rest) > 0 {
				out |= msgTrunc
			}

			if flags&msgTrunc != 0 {
				n = len(data)
			}

			return n, out, nil
		}

		if nonblock {
			return 0, 0, syscall.EAGAIN
		}

		if _, err := p.waitObject(r, pollIn, -1, true); err != nil {
			return 0, 0, err
		}
	}
}

// A replyEnd is a File, a Transfer and a Socket to its holder, as the end
// of a pair it stands for was: an answer is a send, the waiter's word a
// receive.
func (r *replyEnd) asFile() File              { return r }
func (r *replyEnd) asTransfer() Transfer      { return r }
func (r *replyEnd) asSeeker() Seeker          { return nil }
func (r *replyEnd) asDir() Dir                { return nil }
func (r *replyEnd) asTerminal() Terminal      { return nil }
func (r *replyEnd) asWindowed() Windowed      { return nil }
func (r *replyEnd) asLink() Link              { return nil }
func (r *replyEnd) asAttrs() Attrs            { return nil }
func (r *replyEnd) asSocket() Socket          { return r }
func (r *replyEnd) asIoctl() Ioctl            { return nil }
func (r *replyEnd) asLocker() Locker          { return nil }
func (r *replyEnd) asEpoll() *epoll           { return nil }
func (r *replyEnd) asMemfd() *memfdFile       { return nil }
func (r *replyEnd) asPair() *pairEnd          { return nil }
func (r *replyEnd) asListener() *unixListener { return nil }
func (r *replyEnd) asConsole() *consoleEnd    { return nil }
func (r *replyEnd) asVFS() *remoteDir         { return nil }

// poll: an answer can always be sent; what the waiter said is there to
// read, and its going is a hangup.
func (r *replyEnd) poll(events int16) int16 {
	mask := int16(pollOut | pollWrnorm)
	said := r.said.Load()

	if said != 0 {
		mask |= pollIn | pollRdnorm
	}

	if said&replyLeft != 0 {
		mask |= pollHup
	}

	return mask & (events | pollErr | pollHup)
}

func (r *replyEnd) subscribe(s *subscription, events int16) {
	r.watchers.add(&s.in)
}

func (r *replyEnd) unsubscribe(s *subscription) {
	r.watchers.remove(&s.in)
}

func (r *replyEnd) stat(p *Process, st *syscall.Stat_t) error {
	if r.ino.Load() == 0 {
		r.ino.CompareAndSwap(0, pairInodes.Add(1))
	}

	*st = syscall.Stat_t{Ino: r.ino.Load(), Mode: syscall.S_IFSOCK | 0o777, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

func (r *replyEnd) flags() (int, error) {
	fl := syscall.O_RDWR

	if r.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (r *replyEnd) setFlags(fl int) error {
	r.nonblock.Store(fl&syscall.O_NONBLOCK != 0)

	return nil
}

func (r *replyEnd) read(p *Process, b []byte, mode ioMode) (int, error) {
	n, _, err := r.receive(p, [][]byte{b}, 0)

	return n, err
}

func (r *replyEnd) write(p *Process, b []byte, mode ioMode) (int, error) {
	if len(b) > packetMax {
		return 0, syscall.EMSGSIZE
	}

	if err := r.put(append([]byte(nil), b...), nil); err != nil {
		return 0, err
	}

	return len(b), nil
}

func (r *replyEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (r *replyEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

// The socket calls: connected and unnamed, as a pair's end, with no
// options.

func (r *replyEnd) bind(p *Process, at, n uint32) error {
	return syscall.EINVAL
}

func (r *replyEnd) connect(p *Process, at, n uint32) error {
	return syscall.EISCONN
}

func (r *replyEnd) listen(backlog int) error {
	return syscall.EINVAL
}

func (r *replyEnd) accept(p *Process, flags uint32) (Object, []byte, error) {
	return nil, nil, syscall.EINVAL
}

func (r *replyEnd) sockname(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (r *replyEnd) peername(p *Process) ([]byte, error) {
	return unixAddress(nil), nil
}

func (r *replyEnd) socketKind() int {
	return sockSeqpacket
}

func (r *replyEnd) shutdown(how int) error {
	return syscall.EOPNOTSUPP
}

func (r *replyEnd) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	return syscall.ENOPROTOOPT
}

func (r *replyEnd) setsockopt(p *Process, level, name, at, n uint32) error {
	return syscall.ENOPROTOOPT
}

// sendmsg is the answer of the guest's msghdr: the descriptors of its
// SCM_RIGHTS the packet's files, its iovecs gathered into its bytes.
func (r *replyEnd) sendmsg(p *Process, at, flags uint32) (int, error) {
	h := p.span(at, sizeofMsghdr)

	if binary.LittleEndian.Uint32(h) != 0 {
		return 0, syscall.EISCONN
	}

	files, _, err := p.rights(binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:]))

	if err != nil {
		return 0, err
	}

	data, err := p.gatherIn(r.small[:], binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:]), packetMax)

	if err != nil {
		return 0, err
	}

	if err := r.put(data, files); err != nil {
		return 0, err
	}

	return len(data), nil
}

func (r *replyEnd) sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error) {
	if addr != 0 {
		return 0, syscall.EISCONN
	}

	return r.write(p, p.span(buf, n), ioMode{})
}

// recvmsg reads what the waiter said into the guest's msghdr: no files
// come with it, and no address.
func (r *replyEnd) recvmsg(p *Process, at, flags uint32) (int, error) {
	var few [4][]byte
	spans, err := p.ioSpansIn(few[:0], p.u32(at+8), p.u32(at+12))

	if err != nil {
		return 0, err
	}

	n, out, err := r.receive(p, spans, flags)

	if err != nil {
		return 0, err
	}

	if p.u32(at) != 0 {
		p.put32(at+4, 0)
	}

	p.put32(at+20, 0)
	p.put32(at+24, out|flags&msgCmsgCloexec)

	return n, nil
}

func (r *replyEnd) recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error) {
	got, _, err := r.receive(p, [][]byte{p.span(buf, n)}, flags)

	return got, nil, err
}
