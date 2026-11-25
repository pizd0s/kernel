package main

import (
	"encoding/binary"
)

type VfsSymlink struct {
	root   uint64
	target string
	path   string
}

func (r *VfsSymlink) op() uint32 {
	return vSymlink
}

func (r *VfsSymlink) unserved() error {
	return ENOTDIR
}

func (r *VfsSymlink) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(append(append(binary.LittleEndian.AppendUint64(b, r.root), r.target...), 0), r.path...), nil
}

func (r *VfsSymlink) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
