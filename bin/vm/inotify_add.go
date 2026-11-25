package main

type InotifyAdd struct {
	path string
	mask uint32
	wd   int32
}

func (r *InotifyAdd) op() uint32 {
	return 0
}

func (r *InotifyAdd) unserved() error {
	return EINVAL
}

func (r *InotifyAdd) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *InotifyAdd) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
