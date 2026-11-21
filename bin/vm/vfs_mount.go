//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsMount struct {
	root   uint64
	target string
	source string
	conn   Object
}

func (r *VfsMount) op() uint32 {
	return vMount
}

func (r *VfsMount) unserved() error {
	return ENOTDIR
}

func (r *VfsMount) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	var files []Object

	if r.conn != nil {
		files = []Object{r.conn}
	}

	return 0, append(append(append(binary.LittleEndian.AppendUint64(b, r.root), r.target...), 0), r.source...), files
}

func (r *VfsMount) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
