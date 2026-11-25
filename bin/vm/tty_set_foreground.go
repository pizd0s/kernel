package main

type TtySetForeground struct {
	group uint32
}

func (r *TtySetForeground) op() uint32 {
	return rSetPgrp
}

func (r *TtySetForeground) unserved() error {
	return ENOTTY
}

func (r *TtySetForeground) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return r.group, b, nil
}

func (r *TtySetForeground) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
