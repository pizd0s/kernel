//go:build linux && amd64

package main

type Slots[T any] struct {
	free []*T
}

func (s *Slots[T]) get() *T {
	n := len(s.free)

	if n == 0 {
		return new(T)
	}

	r := s.free[n-1]

	s.free = s.free[:n-1]

	return r
}

func (s *Slots[T]) put(r *T) {
	var none T

	*r = none
	s.free = append(s.free, r)
}
