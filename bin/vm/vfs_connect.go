//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsConnect struct {
	root uint64
	path string
	file Object
}

func (r *VfsConnect) op() uint32 {
	return vConnect
}

func (r *VfsConnect) unserved() error {
	return ENOTDIR
}

func (r *VfsConnect) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(binary.LittleEndian.AppendUint64(b, r.root), r.path...), nil
}

func (r *VfsConnect) des(p *Process, pk *Packet) error {
	if len(pk.files) == 0 {
		return ECONNREFUSED
	}

	dropFiles(pk.files[1:])

	r.file = pk.files[0]

	return nil
}
