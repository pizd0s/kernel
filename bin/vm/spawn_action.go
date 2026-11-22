//go:build linux && amd64

package main

const (
	spawnOpen = iota
	spawnClose
	spawnDup2
	spawnChdir
	spawnFchdir
)

type SpawnAction struct {
	kind  int
	fd    uint32
	to    uint32
	path  string
	flags uint32
	mode  uint32
}
