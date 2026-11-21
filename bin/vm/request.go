//go:build linux && amd64

package main

type Request interface {
	op() uint32
	unserved() error
	ser(p *Process, b []byte) (uint32, []byte, []Object)
	des(p *Process, pk *Packet) error
}
