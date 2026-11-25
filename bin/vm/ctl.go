package main

import "bytes"

type Ctl struct {
	data   []byte
	files  []*Description
	opened uint32
	answer []byte
	got    []*Description
}

func (r *Ctl) op() uint32 {
	return rCtl
}

func (r *Ctl) unserved() error {
	return ENOTTY
}

func (r *Ctl) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, append(b, r.data...), r.files
}

func (r *Ctl) des(p *Process, pk *Packet) error {
	r.opened, r.answer, r.got = pk.arg, bytes.Clone(pk.data), pk.files

	return nil
}
