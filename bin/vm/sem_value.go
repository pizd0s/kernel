//go:build linux && amd64

package main

type SemValue struct {
	value int32
	pid   int32
	ncnt  int32
	zcnt  int32
}
