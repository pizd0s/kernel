package main

type TtyFlush struct {
	queue uint32
}

func (r *TtyFlush) op() uint32 {
	return rTcflush
}

func (r *TtyFlush) unserved() error {
	return ENOTTY
}

func (r *TtyFlush) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.queue, b, nil
}

func (r *TtyFlush) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
