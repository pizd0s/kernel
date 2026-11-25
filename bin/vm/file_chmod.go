package main

type FileChmod struct {
	mode uint32
}

func (r *FileChmod) op() uint32 {
	return rFchmod
}

func (r *FileChmod) unserved() error {
	return EINVAL
}

func (r *FileChmod) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.mode, b, nil
}

func (r *FileChmod) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
