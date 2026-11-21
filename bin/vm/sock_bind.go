//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type SockBind struct {
	addr  Address
	moved Object
}

func (r *SockBind) op() uint32 {
	return sBind
}

func (r *SockBind) unserved() error {
	return ENOTSOCK
}

func (r *SockBind) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(binary.LittleEndian.AppendUint32(b, uint32(len(r.addr.raw))), r.addr.raw...), nil
}

func (r *SockBind) des(p *Process, pk *Packet) error {
	if len(pk.files) == 0 {
		return nil
	}

	moved, _, err := p.attached(pk)

	r.moved = moved

	return err
}
