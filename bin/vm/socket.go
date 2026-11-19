//go:build linux && amd64

package main

import (
	"sync/atomic"
	"time"
)

// The socket calls: every socket is the kernel's, of AF_UNIX, a pair's end
// (pair_end.go) or a socket of a name of the VFS's (unix_socket.go);
// the system has no network. The address structures are Linux's.

const (
	afUnix         = 1
	afInet         = 2
	afInet6        = 10
	sockNonblock   = 0o4000
	sockCloexec    = 0o2000000
	msgDontwait    = 0x40
	msgNosignal    = 0x4000
	msgCtrunc      = 0x8
	msgCmsgCloexec = 0x40000000
	solSocket      = 1
	soAcceptconn   = 30
	scmRights      = 1
	sunPathMax     = 108
	sockaddrMax    = 128
)

func unixAddress(path []byte) []byte {
	if len(path) == 0 {
		return []byte{afUnix, 0}
	}

	return append(append([]byte{afUnix, 0}, path...), 0)
}

// Messages. A guest's msghdr is seven words; its control messages are
// headed by three words and aligned to four bytes, the host's by a long
// and two words and aligned to eight.
const (
	guestCmsgHeader = 12
	hostCmsgHeader  = 16
)

func guestAlign(n uint32) uint32 {
	return (n + 3) &^ 3
}

// The bits of a socket's type that are its kind; SOCK_RAW.
const (
	sockTypeMask = 0xf
	sockRaw      = 3
	// SOCK_CALL, a call pair's (call_end.go): lib/c's own, of
	// sys/socket.h.
	sockCall = 7
)

const (
	sockStream     = 1
	sockDgram      = 2
	sockSeqpacket  = 5
	packetMax      = 65536
	queueMax       = 64
	msgTrunc       = 0x20
	msgPeek        = 0x2
	msgWaitall     = 0x100
	soType         = 3
	soError        = 4
	soSndbuf       = 7
	soRcvbuf       = 8
	soPasscred     = 16
	soPeercred     = 17
	soRcvtimeo     = 20
	soSndtimeo     = 21
	scmCredentials = 2
	ucredSize      = 12
	shutRd         = 0
	shutWr         = 1
	shutRdwr       = 2
)

var pairInodes atomic.Uint64

// ioSides tells whether events ask for input, for output; both when they
// ask for neither, for a wait that is for the hangup.
func ioSides(events int16) (in, out bool) {
	in, out = events&(pollIn|pollRdnorm|pollPri) != 0, events&(pollOut|pollWrnorm) != 0

	if !in && !out {
		in, out = true, true
	}

	return in, out
}

// until is when a wait with the timeout of to ends, SO_RCVTIMEO's or
// SO_SNDTIMEO's, zero for none, and how a signal ends it: after the
// handlers, which restart the call only without a timeout.
func until(to *atomic.Int64) (time.Time, signalled) {
	if ns := to.Load(); ns > 0 {
		return deadline(waitMs(time.Duration(ns))), interrupts
	}

	return time.Time{}, restarts
}
