//go:build linux && amd64

package main

type TtyReopen struct {
	flags int
	file  Object
}

func (r *TtyReopen) op() uint32 {
	return rOpen
}

func (r *TtyReopen) unserved() error {
	return ENOTTY
}

func (r *TtyReopen) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return uint32(r.flags), b, nil
}

func (r *TtyReopen) des(p *Process, pk *Packet) error {
	if len(pk.files) != 1 {
		dropFiles(pk.files)

		return EIO
	}

	var err error

	r.file, _, err = p.opened(pk)

	return err
}
