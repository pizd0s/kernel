package main

type WasmPiece struct {
	data   []byte
	align  uint32
	relocs []WasmReloc
}
