//go:build linux && amd64

package main

import (
	"sync/atomic"
)

type SockKnock struct {
	kind     int
	nonblock bool
	timeout  *atomic.Int64
	conn     *PairEnd
}

func (r *SockKnock) op() uint32 {
	return 0
}

func (r *SockKnock) unserved() error {
	return ECONNREFUSED
}

func (r *SockKnock) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *SockKnock) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
