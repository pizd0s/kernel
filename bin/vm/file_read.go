package main

import (
	"encoding/binary"
)

type FileRead struct {
	into     *MemfdFile
	n        int
	mode     IoMode
	at       int64
	explicit bool
	count    int
}

func (r *FileRead) op() uint32 {
	return rRead
}

func (r *FileRead) unserved() error {
	if r.explicit {
		return ESPIPE
	}

	return EBADF
}

func (r *FileRead) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return uint32(r.n), ioBody(b, r.at, r.explicit), []Object{r.into}
}

func (r *FileRead) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	if len(pk.data) < 4 || int(binary.LittleEndian.Uint32(pk.data)) > r.n {
		return EIO
	}

	r.count = int(binary.LittleEndian.Uint32(pk.data))

	return nil
}

func ioBody(b []byte, at int64, explicit bool) []byte {
	if !explicit {
		at = 0
	}

	b = binary.LittleEndian.AppendUint64(b, uint64(at))

	return binary.LittleEndian.AppendUint32(b, bit(explicit)*ioAt)
}
