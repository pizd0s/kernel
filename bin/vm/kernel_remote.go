//go:build linux && amd64

package main

import (
	"encoding/binary"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// A server's files: the kernel speaks the protocol of
// lib/protocol/protocol.h here for the POSIX calls of a process, as the
// libc of the process would. An open file is the second end of a pair its
// server made with servedpair, born what the server described: a file, a
// terminal's (kernel_terminal.go), a handle of a directory of rootfs's,
// which the calls with a path go to whole (kernel_vfs.go), or one of what
// O_PATH opened that is no directory; the server holds the first end and
// answers on it.
//
// A packet is a header and a body; files ride with it.
//
//	header { op u32, arg u32, uid u32, gid u32 }
//
//	uid and gid are who asks: the process the kernel calls for, whose ids
//	are vm's; a server makes what it creates theirs and lets them do what
//	its modes say, gid their group (permits). A reply's are zero.
//
//	Every request brings a fresh endpoint, last among its files: the reply
//	comes on it. An open's OK brings the open file, the second end of a
//	pair the server made with servedpair, whose first end it keeps. A READ
//	or a WRITE brings before it a memfd of the memory the process reads
//	into or writes from (kernel_memfd.go): the server writes the bytes
//	there or reads them from there itself, from the memfd's byte woff on,
//	one copy, as many as asked, and closes it as it answers. A server that
//	is gone is EIO. An open file's calls:
//	  READ      arg len   io               OK count u32
//	  WRITE     arg len   io               OK count u32 at u64, where an
//	                                       append went
//	  READDIR             after u64        OK (ino u64, pos u64, type u8, name, 0)*
//	  FSTAT                                OK stat
//	  FCHMOD    arg mode
//	  FCHOWN              uid u32 gid u32
//	  FUTIMES   arg when  atime u64 mtime u64   when: 2 atime now, 4 mtime
//	                                       now, 8 atime kept, 16 mtime kept
//	  FTRUNCATE           size u64
//	  FSYNC     arg data
//	  FSTATFS                              OK statfs
//	  FLOCK     arg op                     LOCK_SH, LOCK_EX, LOCK_UN
//	  LOCK      arg get   type u32 start u64 len u64   OK; with get the
//	                      lock in the way, F_UNLCK for none:
//	                      type u32 start u64 len u64
//	  CANCEL              on the endpoint of a READ or a WRITE the server
//	                      keeps while it waits, from the kernel (waiting):
//	                      the server answers that request now, with the
//	                      count so far or ERR EINTR
//	  READY     arg events                 from the server, on its end of an
//	                                       open file it made telling
//	                                       (servedTells): what poll finds
//	                                       of it now, the last counts
//	replies: OK, ERR with arg the errno
//	io { off u64, woff u32, flags u32 }   flags: ioAppend, a WRITE at the
//	       end the file has, O_APPEND's; ioNonblock, answered at once, EAGAIN
//	       where it would wait, O_NONBLOCK's
//	stat { ino u64, size u64, mtime u64 nanoseconds, mode u32, nlink u32,
//	       uid u32, gid u32, atime u64, dev u64, ctime u64 }
//	statfs { blocks u64, bfree u64, files u64, ffree u64, dev u64,
//	         bsize u32, namemax u32, flags u32 }   flags: 1 read-only
//
// A directory's entries have positions, each after those before it, and a
// READDIR gives those after one: removing what was read moves nothing a
// reading has to come to. dev is the file system's own, the files of two
// never share one. A WRITE with append goes at the end the file has when
// the server takes it: O_APPEND is the server's, so no write of another
// comes between. A lock is the endpoint's, an open file's, as the host's
// open file description locks are, and goes when it closes; a FIFO's pipe
// is the kernel's (kernel_fifo.go).
const (
	rRead uint32 = iota + 64
	rWrite
	rReaddir
	rFstat
	rFchmod
	rFchown
	rFutimes
	rFtruncate
	rFsync
	rFstatfs
	rFlock
	rLock
	rCancel
	rReady
)

// The kinds of struct served (lib/c's sys/served.h), what a server's pair
// for what it serves is born, and its server's telling of its readiness.
// READY's flags: the terminal's TOSTOP.
const (
	servedFile     = 1
	servedTerminal = 2
	servedDir      = 3
	servedPath     = 4
	servedTells    = 16
	readyTostop    = 1
)

// The flags of an io.
const (
	ioAppend   = 1
	ioNonblock = 2
)

const (
	rOk          uint32 = 128
	rErr         uint32 = 129
	remoteHeader        = 16
	remoteStat          = 64
	remoteStatfs        = 52
	// The whens of UTIMES and FUTIMES.
	whenAtimeNow  = 2
	whenMtimeNow  = 4
	whenAtimeKept = 8
	whenMtimeKept = 16
	utimeNow      = 1<<30 - 1
	utimeOmit     = 1<<30 - 2
)

// remoteNode is an open file of a server: the end of its pair, and what
// the kernel keeps of it.
type remoteNode struct {
	end  *pairEnd
	refs atomic.Int32
	// accmode is the O_ACCMODE it was opened with, append its O_APPEND,
	// which F_SETFL sets as well as O_NONBLOCK.
	accmode  int
	append   atomic.Bool
	nonblock atomic.Bool
	// pos is the position of read, write and seek; after the position of
	// the last entry readdir gave.
	pos   int64
	after uint64
	// tells is set for a file whose server tells its readiness, READY on
	// its endpoint, whose channel keeps only the last; ready is what that
	// said, and a terminal's foreground group and TOSTOP.
	tells      bool
	ready      atomic.Int32
	foreground atomic.Int32
	tostop     atomic.Bool
}

type remoteFile struct {
	remoteNode
}

// remoteHandle is a handle of rootfs's, which the calls with a path go to
// whole (kernel_vfs.go); id is its, which a call names the root by.
type remoteHandle struct {
	remoteNode
	id uint64
}

// remoteDir is a handle of a directory of the VFS's: the Dir.
type remoteDir struct {
	remoteHandle
}

// remotePath is a handle of what O_PATH opened that is no directory: its
// calls are those of what it is, by an empty path, and no path walks from
// it.
type remotePath struct {
	remoteHandle
}

var (
	_ Transfer = (*remoteFile)(nil)
	_ Windowed = (*remoteFile)(nil)
	_ Seeker   = (*remoteFile)(nil)
	_ Attrs    = (*remoteFile)(nil)
	_ Locker   = (*remoteFile)(nil)
	_ Dir      = (*remoteDir)(nil)
	_ Seeker   = (*remoteDir)(nil)
	_ Attrs    = (*remoteDir)(nil)
	_ Locker   = (*remoteDir)(nil)
	_ Attrs    = (*remotePath)(nil)
	_ Locker   = (*remotePath)(nil)
	_ Dir      = (*noRoot)(nil)
)

// servedpair is a pair a server makes for what it serves: the first end
// is the server's, a plain one; the second is born the open file struct
// served at what describes, whose calls are requests on the first. Both
// close on exec.
func (p *Process) libcServedpair(what, sv uint32) uint32 {
	a, b := newPair(false)
	f, err := served(p.u32(what), int(p.u32(what+4)), p.u64(what+8), p.u64(what+16), p.u64(what+24), b)

	if err != nil {
		a.unref()
		b.unref()

		return p.result(0, err)
	}

	x, err := p.install(a, 0, true)

	if err != nil {
		f.unref()

		return p.result(0, err)
	}

	y, err := p.install(f, 0, true)

	if err != nil {
		p.closeFd(x)

		return p.result(0, err)
	}

	p.put32(sv, uint32(x))
	p.put32(sv+4, uint32(y))

	return 0
}

// served is the open file a kind of struct served makes of a pair's
// second end, as the open flags say: a file, a terminal's of device dev
// and ino, or a handle of id, of a directory or of what O_PATH opened;
// with servedTells its server's READY is its readiness.
func served(kind uint32, flags int, dev, ino, id uint64, end *pairEnd) (File, error) {
	var n *remoteNode
	var f File

	switch kind &^ servedTells {
	case servedFile:
		r := &remoteFile{}
		n, f = &r.remoteNode, r
	case servedTerminal:
		t := &remoteTerminal{dev: dev, ino: ino}
		n, f = &t.remoteNode, t
	case servedDir:
		d := &remoteDir{remoteHandle{id: id}}
		n, f = &d.remoteNode, d
	case servedPath:
		h := &remotePath{remoteHandle{id: id}}
		n, f = &h.remoteNode, h
	default:
		return nil, syscall.EINVAL
	}

	n.end, n.accmode, n.tells = end, flags&syscall.O_ACCMODE, kind&servedTells != 0
	n.refs.Store(1)
	n.append.Store(flags&syscall.O_APPEND != 0)
	n.nonblock.Store(flags&syscall.O_NONBLOCK != 0)

	if n.tells {
		end.from.latest.Store(true)
	}

	return f, nil
}

// asRoot is the Dir a descriptor's file is for fchdir, with a reference
// of the caller's: a directory's; nil for anything else.
func asRoot(f File) Dir {
	if d, ok := f.(Dir); ok {
		d.ref()

		return d
	}

	return nil
}

func (n *remoteNode) ref() {
	n.refs.Add(1)
}

func (n *remoteNode) unref() {
	if n.refs.Add(-1) == 0 {
		n.end.unref()
	}
}

// A file whose server tells its readiness wakes its subscriptions as a
// READY comes; any other is ready whenever asked.
func (n *remoteNode) subscribe(to waker, id uint64, events int16) bool {
	if !n.tells {
		return false
	}

	n.end.from.readers.add(to, id)

	return true
}

func (n *remoteNode) unsubscribe(to waker, id uint64) {
	if n.tells {
		n.end.from.readers.remove(to, id)
	}
}

func (n *remoteNode) poll(events int16) int16 {
	if !n.tells {
		return events & (pollIn | pollOut | pollRdnorm | pollWrnorm)
	}

	return n.readiness() & (events | pollErr | pollHup)
}

// readiness is what the server said last of the file, the READY that came
// on its endpoint; a server gone is a hangup and an error.
func (n *remoteNode) readiness() int16 {
	for {
		pk, eof, ok := n.end.take(false)

		if !ok {
			break
		}

		if eof {
			n.ready.Store(int32(pollHup | pollErr))

			break
		}

		if len(pk.data) >= remoteHeader+8 && binary.LittleEndian.Uint32(pk.data) == rReady {
			n.foreground.Store(int32(binary.LittleEndian.Uint32(pk.data[remoteHeader:])))
			n.tostop.Store(binary.LittleEndian.Uint32(pk.data[remoteHeader+4:])&readyTostop != 0)

			events := int16(binary.LittleEndian.Uint32(pk.data[4:])) & (pollIn | pollPri | pollOut | pollErr | pollHup)

			if events&pollIn != 0 {
				events |= pollRdnorm
			}

			if events&pollOut != 0 {
				events |= pollWrnorm
			}

			n.ready.Store(int32(events))
		}

		dropFiles(pk.files)
	}

	return int16(n.ready.Load())
}

func (n *remoteNode) flags() (int, error) {
	fl := n.accmode

	if n.append.Load() {
		fl |= syscall.O_APPEND
	}

	if n.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (n *remoteNode) setFlags(fl int) error {
	n.nonblock.Store(fl&syscall.O_NONBLOCK != 0)
	n.append.Store(fl&syscall.O_APPEND != 0)

	return nil
}

// request sends a request on an endpoint, the files with it, as the
// process's effective user and group. The kernel waits as itself, not as
// the process: nothing of the process's interrupts a call to a server.
func request(p *Process, to *pairEnd, op, arg uint32, body []byte, files []File) error {
	data := make([]byte, remoteHeader+len(body))
	binary.LittleEndian.PutUint32(data, op)
	binary.LittleEndian.PutUint32(data[4:], arg)
	binary.LittleEndian.PutUint32(data[8:], p.cred.euid())
	binary.LittleEndian.PutUint32(data[12:], p.cred.egid())
	copy(data[remoteHeader:], body)

	return to.kernelSend(p, data, files)
}

// reply waits for the reply on an endpoint: the reply must come to the
// request that was sent. A server that goes away is EIO; its error is
// the reply's.
func reply(p *Process, on *pairEnd) (packet, error) {
	pk, eof, err := on.kernelRecv(p)

	if err != nil {
		return pk, err
	}

	return replyOf(pk, eof)
}

// replyOf is the reply a packet is: its body after the header, or the
// server's error.
func replyOf(pk packet, eof bool) (packet, error) {
	if eof || len(pk.data) < remoteHeader {
		return pk, syscall.EIO
	}

	if binary.LittleEndian.Uint32(pk.data) == rErr {
		dropFiles(pk.files)

		return pk, syscall.Errno(binary.LittleEndian.Uint32(pk.data[4:]))
	}

	pk.arg = binary.LittleEndian.Uint32(pk.data[4:])
	pk.data = pk.data[remoteHeader:]

	return pk, nil
}

// waiting is a READ or a WRITE, which may wait as long as the file says:
// once the process has a signal to take, the kernel sends CANCEL on the
// request's endpoint and waits on for the reply it brings, the count so
// far or EINTR, which cancelled marks: the caller restarts the call when
// the handlers ask for that. For SIGKILL it waits no more: the process
// ends. The reply brings no files.
func (n *remoteNode) waiting(p *Process, op, arg uint32, body []byte, files []File) (pk packet, cancelled bool, err error) {
	mine, theirs := newPair(false)
	err = request(p, n.end, op, arg, body, append(files, theirs))
	theirs.unref()

	if err == syscall.EPIPE {
		err = syscall.EIO // the server is gone
	}

	if err != nil {
		mine.unref()

		return packet{}, false, err
	}

	defer mine.unref()
	mine.from.readers.add(p, 0)
	defer mine.from.readers.remove(p, 0)

	for {
		p.takeMail()

		if got, eof, ok := mine.take(false); ok {
			pk, err = replyOf(got, eof)
			dropFiles(pk.files)
			pk.files = nil

			return pk, cancelled, err
		}

		signals := p.sig.deliverable()

		if signals&sigBit(int(syscall.SIGKILL)) != 0 {
			return packet{}, true, syscall.EINTR
		}

		if signals != 0 && !cancelled {
			// A server that answered already closed its end.
			cancelled = true
			request(p, mine, rCancel, 0, nil, nil)

			continue
		}

		p.waitMail(time.Time{})
	}
}

// call is a call on the node: a fresh pair, its one end with the
// request, the reply on the other, the reply's packet. files go with the
// request and stay the caller's. Only an OPEN's reply brings a file, the
// open file's endpoint, and a CONNECT's, the listener, which the packet
// keeps; the files of any other reply, or the rest of theirs, go here.
func (n *remoteNode) call(p *Process, op, arg uint32, body []byte, files []File) (packet, error) {
	mine, theirs := newPair(false)
	defer mine.unref()
	err := request(p, n.end, op, arg, body, append(files, theirs))
	theirs.unref()

	if err == syscall.EPIPE {
		err = syscall.EIO // the server is gone
	}

	if err != nil {
		return packet{}, err
	}

	pk, err := reply(p, mine)

	if err != nil {
		return packet{}, err
	}

	if (op == vOpen || op == vConnect || op == rOpen) && len(pk.files) > 0 {
		dropFiles(pk.files[1:])
		pk.files = pk.files[:1]
	} else {
		dropFiles(pk.files)
		pk.files = nil
	}

	return pk, nil
}

// on is a call on the node that brings no file: the reply's packet.
func (n *remoteNode) on(p *Process, op, arg uint32, body []byte) (packet, error) {
	return n.call(p, op, arg, body, nil)
}

// decodeStat is a Stat_t of the protocol's stat.
func decodeStat(b []byte, st *syscall.Stat_t) error {
	if len(b) < remoteStat {
		return syscall.EIO
	}

	le := binary.LittleEndian
	*st = syscall.Stat_t{Ino: le.Uint64(b), Size: int64(le.Uint64(b[8:])), Mode: le.Uint32(b[24:]), Nlink: uint64(le.Uint32(b[28:])),
		Uid: le.Uint32(b[32:]), Gid: le.Uint32(b[36:]), Blksize: 4096}
	st.Blocks = (st.Size + 511) / 512
	st.Mtim = syscall.NsecToTimespec(int64(le.Uint64(b[16:])))
	st.Atim = syscall.NsecToTimespec(int64(le.Uint64(b[40:])))
	st.Dev = le.Uint64(b[48:])
	st.Ctim = syscall.NsecToTimespec(int64(le.Uint64(b[56:])))

	return nil
}

// decodeStatfs is a Statfs_t of the protocol's statfs: the server's
// device is the fsid.
func decodeStatfs(b []byte, st *syscall.Statfs_t) error {
	if len(b) < remoteStatfs {
		return syscall.EIO
	}

	le := binary.LittleEndian
	bsize := int64(le.Uint32(b[40:]))
	dev := le.Uint64(b[32:])
	*st = syscall.Statfs_t{Type: 0x70697a64, Bsize: bsize, Frsize: bsize, Blocks: le.Uint64(b), Bfree: le.Uint64(b[8:]),
		Bavail: le.Uint64(b[8:]), Files: le.Uint64(b[16:]), Ffree: le.Uint64(b[24:]), Namelen: int64(le.Uint32(b[44:])),
		Flags: int64(le.Uint32(b[48:]))}
	st.Fsid.X__val[0], st.Fsid.X__val[1] = int32(uint32(dev)), int32(uint32(dev>>32))

	return nil
}

// permits tells whether the process may read (4), write (2) or execute
// (1) what a stat describes, as a server decides it: root everything but
// executing what nobody may, the owner by the owner's bits, a member of
// the group, its effective gid or a supplementary group, by the group's,
// everyone else by the others'.
func (p *Process) permits(st *syscall.Stat_t, want uint32) bool {
	if p.cred.privileged() {
		return want&1 == 0 || st.Mode&syscall.S_IFMT == syscall.S_IFDIR || st.Mode&0o111 != 0
	}

	bits := st.Mode

	switch {
	case p.cred.euid() == st.Uid:
		bits >>= 6
	case p.cred.inGroup(st.Gid):
		bits >>= 3
	}

	return bits&want == want
}

// whens is the when of UTIMES for the times of a call: nil is now for
// both, UTIME_NOW and UTIME_OMIT in a nanoseconds field are what they say.
func whens(ts []syscall.Timespec) (uint32, []byte) {
	when := uint32(0)
	body := make([]byte, 16)

	if ts == nil {
		return when | whenAtimeNow | whenMtimeNow, body
	}

	for i, t := range ts[:2] {
		now, kept := uint32(whenAtimeNow), uint32(whenAtimeKept)

		if i == 1 {
			now, kept = whenMtimeNow, whenMtimeKept
		}

		switch t.Nsec {
		case utimeNow:
			when |= now
		case utimeOmit:
			when |= kept
		default:
			binary.LittleEndian.PutUint64(body[8*i:], uint64(t.Sec)*1e9+uint64(t.Nsec))
		}
	}

	return when, body
}

// The Attrs role, of a file or a directory of a server.

func (n *remoteNode) simple(p *Process, op, arg uint32, body []byte) error {
	_, err := n.on(p, op, arg, body)

	return err
}

func (n *remoteNode) truncate(p *Process, size int64) error {
	var body [8]byte
	binary.LittleEndian.PutUint64(body[:], uint64(size))

	return n.simple(p, rFtruncate, 0, body[:])
}

func (n *remoteNode) sync(p *Process, data bool) error {
	arg := uint32(0)

	if data {
		arg = 1
	}

	return n.simple(p, rFsync, arg, nil)
}

func (n *remoteNode) chmod(p *Process, mode uint32) error {
	return n.simple(p, rFchmod, mode, nil)
}

func (n *remoteNode) chown(p *Process, uid, gid int) error {
	var body [8]byte
	binary.LittleEndian.PutUint32(body[:], uint32(uid))
	binary.LittleEndian.PutUint32(body[4:], uint32(gid))

	return n.simple(p, rFchown, 0, body[:])
}

func (n *remoteNode) utimes(p *Process, ts []syscall.Timespec) error {
	when, body := whens(ts)

	return n.simple(p, rFutimes, when, body)
}

func (n *remoteNode) stat(p *Process, st *syscall.Stat_t) error {
	pk, err := n.on(p, rFstat, 0, nil)

	if err != nil {
		return err
	}

	return decodeStat(pk.data, st)
}

func (n *remoteNode) statfs(p *Process, st *syscall.Statfs_t) error {
	pk, err := n.on(p, rFstatfs, 0, nil)

	if err != nil {
		return err
	}

	return decodeStatfs(pk.data, st)
}

// The Locker role: the locks are the endpoint's, the open file's.

func (n *remoteNode) flock(p *Process, op int) error {
	return n.simple(p, rFlock, uint32(op), nil)
}

// recordLock is fcntl's lock on the bytes a struct flock says, from the
// start, the position or the end, before the start for a negative length;
// F_OFD_GETLK tells the lock in the way, from the start.
func (n *remoteNode) recordLock(p *Process, cmd int, lk *syscall.Flock_t) error {
	start, length := lk.Start, lk.Len

	switch lk.Whence {
	case 0:
	case 1:
		start += atomic.LoadInt64(&n.pos)
	case 2:
		var st syscall.Stat_t

		if err := n.stat(p, &st); err != nil {
			return err
		}

		start += st.Size
	default:
		return syscall.EINVAL
	}

	if length < 0 {
		start, length = start+length, -length
	}

	if start < 0 {
		return syscall.EINVAL
	}

	body := make([]byte, 20)
	binary.LittleEndian.PutUint32(body, uint32(lk.Type))
	binary.LittleEndian.PutUint64(body[4:], uint64(start))
	binary.LittleEndian.PutUint64(body[12:], uint64(length))

	if cmd != fOfdGetlk {
		return n.simple(p, rLock, 0, body)
	}

	pk, err := n.on(p, rLock, 1, body)

	if err != nil {
		return err
	}

	if len(pk.data) < 20 {
		return syscall.EIO
	}

	le := binary.LittleEndian
	lk.Type, lk.Whence = int16(le.Uint32(pk.data)), 0
	lk.Start, lk.Len, lk.Pid = int64(le.Uint64(pk.data[4:])), int64(le.Uint64(pk.data[12:])), -1

	return nil
}

// The Dir role, of a handle (kernel_vfs.go).

// readdir fills the entries after the last it gave, as many as fit, in
// the guest's struct dirent layout: ino, off, reclen, type, name; an
// entry's off is its position.
func (d *remoteDir) readdir(p *Process, b []byte) (int, error) {
	var after [8]byte
	binary.LittleEndian.PutUint64(after[:], d.after)
	pk, err := d.on(p, rReaddir, 0, after[:])

	if err != nil {
		return 0, err
	}

	data := pk.data
	n := 0
	le := binary.LittleEndian

	for len(data) >= 17 {
		ino, pos := le.Uint64(data), le.Uint64(data[8:])
		kind := data[16]
		end := 17

		for end < len(data) && data[end] != 0 {
			end++
		}

		name := data[17:end]
		reclen := (19 + len(name) + 1 + 7) &^ 7

		if n+reclen > len(b) {
			break
		}

		clear(b[n : n+reclen])
		le.PutUint64(b[n:], ino)
		le.PutUint64(b[n+8:], pos)
		le.PutUint16(b[n+16:], uint16(reclen))
		b[n+18] = kind
		copy(b[n+19:], name)
		n += reclen
		d.after = pos
		data = data[min(end+1, len(data)):]
	}

	return n, nil
}

// seek of a directory is rewinddir.
func (d *remoteDir) seek(p *Process, off int64, whence int) (int64, error) {
	if off != 0 || whence != 0 {
		return 0, syscall.EINVAL
	}

	d.after = 0

	return 0, nil
}

// The Transfer role, of a file.

// readAt reads into b, the process's memory, at off: the READ brings a
// memfd of b.
func (f *remoteFile) readAt(p *Process, b []byte, off int64) (int, error) {
	if off < 0 || len(b) == 0 {
		return f.readInto(p, nil, 0, 0, off)
	}

	w := window(p, b, syscall.O_WRONLY)
	n, err := f.readInto(p, w, 0, len(b), off)
	w.unref()

	return n, err
}

// readInto reads n bytes at off into the memfd w, from its byte woff on:
// the READ brings w, and the server writes the bytes there itself. A
// count past n is the server's lie: EIO.
func (f *remoteFile) readInto(p *Process, w *memfdFile, woff uint32, n int, off int64) (int, error) {
	if off < 0 {
		return 0, syscall.EINVAL
	}

	if n == 0 {
		return 0, nil
	}

	body := ioBody(off, woff, f.ioFlags())

	for {
		pk, cancelled, err := f.waiting(p, rRead, uint32(n), body[:], []File{w})

		if err == syscall.EINTR && cancelled && p.interrupted() {
			continue
		}

		if err != nil {
			return 0, err
		}

		if len(pk.data) < 4 || int(binary.LittleEndian.Uint32(pk.data)) > n {
			return 0, syscall.EIO
		}

		return int(binary.LittleEndian.Uint32(pk.data)), nil
	}
}

// ioBody is a READ's or a WRITE's body, an io.
func ioBody(off int64, woff, flags uint32) [16]byte {
	var b [16]byte
	binary.LittleEndian.PutUint64(b[:], uint64(off))
	binary.LittleEndian.PutUint32(b[8:], woff)
	binary.LittleEndian.PutUint32(b[12:], flags)

	return b
}

// ioFlags are the flags of the file's reads and writes, O_NONBLOCK's.
func (n *remoteNode) ioFlags() uint32 {
	if n.nonblock.Load() {
		return ioNonblock
	}

	return 0
}

func (f *remoteFile) writeAt(p *Process, b []byte, off int64) (int, error) {
	n, _, err := f.put(p, b, off, false)

	return n, err
}

// put writes b, the process's memory, at off, or atEnd at the end the
// file has when the server takes the write: the WRITE brings a memfd of b.
func (f *remoteFile) put(p *Process, b []byte, off int64, atEnd bool) (int, int64, error) {
	if off < 0 || len(b) == 0 {
		return f.writeFrom(p, nil, 0, 0, off, atEnd)
	}

	w := window(p, b, syscall.O_RDONLY)
	n, at, err := f.writeFrom(p, w, 0, len(b), off, atEnd)
	w.unref()

	return n, at, err
}

// writeFrom writes n bytes of the memfd w, from its byte woff on, at off,
// or atEnd at the end the file has when the server takes the write: the
// WRITE brings w, and the server reads the bytes from there itself. The
// count and where they went; a count past n is the server's lie: EIO.
func (f *remoteFile) writeFrom(p *Process, w *memfdFile, woff uint32, n int, off int64, atEnd bool) (int, int64, error) {
	if off < 0 {
		return 0, 0, syscall.EINVAL
	}

	if n == 0 {
		return 0, off, nil
	}

	flags := f.ioFlags()

	if atEnd {
		flags |= ioAppend
	}

	body := ioBody(off, woff, flags)

	for {
		pk, cancelled, err := f.waiting(p, rWrite, uint32(n), body[:], []File{w})

		if err == syscall.EINTR && cancelled && p.interrupted() {
			continue
		}

		if err != nil {
			return 0, 0, err
		}

		if len(pk.data) < 12 || int(binary.LittleEndian.Uint32(pk.data)) > n {
			return 0, 0, syscall.EIO
		}

		return int(binary.LittleEndian.Uint32(pk.data)), int64(binary.LittleEndian.Uint64(pk.data[4:])), nil
	}
}

func (f *remoteFile) read(p *Process, b []byte, mode ioMode) (int, error) {
	if p.chaosFail("read") {
		return 0, syscall.EIO
	}

	n, err := f.readAt(p, b, atomic.LoadInt64(&f.pos))
	atomic.AddInt64(&f.pos, int64(n))

	return n, err
}

// write writes at the position, or with O_APPEND at the end, which the
// server finds as it writes and says; the position is after what was
// written. Only an append's place is the server's to say: a server that
// handed the WRITE on, moved, answers with where it went on its device.
func (f *remoteFile) write(p *Process, b []byte, mode ioMode) (int, error) {
	if p.chaosFail("write") {
		return 0, syscall.EIO
	}

	off := atomic.LoadInt64(&f.pos)
	atEnd := f.append.Load()
	n, at, err := f.put(p, b, off, atEnd)

	if err == nil {
		if !atEnd {
			at = off
		}

		atomic.StoreInt64(&f.pos, at+int64(n))
	}

	return n, err
}

func (f *remoteFile) pread(p *Process, b []byte, off int64) (int, error) {
	return f.readAt(p, b, off)
}

func (f *remoteFile) pwrite(p *Process, b []byte, off int64) (int, error) {
	return f.writeAt(p, b, off)
}

func (f *remoteFile) seek(p *Process, off int64, whence int) (int64, error) {
	base := int64(0)

	switch whence {
	case 0:
	case 1:
		base = atomic.LoadInt64(&f.pos)
	case 2:
		var st syscall.Stat_t

		if err := f.stat(p, &st); err != nil {
			return 0, err
		}

		base = st.Size
	default:
		return 0, syscall.EINVAL
	}

	if base+off < 0 {
		return 0, syscall.EINVAL
	}

	atomic.StoreInt64(&f.pos, base+off)

	return base + off, nil
}

// noRoot is the root of a process that has none, the first process's
// when `vm run` is given no root: an empty directory nothing is under.
type noRoot struct {
	refs atomic.Int32
	made time.Time
}

func newNoRoot() *noRoot {
	r := &noRoot{made: time.Now()}
	r.refs.Store(1)

	return r
}

func (r *noRoot) ref()   { r.refs.Add(1) }
func (r *noRoot) unref() { r.refs.Add(-1) }

func (r *noRoot) stat(p *Process, st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Ino: 1, Mode: syscall.S_IFDIR | 0o555, Nlink: 2, Blksize: 4096}
	st.Mtim = syscall.NsecToTimespec(r.made.UnixNano())
	st.Atim, st.Ctim = st.Mtim, st.Mtim

	return nil
}

func (r *noRoot) poll(events int16) int16 {
	return events & (pollIn | pollOut | pollRdnorm | pollWrnorm)
}

func (r *noRoot) subscribe(to waker, id uint64, events int16) bool { return false }
func (r *noRoot) unsubscribe(to waker, id uint64)                  {}
func (r *noRoot) flags() (int, error)                              { return syscall.O_RDONLY, nil }
func (r *noRoot) setFlags(fl int) error                            { return nil }
func (r *noRoot) readdir(p *Process, b []byte) (int, error)        { return 0, nil }
func (r *noRoot) path(p *Process) (string, error)                  { return "/", nil }

// itself tells whether a path names the root itself: none but dots and
// slashes, ".", "..", "/". Anything else is not there.
func (r *noRoot) itself(path string) bool {
	for _, name := range strings.Split(path, "/") {
		if name != "" && name != "." && name != ".." {
			return false
		}
	}

	return true
}

// gone is the error of a call on a path: the root's own, or ENOENT for
// what is not there.
func (r *noRoot) gone(path string, own error) error {
	if r.itself(path) {
		return own
	}

	return syscall.ENOENT
}

func (r *noRoot) openPath(p *Process, path string, flags int, mode uint32) (File, error) {
	switch {
	case !r.itself(path) && flags&syscall.O_CREAT != 0:
		return nil, syscall.EROFS
	case !r.itself(path):
		return nil, syscall.ENOENT
	case flags&syscall.O_CREAT != 0 || flags&3 != syscall.O_RDONLY:
		return nil, syscall.EISDIR
	}

	r.ref()

	return r, nil
}

func (r *noRoot) statPath(p *Process, path string, follow bool, st *syscall.Stat_t) error {
	if !r.itself(path) {
		return syscall.ENOENT
	}

	return r.stat(p, st)
}

func (r *noRoot) readlinkPath(p *Process, path string) (string, error) {
	return "", r.gone(path, syscall.EINVAL)
}

// Nothing is made in the root: what names it is there, EEXIST.
func (r *noRoot) mkdirPath(p *Process, path string, mode uint32) error {
	return r.makes(path)
}

func (r *noRoot) mknodPath(p *Process, path string, mode uint32) error {
	return r.makes(path)
}

func (r *noRoot) symlinkPath(p *Process, target, path string) error {
	return r.makes(path)
}

func (r *noRoot) makes(path string) error {
	if r.itself(path) {
		return syscall.EEXIST
	}

	return syscall.EROFS
}

func (r *noRoot) unlinkPath(p *Process, path string, dir bool) error {
	if dir {
		return r.gone(path, syscall.EBUSY)
	}

	return r.gone(path, syscall.EISDIR)
}

func (r *noRoot) renamePath(p *Process, old string, to Dir, new string) error {
	return r.gone(old, syscall.EBUSY)
}

func (r *noRoot) linkPath(p *Process, old string, to Dir, new string, follow bool) error {
	return r.gone(old, syscall.EPERM)
}

func (r *noRoot) chmodPath(p *Process, path string, mode uint32, follow bool) error {
	return r.gone(path, syscall.EROFS)
}

func (r *noRoot) chownPath(p *Process, path string, uid, gid int, follow bool) error {
	return r.gone(path, syscall.EROFS)
}

func (r *noRoot) utimesPath(p *Process, path string, ts []syscall.Timespec, follow bool) error {
	return r.gone(path, syscall.EROFS)
}

func (r *noRoot) truncatePath(p *Process, path string, size int64) error {
	return r.gone(path, syscall.EISDIR)
}

func (r *noRoot) statfsPath(p *Process, path string, st *syscall.Statfs_t) error {
	if !r.itself(path) {
		return syscall.ENOENT
	}

	*st = syscall.Statfs_t{Bsize: 4096, Frsize: 4096, Namelen: 255}

	return nil
}

func (r *noRoot) realpathOf(p *Process, path string) (string, error) {
	return "/", r.gone(path, nil)
}

// No file system is mounted on the root, and no socket is in it.
func (r *noRoot) mountPath(p *Process, target, source string, conn File) error {
	return syscall.EINVAL
}

func (r *noRoot) unmountPath(p *Process, path string, flags uint32) error {
	return syscall.EINVAL
}

func (r *noRoot) bindPath(p *Process, path string, mode uint32, held File) error {
	return syscall.EOPNOTSUPP
}

func (r *noRoot) connectPath(p *Process, path string) (File, error) {
	return nil, syscall.EOPNOTSUPP
}
