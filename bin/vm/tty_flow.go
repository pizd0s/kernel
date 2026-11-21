//go:build linux && amd64

package main

type TtyFlow struct {
	action uint32
}

func (r *TtyFlow) op() uint32 {
	return rTcflow
}

func (r *TtyFlow) unserved() error {
	return ENOTTY
}

func (r *TtyFlow) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.action, b, nil
}

func (r *TtyFlow) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
