//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsLink struct {
	root   uint64
	old    string
	to     uint64
	new    string
	follow bool
}

func (r *VfsLink) op() uint32 {
	return vLink
}

func (r *VfsLink) unserved() error {
	return ENOTDIR
}

func (r *VfsLink) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return bit(r.follow), append(append(append(binary.LittleEndian.AppendUint64(binary.LittleEndian.AppendUint64(b, r.root), r.to), r.old...), 0), r.new...), nil
}

func (r *VfsLink) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
