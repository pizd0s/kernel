package main

type Jump struct {
	frame [jumpWords]uint64
	code  [2]uint64
}

func (j *Jump) sp() uint64 {
	return j.frame[jumpSP]
}
