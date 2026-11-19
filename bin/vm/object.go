//go:build linux && amd64

package main

import (
	"time"
	"unsafe"
)

const (
	maxPollWaitMs = 1 << 30
	fOfdGetlk     = 36
	fOfdSetlk     = 37
	fOfdSetlkw    = 38
)

type Object interface {
	ref()
	unref()

	poll(events int16) int16
	subscribe(s *Subscription, events int16)
	unsubscribe(s *Subscription)

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
	asEpoll() *Epoll
	asMemfd() *MemfdFile
	asPair() *PairEnd
	asListener() *UnixListener
	asConsole() *ConsoleEnd
	asVFS() *RemoteDir
}

type File interface {
	Object

	stat(p *Process, st *Stat) error

	flags() (int, error)
	setFlags(fl int) error
}

type IoMode struct {
	nonblock bool
	quiet    bool
}

type Transfer interface {
	Object
	read(p *Process, b []byte, mode IoMode) (int, error)
	write(p *Process, b []byte, mode IoMode) (int, error)
	pread(p *Process, b []byte, off int64) (int, error)
	pwrite(p *Process, b []byte, off int64) (int, error)
}

type Seeker interface {
	Object
	seek(p *Process, off int64, whence int) (int64, error)
}

type Dir interface {
	Object

	path(p *Process) (string, error)

	readdir(p *Process, b []byte) (int, error)
	openPath(p *Process, path string, flags int, mode uint32) (Object, error)
	statPath(p *Process, path string, follow bool, st *Stat) error
	readlinkPath(p *Process, path string) (string, error)
	mkdirPath(p *Process, path string, mode uint32) error

	mknodPath(p *Process, path string, mode uint32) error
	symlinkPath(p *Process, target, path string) error
	unlinkPath(p *Process, path string, dir bool) error

	renamePath(p *Process, old string, to Dir, new string) error
	linkPath(p *Process, old string, to Dir, new string, follow bool) error
	chmodPath(p *Process, path string, mode uint32, follow bool) error
	chownPath(p *Process, path string, uid, gid int, follow bool) error
	utimesPath(p *Process, path string, ts []Timespec, follow bool) error
	truncatePath(p *Process, path string, size int64) error
	statfsPath(p *Process, path string, st *Statfs) error

	realpathOf(p *Process, path string) (string, error)

	mountPath(p *Process, target, source string, conn Object) error
	unmountPath(p *Process, path string, flags uint32) error

	bindPath(p *Process, path string, mode uint32, held Object) error
	connectPath(p *Process, path string) (Object, error)
}

type Terminal interface {
	Transfer

	device() FileKey

	jobs() (foreground int32, tostop, hungUp bool)
	getattr(p *Process) ([]byte, error)

	setattr(p *Process, when uint32, termios []byte) error
	flush(p *Process, queue uint32) error
	flow(p *Process, action uint32) error
	drain(p *Process) error
	window(p *Process) ([]byte, error)
	setWindow(p *Process, size []byte) error

	pending(p *Process) (uint32, error)
	unsent(p *Process) (uint32, error)

	name(p *Process, peer bool) (string, error)
	foreground(p *Process) (uint32, error)
	setForeground(p *Process, pgrp uint32) error

	control(p *Process, sid, pgrp int32) error

	reopen(p *Process, flags int) (Terminal, error)
}

type Windowed interface {
	Object
	readWindow(p *Process, w *MemfdFile, woff uint32, n int, from *int64) (int, error)
	writeWindow(p *Process, w *MemfdFile, woff uint32, n int, to *int64) (int, error)
}

type Link interface {
	Object
	readlink(p *Process) (string, error)
}

type Attrs interface {
	Object
	truncate(p *Process, size int64) error
	sync(p *Process, data bool) error
	chmod(p *Process, mode uint32) error
	chown(p *Process, uid, gid int) error
	utimes(p *Process, ts []Timespec) error
	statfs(p *Process, st *Statfs) error
}

type Socket interface {
	Object
	bind(p *Process, at, n uint32) error
	connect(p *Process, at, n uint32) error
	listen(backlog int) error

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

	socketKind() int
}

type Ioctl interface {
	Object
	ioctl(req uintptr, arg unsafe.Pointer) error
}

type Locker interface {
	Object
	flock(p *Process, op int) error
	recordLock(p *Process, cmd int, lk *Flock) error
}

func closed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func deadline(timeout int) time.Time {
	if timeout < 0 {
		return time.Time{}
	}

	return time.Now().Add(time.Duration(timeout) * time.Millisecond)
}

func waitMs(d time.Duration) int {
	return int(min((d+time.Millisecond-1)/time.Millisecond, maxPollWaitMs))
}

func bit(b bool) uint32 {
	if b {
		return 1
	}

	return 0
}
