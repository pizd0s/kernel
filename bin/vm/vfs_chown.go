//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsChown struct {
	root     uint64
	path     string
	uid, gid int
	follow   bool
}

func (r *VfsChown) op() uint32 {
	return vChown
}

func (r *VfsChown) unserved() error {
	return ENOTDIR
}

func (r *VfsChown) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint64(b, r.root), flagOf(r.follow)), uint32(r.uid)), uint32(r.gid)), r.path...), nil
}

func (r *VfsChown) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
