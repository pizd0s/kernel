package main

import (
	"encoding/binary"
)

type TtyQueue struct {
	out bool
	n   uint32
}

func (r *TtyQueue) op() uint32 {
	if r.out {
		return rOutq
	}

	return rInq
}

func (r *TtyQueue) unserved() error {
	return ENOTTY
}

func (r *TtyQueue) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *TtyQueue) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	if len(pk.data) != 4 {
		return EIO
	}

	r.n = binary.LittleEndian.Uint32(pk.data)

	return nil
}
