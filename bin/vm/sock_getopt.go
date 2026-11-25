package main

import (
	"bytes"
	"encoding/binary"
)

type SockGetopt struct {
	level, name uint32
	value       []byte
}

func (r *SockGetopt) op() uint32 {
	return sGetopt
}

func (r *SockGetopt) unserved() error {
	return ENOTSOCK
}

func (r *SockGetopt) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(b, r.level), r.name), nil
}

func (r *SockGetopt) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	r.value = bytes.Clone(pk.data)

	return nil
}
