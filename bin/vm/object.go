//go:build linux && amd64

package main

import (
	"sync/atomic"
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

	poll(events int16, through []*Epoll) int16
	subscribe(s *Subscription, events int16)
	unsubscribe(s *Subscription)

	flags(p *Process) (int, error)
	setFlags(p *Process, fl int) error

	read(p *Process, into Object, n int, mode IoMode) (int, error)
	write(p *Process, from Object, n int, mode IoMode) (int, error)
	pread(p *Process, into Object, n int, off int64) (int, error)
	pwrite(p *Process, from Object, n int, off int64) (int, error)
	seek(p *Process, off int64, whence int) (int64, error)
	readdir(p *Process, into Object, n int) (int, error)
	with(at int64, n int, writes bool, fn func(b []byte) (int, error)) (int, error)
	copyTo(p *Process, out Object, from, to int64, n int) (int, error)
	copyFrom(p *Process, in Object, from, to int64, n int) (int, error)

	call(p *Process, r Request, group uint32, waits bool) (bool, error)

	ioctl(req uintptr, arg unsafe.Pointer) error

	epollCtl(op uint32, key EpollKey, events uint32, data uint64) error
	epollWait(p *Process, buf func() []byte, max int, end time.Time) (int, error)
}

type IoMode struct {
	nonblock bool
	quiet    bool
	kernel   bool
	timeout  *atomic.Int64
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
