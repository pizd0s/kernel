//go:build linux && amd64

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
	if len(pk.files) == 0 {
		return EIO
	}

	dropFiles(pk.files[1:])

	r.conn, r.peer = pk.files[0], bytes.Clone(pk.data)

	return nil
}
