package main

type SockConsume struct {
	sink func(pk *Packet)
}

func (r *SockConsume) op() uint32 {
	return 0
}

func (r *SockConsume) unserved() error {
	return nil
}

func (r *SockConsume) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, b, nil
}

func (r *SockConsume) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
