package main

type TtySetattr struct {
	when    uint32
	termios []byte
}

func (r *TtySetattr) op() uint32 {
	return rTcsetattr
}

func (r *TtySetattr) unserved() error {
	return ENOTTY
}

func (r *TtySetattr) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.when, append(b, r.termios...), nil
}

func (r *TtySetattr) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
