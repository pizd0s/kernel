//go:build linux && amd64

package main

type MqAttr struct {
	maxmsg, msgsize, curmsgs int
	nonblock                 bool
	set                      bool
	want                     bool
}

func (r *MqAttr) op() uint32 {
	return 0
}

func (r *MqAttr) unserved() error {
	return EBADF
}

func (r *MqAttr) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *MqAttr) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
