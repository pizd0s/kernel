package main

type FileUtimes struct {
	ts []Timespec
}

func (r *FileUtimes) op() uint32 {
	return rFutimes
}

func (r *FileUtimes) unserved() error {
	return EINVAL
}

func (r *FileUtimes) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	when, times := whens(r.ts)

	return when, append(b, times...), nil
}

func (r *FileUtimes) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
