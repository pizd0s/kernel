package main

import (
	"encoding/binary"
)

type VfsTruncate struct {
	root uint64
	path string
	size int64
}

func (r *VfsTruncate) op() uint32 {
	return vTruncate
}

func (r *VfsTruncate) unserved() error {
	return ENOTDIR
}

func (r *VfsTruncate) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(binary.LittleEndian.AppendUint64(binary.LittleEndian.AppendUint64(b, r.root), uint64(r.size)), r.path...), nil
}

func (r *VfsTruncate) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
