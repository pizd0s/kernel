//go:build linux && amd64

package main

type SemOpen struct {
	at    uint32
	count int
}
