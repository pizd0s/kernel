package main

type FileSync struct {
	data bool
}

func (r *FileSync) op() uint32 {
	return rFsync
}

func (r *FileSync) unserved() error {
	return EINVAL
}

func (r *FileSync) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return bit(r.data), b, nil
}

func (r *FileSync) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
