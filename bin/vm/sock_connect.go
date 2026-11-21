//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type SockConnect struct {
	addr Address
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
	dropFiles(pk.files)

	return nil
}
