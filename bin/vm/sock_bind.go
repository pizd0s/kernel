//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type SockBind struct {
	addr Address
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
	dropFiles(pk.files)

	return nil
}
