//go:build linux && amd64

package main

type WasmPiece struct {
	data   []byte
	align  uint32
	relocs []WasmReloc
}
