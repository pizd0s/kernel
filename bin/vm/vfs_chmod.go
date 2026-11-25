package main

import (
	"encoding/binary"
)

type VfsChmod struct {
	root   uint64
	path   string
	mode   uint32
	follow bool
}

func (r *VfsChmod) op() uint32 {
	return vChmod
}

func (r *VfsChmod) unserved() error {
	return ENOTDIR
}

func (r *VfsChmod) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.mode, append(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint64(b, r.root), flagOf(r.follow)), r.path...), nil
}

func (r *VfsChmod) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
