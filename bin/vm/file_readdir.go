package main

import (
	"encoding/binary"
)

type FileReaddir struct {
	into  *MemfdFile
	n     int
	count int
}

func (r *FileReaddir) op() uint32 {
	return rReaddir
}

func (r *FileReaddir) unserved() error {
	return ENOTDIR
}

func (r *FileReaddir) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return uint32(r.n), b, nil
}

func (r *FileReaddir) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	data := pk.data
	b := make([]byte, r.n)
	n := 0
	le := binary.LittleEndian

	for len(data) >= 17 {
		ino, pos := le.Uint64(data), le.Uint64(data[8:])
		kind := data[16]
		end := 17

		for end < len(data) && data[end] != 0 {
			end++
		}

		name := data[17:end]
		reclen := (19 + len(name) + 1 + 7) &^ 7

		if n+reclen > len(b) {
			break
		}

		clear(b[n : n+reclen])
		le.PutUint64(b[n:], ino)
		le.PutUint64(b[n+8:], pos)
		le.PutUint16(b[n+16:], uint16(reclen))
		b[n+18] = kind
		copy(b[n+19:], name)
		n += reclen
		data = data[min(end+1, len(data)):]
	}

	count, err := r.into.with(0, n, true, func(dst []byte) (int, error) {
		return copy(dst, b[:n]), nil
	})

	r.count = count

	return err
}
