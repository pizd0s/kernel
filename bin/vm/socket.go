//go:build linux && amd64

package main

import (
	"sync/atomic"
	"time"
)

var pairInodes atomic.Uint64

const (
	afUnix          = 1
	afInet          = 2
	afInet6         = 10
	sockNonblock    = 0o4000
	sockCloexec     = 0o2000000
	msgDontwait     = 0x40
	msgNosignal     = 0x4000
	msgCtrunc       = 0x8
	msgCmsgCloexec  = 0x40000000
	solSocket       = 1
	soAcceptconn    = 30
	scmRights       = 1
	sunPathMax      = 108
	sockaddrMax     = 128
	guestCmsgHeader = 12
	hostCmsgHeader  = 16
	sockTypeMask    = 0xf
	sockRaw         = 3
	sockCall        = 7
	sockStream      = 1
	sockDgram       = 2
	sockSeqpacket   = 5
	packetMax       = 65536
	queueMax        = 64
	msgTrunc        = 0x20
	msgPeek         = 0x2
	msgWaitall      = 0x100
	soType          = 3
	soError         = 4
	soSndbuf        = 7
	soRcvbuf        = 8
	soPasscred      = 16
	soPeercred      = 17
	soRcvtimeo      = 20
	soSndtimeo      = 21
	scmCredentials  = 2
	ucredSize       = 12
	shutRd          = 0
	shutWr          = 1
	shutRdwr        = 2
)

func unixAddress(path []byte) []byte {
	if len(path) == 0 {
		return []byte{afUnix, 0}
	}

	return append(append([]byte{afUnix, 0}, path...), 0)
}

func guestAlign(n uint32) uint32 {
	return (n + 3) &^ 3
}

func ioSides(events int16) (in, out bool) {
	in, out = events&(pollIn|pollRdnorm|pollPri) != 0, events&(pollOut|pollWrnorm) != 0

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
