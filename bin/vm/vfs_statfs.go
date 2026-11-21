//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsStatfs struct {
	root uint64
	path string
	st   Statfs
}

func (r *VfsStatfs) op() uint32 {
	return vStatfs
}

func (r *VfsStatfs) unserved() error {
	return ENOTDIR
}

func (r *VfsStatfs) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(binary.LittleEndian.AppendUint64(b, r.root), r.path...), nil
}

func (r *VfsStatfs) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return decodeStatfs(pk.data, &r.st)
}
