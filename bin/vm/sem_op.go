//go:build linux && amd64

package main

type SemOp struct {
	num   uint16
	op    int16
	flags int16
}
