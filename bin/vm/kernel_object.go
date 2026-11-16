//go:build linux && amd64

package main

import (
	"syscall"
	"time"
	"unsafe"
)

// An open file is a File: what a descriptor refers to, shared by the
// descriptors and processes that refer to it, and what every kind of call
// asks of it. What else a file can do is a role, an interface a call
// casts to: a Transfer reads and writes, a Seeker seeks, a Dir takes the
// calls with a path, a Terminal a terminal's, a Socket speaks sockets, an
// Ioctl takes ioctls, a Locker takes locks, an Epoll watches files
// (kernel_epoll.go). Every file is the kernel's: one
// of a server (kernel_remote.go), a pipe (kernel_pipe.go), a socket, a
// memfd, the console or a disk (kernel_console.go); each plays its own
// roles.
type File interface {
	// ref takes a reference, unref drops one; the last closes the object.
	ref()
	unref()
	// stat is fstat.
	stat(p *Process, st *syscall.Stat_t) error
	// poll is the events ready now of those asked. subscribe makes the
	// file wake to, with id, as it may become ready for events, and tells
	// whether it ever changes: a file ready whenever asked does not, and
	// takes no subscription; unsubscribe takes one back. A file poll found
	// not ready for events wakes its subscriptions for them as it may
	// become ready (kernel_queue.go).
	poll(events int16) int16
	subscribe(to waker, id uint64, events int16) bool
	unsubscribe(to waker, id uint64)
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
	File
	read(p *Process, b []byte, mode ioMode) (int, error)
	write(p *Process, b []byte, mode ioMode) (int, error)
	pread(p *Process, b []byte, off int64) (int, error)
	pwrite(p *Process, b []byte, off int64) (int, error)
}

// A Vectored transfer runs readv, writev, preadv or pwritev on the host
// with iovecs into the guest's memory: call is the system call, off its
// offset. A Transfer without the role gathers and scatters instead.
type Vectored interface {
	Transfer
	vectored(p *Process, call uintptr, iov []syscall.Iovec, off uint64, events int16) (int, error)
}

type Seeker interface {
	File
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
	File
	// path is the directory's own path from the process's root.
	path(p *Process) (string, error)
	// readdir gives the entries after the last it gave, as getdents64
	// does, in the guest's struct dirent layout.
	readdir(p *Process, b []byte) (int, error)
	openPath(p *Process, path string, flags int, mode uint32) (File, error)
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
	mountPath(p *Process, target, source string, conn File) error
	unmountPath(p *Process, path string, flags uint32) error
	// bindPath makes a socket node of path, which holds held;
	// connectPath is what the socket node path names holds.
	bindPath(p *Process, path string, mode uint32, held File) error
	connectPath(p *Process, path string) (File, error)
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
	File
	readWindow(p *Process, w *memfdFile, woff uint32, n int, from *int64) (int, error)
	writeWindow(p *Process, w *memfdFile, woff uint32, n int, to *int64) (int, error)
}

// A Link is a descriptor of what O_PATH opened that is no directory,
// which readlinkat reads by an empty path: a symbolic link's target, or
// ENOENT for what is no link, as Linux has it.
type Link interface {
	File
	readlink(p *Process) (string, error)
}

// An Attrs has the attributes a descriptor's calls set, and knows the file
// system it is on: an open file of a server (kernel_remote.go).
type Attrs interface {
	File
	truncate(p *Process, size int64) error
	sync(p *Process, data bool) error
	chmod(p *Process, mode uint32) error
	chown(p *Process, uid, gid int) error
	utimes(p *Process, ts []syscall.Timespec) error
	statfs(p *Process, st *syscall.Statfs_t) error
}

// A Socket takes the socket calls as libc makes them, with the guest's
// addresses and buffers; the addresses it returns are the guest's. A pair
// of the kernel's is a Transfer too; an open file of a server
// (kernel_remote.go) is a Socket for the program that speaks the protocol
// on it itself, and a Transfer only when a file.
type Socket interface {
	File
	bind(p *Process, at, n uint32) error
	connect(p *Process, at, n uint32) error
	listen(backlog int) error
	// accept returns the connection and the peer's address.
	accept(p *Process, flags uint32) (File, []byte, error)
	sendmsg(p *Process, at, flags uint32) (int, error)
	recvmsg(p *Process, at, flags uint32) (int, error)
	sendto(p *Process, buf, n, flags, addr, alen uint32) (int, error)
	recvfrom(p *Process, buf, n, flags uint32) (int, []byte, error)
	sockname(p *Process) ([]byte, error)
	peername(p *Process) ([]byte, error)
	getsockopt(p *Process, level, name, at, lenAt uint32) error
	setsockopt(p *Process, level, name, at, n uint32) error
	shutdown(how int) error
}

// An Ioctl takes ioctls with the host's argument layout, which the libc
// functions convert the guest's to; a request that takes an int, TCSBRK,
// TCXONC, TCFLSH, takes it pointed to.
type Ioctl interface {
	File
	ioctl(req uintptr, arg unsafe.Pointer) error
}

// A Locker takes flock's locks and the record locks of fcntl, which try
// without blocking: EAGAIN or EACCES is a lock held by another.
type Locker interface {
	File
	flock(p *Process, op int) error
	recordLock(p *Process, cmd int, lk *syscall.Flock_t) error
}

type Descriptor struct {
	obj     File
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
func (p *Process) fd(fd uint32) (File, error) {
	d, ok := p.files.at(int(fd))

	if !ok {
		return nil, syscall.EBADF
	}

	return d.obj, nil
}

// transferAt is the Transfer of a descriptor: read on a directory is
// EISDIR, as Linux has it, write and the rest EBADF.
func (p *Process) transferAt(fd uint32, read bool) (Transfer, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if t, ok := obj.(Transfer); ok {
		return t, nil
	}

	if _, ok := obj.(Dir); ok && read {
		return nil, syscall.EISDIR
	}

	return nil, syscall.EBADF
}

func (p *Process) seekerAt(fd uint32) (Seeker, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if s, ok := obj.(Seeker); ok {
		return s, nil
	}

	return nil, syscall.ESPIPE
}

func (p *Process) dirAt(fd uint32) (Dir, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if d, ok := obj.(Dir); ok {
		return d, nil
	}

	return nil, syscall.ENOTDIR
}

func (p *Process) attrsAt(fd uint32) (Attrs, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if a, ok := obj.(Attrs); ok {
		return a, nil
	}

	return nil, syscall.EINVAL
}

func (p *Process) socketAt(fd uint32) (Socket, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if s, ok := obj.(Socket); ok {
		return s, nil
	}

	return nil, syscall.ENOTSOCK
}

func (p *Process) ioctlAt(fd uint32) (Ioctl, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if i, ok := obj.(Ioctl); ok {
		return i, nil
	}

	return nil, syscall.ENOTTY
}

func (p *Process) lockerAt(fd uint32) (Locker, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if l, ok := obj.(Locker); ok {
		return l, nil
	}

	return nil, syscall.EBADF
}

// install puts an object at the lowest free descriptor from min; the
// descriptor takes the reference.
func (p *Process) install(obj File, min int, cloexec bool) (int, error) {
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
func (p *Process) placeAt(obj File, fd uint32, cloexec bool) {
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
	obj     File
	events  int16
	revents int16
}

// waitObjects waits until an object is ready for its events: it
// subscribes the process to them all, then takes its mail, asks them all
// and sleeps on the mail until the deadline of timeout milliseconds, as
// long as none is ready. A signal interrupts it: then the error is EINTR,
// after the handlers ran when all of them restart calls and restart is
// set. It returns how many are ready.
func (p *Process) waitObjects(entries []pollEntry, timeout int, restart bool) (int, error) {
	end := deadline(timeout)

	for i := range entries {
		entries[i].obj.subscribe(p, uint64(i), entries[i].events)
	}

	defer func() {
		for i := range entries {
			entries[i].obj.unsubscribe(p, uint64(i))
		}
	}()

	for {
		p.takeMail()

		if p.sig.deliverable() != 0 {
			if restart && p.interrupted() {
				continue
			}

			return 0, syscall.EINTR
		}

		if ready := pollEntries(entries); ready > 0 {
			return ready, nil
		}

		if p.waitMail(end) {
			return 0, nil
		}
	}
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
	list.add(p, 0)
	defer list.remove(p, 0)

	for {
		p.takeMail()

		if p.sig.deliverable() != 0 {
			if restart && p.interrupted() {
				continue
			}

			return syscall.EINTR
		}

		if ready() {
			return nil
		}

		p.waitMail(time.Time{})
	}
}

// waitObject waits until one object is ready for events.
func (p *Process) waitObject(obj File, events int16, timeout int, restart bool) (int, error) {
	return p.waitObjects([]pollEntry{{obj: obj, events: events}}, timeout, restart)
}

// waitMs is a timeout in milliseconds, rounded up and bounded.
func waitMs(d time.Duration) int {
	return int(min((d+time.Millisecond-1)/time.Millisecond, maxPollWaitMs))
}

const maxPollWaitMs = 1 << 30
