//go:build linux && amd64

package main

type SigEvent struct {
	kind   uint32
	signal int
	value  uint32
}
