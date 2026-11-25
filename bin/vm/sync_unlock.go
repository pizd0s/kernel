package main

type SyncUnlock struct {
	thread int32
}

func (r *SyncUnlock) op() uint32 {
	return 0
}

func (r *SyncUnlock) unserved() error {
	return EINVAL
}

func (r *SyncUnlock) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, b, nil
}

func (r *SyncUnlock) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
