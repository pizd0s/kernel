//go:build linux && amd64

package main

import (
	"encoding/binary"
)

type FileChown struct {
	uid, gid int
}

func (r *FileChown) op() uint32 {
	return rFchown
}

func (r *FileChown) unserved() error {
	return EINVAL
}

func (r *FileChown) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(b, uint32(r.uid)), uint32(r.gid)), nil
}

func (r *FileChown) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
