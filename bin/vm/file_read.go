package main

import (
	"bytes"
	"encoding/binary"
)

type FileRead struct {
	into     *Description
	n        int
	mode     IoMode
	at       int64
	explicit bool
	count    int
	sock     bool
	flags    uint32
	out      uint32
	addr     []byte
	files    []*Description
	pk       *Packet
}

func (r *FileRead) op() uint32 {
	if r.sock {
		return sRecv
	}

	return rRead
}

func (r *FileRead) unserved() error {
	switch {
	case r.sock:
		return ENOTSOCK
	case r.explicit:
		return ESPIPE
	}

	return EBADF
}

func (r *FileRead) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	if r.sock {
		return uint32(r.n), binary.LittleEndian.AppendUint32(b, r.flags), []*Description{r.into}
	}

	return uint32(r.n), ioBody(b, r.at, r.explicit), []*Description{r.into}
}

func (r *FileRead) des(p *Process, pk *Packet) error {
	d := pk.data

	if len(d) < 4 || int(binary.LittleEndian.Uint32(d)) > r.n {
		dropFiles(pk.files)

		return EIO
	}

	r.count = int(binary.LittleEndian.Uint32(d))

	if !r.sock {
		dropFiles(pk.files)

		return nil
	}

	if len(d) < 12 || len(d)-12 < int(binary.LittleEndian.Uint32(d[8:])) {
		dropFiles(pk.files)

		return EIO
	}

	r.out, r.addr, r.files = binary.LittleEndian.Uint32(d[4:]), bytes.Clone(d[12:12+binary.LittleEndian.Uint32(d[8:])]), pk.files

	return nil
}

func ioBody(b []byte, at int64, explicit bool) []byte {
	if !explicit {
		at = 0
	}

	b = binary.LittleEndian.AppendUint64(b, uint64(at))

	return binary.LittleEndian.AppendUint32(b, bit(explicit)*ioAt)
}
