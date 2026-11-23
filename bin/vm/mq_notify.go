//go:build linux && amd64

package main

type MqNotify struct {
	set    bool
	kind   uint32
	signal int
	value  uint32
}

func (r *MqNotify) op() uint32 {
	return 0
}

func (r *MqNotify) unserved() error {
	return EBADF
}

func (r *MqNotify) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *MqNotify) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
