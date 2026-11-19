//go:build linux && amd64

package main

type Flock struct {
	Type   int16
	Whence int16
	Start  int64
	Len    int64
	Pid    int32
}
