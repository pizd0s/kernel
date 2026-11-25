package main

type TtyBreak struct {
}

func (r *TtyBreak) op() uint32 {
	return rTcsbrk
}

func (r *TtyBreak) unserved() error {
	return ENOTTY
}

func (r *TtyBreak) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *TtyBreak) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
