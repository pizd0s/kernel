package main

import (
	"encoding/binary"
)

type TtyForeground struct {
	group, sid uint32
}

func (r *TtyForeground) op() uint32 {
	return rGetPgrp
}

func (r *TtyForeground) unserved() error {
	return ENOTTY
}

func (r *TtyForeground) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *TtyForeground) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	if len(pk.data) != 8 {
		return EIO
	}

	r.group, r.sid = binary.LittleEndian.Uint32(pk.data), binary.LittleEndian.Uint32(pk.data[4:])

	return nil
}
