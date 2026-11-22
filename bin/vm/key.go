//go:build linux && amd64

package main

type Key struct {
	used       bool
	destructor uint32
}
