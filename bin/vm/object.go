//go:build linux && amd64

package main

import (
	"syscall"
	"time"
	"unsafe"
)

// What a descriptor refers to is an Object: a handle the kernel counts
// the references of, the descriptors and processes that hold it and what
// else does, a packet in flight or a member of an epoll, its last unref
// for its type to clean up after; and what a wait waits for, any object,
// by its readiness and its changes (subscription.go): a poll or a select
// of a process's, a set of epoll's, a call that waits. All else it is, it
// is in a role, which
// its as methods give, nil for a role it does not play: a File, what any
// descriptor's object is, a Transfer reads and writes, a Seeker seeks, a
// Dir takes the calls with a path, a Terminal a terminal's, a Windowed
// file moves bytes to and from a memfd's window, a Link is what
// readlinkat reads by an empty path, an Attrs has the attributes, a
// Socket speaks sockets, an Ioctl takes ioctls, a Locker takes locks. A
// few calls want a type of the kernel's itself: an epoll
// (epoll.go), a memfd (memfd_file.go), an end of a socket pair
// (pair_end.go), a socket that listens (unix_listener.go), the
// console (console.go) or a handle of the VFS's (remote_dir.go).
// Every object is the kernel's: an open file of a server
// (remote_node.go), a pipe's end (pipe_end.go), a socket, a memfd,
// the console's, a disk's or an epoll. Each says all it is itself: no
// object type embeds another, nor has a method of another's.
type Object interface {
	// ref takes a reference, unref drops one; the last closes the object.
	ref()
	unref()
	// poll is the events ready now of those asked. subscribe links the
	// subscription, its subscriber's, in the lists of the object's changes
	// as it may become ready for events, which wake its waker; unsubscribe
	// takes it back. An object poll found not ready for events wakes its
	// subscriptions for them as it may become ready; one that never
	// changes, ready whenever asked, keeps no subscription and wakes none.
	poll(events int16) int16
	subscribe(s *subscription, events int16)
	unsubscribe(s *subscription)
	// asFile and the rest are the object in a role, nil for one it does
	// not play, or the type of the kernel's it is.
	asFile() File
	asTransfer() Transfer
	asSeeker() Seeker
	asDir() Dir
	asTerminal() Terminal
	asWindowed() Windowed
	asLink() Link
	asAttrs() Attrs
	asSocket() Socket
	asIoctl() Ioctl
	asLocker() Locker
	asEpoll() *epoll
	asMemfd() *memfdFile
	asPair() *pairEnd
	asListener() *unixListener
	asConsole() *consoleEnd
	asVFS() *remoteDir
	asCall() *callEnd
}

// A File is an open file, what any descriptor's object is: what fstat
// and fcntl's F_GETFL and F_SETFL ask of it.
type File interface {
	Object
	// stat is fstat.
	stat(p *Process, st *syscall.Stat_t) error
	// flags are the status flags of fcntl: the access mode, O_NONBLOCK and
	// what else the object keeps; setFlags sets those F_SETFL may.
	flags() (int, error)
	setFlags(fl int) error
}

// ioMode is how one transfer goes: nonblock as with MSG_DONTWAIT, quiet
// with MSG_NOSIGNAL, an EPIPE without SIGPIPE.
type ioMode struct {
	nonblock bool
	quiet    bool
}

type Transfer interface {
	Object
	read(p *Process, b []byte, mode ioMode) (int, error)
	write(p *Process, b []byte, mode ioMode) (int, error)
	pread(p *Process, b []byte, off int64) (int, error)
	pwrite(p *Process, b []byte, off int64) (int, error)
}

type Seeker interface {
	Object
	seek(p *Process, off int64, whence int) (int64, error)
}

// A Dir is a directory a process holds: its root, its working directory,
// a directory descriptor. A call with a path goes whole to the directory
// it starts at (Process.start), the root for an absolute path, else the
// working directory or the descriptor's, and what serves the directory
// walks it, follows its links and makes its last step as Linux's does:
// rootfs, the VFS, for its handles (remote_dir.go), and the empty root of
// a process that has none (noRoot). follow says whether a last symbolic
// link is followed.
type Dir interface {
	Object
	// path is the directory's own path from the process's root.
	path(p *Process) (string, error)
	// readdir gives the entries after the last it gave, as getdents64
	// does, in the guest's struct dirent layout.
	readdir(p *Process, b []byte) (int, error)
	openPath(p *Process, path string, flags int, mode uint32) (Object, error)
	statPath(p *Process, path string, follow bool, st *syscall.Stat_t) error
	readlinkPath(p *Process, path string) (string, error)
	mkdirPath(p *Process, path string, mode uint32) error
	// mknodPath makes a FIFO or a regular file.
	mknodPath(p *Process, path string, mode uint32) error
	symlinkPath(p *Process, target, path string) error
	unlinkPath(p *Process, path string, dir bool) error
	// renamePath and linkPath take the new path from to.
	renamePath(p *Process, old string, to Dir, new string) error
	linkPath(p *Process, old string, to Dir, new string, follow bool) error
	chmodPath(p *Process, path string, mode uint32, follow bool) error
	chownPath(p *Process, path string, uid, gid int, follow bool) error
	utimesPath(p *Process, path string, ts []syscall.Timespec, follow bool) error
	truncatePath(p *Process, path string, size int64) error
	statfsPath(p *Process, path string, st *syscall.Statfs_t) error
	// realpathOf is the path from the root of what path names, with no
	// ".", ".." or link in it.
	realpathOf(p *Process, path string) (string, error)
	// mountPath mounts on target the directory source names, or the root
	// of the file system conn is a connection to; unmountPath lets what
	// is mounted on path go.
	mountPath(p *Process, target, source string, conn Object) error
	unmountPath(p *Process, path string, flags uint32) error
	// bindPath makes a socket node of path, which holds held;
	// connectPath is what the socket node path names holds.
	bindPath(p *Process, path string, mode uint32, held Object) error
	connectPath(p *Process, path string) (Object, error)
}

// A Terminal is a terminal's open file (remote_terminal.go): its server
// keeps all of the terminal, its settings, its window, its foreground
// group and its discipline, which these calls ask; the kernel keeps the
// processes' side of it, the session it controls and job control, which
// its reads and writes go through.
type Terminal interface {
	Transfer
	// device names the terminal, the same for each open of it (fileKey).
	device() fileKey
	// jobs is what job control asks of the terminal, as its server said
	// it last: the foreground group, TOSTOP, and whether it hung up.
	jobs() (foreground int32, tostop, hungUp bool)
	getattr(p *Process) ([]byte, error)
	// setattr sets the settings when TCSANOW, TCSADRAIN or TCSAFLUSH
	// says: one that drains waits for the output to go.
	setattr(p *Process, when uint32, termios []byte) error
	flush(p *Process, queue uint32) error
	flow(p *Process, action uint32) error
	drain(p *Process) error
	window(p *Process) ([]byte, error)
	setWindow(p *Process, size []byte) error
	// pending is what a read would find, unsent what was written that the
	// other end has not read.
	pending(p *Process) (uint32, error)
	unsent(p *Process) (uint32, error)
	// name is the path of the terminal's device; with peer a master's
	// slave's, ENOTTY of a slave.
	name(p *Process, peer bool) (string, error)
	foreground(p *Process) (uint32, error)
	setForeground(p *Process, pgrp uint32) error
	// control makes the terminal control the session sid, pgrp its
	// foreground group; sid 0 none.
	control(p *Process, sid, pgrp int32) error
	// reopen is another open of the terminal, /dev/tty's.
	reopen(p *Process, flags int) (Terminal, error)
}

// A Windowed file moves bytes straight between itself and a memfd's
// window, the memory the memfd is of (memfd_file.go): a server's file,
// by a READ or WRITE that brings the memfd, and a disk of vm's, which vm
// reads into it or writes from it. from and to are the file's offsets,
// nil for its position, which moves.
type Windowed interface {
	Object
	readWindow(p *Process, w *memfdFile, woff uint32, n int, from *int64) (int, error)
	writeWindow(p *Process, w *memfdFile, woff uint32, n int, to *int64) (int, error)
}

// A Link is a descriptor of what O_PATH opened that is no directory,
// which readlinkat reads by an empty path: a symbolic link's target, or
// ENOENT for what is no link, as Linux has it.
type Link interface {
	Object
	readlink(p *Process) (string, error)
}

// An Attrs has the attributes a descriptor's calls set, and knows the file
// system it is on: an open file of a server (remote_node.go).
type Attrs interface {
	Object
	truncate(p *Process, size int64) error
	sync(p *Process, data bool) error
	chmod(p *Process, mode uint32) error
	chown(p *Process, uid, gid int) error
	utimes(p *Process, ts []syscall.Timespec) error
	statfs(p *Process, st *syscall.Statfs_t) error
}

// A Socket takes the socket calls as libc makes them, with the guest's
// addresses and buffers; the addresses it returns are the guest's. A pair
// of the kernel's is a Transfer too. An open file of a server is none: a
// program that speaks the protocol itself has a plain socket for it, as
// /dev/init's open is.
type Socket interface {
	Object
	bind(p *Process, at, n uint32) error
	connect(p *Process, at, n uint32) error
	listen(backlog int) error
	// accept returns the connection and the peer's address.
	accept(p *Process, flags uint32) (Object, []byte, error)
	sendmsg(p *Process, at, flags uint32) (int, error)
	recvmsg(p *Process, at, flags uint32) (int, error)
	sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error)
	recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error)
	sockname(p *Process) ([]byte, error)
	peername(p *Process) ([]byte, error)
	getsockopt(p *Process, level, name, at, lenAt uint32) error
	setsockopt(p *Process, level, name, at, n uint32) error
	shutdown(how int) error
	// socketKind is SOCK_STREAM, SOCK_DGRAM or SOCK_SEQPACKET.
	socketKind() int
}

// An Ioctl takes ioctls with the host's argument layout, which the libc
// functions convert the guest's to; a request that takes an int, TCSBRK,
// TCXONC, TCFLSH, takes it pointed to.
type Ioctl interface {
	Object
	ioctl(req uintptr, arg unsafe.Pointer) error
}

// A Locker takes flock's locks and the record locks of fcntl, which the
// kernel keeps (lock_table.go): without LOCK_NB, and with F_OFD_SETLKW,
// the call waits for a lock another open file holds, else that is
// EAGAIN.
type Locker interface {
	Object
	flock(p *Process, op int) error
	recordLock(p *Process, cmd int, lk *syscall.Flock_t) error
}

const (
	pollIn     = 0x1
	pollPri    = 0x2
	pollOut    = 0x4
	pollErr    = 0x8
	pollHup    = 0x10
	pollNval   = 0x20
	pollRdnorm = 0x40
	pollWrnorm = 0x100
)

// closed tells whether a channel of wait has closed since.
func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// deadline is when a wait of timeout milliseconds ends, or zero for none.
func deadline(timeout int) time.Time {
	if timeout < 0 {
		return time.Time{}
	}

	return time.Now().Add(time.Duration(timeout) * time.Millisecond)
}

// waitMs is a timeout in milliseconds, rounded up and bounded.
func waitMs(d time.Duration) int {
	return int(min((d+time.Millisecond-1)/time.Millisecond, maxPollWaitMs))
}

const maxPollWaitMs = 1 << 30

const (
	fGetlk     = 5
	fSetlk     = 6
	fSetlkw    = 7
	fOfdGetlk  = 36
	fOfdSetlk  = 37
	fOfdSetlkw = 38
	fRdlck     = 0
	fWrlck     = 1
	fUnlck     = 2
)

const (
	tcgets     = 0x5401
	tcsets     = 0x5402
	tcsbrk     = 0x5409
	tcxonc     = 0x540a
	tcflsh     = 0x540b
	tiocgpgrp  = 0x540f
	tiocspgrp  = 0x5410
	tiocgwinsz = 0x5413
	tiocswinsz = 0x5414
	fionread   = 0x541b
	fionbio    = 0x5421
	fionclex   = 0x5450
	fioclex    = 0x5451
)

const (
	oPath             = 0o10000000
	atFdcwd           = -100
	atSymlinkNofollow = 0x100
	atRemovedir       = 0x200
	atEaccess         = 0x200
	atSymlinkFollow   = 0x400
	atEmptyPath       = 0x1000
	sIfifo            = 0o10000
)

func bit(b bool) uint32 {
	if b {
		return 1
	}

	return 0
}
