package main

import "bytes"

type Ctl struct {
	data   []byte
	files  []Object
	opened uint32
	answer []byte
	got    []Object
}

func (r *Ctl) op() uint32 {
	return rCtl
}

func (r *Ctl) unserved() error {
	return ENOTTY
}

func (r *Ctl) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, append(b, r.data...), r.files
}

func (r *Ctl) des(p *Process, pk *Packet) error {
	r.opened, r.answer, r.got = pk.arg, bytes.Clone(pk.data), pk.files

	return nil
}
