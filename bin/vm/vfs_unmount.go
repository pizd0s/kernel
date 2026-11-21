//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsUnmount struct {
	root  uint64
	path  string
	flags uint32
}

func (r *VfsUnmount) op() uint32 {
	return vUnmount
}

func (r *VfsUnmount) unserved() error {
	return ENOTDIR
}

func (r *VfsUnmount) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.flags, append(binary.LittleEndian.AppendUint64(b, r.root), r.path...), nil
}

func (r *VfsUnmount) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
