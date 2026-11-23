//go:build linux && amd64

package main

import (
	"bytes"
	"slices"
	"sort"
	"strings"
)

const (
	wasmFunctionIndexLeb   = 0
	wasmTableIndexSleb     = 1
	wasmTableIndexI32      = 2
	wasmMemoryAddrLeb      = 3
	wasmMemoryAddrSleb     = 4
	wasmMemoryAddrI32      = 5
	wasmTypeIndexLeb       = 6
	wasmGlobalIndexLeb     = 7
	wasmGlobalIndexI32     = 13
	wasmTableNumberLeb     = 20
	wasmFunctionIndexI32   = 26
	wasmSymbolFunction     = 0
	wasmSymbolData         = 1
	wasmSymbolGlobal       = 2
	wasmSymbolSection      = 3
	wasmSymbolTag          = 4
	wasmSymbolTable        = 5
	wasmSymbolLocal        = 0x2
	wasmSymbolUndefined    = 0x10
	wasmSymbolExplicitName = 0x40
)

type WasmObject struct {
	types         [][]byte
	funcImports   []WasmImport
	globalImports []WasmImport
	tableImports  []WasmImport
	funcs         []uint32
	bodies        []WasmPiece
	segments      []WasmPiece
	symbols       []WasmSymbol
	names         map[uint32]string
	kernel        map[string]uint32
}

func parseWasmObject(data []byte) *WasmObject {
	if !bytes.HasPrefix(data, wasmHeader) {
		throwFmt("libc.wasm: no wasm module")
	}

	o := &WasmObject{names: map[uint32]string{}, kernel: map[string]uint32{}}
	relocs := map[int][]WasmReloc{}
	codeAt, dataAt := -1, -1

	var code, segments, linking []byte

	for i, s := range wasmSections(data) {
		r := WasmReader{data: s.body}

		switch s.id {
		case 0:
			r.name()

			if s.name == "linking" {
				linking = s.body[r.pos:]
			} else if strings.HasPrefix(s.name, "reloc.") {
				target := int(r.u32())

				relocs[target] = wasmRelocs(&r)
			}
		case 1:
			o.types = r.types()
		case 2:
			for _, imp := range r.imports() {
				switch imp.kind {
				case 0:
					o.funcImports = append(o.funcImports, imp)
				case 1:
					o.tableImports = append(o.tableImports, imp)
				case 3:
					o.globalImports = append(o.globalImports, imp)
				}
			}
		case 3:
			for n := r.count(1); n > 0; n-- {
				o.funcs = append(o.funcs, r.u32())
			}
		case 4, 6, 13:
			throwFmt("libc.wasm: a section %d of its own", s.id)
		case 10:
			code, codeAt = s.body, i
		case 11:
			segments, dataAt = s.body, i
		}
	}

	aligns := o.link(linking)

	o.code(code, relocs[codeAt])
	o.data(segments, aligns, relocs[dataAt])
	o.exports()

	return o
}

func wasmRelocs(r *WasmReader) []WasmReloc {
	var out []WasmReloc

	for n := r.count(3); n > 0; n-- {
		x := WasmReloc{kind: r.byte(), offset: r.u32(), index: r.u32()}

		switch x.kind {
		case 3, 4, 5, 8, 9, 11, 14, 15, 16, 17, 21, 22, 23, 25:
			x.addend = r.s32()
		}

		out = append(out, x)
	}

	slices.SortFunc(out, func(a, b WasmReloc) int {
		return int(a.offset) - int(b.offset)
	})

	return out
}

func wasmPlaceRelocs(pieces []WasmPiece, starts []int, relocs []WasmReloc) {
	for _, x := range relocs {
		at := int(x.offset)
		k := sort.Search(len(starts), func(i int) bool { return starts[i] > at }) - 1

		if k < 0 || at+int(x.width()) > starts[k]+len(pieces[k].data) {
			throwFmt("libc.wasm: a relocation at %d of no function or segment", at)
		}

		x.offset = uint32(at - starts[k])
		pieces[k].relocs = append(pieces[k].relocs, x)
	}
}

func (o *WasmObject) link(linking []byte) []uint32 {
	r := WasmReader{data: linking}

	if len(linking) == 0 || r.u32() != 2 {
		throwFmt("libc.wasm: no linking section of version 2")
	}

	var aligns []uint32

	for r.more() {
		kind := r.byte()
		s := WasmReader{data: r.bytes(r.u32())}

		switch kind {
		case 5:
			for n := s.count(3); n > 0; n-- {
				s.name()

				aligns = append(aligns, s.u32())

				s.u32()
			}
		case 6:
			if s.u32() != 0 {
				throwFmt("libc.wasm: constructors")
			}
		case 8:
			o.symbolTable(&s)
		}
	}

	return aligns
}

func (o *WasmObject) symbolTable(r *WasmReader) {
	for n := r.count(2); n > 0; n-- {
		sym := WasmSymbol{kind: r.byte(), flags: r.u32()}
		defined := sym.flags&wasmSymbolUndefined == 0

		switch sym.kind {
		case wasmSymbolFunction, wasmSymbolGlobal, wasmSymbolTag, wasmSymbolTable:
			sym.index = r.u32()

			if defined || sym.flags&wasmSymbolExplicitName != 0 {
				sym.name = r.name()
			}
		case wasmSymbolData:
			sym.name = r.name()

			if defined {
				sym.index = r.u32()
				sym.offset = r.u32()

				r.u32()
			}
		case wasmSymbolSection:
			sym.index = r.u32()
		default:
			throwFmt("libc.wasm: a symbol of kind %d", sym.kind)
		}

		o.symbols = append(o.symbols, sym)
	}
}

func (o *WasmObject) code(code []byte, relocs []WasmReloc) {
	var starts []int

	if len(code) > 0 {
		r := WasmReader{data: code}

		for n := r.count(1); n > 0; n-- {
			size := r.u32()

			starts = append(starts, r.pos)
			o.bodies = append(o.bodies, WasmPiece{data: r.bytes(size)})
		}
	}

	if len(o.bodies) != len(o.funcs) {
		throwFmt("libc.wasm: %d bodies of %d functions", len(o.bodies), len(o.funcs))
	}

	wasmPlaceRelocs(o.bodies, starts, relocs)
}

func (o *WasmObject) data(segments []byte, aligns []uint32, relocs []WasmReloc) {
	var starts []int

	if len(segments) > 0 {
		r := WasmReader{data: segments}

		for n := r.count(1); n > 0; n-- {
			if r.u32() != 0 || r.byte() != 0x41 {
				throwFmt("libc.wasm: a data segment of no fixed place")
			}

			r.s32()

			if r.byte() != 0x0b {
				throwFmt("libc.wasm: a data segment of no fixed place")
			}

			size := r.u32()

			starts = append(starts, r.pos)
			o.segments = append(o.segments, WasmPiece{data: r.bytes(size)})
		}
	}

	if len(aligns) != len(o.segments) {
		throwFmt("libc.wasm: %d alignments of %d data segments", len(aligns), len(o.segments))
	}

	for i := range o.segments {
		o.segments[i].align = min(aligns[i], 12)
	}

	wasmPlaceRelocs(o.segments, starts, relocs)
}

func (o *WasmObject) exports() {
	all := uint32(len(o.funcImports) + len(o.funcs))

	for _, sym := range o.symbols {
		if sym.kind != wasmSymbolFunction || sym.name == "" {
			continue
		}

		if sym.index >= all {
			throwFmt("libc.wasm: function %s of %d", sym.name, sym.index)
		}

		if _, ok := o.names[sym.index]; !ok {
			o.names[sym.index] = sym.name
		}

		if sym.flags&(wasmSymbolUndefined|wasmSymbolLocal) != 0 || !strings.HasPrefix(sym.name, "__kernel_") {
			continue
		}

		name := strings.TrimPrefix(sym.name, "__kernel_")

		if _, ok := o.kernel[name]; ok {
			throwFmt("libc.wasm: kernel function %s twice", name)
		}

		o.kernel[name] = sym.index
	}
}
