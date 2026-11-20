//go:build linux && amd64

package main

import "math"

const noMove = math.MaxUint64

type Extent struct {
	at, used, capacity uint64
	to, toCapacity     uint64
	gone               bool
	owner              *Instance
}

func (x *Extent) end() uint64 {
	return x.at + x.capacity
}
