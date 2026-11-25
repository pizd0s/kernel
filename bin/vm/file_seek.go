package main

import (
	"encoding/binary"
)

type FileSeek struct {
	off    int64
	whence int
	pos    int64
}

func (r *FileSeek) op() uint32 {
	return rSeek
}

func (r *FileSeek) unserved() error {
	return ESPIPE
}

func (r *FileSeek) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return uint32(r.whence), binary.LittleEndian.AppendUint64(b, uint64(r.off)), nil
}

func (r *FileSeek) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	if len(pk.data) < 8 {
		return EIO
	}

	r.pos = int64(binary.LittleEndian.Uint64(pk.data))

	return nil
}
