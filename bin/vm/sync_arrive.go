package main

type SyncArrive struct {
	serial bool
}

func (r *SyncArrive) op() uint32 {
	return 0
}

func (r *SyncArrive) unserved() error {
	return EINVAL
}

func (r *SyncArrive) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, b, nil
}

func (r *SyncArrive) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
