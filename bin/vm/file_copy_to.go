//go:build linux && amd64

package main

type FileCopyTo struct {
	out      Object
	from, to int64
	n        int
	count    int
}

func (r *FileCopyTo) op() uint32 {
	return 0
}

func (r *FileCopyTo) unserved() error {
	return EINVAL
}

func (r *FileCopyTo) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *FileCopyTo) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
