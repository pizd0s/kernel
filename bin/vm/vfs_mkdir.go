//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsMkdir struct {
	root uint64
	path string
	mode uint32
}

func (r *VfsMkdir) op() uint32 {
	return vMkdir
}

func (r *VfsMkdir) unserved() error {
	return ENOTDIR
}

func (r *VfsMkdir) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.mode, append(binary.LittleEndian.AppendUint64(b, r.root), r.path...), nil
}

func (r *VfsMkdir) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
