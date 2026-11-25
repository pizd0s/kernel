package main

import (
	"encoding/binary"
)

type VfsMknod struct {
	root uint64
	path string
	mode uint32
}

func (r *VfsMknod) op() uint32 {
	return vMknod
}

func (r *VfsMknod) unserved() error {
	return ENOTDIR
}

func (r *VfsMknod) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.mode, append(binary.LittleEndian.AppendUint64(b, r.root), r.path...), nil
}

func (r *VfsMknod) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
