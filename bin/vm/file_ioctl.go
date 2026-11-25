package main

import (
	"unsafe"
)

type FileIoctl struct {
	req uintptr
	arg unsafe.Pointer
}

func (r *FileIoctl) op() uint32 {
	return 0
}

func (r *FileIoctl) unserved() error {
	return ENOTTY
}

func (r *FileIoctl) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, b, nil
}

func (r *FileIoctl) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
