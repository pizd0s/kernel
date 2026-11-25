package main

type MqReceive struct {
	room    int
	at      uint32
	timed   bool
	message QueuedMessage
}

func (r *MqReceive) op() uint32 {
	return 0
}

func (r *MqReceive) unserved() error {
	return EBADF
}

func (r *MqReceive) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *MqReceive) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
