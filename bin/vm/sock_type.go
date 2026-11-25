package main

import (
	"encoding/binary"
)

type SockType struct {
	kind int
}

func (r *SockType) op() uint32 {
	return sType
}

func (r *SockType) unserved() error {
	return ENOTSOCK
}

func (r *SockType) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *SockType) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	if len(pk.data) != 4 {
		return EIO
	}

	r.kind = int(binary.LittleEndian.Uint32(pk.data))

	return nil
}
