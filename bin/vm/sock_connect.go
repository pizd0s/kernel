//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type SockConnect struct {
	addr  Address
	moved Object
}

func (r *SockConnect) op() uint32 {
	return sConnect
}

func (r *SockConnect) unserved() error {
	return ENOTSOCK
}

func (r *SockConnect) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(binary.LittleEndian.AppendUint32(b, uint32(len(r.addr.raw))), r.addr.raw...), nil
}

func (r *SockConnect) des(p *Process, pk *Packet) error {
	if len(pk.files) == 0 {
		return nil
	}

	moved, rest, err := p.attached(pk)

	if err != nil {
		return err
	}

	r.moved = moved

	if len(rest) < 4 {
		return EIO
	}

	if e := Errno(binary.LittleEndian.Uint32(rest)); e != 0 {
		return e
	}

	return nil
}
