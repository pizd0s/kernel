package main

import (
	"encoding/binary"
)

type VfsReadlink struct {
	root   uint64
	path   string
	target string
}

func (r *VfsReadlink) op() uint32 {
	return vReadlink
}

func (r *VfsReadlink) unserved() error {
	return ENOTDIR
}

func (r *VfsReadlink) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(binary.LittleEndian.AppendUint64(b, r.root), r.path...), nil
}

func (r *VfsReadlink) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	r.target = string(pk.data)

	return nil
}
