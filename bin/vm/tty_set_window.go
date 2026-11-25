package main

type TtySetWindow struct {
	size []byte
}

func (r *TtySetWindow) op() uint32 {
	return rSetWinsize
}

func (r *TtySetWindow) unserved() error {
	return ENOTTY
}

func (r *TtySetWindow) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(b, r.size...), nil
}

func (r *TtySetWindow) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
