//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsRealpath struct {
	root uint64
	path string
	out  string
}

func (r *VfsRealpath) op() uint32 {
	return vRealpath
}

func (r *VfsRealpath) unserved() error {
	return ENOTDIR
}

func (r *VfsRealpath) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(binary.LittleEndian.AppendUint64(b, r.root), r.path...), nil
}

func (r *VfsRealpath) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	r.out = string(pk.data)

	return nil
}
