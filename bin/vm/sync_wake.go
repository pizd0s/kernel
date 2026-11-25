package main

type SyncWake struct {
	all bool
}

func (r *SyncWake) op() uint32 {
	return 0
}

func (r *SyncWake) unserved() error {
	return EINVAL
}

func (r *SyncWake) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *SyncWake) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
