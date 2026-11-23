//go:build linux && amd64

package main

type WasmSymbol struct {
	kind   byte
	flags  uint32
	name   string
	index  uint32
	offset uint32
}
