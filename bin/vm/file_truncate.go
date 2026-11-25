package main

import (
	"encoding/binary"
)

type FileTruncate struct {
	size int64
}

func (r *FileTruncate) op() uint32 {
	return rFtruncate
}

func (r *FileTruncate) unserved() error {
	return EINVAL
}

func (r *FileTruncate) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, binary.LittleEndian.AppendUint64(b, uint64(r.size)), nil
}

func (r *FileTruncate) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
