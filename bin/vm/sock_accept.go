package main

import (
	"bytes"
)

type SockAccept struct {
	flags uint32
	conn  Object
	peer  []byte
}

func (r *SockAccept) op() uint32 {
	return sAccept
}

func (r *SockAccept) unserved() error {
	return ENOTSOCK
}

func (r *SockAccept) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.flags, b, nil
}

func (r *SockAccept) des(p *Process, pk *Packet) error {
	conn, peer, err := p.attached(pk)

	if err != nil {
		return err
	}

	r.conn, r.peer = conn, bytes.Clone(peer)

	return nil
}
