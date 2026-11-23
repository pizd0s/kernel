//go:build linux && amd64

package main

type WasmImport struct {
	module string
	name   string
	kind   byte
	typ    uint32
}
