package main

type FileStatfs struct {
	st *Statfs
}

func (r *FileStatfs) op() uint32 {
	return rFstatfs
}

func (r *FileStatfs) unserved() error {
	return EINVAL
}

func (r *FileStatfs) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, b, nil
}

func (r *FileStatfs) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return decodeStatfs(pk.data, r.st)
}
