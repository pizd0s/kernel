package main

import (
	"encoding/binary"
)

type VfsOpen struct {
	root     uint64
	path     string
	flags    int
	mode     uint32
	file     Object
	terminal bool
	fifo     bool
	st       Stat
}

func (r *VfsOpen) op() uint32 {
	return vOpen
}

func (r *VfsOpen) unserved() error {
	return ENOTDIR
}

func (r *VfsOpen) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return uint32(r.flags), append(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint64(b, r.root), r.mode), r.path...), nil
}

func (r *VfsOpen) des(p *Process, pk *Packet) error {
	if len(pk.files) == 0 {
		if err := decodeStat(pk.data, &r.st); err != nil || r.st.Mode&S_IFMT != S_IFIFO {
			return EIO
		}

		r.fifo = true

		return nil
	}

	dropFiles(pk.files[1:])
	pk.files = pk.files[:1]

	var err error

	r.file, r.terminal, err = p.opened(pk)

	return err
}
