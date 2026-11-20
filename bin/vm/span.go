//go:build linux && amd64

package main

type Span struct {
	b     []byte
	owner *Instance
}
