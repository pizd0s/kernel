//go:build linux && amd64

package main

type MqSend struct {
	from, n, prio, at uint32
	timed             bool
}

func (r *MqSend) op() uint32 {
	return 0
}

func (r *MqSend) unserved() error {
	return EBADF
}

func (r *MqSend) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *MqSend) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
