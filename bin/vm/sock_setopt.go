package main

import (
	"encoding/binary"
)

type SockSetopt struct {
	level, name uint32
	value       []byte
}

func (r *SockSetopt) op() uint32 {
	return sSetopt
}

func (r *SockSetopt) unserved() error {
	return ENOTSOCK
}

func (r *SockSetopt) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(b, r.level), r.name), r.value...), nil
}

func (r *SockSetopt) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
