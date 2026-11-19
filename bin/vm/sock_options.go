//go:build linux && amd64

package main

import (
	"encoding/binary"
	"maps"
	"sync"
	"sync/atomic"
	"time"
)

var soInts = map[uint32]bool{1: true, 2: true, 5: true, 6: true, 9: true, 10: true, 12: false, 15: true, soRcvlowat: false}

type SockOptions struct {
	rcvTimeout, sndTimeout atomic.Int64
	passcred               atomic.Bool
	intsMu                 sync.Mutex
	ints                   map[uint32]uint32
}

func (o *SockOptions) copyOptions(from *SockOptions) {
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

func (o *SockOptions) intOption(name uint32) uint32 {
	o.intsMu.Lock()

	defer o.intsMu.Unlock()

	if v, ok := o.ints[name]; ok {
		return v
	}

	return bit(name == soRcvlowat)
}

func (o *SockOptions) get(p *Process, kind int, level, name, at, lenAt uint32) error {
	if level != solSocket {
		return eNoprotoopt
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

		b := make([]byte, 12)

		binary.LittleEndian.PutUint32(b, uint32(p.pid))
		binary.LittleEndian.PutUint32(b[4:], p.cred.uid())
		binary.LittleEndian.PutUint32(b[8:], p.cred.gid())

		length := min(p.u32(lenAt), 12)

		copy(p.span(at, length), b)
		p.put32(lenAt, length)

		return nil
	default:
		return eNoprotoopt
	}

	length := min(p.u32(lenAt), 4)

	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], value)
	copy(p.span(at, length), b[:])
	p.put32(lenAt, length)

	return nil
}

func (o *SockOptions) set(p *Process, level, name, at, n uint32) error {
	if level != solSocket {
		return eNoprotoopt
	}

	switch name {
	case soRcvtimeo, soSndtimeo:
		if n < 16 {
			return eInval
		}

		sec, usec := int64(p.u64(at)), int64(p.u64(at+8))

		if usec < 0 || usec >= 1e6 {
			return eDom
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
			return eInval
		}

		o.passcred.Store(p.u32(at) != 0)
	case 1, 2, 5, 6, 9, 10, 12, 15, soRcvlowat:
		if n < 4 {
			return eInval
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
