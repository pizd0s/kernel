//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/binary"
)

type SockRecv struct {
	into  Into
	want  int
	flags uint32
	n     int
	out   uint32
	files []Object
	cred  *Ucred
	addr  []byte
}

func (r *SockRecv) op() uint32 {
	return sRecv
}

func (r *SockRecv) unserved() error {
	return ENOTSOCK
}

func (r *SockRecv) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.flags, binary.LittleEndian.AppendUint32(b, uint32(r.want)), nil
}

func (r *SockRecv) des(p *Process, pk *Packet) error {
	d := pk.data

	if len(d) < 8 || len(d)-8 < int(binary.LittleEndian.Uint32(d[4:])) {
		dropFiles(pk.files)

		return EIO
	}

	r.out, d = binary.LittleEndian.Uint32(d), d[4:]

	addr := d[4 : 4+binary.LittleEndian.Uint32(d)]
	data := d[4+len(addr):]

	r.n = r.into.put(0, data[:min(len(data), r.want)])
	r.files, r.addr = pk.files, bytes.Clone(addr)

	return nil
}
