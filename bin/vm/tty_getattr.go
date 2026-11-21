//go:build linux && amd64

package main

import (
	"bytes"
)

type TtyGetattr struct {
	termios []byte
}

func (r *TtyGetattr) op() uint32 {
	return rTcgetattr
}

func (r *TtyGetattr) unserved() error {
	return ENOTTY
}

func (r *TtyGetattr) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *TtyGetattr) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	if len(pk.data) != sizeofTermios {
		return EIO
	}

	r.termios = bytes.Clone(pk.data)

	return nil
}
