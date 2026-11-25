package main

import (
	"encoding/binary"
)

type FileWrite struct {
	from     *MemfdFile
	n        int
	mode     IoMode
	at       int64
	explicit bool
	count    int
}

func (r *FileWrite) op() uint32 {
	return rWrite
}

func (r *FileWrite) unserved() error {
	if r.explicit {
		return ESPIPE
	}

	return EBADF
}

func (r *FileWrite) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return uint32(r.n), ioBody(b, r.at, r.explicit), []Object{r.from}
}

func (r *FileWrite) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	if len(pk.data) < 4 || int(binary.LittleEndian.Uint32(pk.data)) > r.n {
		return EIO
	}

	r.count = int(binary.LittleEndian.Uint32(pk.data))

	return nil
}
