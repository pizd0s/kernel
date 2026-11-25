package main

import (
	"encoding/binary"
)

type SockCreate struct {
	domain   uint32
	kind     uint32
	protocol uint32
}

func (r *SockCreate) op() uint32 {
	return sCreate
}

func (r *SockCreate) unserved() error {
	return EAFNOSUPPORT
}

func (r *SockCreate) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return r.kind, binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(b, r.domain), r.protocol), nil
}

func (r *SockCreate) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
