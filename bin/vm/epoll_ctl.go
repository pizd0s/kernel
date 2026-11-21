//go:build linux && amd64

package main

type EpollCtl struct {
	ctl    uint32
	key    EpollKey
	events uint32
	data   uint64
}

func (r *EpollCtl) op() uint32 {
	return 0
}

func (r *EpollCtl) unserved() error {
	return EINVAL
}

func (r *EpollCtl) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *EpollCtl) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
