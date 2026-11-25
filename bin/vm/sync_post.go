package main

type SyncPost struct {
}

func (r *SyncPost) op() uint32 {
	return 0
}

func (r *SyncPost) unserved() error {
	return EINVAL
}

func (r *SyncPost) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, b, nil
}

func (r *SyncPost) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
