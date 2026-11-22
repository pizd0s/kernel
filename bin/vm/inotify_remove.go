//go:build linux && amd64

package main

type InotifyRemove struct {
	wd int32
}

func (r *InotifyRemove) op() uint32 {
	return 0
}

func (r *InotifyRemove) unserved() error {
	return EINVAL
}

func (r *InotifyRemove) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *InotifyRemove) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
