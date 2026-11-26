package main

import (
	"encoding/binary"
)

type FileWrite struct {
	from     *Description
	n        int
	mode     IoMode
	at       int64
	explicit bool
	count    int
	sock     bool
	flags    uint32
	to       []byte
	files    []*Description
	pk       *Packet
}

func (r *FileWrite) op() uint32 {
	if r.sock {
		return sSend
	}

	return rWrite
}

func (r *FileWrite) unserved() error {
	switch {
	case r.sock:
		return ENOTSOCK
	case r.explicit:
		return ESPIPE
	}

	return EBADF
}

func (r *FileWrite) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	if r.sock {
		b = binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(b, r.flags), uint32(len(r.to)))

		return uint32(r.n), append(b, r.to...), append([]*Description{r.from}, r.files...)
	}

	return uint32(r.n), ioBody(b, r.at, r.explicit), []*Description{r.from}
}

func (r *FileWrite) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	if len(pk.data) < 4 || int(binary.LittleEndian.Uint32(pk.data)) > r.n {
		return EIO
	}

	r.count = int(binary.LittleEndian.Uint32(pk.data))

	return nil
}
