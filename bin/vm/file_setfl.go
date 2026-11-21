//go:build linux && amd64

package main

type FileSetfl struct {
	flags int
}

func (r *FileSetfl) op() uint32 {
	return rSetfl
}

func (r *FileSetfl) unserved() error {
	return EBADF
}

func (r *FileSetfl) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return uint32(r.flags & (O_APPEND | O_NONBLOCK)), b, nil
}

func (r *FileSetfl) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
