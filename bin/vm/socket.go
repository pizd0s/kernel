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
	rightsMax    = 253
)

func ioSides(events int16) (in, out bool) {
	in, out = events&(POLLIN|POLLRDNORM|POLLPRI) != 0, events&(POLLOUT|POLLWRNORM) != 0

	if !in && !out {
		in, out = true, true
	}

	return in, out
}

func until(to *atomic.Int64) (time.Time, Signalled) {
	if ns := to.Load(); ns > 0 {
		return deadline(waitMs(time.Duration(ns))), restarts
	}

	return time.Time{}, restarts
}
