//go:build linux && amd64

package main

import "time"

type TimerSet struct {
	value    time.Duration
	interval time.Duration
	absolute bool
	left     time.Duration
	every    time.Duration
}

func (r *TimerSet) op() uint32 {
	return 0
}

func (r *TimerSet) unserved() error {
	return EINVAL
}

func (r *TimerSet) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *TimerSet) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
