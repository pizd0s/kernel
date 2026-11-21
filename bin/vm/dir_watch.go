//go:build linux && amd64

package main

type DirWatch struct {
	flags uint32
	file  Object
}

func (r *DirWatch) op() uint32 {
	return rWatch
}

func (r *DirWatch) unserved() error {
	return ENOTDIR
}

func (r *DirWatch) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.flags, b, nil
}

func (r *DirWatch) des(p *Process, pk *Packet) error {
	if len(pk.files) == 0 {
		return EIO
	}

	file, _, err := p.opened(pk)

	r.file = file

	return err
}
