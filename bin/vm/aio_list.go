//go:build linux && amd64

package main

type AioList struct {
	left  int
	event SigEvent
}
