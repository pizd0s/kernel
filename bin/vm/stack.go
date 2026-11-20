package main

import "maps"

const (
	stackBlock = 64 << 10
	stackLimit = 64 << 20
)

type Stack struct {
	top   uint32
	next  map[uint32]uint32
	sizes map[uint32]uint32
	total uint64
}

func newStack(top uint32) *Stack {
	return &Stack{top: top, next: map[uint32]uint32{}, sizes: map[uint32]uint32{}}
}

func (s *Stack) clone() *Stack {
	if s == nil {
		return nil
	}

	c := *s

	c.next, c.sizes = maps.Clone(s.next), maps.Clone(s.sizes)

	return &c
}

func (s *Stack) live(sp uint32) uint64 {
	if s != nil && sp >= s.top {
		return 0
	}

	return uint64(sp) &^ 4095
}
