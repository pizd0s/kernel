//go:build linux && amd64

package main

import (
	"time"
)

type EpollWait struct {
	buf   func() []byte
	max   int
	end   time.Time
	count int
}

func (r *EpollWait) op() uint32 {
	return 0
}

func (r *EpollWait) unserved() error {
	return EINVAL
}

func (r *EpollWait) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *EpollWait) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
