//go:build linux && amd64

package main

import (
	"encoding/binary"
	"sync/atomic"
	"syscall"
	"time"
)

// A server's files: the kernel speaks the protocol of
// lib/protocol/protocol.h here for the POSIX calls of a process, as the
// libc of the process would. An open file is one end of a seqpacket pair
// (kernel_endpoint.go), whose server holds the other and answers on it: a
// file system's, a device's service's, or rootfs's for a handle, which the
// calls with a path go to whole (kernel_vfs.go).
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
//	comes on it, and an open's stays the open file's. A READ or a WRITE
//	brings before it a memfd of the memory the process reads into or
//	writes from (kernel_memfd.go): the server writes the bytes there or
//	reads them from there itself, from the memfd's byte woff on, one copy,
//	as many as asked, and closes it as it answers. A server that is gone
//	is EIO. An open file's calls:
//	  READ      arg len   off u64 woff u32 OK count u32
//	  WRITE     arg append off u64 len u32 woff u32
//	                                       OK count u32 at u64, where an
//	                                       append went
//	  READDIR             after u64        OK (ino u64, pos u64, type u8, name, 0)*
//	  FSTAT                                OK stat; a handle's that rootfs
//	                                       answers itself: stat, id u64
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
//	replies: OK, ERR with arg the errno
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
	// accmode is the O_ACCMODE it was opened with, append its O_APPEND.
	accmode  int
	append   bool
	nonblock atomic.Bool
	// pos is the position of read, write and seek; after the position of
	// the last entry readdir gave.
	pos   int64
	after uint64
}

type remoteFile struct {
	remoteNode
}

// remoteDir is a handle of rootfs's (kernel_vfs.go): a directory of the
// VFS's, or what O_PATH opened, of mode; dev and ino are its, once known,
// and its id.
type remoteDir struct {
	remoteNode
	known    atomic.Bool
	dev, ino atomic.Uint64
	id       atomic.Uint64
	mode     uint32
}

var (
	_ Transfer = (*remoteFile)(nil)
	_ Seeker   = (*remoteFile)(nil)
	_ Socket   = (*remoteFile)(nil)
	_ Attrs    = (*remoteFile)(nil)
	_ Locker   = (*remoteFile)(nil)
	_ Dir      = (*remoteDir)(nil)
	_ Seeker   = (*remoteDir)(nil)
	_ Socket   = (*remoteDir)(nil)
	_ Attrs    = (*remoteDir)(nil)
	_ Locker   = (*remoteDir)(nil)
	_ Dir      = (*noRoot)(nil)
)

// remoteDirOn is the Dir of an endpoint of a server's directory: the
// endpoint is its own, with the reference the Dir holds.
func remoteDirOn(end *pairEnd) *remoteDir {
	d := &remoteDir{remoteNode: remoteNode{end: end}}
	d.refs.Store(1)

	return d
}

// asRoot is the Dir a descriptor's file is for fchdir, with a reference
// of the caller's: a directory, or a connection to a server, the root of
// what it serves; nil for anything else.
func asRoot(f File) Dir {
	if d, ok := f.(Dir); ok {
		d.ref()

		return d
	}

	if c, ok := f.(interface{ endpoint() *pairEnd }); ok {
		c.endpoint().ref()

		return remoteDirOn(c.endpoint())
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

func (n *remoteNode) subscribe(to waker, id uint64, events int16) bool {
	return false
}

func (n *remoteNode) unsubscribe(to waker, id uint64) {
}

func (n *remoteNode) poll(events int16) int16 {
	return events & (pollIn | pollOut | pollRdnorm | pollWrnorm)
}

func (n *remoteNode) flags() (int, error) {
	fl := n.accmode

	if n.append {
		fl |= syscall.O_APPEND
	}

	if n.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (n *remoteNode) setFlags(fl int) error {
	n.nonblock.Store(fl&syscall.O_NONBLOCK != 0)

	return nil
}

// The Socket role: an open file of a server is a connection to it, and a
// program that knows the protocol may speak it there itself; the messages
// are the endpoint's.

func (n *remoteNode) sendmsg(p *Process, at, flags uint32) (int, error) {
	return n.end.sendmsg(p, at, flags)
}

func (n *remoteNode) recvmsg(p *Process, at, flags uint32) (int, error) {
	return n.end.recvmsg(p, at, flags)
}

func (n *remoteNode) sendto(p *Process, buf, size, flags, addr, alen uint32) (int, error) {
	return n.end.sendto(p, buf, size, flags, addr, alen)
}

func (n *remoteNode) recvfrom(p *Process, buf, size, flags uint32) (int, []byte, error) {
	return n.end.recvfrom(p, buf, size, flags)
}

func (n *remoteNode) bind(p *Process, at, size uint32) error    { return syscall.EINVAL }
func (n *remoteNode) connect(p *Process, at, size uint32) error { return syscall.EISCONN }
func (n *remoteNode) listen(backlog int) error                  { return syscall.EINVAL }
func (n *remoteNode) accept(p *Process, flags uint32) (File, []byte, error) {
	return nil, nil, syscall.EINVAL
}
func (n *remoteNode) sockname(p *Process) ([]byte, error) { return n.end.sockname(p) }
func (n *remoteNode) peername(p *Process) ([]byte, error) { return n.end.peername(p) }
func (n *remoteNode) shutdown(how int) error              { return n.end.shutdown(how) }
func (n *remoteNode) getsockopt(p *Process, level, name, at, lenAt uint32) error {
	return n.end.getsockopt(p, level, name, at, lenAt)
}
func (n *remoteNode) setsockopt(p *Process, level, name, at, size uint32) error {
	return n.end.setsockopt(p, level, name, at, size)
}

// request sends a request on an endpoint, the files with it, as the
// host's user vm runs as, whose ids are taken once: vm never changes them.
// The kernel waits as itself, not as the process: nothing of the process's
// interrupts a call to a server.
func request(p *Process, to *pairEnd, op, arg uint32, body []byte, files []File) error {
	data := make([]byte, remoteHeader+len(body))
	binary.LittleEndian.PutUint32(data, op)
	binary.LittleEndian.PutUint32(data[4:], arg)
	binary.LittleEndian.PutUint32(data[8:], hostEuid)
	binary.LittleEndian.PutUint32(data[12:], hostEgid)
	copy(data[remoteHeader:], body)

	return to.kernelSend(p, data, files)
}

var hostEuid, hostEgid = uint32(syscall.Geteuid()), uint32(syscall.Getegid())

// reply waits for the reply on an endpoint: the reply must come to the
// request that was sent. A server that goes away is EIO; its error is
// the reply's.
func reply(p *Process, on *pairEnd) (packet, error) {
	for {
		pk, eof, err := on.kernelRecv(p)

		if err != nil {
			return pk, err
		}

		if eof || len(pk.data) < remoteHeader {
			return pk, syscall.EIO
		}

		if binary.LittleEndian.Uint32(pk.data) == rErr {
			dropFiles(pk.files)

			return pk, syscall.Errno(binary.LittleEndian.Uint32(pk.data[4:]))
		}

		pk.data = pk.data[remoteHeader:]

		return pk, nil
	}
}

// call is a call on the node: a fresh pair, its one end with the
// request, the reply on the other. The reply's packet and that end come
// back; the caller keeps the end for an open file or drops it. files go
// with the request and stay the caller's. Only an OPEN's reply brings a
// file, the file itself, which the packet keeps; the files of any other
// reply, or the rest of an OPEN's, go here.
func (n *remoteNode) call(p *Process, op, arg uint32, body []byte, files []File) (packet, *pairEnd, error) {
	mine, theirs := newPair(false)
	err := request(p, n.end, op, arg, body, append(files, theirs))
	theirs.unref()

	if err == syscall.EPIPE {
		err = syscall.EIO // the server is gone
	}

	if err != nil {
		mine.unref()

		return packet{}, nil, err
	}

	pk, err := reply(p, mine)

	if err != nil {
		mine.unref()

		return packet{}, nil, err
	}

	if (op == vOpen || op == vConnect) && len(pk.files) > 0 {
		dropFiles(pk.files[1:])
		pk.files = pk.files[:1]
	} else {
		dropFiles(pk.files)
		pk.files = nil
	}

	return pk, mine, nil
}

// on is a call on the node that opens nothing: the reply's packet.
func (n *remoteNode) on(p *Process, op, arg uint32, body []byte) (packet, error) {
	pk, mine, err := n.call(p, op, arg, body, nil)

	if err != nil {
		return packet{}, err
	}

	mine.unref()

	return pk, nil
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
// executing what nobody may, the owner by the owner's bits, the process's
// group, its effective gid, by the group's, everyone else by the others'.
func permits(st *syscall.Stat_t, want uint32) bool {
	uid, gid := hostEuid, hostEgid

	if uid == 0 {
		return want&1 == 0 || st.Mode&syscall.S_IFMT == syscall.S_IFDIR || st.Mode&0o111 != 0
	}

	bits := st.Mode

	switch {
	case uid == st.Uid:
		bits >>= 6
	case gid == st.Gid:
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

// identity is a directory's device and inode, which a server finds the
// root of a process by.
func identity(p *Process, d Dir) (uint64, uint64) {
	switch d := d.(type) {
	case *remoteDir:
		return d.ident(p)
	case *hostDir:
		var st syscall.Stat_t

		if syscall.Fstat(d.fd, &st) == nil {
			return st.Dev, st.Ino
		}
	}

	return 0, 0
}

// ident is the directory's device and inode, asked once.
func (d *remoteDir) ident(p *Process) (uint64, uint64) {
	if !d.known.Load() {
		var st syscall.Stat_t

		if d.stat(p, &st) != nil {
			return 0, 0
		}

		d.dev.Store(st.Dev)
		d.ino.Store(st.Ino)
		d.known.Store(true)
	}

	return d.dev.Load(), d.ino.Load()
}

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

	var body [12]byte
	binary.LittleEndian.PutUint64(body[:], uint64(off))
	binary.LittleEndian.PutUint32(body[8:], woff)
	pk, mine, err := f.call(p, rRead, uint32(n), body[:], []File{w})

	if err != nil {
		return 0, err
	}

	mine.unref()

	if len(pk.data) < 4 || int(binary.LittleEndian.Uint32(pk.data)) > n {
		return 0, syscall.EIO
	}

	return int(binary.LittleEndian.Uint32(pk.data)), nil
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

	arg := uint32(0)

	if atEnd {
		arg = 1
	}

	var body [16]byte
	binary.LittleEndian.PutUint64(body[:], uint64(off))
	binary.LittleEndian.PutUint32(body[8:], uint32(n))
	binary.LittleEndian.PutUint32(body[12:], woff)
	pk, mine, err := f.call(p, rWrite, arg, body[:], []File{w})

	if err != nil {
		return 0, 0, err
	}

	mine.unref()

	if len(pk.data) < 12 || int(binary.LittleEndian.Uint32(pk.data)) > n {
		return 0, 0, syscall.EIO
	}

	return int(binary.LittleEndian.Uint32(pk.data)), int64(binary.LittleEndian.Uint64(pk.data[4:])), nil
}

func (f *remoteFile) read(p *Process, b []byte, mode ioMode) (int, error) {
	if chaosFail("read") {
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
	if chaosFail("write") {
		return 0, syscall.EIO
	}

	off := atomic.LoadInt64(&f.pos)
	n, at, err := f.put(p, b, off, f.append)

	if err == nil {
		if !f.append {
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

// resolve walks a path in the one directory there is: a name in it is
// none, but ".", ".." and the last name, which the calls find missing.
func (r *noRoot) resolve(p *Process, path string, w *walk) (step, error) {
	names, slash := splitPath(path)

	for len(names) > 1 {
		if names[0] != "." && names[0] != ".." {
			return step{}, syscall.ENOENT
		}

		names = names[1:]
	}

	r.ref()
	s := step{dir: r, kind: lastNone}

	if len(names) == 1 {
		s.last, s.kind, s.slash = lastOf(names[0], slash)
	}

	return s, nil
}

func (r *noRoot) statName(p *Process, name string, st *syscall.Stat_t) error {
	if name != "" {
		return syscall.ENOENT
	}

	return r.stat(p, st)
}

func (r *noRoot) readlinkName(p *Process, name string) (string, error) {
	if name != "" {
		return "", syscall.ENOENT
	}

	return "", syscall.EINVAL
}

func (r *noRoot) openName(p *Process, name string, flags int, mode uint32) (File, error) {
	switch {
	case name != "" && flags&syscall.O_CREAT != 0:
		return nil, syscall.EROFS
	case name != "":
		return nil, syscall.ENOENT
	case flags&3 != syscall.O_RDONLY:
		return nil, syscall.EISDIR
	}

	r.ref()

	return r, nil
}

func (r *noRoot) mkdir(p *Process, name string, mode uint32) error             { return syscall.EROFS }
func (r *noRoot) mknod(p *Process, name string, mode uint32, dev uint64) error { return syscall.EROFS }
func (r *noRoot) symlink(p *Process, target, name string) error                { return syscall.EROFS }
func (r *noRoot) unlink(p *Process, name string, dir bool) error               { return syscall.ENOENT }
func (r *noRoot) rename(p *Process, name string, to Dir, toName string) error  { return syscall.ENOENT }
func (r *noRoot) link(p *Process, name string, to Dir, toName string) error    { return syscall.ENOENT }

func (r *noRoot) accessName(p *Process, name string, mode, flags uint32) error {
	if name != "" {
		return syscall.ENOENT
	}

	if mode&2 != 0 {
		return syscall.EROFS
	}

	return nil
}

func (r *noRoot) chmodName(p *Process, name string, mode uint32) error { return syscall.EROFS }
func (r *noRoot) chownName(p *Process, name string, uid, gid int) error {
	return syscall.EROFS
}
func (r *noRoot) utimesName(p *Process, name string, ts []syscall.Timespec) error {
	return syscall.EROFS
}
func (r *noRoot) truncateName(p *Process, name string, size int64) error { return syscall.EISDIR }

func (r *noRoot) statfsName(p *Process, name string, st *syscall.Statfs_t) error {
	if name != "" {
		return syscall.ENOENT
	}

	*st = syscall.Statfs_t{Bsize: 4096, Frsize: 4096, Namelen: 255}

	return nil
}
