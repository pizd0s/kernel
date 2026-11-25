package main

import "encoding/binary"

const watchBodyMax = 64

type DirWatch struct {
	flags uint32
	body  []byte
	file  Object
	value uint32
}

func (r *DirWatch) op() uint32 {
	return rWatch
}

func (r *DirWatch) unserved() error {
	return ENOTDIR
}

func (r *DirWatch) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.flags, append(b, r.body...), nil
}

func (r *DirWatch) des(p *Process, pk *Packet) error {
	if len(pk.files) > 0 {
		file, _, err := p.opened(pk)

		r.file = file

		return err
	}

	switch len(pk.data) {
	case 0:
		return nil
	case 4:
		r.value = binary.LittleEndian.Uint32(pk.data)

		return nil
	}

	return EIO
}
