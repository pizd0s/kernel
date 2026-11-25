package main

import (
	"encoding/binary"
)

type SockSend struct {
	pk    *Packet
	to    Address
	mode  IoMode
	n     int
	moved Object
}

func (r *SockSend) op() uint32 {
	return sSend
}

func (r *SockSend) unserved() error {
	return ENOTSOCK
}

func (r *SockSend) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	b = append(binary.LittleEndian.AppendUint32(b, uint32(len(r.to.raw))), r.to.raw...)

	return bit(r.mode.nonblock)*MSG_DONTWAIT | bit(r.mode.quiet)*MSG_NOSIGNAL, append(b, r.pk.data...), r.pk.files
}

func (r *SockSend) des(p *Process, pk *Packet) error {
	data := pk.data

	if len(pk.files) > 0 {
		moved, rest, err := p.attached(pk)

		if err != nil {
			return err
		}

		r.moved, data = moved, rest
	}

	if len(data) < 4 {
		return EIO
	}

	r.n = int(binary.LittleEndian.Uint32(data))

	return nil
}
