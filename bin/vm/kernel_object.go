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
// by its readiness and its changes (kernel_queue.go): a poll or a select
// of a process's, a set of epoll's, a call that waits. All else it is, it
// is in a role, which
// its as methods give, nil for a role it does not play: a File, what any
// descriptor's object is, a Transfer reads and writes, a Seeker seeks, a
// Dir takes the calls with a path, a Terminal a terminal's, a Windowed
// file moves bytes to and from a memfd's window, a Link is what
// readlinkat reads by an empty path, an Attrs has the attributes, a
// Socket speaks sockets, an Ioctl takes ioctls, a Locker takes locks. A
// few calls want a type of the kernel's itself: an epoll
// (kernel_epoll.go), a memfd (kernel_memfd.go), an end of a socket pair
// (kernel_endpoint.go), a socket that listens (kernel_unix.go), the
// console (kernel_console.go) or a handle of the VFS's (kernel_vfs.go).
// Every object is the kernel's: an open file of a server
// (kernel_remote.go), a pipe's end (kernel_pipe.go), a socket, a memfd,
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
// rootfs, the VFS, for its handles (kernel_vfs.go), and the empty root of
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

// A Terminal is a terminal's open file (kernel_terminal.go): its server
// keeps all of the terminal, its settings, its window, its foreground
// group and its discipline, which these calls ask; the kernel keeps the
// processes' side of it, the session it controls and job control, which
// its reads and writes go through.
type Terminal interface {
	Transfer
	// device is the terminal's device, the same for each open of it.
	device() (uint64, uint64)
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
// window, the memory the memfd is of (kernel_memfd.go): a server's file,
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
// system it is on: an open file of a server (kernel_remote.go).
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

// A Locker takes flock's locks and the record locks of fcntl, which try
// without blocking: EAGAIN or EACCES is a lock held by another.
type Locker interface {
	Object
	flock(p *Process, op int) error
	recordLock(p *Process, cmd int, lk *syscall.Flock_t) error
}

type Descriptor struct {
	obj     Object
	cloexec bool
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

// fd is the object of a descriptor.
func (p *Process) fd(fd uint32) (Object, error) {
	d, ok := p.files.at(int(fd))

	if !ok {
		return nil, syscall.EBADF
	}

	return d.obj, nil
}

// fileAt is the File of a descriptor: what any descriptor's object is.
func (p *Process) fileAt(fd uint32) (File, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if f := obj.asFile(); f != nil {
		return f, nil
	}

	return nil, syscall.EBADF
}

// transferAt is the Transfer of a descriptor: read on a directory is
// EISDIR, as Linux has it, write and the rest EBADF.
func (p *Process) transferAt(fd uint32, read bool) (Transfer, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if t := obj.asTransfer(); t != nil {
		return t, nil
	}

	if obj.asDir() != nil && read {
		return nil, syscall.EISDIR
	}

	return nil, syscall.EBADF
}

func (p *Process) seekerAt(fd uint32) (Seeker, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if s := obj.asSeeker(); s != nil {
		return s, nil
	}

	return nil, syscall.ESPIPE
}

func (p *Process) dirAt(fd uint32) (Dir, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if d := obj.asDir(); d != nil {
		return d, nil
	}

	return nil, syscall.ENOTDIR
}

func (p *Process) attrsAt(fd uint32) (Attrs, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if a := obj.asAttrs(); a != nil {
		return a, nil
	}

	return nil, syscall.EINVAL
}

func (p *Process) socketAt(fd uint32) (Socket, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if s := obj.asSocket(); s != nil {
		return s, nil
	}

	return nil, syscall.ENOTSOCK
}

func (p *Process) lockerAt(fd uint32) (Locker, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if l := obj.asLocker(); l != nil {
		return l, nil
	}

	return nil, syscall.EBADF
}

// install puts an object at the lowest free descriptor from min; the
// descriptor takes the reference.
func (p *Process) install(obj Object, min int, cloexec bool) (int, error) {
	if fd := p.files.lowest(min); fd < p.fdLimit() {
		p.files.put(fd, obj, cloexec)

		return fd, nil
	}

	obj.unref()

	return -1, syscall.EMFILE
}

func (p *Process) fdLimit() int {
	return int(min64(p.limits[rlimitNofile].cur, 1<<20))
}

// descriptorFree is EMFILE when the process has no descriptor free: what
// takes one checks first, as Linux takes one before the rest of the call,
// so open makes no file and accept takes no connection then.
func (p *Process) descriptorFree() error {
	if p.files.lowest(0) >= p.fdLimit() {
		return syscall.EMFILE
	}

	return nil
}

func min64(a, b uint64) uint64 {
	return min(a, b)
}

func (p *Process) closeFd(fd int) bool {
	obj := p.files.take(fd)

	if obj == nil {
		return false
	}

	obj.unref()

	return true
}

// dupTo makes new refer to old's object, closing what new referred to.
func (p *Process) dupTo(old, new uint32, cloexec bool) (int, error) {
	obj, err := p.fd(old)

	if err != nil {
		return 0, err
	}

	if uint64(new) >= p.limits[rlimitNofile].cur {
		return 0, syscall.EBADF
	}

	p.placeAt(obj, new, cloexec)

	return int(new), nil
}

// placeAt makes a descriptor refer to an object, closing what it referred
// to; the descriptor is below the process's limit.
func (p *Process) placeAt(obj Object, fd uint32, cloexec bool) {
	obj.ref()
	p.closeFd(int(fd))
	p.files.put(int(fd), obj, cloexec)
}

// closed tells whether a channel of wait has closed since.
func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// idle waits for timeout milliseconds, forever when negative, or until a
// signal interrupts: then the error is EINTR, after the handlers ran when
// all of them restart calls and restart is set.
func (p *Process) idle(timeout int, restart bool) error {
	_, err := p.waitObjects(nil, timeout, restart)

	return err
}

// deadline is when a wait of timeout milliseconds ends, or zero for none.
func deadline(timeout int) time.Time {
	if timeout < 0 {
		return time.Time{}
	}

	return time.Now().Add(time.Duration(timeout) * time.Millisecond)
}

// pollEntry is an object and the events asked of it, revents those ready.
type pollEntry struct {
	obj     Object
	events  int16
	revents int16
	sub     *subscription
}

// waitObjects waits until an object is ready for its events: it
// subscribes the process to them all and asks them all, then, as long as
// none is ready, asks one on its event, until the deadline of timeout
// milliseconds. A signal interrupts it: then the error is EINTR,
// after the handlers ran when all of them restart calls and restart is
// set. It returns how many are ready.
func (p *Process) waitObjects(entries []pollEntry, timeout int, restart bool) (int, error) {
	for i := range entries {
		entries[i].sub = p.subscription(uint64(i))
		entries[i].obj.subscribe(entries[i].sub, entries[i].events)
	}

	defer func() {
		for i := range entries {
			entries[i].obj.unsubscribe(entries[i].sub)
			p.release(entries[i].sub)
		}
	}()

	ready := 0
	_, err := p.wait(deadline(timeout), func(e *event) (bool, error) {
		switch {
		case e == nil || e.kind == eventSignal:
			// The first look, or a signal: all of it.
		case e.kind != eventChanged || e.id >= uint64(len(entries)) || e != &entries[e.id].sub.event:
			return false, nil
		case entries[e.id].obj.poll(entries[e.id].events) == 0:
			return false, nil
		}

		for p.sig.deliverable() != 0 {
			if !restart || !p.interrupted() {
				return true, syscall.EINTR
			}
		}

		ready = pollEntries(entries)

		return ready > 0, nil
	})

	return ready, err
}

// pollEntries asks every entry's object for its events and counts the
// ready ones.
func pollEntries(entries []pollEntry) int {
	ready := 0

	for i := range entries {
		entries[i].revents = entries[i].obj.poll(entries[i].events)

		if entries[i].revents != 0 {
			ready++
		}
	}

	return ready
}

// waitFor waits until ready says so, as waitObjects waits for files: the
// process subscribed to the changes of list. A signal interrupts it,
// restart as there.
func (p *Process) waitFor(ready func() bool, list *subscribers, restart bool) error {
	sub := p.subscription(0)
	list.add(&sub.in)

	defer p.release(sub)
	defer list.remove(&sub.in)

	_, err := p.wait(time.Time{}, func(*event) (bool, error) {
		for p.sig.deliverable() != 0 {
			if !restart || !p.interrupted() {
				return true, syscall.EINTR
			}
		}

		return ready(), nil
	})

	return err
}

// waitObject waits until one object is ready for events.
func (p *Process) waitObject(obj Object, events int16, timeout int, restart bool) (int, error) {
	return p.waitObjects([]pollEntry{{obj: obj, events: events}}, timeout, restart)
}

// waitMs is a timeout in milliseconds, rounded up and bounded.
func waitMs(d time.Duration) int {
	return int(min((d+time.Millisecond-1)/time.Millisecond, maxPollWaitMs))
}

const maxPollWaitMs = 1 << 30
