package main

import (
	"bytes"
)

type SockAccept struct {
	flags    uint32
	nonblock bool
	conn     *Description
	peer     []byte
}

func (r *SockAccept) op() uint32 {
	return sAccept
}

func (r *SockAccept) unserved() error {
	return ENOTSOCK
}

func (r *SockAccept) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return r.flags, b, nil
}

func (r *SockAccept) des(p *Process, pk *Packet) error {
	if pk.arg == 0 {
		if len(pk.files) != 1 {
			dropFiles(pk.files)

			return EIO
		}

		r.conn, r.peer = pk.files[0], bytes.Clone(pk.data)

		return nil
	}

	conn, peer, err := p.attached(pk)

	if err != nil {
		return err
	}

	r.conn, r.peer = conn, bytes.Clone(peer)

	return nil
}
