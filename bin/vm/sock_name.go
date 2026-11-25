package main

import (
	"bytes"
)

type SockName struct {
	peer bool
	addr []byte
}

func (r *SockName) op() uint32 {
	return sName
}

func (r *SockName) unserved() error {
	return ENOTSOCK
}

func (r *SockName) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return bit(r.peer), b, nil
}

func (r *SockName) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	r.addr = bytes.Clone(pk.data)

	return nil
}
