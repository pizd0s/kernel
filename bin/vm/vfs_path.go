//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsPath struct {
	root uint64
	out  string
}

func (r *VfsPath) op() uint32 {
	return vPath
}

func (r *VfsPath) unserved() error {
	return ENOTDIR
}

func (r *VfsPath) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, binary.LittleEndian.AppendUint64(b, r.root), nil
}

func (r *VfsPath) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	r.out = string(pk.data)

	return nil
}
