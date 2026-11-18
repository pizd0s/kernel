//go:build linux && amd64

package main

import (
	"encoding/binary"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// A server's files: the kernel speaks the protocol of
// lib/protocol/protocol.h here for the POSIX calls of a process, as the
// libc of the process would. An open file is the second end of a pair its
// server made, a seqpacket socketpair's, which an OPEN's OK brings with
// what it is (opened): a file, a terminal's (kernel_terminal.go), a
// handle of a directory of rootfs's, which the calls with a path go to
// whole (kernel_vfs.go), or one of what O_PATH opened that is no
// directory; the server holds the first end, its endpoint of the open
// file. And an end of a pair a process makes its working directory is a
// directory, the root of the server behind it (directory).
//
// A call on an open file goes on a connection to it. The kernel connects
// for the process: CONNECT on the open file's endpoint, a question, which
// the server answers there, the questions' answers in their order, with
// the asking end of a call pair it made (kernel_call.go), whose serving
// end it keeps. The request is the call sent on the connection, and the
// reply its answer, the server's own or that of one it handed the
// request on to, serving end and all. The process keeps the connection
// for its next call on the open file (connected): a call is a connection
// made only when it has none to it, or the one it has is in a call, a
// handler's call inside a call it interrupted.
//
// A packet is a header and a body; files ride with it.
//
//	header { op u32, arg u32, uid u32, gid u32 }
//
//	uid and gid are who asks: the process the kernel calls for, whose ids
//	are vm's; a server makes what it creates theirs and lets them do what
//	its modes say, gid their group (permits). A reply's are zero.
//
//	An open's OK brings the open file, the second end of a pair the server
//	made, whose first end it keeps, and its body says what it is:
//	  opened { kind u32, flags u32, dev u64, ino u64, id u64 }
//	With no body the end is the opener's as it is, a socket. A READ or a
//	WRITE brings a memfd of the
//	memory the process reads into or writes from (kernel_memfd.go): the
//	server writes the bytes there or reads them from there itself, from
//	the memfd's byte woff on, one copy, as many as asked, and closes it as
//	it answers. A server that is gone is EIO. An open file's calls:
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
//	  CANCEL              on the connection of a READ or a WRITE the server
//	                      keeps while it waits, from the kernel (waiting):
//	                      the server answers that request now, with the
//	                      count so far or ERR EINTR
//	  CONNECT             on the open file's endpoint: OK, fds[0] the asking
//	                      end of a call pair, a connection
//	  READY     arg events                 from the server, on its end of an
//	                                       open file it made telling
//	                                       (openedTells): what poll finds
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
// comes between. The locks of flock and fcntl are the kernel's
// (kernel_lock.go), kept by the dev and ino the server made the open file
// with; a FIFO's pipe is the kernel's too (kernel_fifo.go).
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
	rCancel
	rReady
	rConnect
)

// The kinds of an opened (lib/protocol/protocol.h), what an OPEN's OK says
// the end it brings is, and its server's telling of its readiness; the
// size of one. READY's flags: the terminal's TOSTOP.
const (
	openedFile     = 1
	openedTerminal = 2
	openedDir      = 3
	openedPath     = 4
	openedTells    = 16
	sizeofOpened   = 32
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

// remoteNode is what the kernel keeps of an open file of a server: the
// end of its pair, its flags and position, and what its server told of
// it. Each kind of open file of a server, remoteFile, remoteTerminal
// (kernel_terminal.go), remoteDir and remotePath, has one and answers its
// calls with it, each call by a method of its own; a node is no object.
type remoteNode struct {
	end  *pairEnd
	refs atomic.Int32
	// connecting are who asked the server for a connection and has no
	// answer yet, the first to ask first, with mu; gone is set once the
	// server's end closed. watchers are the subscriptions of a file whose
	// server tells its readiness.
	mu         sync.Mutex
	connecting []*connector
	gone       bool
	watchers   subscribers
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
	// its endpoint; ready is what the last said, and a terminal's
	// foreground group and TOSTOP.
	tells      bool
	ready      atomic.Int32
	foreground atomic.Int32
	tostop     atomic.Bool
	// locks is where the locks of flock and fcntl on the file are kept,
	// by file, what names it there (kernel_lock.go): none of a file its
	// server made with no ino. locked is set once it asked for a lock.
	locks  *lockTable
	file   fileKey
	locked atomic.Bool
}

// fileKey names to the kernel what a server serves, a file, a directory,
// a FIFO or a terminal: the dev and ino the server says, its file
// system's own, as its stat has them. A dev is a number of the kernel's
// each file system takes once (a pair's), so the two are unique while
// servers say their own, and they pass by value: rootfs says a
// directory's as its file system does. The kernel takes a server's word
// for them, as for the rest of a stat. The locks of flock and fcntl are
// kept by it (kernel_lock.go), a FIFO's pipe (kernel_fifo.go), and two
// opens of a terminal are one terminal by it (kernel_terminal.go).
type fileKey struct {
	dev, ino uint64
}

// remoteFile is a server's file.
type remoteFile struct {
	node remoteNode
}

// remoteDir is a handle of a directory of rootfs's, the VFS's, which the
// calls with a path go to whole (kernel_vfs.go); id is its, which a call
// names the root by.
type remoteDir struct {
	node remoteNode
	id   uint64
	// opath is set of one O_PATH opened: it takes no locks, EBADF.
	opath bool
}

// remotePath is a handle of rootfs's of what O_PATH opened that is no
// directory: its calls are those of what it is, by an empty path, and no
// path walks from it.
type remotePath struct {
	node remoteNode
	id   uint64
}

func (f *remoteFile) ref()   { f.node.ref() }
func (f *remoteFile) unref() { f.node.unref() }

// A remoteFile is a File, a Transfer, a Seeker, a Windowed, an Attrs and a
// Locker: its node answers them all.
func (f *remoteFile) asFile() File              { return f }
func (f *remoteFile) asTransfer() Transfer      { return f }
func (f *remoteFile) asSeeker() Seeker          { return f }
func (f *remoteFile) asDir() Dir                { return nil }
func (f *remoteFile) asTerminal() Terminal      { return nil }
func (f *remoteFile) asWindowed() Windowed      { return f }
func (f *remoteFile) asLink() Link              { return nil }
func (f *remoteFile) asAttrs() Attrs            { return f }
func (f *remoteFile) asSocket() Socket          { return nil }
func (f *remoteFile) asIoctl() Ioctl            { return nil }
func (f *remoteFile) asLocker() Locker          { return f }
func (f *remoteFile) asEpoll() *epoll           { return nil }
func (f *remoteFile) asMemfd() *memfdFile       { return nil }
func (f *remoteFile) asPair() *pairEnd          { return nil }
func (f *remoteFile) asListener() *unixListener { return nil }
func (f *remoteFile) asConsole() *consoleEnd    { return nil }
func (f *remoteFile) asVFS() *remoteDir         { return nil }
func (f *remoteFile) asCall() *callEnd          { return nil }

func (f *remoteFile) stat(p *Process, st *syscall.Stat_t) error { return f.node.stat(p, st) }
func (f *remoteFile) poll(events int16) int16                   { return f.node.poll(events) }
func (f *remoteFile) subscribe(s *subscription, events int16)   { f.node.subscribe(s, events) }
func (f *remoteFile) unsubscribe(s *subscription)               { f.node.unsubscribe(s) }
func (f *remoteFile) flags() (int, error)                       { return f.node.flags() }
func (f *remoteFile) setFlags(fl int) error                     { return f.node.setFlags(fl) }

func (f *remoteFile) read(p *Process, b []byte, mode ioMode) (int, error) {
	return f.node.read(p, b, mode)
}
func (f *remoteFile) write(p *Process, b []byte, mode ioMode) (int, error) {
	return f.node.write(p, b, mode)
}
func (f *remoteFile) pread(p *Process, b []byte, off int64) (int, error) {
	return f.node.readAt(p, b, off)
}
func (f *remoteFile) pwrite(p *Process, b []byte, off int64) (int, error) {
	return f.node.writeAt(p, b, off)
}
func (f *remoteFile) seek(p *Process, off int64, whence int) (int64, error) {
	return f.node.seek(p, off, whence)
}
func (f *remoteFile) readWindow(p *Process, w *memfdFile, woff uint32, n int, from *int64) (int, error) {
	return f.node.readWindow(p, w, woff, n, from)
}
func (f *remoteFile) writeWindow(p *Process, w *memfdFile, woff uint32, n int, to *int64) (int, error) {
	return f.node.writeWindow(p, w, woff, n, to)
}

func (f *remoteFile) truncate(p *Process, size int64) error          { return f.node.truncate(p, size) }
func (f *remoteFile) sync(p *Process, data bool) error               { return f.node.sync(p, data) }
func (f *remoteFile) chmod(p *Process, mode uint32) error            { return f.node.chmod(p, mode) }
func (f *remoteFile) chown(p *Process, uid, gid int) error           { return f.node.chown(p, uid, gid) }
func (f *remoteFile) utimes(p *Process, ts []syscall.Timespec) error { return f.node.utimes(p, ts) }
func (f *remoteFile) statfs(p *Process, st *syscall.Statfs_t) error  { return f.node.statfs(p, st) }

func (f *remoteFile) flock(p *Process, op int) error { return f.node.flock(p, op) }
func (f *remoteFile) recordLock(p *Process, cmd int, lk *syscall.Flock_t) error {
	return f.node.recordLock(p, cmd, lk)
}

func (d *remoteDir) ref()   { d.node.ref() }
func (d *remoteDir) unref() { d.node.unref() }

// A remoteDir is a File, a Seeker, a Dir, an Attrs and a Locker, and
// itself to a call that wants the VFS's handle: its node answers but
// the Dir's calls (kernel_vfs.go), readdir and seek.
func (d *remoteDir) asFile() File         { return d }
func (d *remoteDir) asTransfer() Transfer { return nil }
func (d *remoteDir) asSeeker() Seeker     { return d }
func (d *remoteDir) asDir() Dir           { return d }
func (d *remoteDir) asTerminal() Terminal { return nil }
func (d *remoteDir) asWindowed() Windowed { return nil }
func (d *remoteDir) asLink() Link         { return nil }
func (d *remoteDir) asAttrs() Attrs       { return d }
func (d *remoteDir) asSocket() Socket     { return nil }
func (d *remoteDir) asIoctl() Ioctl       { return nil }
func (d *remoteDir) asLocker() Locker {
	if d.opath {
		return nil
	}

	return d
}
func (d *remoteDir) asEpoll() *epoll           { return nil }
func (d *remoteDir) asMemfd() *memfdFile       { return nil }
func (d *remoteDir) asPair() *pairEnd          { return nil }
func (d *remoteDir) asListener() *unixListener { return nil }
func (d *remoteDir) asConsole() *consoleEnd    { return nil }
func (d *remoteDir) asVFS() *remoteDir         { return d }
func (d *remoteDir) asCall() *callEnd          { return nil }

func (d *remoteDir) stat(p *Process, st *syscall.Stat_t) error { return d.node.stat(p, st) }
func (d *remoteDir) poll(events int16) int16                   { return d.node.poll(events) }
func (d *remoteDir) subscribe(s *subscription, events int16)   { d.node.subscribe(s, events) }
func (d *remoteDir) unsubscribe(s *subscription)               { d.node.unsubscribe(s) }
func (d *remoteDir) flags() (int, error)                       { return d.node.flags() }
func (d *remoteDir) setFlags(fl int) error                     { return d.node.setFlags(fl) }

func (d *remoteDir) truncate(p *Process, size int64) error          { return d.node.truncate(p, size) }
func (d *remoteDir) sync(p *Process, data bool) error               { return d.node.sync(p, data) }
func (d *remoteDir) chmod(p *Process, mode uint32) error            { return d.node.chmod(p, mode) }
func (d *remoteDir) chown(p *Process, uid, gid int) error           { return d.node.chown(p, uid, gid) }
func (d *remoteDir) utimes(p *Process, ts []syscall.Timespec) error { return d.node.utimes(p, ts) }
func (d *remoteDir) statfs(p *Process, st *syscall.Statfs_t) error  { return d.node.statfs(p, st) }

func (d *remoteDir) flock(p *Process, op int) error { return d.node.flock(p, op) }
func (d *remoteDir) recordLock(p *Process, cmd int, lk *syscall.Flock_t) error {
	return d.node.recordLock(p, cmd, lk)
}

func (h *remotePath) ref()   { h.node.ref() }
func (h *remotePath) unref() { h.node.unref() }

// A remotePath is a File, a Link, an Attrs and a Locker: its node answers
// but readlink (kernel_vfs.go).
func (h *remotePath) asFile() File              { return h }
func (h *remotePath) asTransfer() Transfer      { return nil }
func (h *remotePath) asSeeker() Seeker          { return nil }
func (h *remotePath) asDir() Dir                { return nil }
func (h *remotePath) asTerminal() Terminal      { return nil }
func (h *remotePath) asWindowed() Windowed      { return nil }
func (h *remotePath) asLink() Link              { return h }
func (h *remotePath) asAttrs() Attrs            { return h }
func (h *remotePath) asSocket() Socket          { return nil }
func (h *remotePath) asIoctl() Ioctl            { return nil }
func (h *remotePath) asLocker() Locker          { return nil }
func (h *remotePath) asEpoll() *epoll           { return nil }
func (h *remotePath) asMemfd() *memfdFile       { return nil }
func (h *remotePath) asPair() *pairEnd          { return nil }
func (h *remotePath) asListener() *unixListener { return nil }
func (h *remotePath) asConsole() *consoleEnd    { return nil }
func (h *remotePath) asVFS() *remoteDir         { return nil }
func (h *remotePath) asCall() *callEnd          { return nil }

func (h *remotePath) stat(p *Process, st *syscall.Stat_t) error { return h.node.stat(p, st) }
func (h *remotePath) poll(events int16) int16                   { return h.node.poll(events) }
func (h *remotePath) subscribe(s *subscription, events int16)   { h.node.subscribe(s, events) }
func (h *remotePath) unsubscribe(s *subscription)               { h.node.unsubscribe(s) }
func (h *remotePath) flags() (int, error)                       { return h.node.flags() }
func (h *remotePath) setFlags(fl int) error                     { return h.node.setFlags(fl) }

func (h *remotePath) truncate(p *Process, size int64) error          { return h.node.truncate(p, size) }
func (h *remotePath) sync(p *Process, data bool) error               { return h.node.sync(p, data) }
func (h *remotePath) chmod(p *Process, mode uint32) error            { return h.node.chmod(p, mode) }
func (h *remotePath) chown(p *Process, uid, gid int) error           { return h.node.chown(p, uid, gid) }
func (h *remotePath) utimes(p *Process, ts []syscall.Timespec) error { return h.node.utimes(p, ts) }
func (h *remotePath) statfs(p *Process, st *syscall.Statfs_t) error  { return h.node.statfs(p, st) }

// opened is the open file an OPEN's OK brings, the first of its files:
// the end of a pair its server made, which the OK's body, an opened,
// says what it is, a reference to it the caller's. With no body the end
// is the opener's as it is, a plain socket (/dev/init's). The end is the
// open file's from then on: the kernel makes requests of the open file's
// calls on connections to the pair's other end, the server's endpoint of
// it. What a server says wrongly of it is EIO.
func (p *Process) opened(pk *packet) (Object, error) {
	f := pk.files[0]

	if len(pk.data) == 0 {
		return f, nil
	}

	end := f.asPair()

	if len(pk.data) != sizeofOpened || end == nil || end.shared.kind != sockSeqpacket {
		f.unref()

		return nil, syscall.EIO
	}

	b := pk.data
	obj, err := served(p, binary.LittleEndian.Uint32(b), int(binary.LittleEndian.Uint32(b[4:])), binary.LittleEndian.Uint64(b[8:]),
		binary.LittleEndian.Uint64(b[16:]), binary.LittleEndian.Uint64(b[24:]), end)

	if err != nil {
		f.unref()

		return nil, syscall.EIO
	}

	return obj, nil
}

// directory is the end of a seqpacket pair as a directory, what a process
// that makes it its working directory has: a handle of id 0, the root of
// the server behind the pair, which the calls with a path go to as to
// any handle (rootfs makes its children's root so). One for the end while
// any holds it, with a reference for the caller.
func (e *pairEnd) directory(p *Process) *remoteDir {
	e.mu.Lock()
	defer e.mu.Unlock()

	if d := e.dir; d != nil && d.node.revive() {
		return d
	}

	e.ref()
	f, _ := served(p, openedDir, syscall.O_RDONLY, 0, 0, 0, e)
	e.dir = f.asVFS()

	return e.dir
}

// revive takes a reference to a node that still has one: false for one
// whose last went.
func (n *remoteNode) revive() bool {
	for {
		r := n.refs.Load()

		if r == 0 {
			return false
		}

		if n.refs.CompareAndSwap(r, r+1) {
			return true
		}
	}
}

// served is the open file an opened makes of a pair's end for a process,
// as the open flags say: a
// file, a terminal's, or a handle of id, of a directory or of what O_PATH
// opened; with openedTells its server's READY is its readiness. The dev
// and ino it gives name it (fileKey): a file and a directory have their
// locks by the name, unless the ino is none, and a terminal is one
// terminal by it.
func served(p *Process, kind uint32, flags int, dev, ino, id uint64, end *pairEnd) (Object, error) {
	var n *remoteNode
	var f Object

	switch kind &^ openedTells {
	case openedFile:
		r := &remoteFile{}
		n, f = &r.node, r

	case openedTerminal:
		t := &remoteTerminal{key: fileKey{dev, ino}}
		n, f = &t.node, t
	case openedDir:
		d := &remoteDir{id: id, opath: flags&oPath != 0}
		n, f = &d.node, d
	case openedPath:
		h := &remotePath{id: id}
		n, f = &h.node, h
	default:
		return nil, syscall.EINVAL
	}

	if what := kind &^ openedTells; ino != 0 && (what == openedFile || what == openedDir) {
		n.locks, n.file = &p.k.locks, fileKey{dev, ino}
	}

	n.end, n.accmode, n.tells = end, flags&syscall.O_ACCMODE, kind&openedTells != 0
	n.refs.Store(1)
	n.append.Store(flags&syscall.O_APPEND != 0)
	n.nonblock.Store(flags&syscall.O_NONBLOCK != 0)

	// What the server sends on its endpoint is the kernel's to take: its
	// READY, and the answers to who connects.
	end.from.drain(n.deliver, n.hungUp)

	return f, nil
}

func (n *remoteNode) ref() {
	n.refs.Add(1)
}

func (n *remoteNode) unref() {
	if n.refs.Add(-1) == 0 {
		if n.locked.Load() {
			n.locks.forget(n)
		}

		n.end.unref()
	}
}

// A file whose server tells its readiness wakes its subscriptions as a
// READY comes; any other is ready whenever asked.
func (n *remoteNode) subscribe(s *subscription, events int16) {
	if n.tells {
		n.watchers.add(&s.in)
	}
}

func (n *remoteNode) unsubscribe(s *subscription) {
	if n.tells {
		n.watchers.remove(&s.in)
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
	return int16(n.ready.Load())
}

// A connector is a process that asked a server to connect: note is its
// event of the answer, pk the answer once done is set, eof for a server
// that went with none; left says the process waits no more, and the
// answer goes.
type connector struct {
	p    *Process
	note event
	done atomic.Bool
	pk   packet
	eof  bool
	left bool
}

// deliver takes what the server sends on its endpoint of the open file:
// READY, the file's readiness now, which wakes who watches it; and the
// answer to a CONNECT, the first's of those that wait, as the answers
// come in the order of the questions.
func (n *remoteNode) deliver(pk *packet) {
	if len(pk.data) < remoteHeader {
		dropFiles(pk.files)

		return
	}

	switch binary.LittleEndian.Uint32(pk.data) {
	case rReady:
		if n.tells && len(pk.data) >= remoteHeader+8 {
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
			n.watchers.tell()
		}
	case rOk, rErr:
		n.mu.Lock()

		if len(n.connecting) == 0 {
			n.mu.Unlock()

			break
		}

		w := n.connecting[0]
		n.connecting[0] = nil
		n.connecting = n.connecting[1:]

		if w.left {
			n.mu.Unlock()

			break
		}

		w.pk = *pk
		w.done.Store(true)
		n.mu.Unlock()
		w.p.post(&w.note)

		return
	}

	dropFiles(pk.files)
}

// hungUp hears that the server's end closed: the file hangs up, and who
// waits for a connection reads the end.
func (n *remoteNode) hungUp() {
	n.ready.Store(int32(pollHup | pollErr))
	n.mu.Lock()
	n.gone = true
	waiting := n.connecting
	n.connecting = nil

	for _, w := range waiting {
		w.eof = true
		w.done.Store(true)
	}

	n.mu.Unlock()

	for _, w := range waiting {
		if !w.left {
			w.p.post(&w.note)
		}
	}

	n.watchers.tell()
}

// connect is a connection of p's to the open file: CONNECT on its
// endpoint, a question the server answers there with the asking end of a
// call pair it made. The process takes its signals as it waits, and waits
// on. A server that is gone is EIO.
func (n *remoteNode) connect(p *Process) (*callEnd, error) {
	w := &connector{p: p}
	w.note.kind = eventChanged
	data := make([]byte, remoteHeader)
	p.head(data, rConnect, 0)
	pk := packet{data: data}

	defer func() {
		n.mu.Lock()

		if !w.done.Load() {
			w.left = true
		}

		n.mu.Unlock()
		p.events.drop(&w.note)
	}()

	// The question and its place among those that wait, as one: the
	// answers come in the questions' order.
	var sub *subscription
	_, err := p.wait(time.Time{}, func(*event) (bool, error) {
		for {
			n.mu.Lock()

			if n.gone {
				n.mu.Unlock()

				return true, syscall.EIO
			}

			full, err := n.end.to.put(&pk)

			if err == nil && !full {
				n.connecting = append(n.connecting, w)
			}

			n.mu.Unlock()

			if err != nil {
				return true, syscall.EIO
			}

			if !full {
				return true, nil
			}

			if sub != nil {
				return false, nil
			}

			// A take from the full channel wakes its writers.
			sub = p.subscription(0)
			n.end.subscribe(sub, pollOut)
		}
	})

	if sub != nil {
		n.end.unsubscribe(sub)
		p.release(sub)
	}

	if err != nil {
		return nil, err
	}

	p.wait(time.Time{}, func(*event) (bool, error) {
		return w.done.Load(), nil
	})

	if !w.done.Load() {
		return nil, syscall.EIO
	}

	if err := replyOf(&w.pk, w.eof); err != nil {
		return nil, err
	}

	if len(w.pk.files) == 0 {
		return nil, syscall.EIO
	}

	dropFiles(w.pk.files[1:])
	c := w.pk.files[0].asCall()

	if c == nil || c.serves {
		w.pk.files[0].unref()

		return nil, syscall.EIO
	}

	return c, nil
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

// head writes a request's header, the process the kernel calls for its
// user and group.
func (p *Process) head(data []byte, op, arg uint32) {
	binary.LittleEndian.PutUint32(data, op)
	binary.LittleEndian.PutUint32(data[4:], arg)
	binary.LittleEndian.PutUint32(data[8:], p.cred.euid())
	binary.LittleEndian.PutUint32(data[12:], p.cred.egid())
}

// replyOf makes of the packet a reply came in the reply itself, its body
// after the header; or it is the server's error, and EIO for a server
// that went with no answer, eof.
func replyOf(pk *packet, eof bool) error {
	if eof || len(pk.data) < remoteHeader {
		dropFiles(pk.files)
		pk.files = nil

		return syscall.EIO
	}

	if binary.LittleEndian.Uint32(pk.data) == rErr {
		dropFiles(pk.files)
		pk.files = nil

		return syscall.Errno(binary.LittleEndian.Uint32(pk.data[4:]))
	}

	pk.arg = binary.LittleEndian.Uint32(pk.data[4:])
	pk.data = pk.data[remoteHeader:]

	return nil
}

// The connections a process keeps: as many as its calls go round among.
const keptConnections = 8

// A keptConnection is a connection of a process's to an open file, node
// its.
type keptConnection struct {
	node *remoteNode
	c    *callEnd
}

// connected is a connection of p's to the open file for a call: the one
// the process kept, which is the call's alone until it gives it back, or
// a new one.
func (n *remoteNode) connected(p *Process) (*callEnd, error) {
	for i := range p.kept {
		if k := &p.kept[i]; k.node == n {
			c := k.c
			copy(p.kept[i:], p.kept[i+1:])
			p.kept[keptConnections-1] = keptConnection{}

			if c.idle() {
				return c, nil
			}

			// The server let go of it.
			c.unref()

			break
		}
	}

	return n.connect(p)
}

// keep gives the connection of a call that is done back to the process,
// for its next call on the open file, when it takes one: a call left
// unanswered, or a server gone, leaves none to keep. The connection the
// process used longest ago goes for it, and another kept to the same open
// file meanwhile, a handler's.
func (n *remoteNode) keep(p *Process, c *callEnd) {
	if !c.idle() || p.disconnected {
		c.unref()

		return
	}

	last := keptConnections - 1

	for i := range p.kept {
		if p.kept[i].node == n {
			last = i

			break
		}
	}

	if old := p.kept[last].c; old != nil {
		old.unref()
	}

	copy(p.kept[1:last+1], p.kept[:last])
	p.kept[0] = keptConnection{n, c}
}

// disconnect lets go of the connections the process keeps, as it ends.
func (p *Process) disconnect() {
	p.disconnected = true

	for i := range p.kept {
		if c := p.kept[i].c; c != nil {
			c.unref()
		}

		p.kept[i] = keptConnection{}
	}
}

// waiting is a READ or a WRITE, which may wait at its server as long as
// the file says. A signal the process takes meanwhile sends CANCEL for
// it (takeSignals), and the server's word comes after all the same: the
// count so far, or EINTR. restart says the request was cancelled so and
// no handler the call ran asked not to restart calls: the caller makes
// the call again. A call such a handler interrupted already asks nothing
// more: EINTR. The reply brings no files.
func (n *remoteNode) waiting(p *Process, op, arg uint32, body []byte, files []Object) (pk packet, restart bool, err error) {
	if p.interrupts(true) {
		return packet{}, false, syscall.EINTR
	}

	c, err := n.connected(p)

	if err != nil {
		return packet{}, false, err
	}

	defer n.keep(p, c)

	if p.interrupts(true) {
		return packet{}, false, syscall.EINTR
	}

	data := make([]byte, remoteHeader+len(body))
	p.head(data, op, arg)
	copy(data[remoteHeader:], body)

	w := p.receives.get(p)
	defer c.leave(p, w)

	if err := c.ask(w, data, files); err != nil {
		return packet{}, false, syscall.EIO // the server is gone
	}

	outer, was := p.ask(c)
	defer p.unask(outer, was)

	p.wait(time.Time{}, func(*event) (bool, error) {
		return w.done.Load(), nil
	})

	pk = w.answer.pk
	err = replyOf(&pk, w.answer.eof)
	dropFiles(pk.files)
	pk.files = nil

	return pk, p.cancelled && !p.interrupts(true), err
}

// call is a call on the node: the request sent on a connection to it,
// the reply's packet. files go with the request and stay the caller's.
// Only an OPEN's reply brings a file, the open file's endpoint, and a
// CONNECT's, the listener, which the packet keeps; the files of any other
// reply, or the rest of theirs, go here.
func (n *remoteNode) call(p *Process, op, arg uint32, body []byte, files []Object) (packet, error) {
	data := make([]byte, remoteHeader+len(body))
	p.head(data, op, arg)
	copy(data[remoteHeader:], body)

	return n.asked(p, op, data, files)
}

// asked is call with the request made already, its header and its body
// in data, which the packet keeps.
func (n *remoteNode) asked(p *Process, op uint32, data []byte, files []Object) (packet, error) {
	c, err := n.connected(p)

	if err != nil {
		return packet{}, err
	}

	defer n.keep(p, c)

	// The answer is given to the waiter as the server sends it: the
	// process takes its signals as it waits, and waits on.
	w := p.receives.get(p)
	defer c.leave(p, w)

	if err := c.ask(w, data, files); err != nil {
		return packet{}, syscall.EIO // the server is gone
	}

	outer, was := p.ask(c)
	defer p.unask(outer, was)

	p.wait(time.Time{}, func(*event) (bool, error) {
		return w.done.Load(), nil
	})

	pk := w.answer.pk

	if err := replyOf(&pk, w.answer.eof); err != nil {
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
	*st = syscall.Stat_t{}
	st.Ino, st.Size, st.Mode, st.Nlink = le.Uint64(b), int64(le.Uint64(b[8:])), le.Uint32(b[24:]), uint64(le.Uint32(b[28:]))
	st.Uid, st.Gid, st.Blksize = le.Uint32(b[32:]), le.Uint32(b[36:]), 4096
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

// The attributes of an open file of a server, what its Attrs role asks.

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

// The locks of an open file of a server, what its Locker role asks, are
// the kernel's (kernel_lock.go): a file its server made with no ino has
// none, EINVAL.

// flock is flock's lock of the open file, shared, exclusive or none: one
// another open file's is in the way of is EAGAIN with LOCK_NB, and waited
// for without.
func (n *remoteNode) flock(p *Process, op int) error {
	if n.locks == nil {
		return syscall.EINVAL
	}

	switch kind := op &^ syscall.LOCK_NB; kind {
	case syscall.LOCK_UN:
		n.locks.unlock(n, false, 0, 0)

		return nil
	case syscall.LOCK_SH, syscall.LOCK_EX:
		return n.locks.lock(p, &fileLock{owner: n, write: kind == syscall.LOCK_EX}, op&syscall.LOCK_NB == 0)
	}

	return syscall.EINVAL
}

// waits is a call its server may keep for as long as it takes, a
// terminal's drain: a signal the process takes cancels it, and it is made
// again when the handlers ask for that.
func (n *remoteNode) waits(p *Process, op, arg uint32, body []byte) error {
	for {
		_, restart, err := n.waiting(p, op, arg, body, nil)

		if err == syscall.EINTR && restart {
			continue
		}

		return err
	}
}

// recordLock is fcntl's lock on the bytes a struct flock says, from the
// start, the position or the end, before the start for a negative length,
// to the end and past for none: F_OFD_SETLK sets it in place of what the
// open file held of the bytes, EAGAIN for another open file's in the way,
// F_OFD_SETLKW waits for it, F_UNLCK lets the bytes go, and F_OFD_GETLK
// tells the lock in the way, from the start, F_UNLCK for none. A lock to
// read is set on a file open to read, one to write on a file open to
// write, as POSIX has it: EBADF else.
func (n *remoteNode) recordLock(p *Process, cmd int, lk *syscall.Flock_t) error {
	if n.locks == nil {
		return syscall.EINVAL
	}

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

	if start < 0 || lk.Type != fRdlck && lk.Type != fWrlck && lk.Type != fUnlck {
		return syscall.EINVAL
	}

	want := &fileLock{owner: n, ranged: true, write: lk.Type == fWrlck, start: uint64(start), stop: lockPast}

	if length != 0 && want.start <= lockPast-uint64(length) {
		want.stop = want.start + uint64(length)
	}

	switch {
	case cmd != fOfdGetlk && (lk.Type == fRdlck && n.accmode == syscall.O_WRONLY || lk.Type == fWrlck && n.accmode == syscall.O_RDONLY):
		return syscall.EBADF
	case cmd == fOfdGetlk:
		lk.Whence, lk.Pid = 0, -1
		way, found := n.locks.way(want)

		if !found {
			lk.Type, lk.Start, lk.Len = fUnlck, start, length

			return nil
		}

		lk.Type, lk.Start, lk.Len = fRdlck, int64(way.start), 0

		if way.write {
			lk.Type = fWrlck
		}

		if way.stop != lockPast {
			lk.Len = int64(way.stop - way.start)
		}

		return nil
	case lk.Type == fUnlck:
		n.locks.unlock(n, true, want.start, want.stop)

		return nil
	}

	return n.locks.lock(p, want, cmd == fOfdSetlkw)
}

// A directory's readdir and seek; its other calls are the VFS's
// (kernel_vfs.go).

// readdir fills the entries after the last it gave, as many as fit, in
// the guest's struct dirent layout: ino, off, reclen, type, name; an
// entry's off is its position.
func (d *remoteDir) readdir(p *Process, b []byte) (int, error) {
	var after [8]byte
	binary.LittleEndian.PutUint64(after[:], d.node.after)
	pk, err := d.node.on(p, rReaddir, 0, after[:])

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
		d.node.after = pos
		data = data[min(end+1, len(data)):]
	}

	return n, nil
}

// seek of a directory is rewinddir.
func (d *remoteDir) seek(p *Process, off int64, whence int) (int64, error) {
	if off != 0 || whence != 0 {
		return 0, syscall.EINVAL
	}

	d.node.after = 0

	return 0, nil
}

// The reads and writes of an open file of a server, a file's or a
// terminal's.

// readAt reads into b, the process's memory, at off: the READ brings a
// memfd of b.
func (n *remoteNode) readAt(p *Process, b []byte, off int64) (int, error) {
	if off < 0 || len(b) == 0 {
		return n.readInto(p, nil, 0, 0, off)
	}

	w := window(p, b, syscall.O_WRONLY)
	count, err := n.readInto(p, w, 0, len(b), off)
	w.unref()

	return count, err
}

// readInto reads size bytes at off into the memfd w, from its byte woff
// on: the READ brings w, and the server writes the bytes there itself. A
// count past size is the server's lie: EIO.
func (n *remoteNode) readInto(p *Process, w *memfdFile, woff uint32, size int, off int64) (int, error) {
	if off < 0 {
		return 0, syscall.EINVAL
	}

	if size == 0 {
		return 0, nil
	}

	body := ioBody(off, woff, n.ioFlags())

	for {
		pk, cancelled, err := n.waiting(p, rRead, uint32(size), body[:], []Object{w})

		if err == syscall.EINTR && cancelled && p.interrupted() {
			continue
		}

		if err != nil {
			return 0, err
		}

		if len(pk.data) < 4 || int(binary.LittleEndian.Uint32(pk.data)) > size {
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

func (n *remoteNode) writeAt(p *Process, b []byte, off int64) (int, error) {
	count, _, err := n.put(p, b, off, false)

	return count, err
}

// put writes b, the process's memory, at off, or atEnd at the end the
// file has when the server takes the write: the WRITE brings a memfd of b.
func (n *remoteNode) put(p *Process, b []byte, off int64, atEnd bool) (int, int64, error) {
	if off < 0 || len(b) == 0 {
		return n.writeFrom(p, nil, 0, 0, off, atEnd)
	}

	w := window(p, b, syscall.O_RDONLY)
	count, at, err := n.writeFrom(p, w, 0, len(b), off, atEnd)
	w.unref()

	return count, at, err
}

// writeFrom writes size bytes of the memfd w, from its byte woff on, at
// off, or atEnd at the end the file has when the server takes the write:
// the WRITE brings w, and the server reads the bytes from there itself.
// The count and where they went; a count past size is the server's lie:
// EIO.
func (n *remoteNode) writeFrom(p *Process, w *memfdFile, woff uint32, size int, off int64, atEnd bool) (int, int64, error) {
	if off < 0 {
		return 0, 0, syscall.EINVAL
	}

	if size == 0 {
		return 0, off, nil
	}

	flags := n.ioFlags()

	if atEnd {
		flags |= ioAppend
	}

	body := ioBody(off, woff, flags)

	for {
		pk, cancelled, err := n.waiting(p, rWrite, uint32(size), body[:], []Object{w})

		if err == syscall.EINTR && cancelled && p.interrupted() {
			continue
		}

		if err != nil {
			return 0, 0, err
		}

		if len(pk.data) < 12 || int(binary.LittleEndian.Uint32(pk.data)) > size {
			return 0, 0, syscall.EIO
		}

		return int(binary.LittleEndian.Uint32(pk.data)), int64(binary.LittleEndian.Uint64(pk.data[4:])), nil
	}
}

func (n *remoteNode) read(p *Process, b []byte, mode ioMode) (int, error) {
	if p.chaosFail("read") {
		return 0, syscall.EIO
	}

	count, err := n.readAt(p, b, atomic.LoadInt64(&n.pos))
	atomic.AddInt64(&n.pos, int64(count))

	return count, err
}

// write writes at the position, or with O_APPEND at the end, which the
// server finds as it writes and says; the position is after what was
// written. Only an append's place is the server's to say: a server that
// handed the WRITE on, moved, answers with where it went on its device.
func (n *remoteNode) write(p *Process, b []byte, mode ioMode) (int, error) {
	if p.chaosFail("write") {
		return 0, syscall.EIO
	}

	off := atomic.LoadInt64(&n.pos)
	atEnd := n.append.Load()
	count, at, err := n.put(p, b, off, atEnd)

	if err == nil {
		if !atEnd {
			at = off
		}

		atomic.StoreInt64(&n.pos, at+int64(count))
	}

	return count, err
}

func (n *remoteNode) seek(p *Process, off int64, whence int) (int64, error) {
	base := int64(0)

	switch whence {
	case 0:
	case 1:
		base = atomic.LoadInt64(&n.pos)
	case 2:
		var st syscall.Stat_t

		if err := n.stat(p, &st); err != nil {
			return 0, err
		}

		base = st.Size
	default:
		return 0, syscall.EINVAL
	}

	if base+off < 0 {
		return 0, syscall.EINVAL
	}

	atomic.StoreInt64(&n.pos, base+off)

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

// A noRoot is a File and a Dir.
func (r *noRoot) asFile() File              { return r }
func (r *noRoot) asTransfer() Transfer      { return nil }
func (r *noRoot) asSeeker() Seeker          { return nil }
func (r *noRoot) asDir() Dir                { return r }
func (r *noRoot) asTerminal() Terminal      { return nil }
func (r *noRoot) asWindowed() Windowed      { return nil }
func (r *noRoot) asLink() Link              { return nil }
func (r *noRoot) asAttrs() Attrs            { return nil }
func (r *noRoot) asSocket() Socket          { return nil }
func (r *noRoot) asIoctl() Ioctl            { return nil }
func (r *noRoot) asLocker() Locker          { return nil }
func (r *noRoot) asEpoll() *epoll           { return nil }
func (r *noRoot) asMemfd() *memfdFile       { return nil }
func (r *noRoot) asPair() *pairEnd          { return nil }
func (r *noRoot) asListener() *unixListener { return nil }
func (r *noRoot) asConsole() *consoleEnd    { return nil }
func (r *noRoot) asVFS() *remoteDir         { return nil }
func (r *noRoot) asCall() *callEnd          { return nil }

func (r *noRoot) poll(events int16) int16 {
	return events & (pollIn | pollOut | pollRdnorm | pollWrnorm)
}

func (r *noRoot) subscribe(s *subscription, events int16)   {}
func (r *noRoot) unsubscribe(s *subscription)               {}
func (r *noRoot) flags() (int, error)                       { return syscall.O_RDONLY, nil }
func (r *noRoot) setFlags(fl int) error                     { return nil }
func (r *noRoot) readdir(p *Process, b []byte) (int, error) { return 0, nil }
func (r *noRoot) path(p *Process) (string, error)           { return "/", nil }

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

func (r *noRoot) openPath(p *Process, path string, flags int, mode uint32) (Object, error) {
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
func (r *noRoot) mountPath(p *Process, target, source string, conn Object) error {
	return syscall.EINVAL
}

func (r *noRoot) unmountPath(p *Process, path string, flags uint32) error {
	return syscall.EINVAL
}

func (r *noRoot) bindPath(p *Process, path string, mode uint32, held Object) error {
	return syscall.EOPNOTSUPP
}

func (r *noRoot) connectPath(p *Process, path string) (Object, error) {
	return nil, syscall.EOPNOTSUPP
}
