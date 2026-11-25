package main

import (
	"encoding/binary"
)

type VfsRename struct {
	root uint64
	old  string
	to   uint64
	new  string
}

func (r *VfsRename) op() uint32 {
	return vRename
}

func (r *VfsRename) unserved() error {
	return ENOTDIR
}

func (r *VfsRename) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(append(append(binary.LittleEndian.AppendUint64(binary.LittleEndian.AppendUint64(b, r.root), r.to), r.old...), 0), r.new...), nil
}

func (r *VfsRename) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
