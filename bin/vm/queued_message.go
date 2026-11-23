//go:build linux && amd64

package main

type QueuedMessage struct {
	data []byte
	prio uint32
}
