package main

type FileCopyFrom struct {
	in       *Description
	from, to int64
	n        int
	count    int
}

func (r *FileCopyFrom) op() uint32 {
	return 0
}

func (r *FileCopyFrom) unserved() error {
	return EINVAL
}

func (r *FileCopyFrom) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, b, nil
}

func (r *FileCopyFrom) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
