//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsUtimes struct {
	root   uint64
	path   string
	ts     []Timespec
	follow bool
}

func (r *VfsUtimes) op() uint32 {
	return vUtimes
}

func (r *VfsUtimes) unserved() error {
	return ENOTDIR
}

func (r *VfsUtimes) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	when, times := whens(r.ts)

	return when, append(append(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint64(b, r.root), flagOf(r.follow)), times...), r.path...), nil
}

func (r *VfsUtimes) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
