//go:build linux && amd64

package main

type WasmReloc struct {
	kind   byte
	offset uint32
	index  uint32
	addend int32
}

func (x WasmReloc) width() uint32 {
	switch x.kind {
	case wasmTableIndexI32, wasmMemoryAddrI32, wasmGlobalIndexI32, wasmFunctionIndexI32:
		return 4
	}

	return 5
}
