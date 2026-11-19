//go:build linux && amd64

package main

import (
	"encoding/binary"
	"maps"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// sockOptions are the options of the socket level a socket of the
// kernel's keeps: SO_RCVTIMEO's and SO_SNDTIMEO's nanoseconds, zero for
// none, SO_PASSCRED, and the options of soInts set, ints, with intsMu.
type sockOptions struct {
	rcvTimeout, sndTimeout atomic.Int64
	passcred               atomic.Bool
	intsMu                 sync.Mutex
	ints                   map[uint32]uint32
}

// copyOptions gives o the options of from, as a connection gets those of
// its socket.
func (o *sockOptions) copyOptions(from *sockOptions) {
	o.rcvTimeout.Store(from.rcvTimeout.Load())
	o.sndTimeout.Store(from.sndTimeout.Load())
	o.passcred.Store(from.passcred.Load())
	from.intsMu.Lock()
	ints := maps.Clone(from.ints)
	from.intsMu.Unlock()
	o.intsMu.Lock()
	o.ints = ints
	o.intsMu.Unlock()
}

// soInts are the options of the socket level that are ints a socket keeps
// as they were set, those true Linux's booleans, 0 or 1: SO_DEBUG,
// SO_REUSEADDR, SO_DONTROUTE, SO_BROADCAST, SO_KEEPALIVE, SO_OOBINLINE,
// SO_PRIORITY, SO_REUSEPORT and SO_RCVLOWAT, at least 1.
var soInts = map[uint32]bool{1: true, 2: true, 5: true, 6: true, 9: true, 10: true, 12: false, 15: true, soRcvlowat: false}

const soRcvlowat = 18

// intOption is the value of an option of soInts.
func (o *sockOptions) intOption(name uint32) uint32 {
	o.intsMu.Lock()
	defer o.intsMu.Unlock()

	if v, ok := o.ints[name]; ok {
		return v
	}

	return bit(name == soRcvlowat)
}

// get is getsockopt of a socket of a kind with the options o.
func (o *sockOptions) get(p *Process, kind int, level, name, at, lenAt uint32) error {
	if level != solSocket {
		return syscall.ENOPROTOOPT
	}

	var value uint32

	switch name {
	case soType:
		value = uint32(kind)
	case soError, soAcceptconn:
		value = 0
	case soPasscred:
		value = bit(o.passcred.Load())
	case soRcvtimeo, soSndtimeo:
		to := &o.rcvTimeout

		if name == soSndtimeo {
			to = &o.sndTimeout
		}

		var b [16]byte
		d := time.Duration(to.Load())
		binary.LittleEndian.PutUint64(b[:], uint64(d/time.Second))
		binary.LittleEndian.PutUint64(b[8:], uint64(d%time.Second/time.Microsecond))
		length := min(p.u32(lenAt), 16)
		copy(p.span(at, length), b[:])
		p.put32(lenAt, length)

		return nil
	case soSndbuf, soRcvbuf:
		value = queueMax * packetMax
	case 1, 2, 5, 6, 9, 10, 12, 15, soRcvlowat:
		value = o.intOption(name)
	case soPeercred:
		// The peer is in this kernel: the ids are the process's own.
		b := make([]byte, 12)
		binary.LittleEndian.PutUint32(b, uint32(p.pid))
		binary.LittleEndian.PutUint32(b[4:], p.cred.uid())
		binary.LittleEndian.PutUint32(b[8:], p.cred.gid())
		length := min(p.u32(lenAt), 12)
		copy(p.span(at, length), b)
		p.put32(lenAt, length)

		return nil
	default:
		return syscall.ENOPROTOOPT
	}

	length := min(p.u32(lenAt), 4)
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], value)
	copy(p.span(at, length), b[:])
	p.put32(lenAt, length)

	return nil
}

// set is setsockopt with the options o.
func (o *sockOptions) set(p *Process, level, name, at, n uint32) error {
	if level != solSocket {
		return syscall.ENOPROTOOPT
	}

	switch name {
	case soRcvtimeo, soSndtimeo:
		if n < 16 {
			return syscall.EINVAL
		}

		sec, usec := int64(p.u64(at)), int64(p.u64(at+8))

		if usec < 0 || usec >= 1e6 {
			return syscall.EDOM
		}

		d := time.Duration(0)

		if sec >= 0 {
			d = time.Duration(min(sec, 1<<33))*time.Second + time.Duration(usec)*time.Microsecond
		}

		if name == soRcvtimeo {
			o.rcvTimeout.Store(int64(d))
		} else {
			o.sndTimeout.Store(int64(d))
		}
	case soPasscred:
		if n < 4 {
			return syscall.EINVAL
		}

		o.passcred.Store(p.u32(at) != 0)
	case 1, 2, 5, 6, 9, 10, 12, 15, soRcvlowat:
		if n < 4 {
			return syscall.EINVAL
		}

		v := p.u32(at)

		switch {
		case soInts[name]:
			v = bit(v != 0)
		case name == soRcvlowat:
			v = uint32(max(int32(v), 1))
		}

		o.intsMu.Lock()

		if o.ints == nil {
			o.ints = map[uint32]uint32{}
		}

		o.ints[name] = v
		o.intsMu.Unlock()
	}

	return nil
}
