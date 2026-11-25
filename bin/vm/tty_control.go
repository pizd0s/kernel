package main

type TtyControl struct {
	how uint32
}

func (r *TtyControl) op() uint32 {
	return rSetCtty
}

func (r *TtyControl) unserved() error {
	return ENOTTY
}

func (r *TtyControl) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.how, b, nil
}

func (r *TtyControl) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
