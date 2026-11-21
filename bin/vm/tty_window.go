//go:build linux && amd64

package main

import (
	"bytes"
)

type TtyWindow struct {
	size []byte
}

func (r *TtyWindow) op() uint32 {
	return rGetWinsize
}

func (r *TtyWindow) unserved() error {
	return ENOTTY
}

func (r *TtyWindow) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *TtyWindow) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	if len(pk.data) != sizeofWinsize {
		return EIO
	}

	r.size = bytes.Clone(pk.data)

	return nil
}
