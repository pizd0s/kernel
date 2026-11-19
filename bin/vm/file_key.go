//go:build linux && amd64

package main

type FileKey struct {
	dev, ino uint64
}
