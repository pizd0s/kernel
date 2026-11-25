package main

type TtyDrain struct {
}

func (r *TtyDrain) op() uint32 {
	return rTcdrain
}

func (r *TtyDrain) unserved() error {
	return ENOTTY
}

func (r *TtyDrain) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *TtyDrain) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
