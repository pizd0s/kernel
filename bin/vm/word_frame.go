//go:build linux && amd64

package main

type WordFrame struct {
	kind  byte
	dq    bool
	depth int
}
