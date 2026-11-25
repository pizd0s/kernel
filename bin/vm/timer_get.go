package main

import "time"

type TimerGet struct {
	left  time.Duration
	every time.Duration
}

func (r *TimerGet) op() uint32 {
	return 0
}

func (r *TimerGet) unserved() error {
	return EINVAL
}

func (r *TimerGet) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, b, nil
}

func (r *TimerGet) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
