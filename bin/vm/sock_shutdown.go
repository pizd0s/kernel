//go:build linux && amd64

package main

type SockShutdown struct {
	how int
}

func (r *SockShutdown) op() uint32 {
	return sShutdown
}

func (r *SockShutdown) unserved() error {
	return ENOTSOCK
}

func (r *SockShutdown) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return uint32(r.how), b, nil
}

func (r *SockShutdown) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
