package main

import (
	"sync/atomic"
	"time"
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

	call(p *Process, r Request, group uint32, waits bool) (bool, error)
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
