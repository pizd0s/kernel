//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsUnlink struct {
	root uint64
	path string
	dir  bool
}

func (r *VfsUnlink) op() uint32 {
	return vUnlink
}

func (r *VfsUnlink) unserved() error {
	return ENOTDIR
}

func (r *VfsUnlink) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return bit(r.dir) * AT_REMOVEDIR, append(binary.LittleEndian.AppendUint64(b, r.root), r.path...), nil
}

func (r *VfsUnlink) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
