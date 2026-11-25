package main

type SyncValue struct {
	value uint32
}

func (r *SyncValue) op() uint32 {
	return 0
}

func (r *SyncValue) unserved() error {
	return EINVAL
}

func (r *SyncValue) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *SyncValue) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
