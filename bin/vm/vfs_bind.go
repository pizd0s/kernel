//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type VfsBind struct {
	root uint64
	path string
	mode uint32
	held Object
}

func (r *VfsBind) op() uint32 {
	return vBind
}

func (r *VfsBind) unserved() error {
	return ENOTDIR
}

func (r *VfsBind) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.mode, append(binary.LittleEndian.AppendUint64(b, r.root), r.path...), []Object{r.held}
}

func (r *VfsBind) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
