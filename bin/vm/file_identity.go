//go:build linux && amd64

package main

type FileIdentity struct {
	id     uint64
	key    FileKey
	access int
}

func (r *FileIdentity) op() uint32 {
	return 0
}

func (r *FileIdentity) unserved() error {
	return EXDEV
}

func (r *FileIdentity) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *FileIdentity) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
