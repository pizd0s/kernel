//go:build linux && amd64

package main

type TtyName struct {
	peer bool
	name string
}

func (r *TtyName) op() uint32 {
	return rName
}

func (r *TtyName) unserved() error {
	return ENOTTY
}

func (r *TtyName) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return bit(r.peer), b, nil
}

func (r *TtyName) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	r.name = string(pk.data)

	return nil
}
