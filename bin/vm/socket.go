package main

import (
	"sync/atomic"
	"time"
)

var pairInodes atomic.Uint64

const (
	sockTypeMask = 0xf
	packetMax    = 65536
	queueMax     = 64
)

type Address struct {
	given bool
	raw   []byte
	path  string
	err   error
	node  *Description
}

func unixAddress(path []byte) []byte {
	if len(path) == 0 {
		return []byte{AF_UNIX, 0}
	}

	return append(append([]byte{AF_UNIX, 0}, path...), 0)
}

func guestAlign(n uint32) uint32 {
	return (n + 3) &^ 3
}

func ioSides(events int16) (in, out bool) {
	in, out = events&(POLLIN|POLLRDNORM|POLLPRI) != 0, events&(POLLOUT|POLLWRNORM) != 0

	if !in && !out {
		in, out = true, true
	}

	return in, out
}

func until(to *atomic.Int64) (time.Time, Signalled) {
	if ns := to.Load(); ns > 0 {
		return deadline(waitMs(time.Duration(ns))), interrupts
	}

	return time.Time{}, restarts
}
