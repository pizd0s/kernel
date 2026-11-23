//go:build linux && amd64

package main

type SysvMessage struct {
	mtype int32
	text  []byte
}
