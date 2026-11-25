package main

import (
	"encoding/binary"
)

type VfsStat struct {
	root   uint64
	path   string
	follow bool
	st     Stat
}

func (r *VfsStat) op() uint32 {
	return vStat
}

func (r *VfsStat) unserved() error {
	return ENOTDIR
}

func (r *VfsStat) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return bit(r.follow), append(binary.LittleEndian.AppendUint64(b, r.root), r.path...), nil
}

func (r *VfsStat) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return decodeStat(pk.data, &r.st)
}
