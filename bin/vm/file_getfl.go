package main

import (
	"encoding/binary"
)

type FileGetfl struct {
	flags int
}

func (r *FileGetfl) op() uint32 {
	return rGetfl
}

func (r *FileGetfl) unserved() error {
	return EBADF
}

func (r *FileGetfl) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *FileGetfl) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	if len(pk.data) < 4 {
		return EIO
	}

	r.flags = int(binary.LittleEndian.Uint32(pk.data))

	return nil
}
