package main

type WasmReader struct {
	data []byte
	pos  int
}

func (r *WasmReader) more() bool {
	return r.pos < len(r.data)
}

func (r *WasmReader) left() int {
	return len(r.data) - r.pos
}

func (r *WasmReader) byte() byte {
	if r.pos >= len(r.data) {
		throwFmt("wasm: the module ends early")
	}

	b := r.data[r.pos]

	r.pos++

	return b
}

func (r *WasmReader) leb(most int) (uint64, int) {
	var v uint64

	for n := 0; n < most; n++ {
		b := r.byte()

		v |= uint64(b&0x7f) << (7 * n)

		if b < 0x80 {
			return v, n + 1
		}
	}

	throwFmt("wasm: a number longer than %d bytes", most)

	return 0, 0
}

func (r *WasmReader) u32() uint32 {
	v, _ := r.leb(5)

	if v > 0xffffffff {
		throwFmt("wasm: a number past 32 bits")
	}

	return uint32(v)
}

func (r *WasmReader) s32() int32 {
	var v int64

	for shift := 0; shift < 35; shift += 7 {
		b := r.byte()

		v |= int64(b&0x7f) << shift

		if b < 0x80 {
			if b&0x40 != 0 {
				v |= -1 << (shift + 7)
			}

			return int32(v)
		}
	}

	throwFmt("wasm: a number longer than 5 bytes")

	return 0
}

func (r *WasmReader) count(least int) int {
	n := r.u32()

	if uint64(n)*uint64(least) > uint64(r.left()) {
		throwFmt("wasm: %d entries in %d bytes", n, r.left())
	}

	return int(n)
}

func (r *WasmReader) bytes(n uint32) []byte {
	if uint64(n) > uint64(r.left()) {
		throwFmt("wasm: the module ends early")
	}

	b := r.data[r.pos : r.pos+int(n)]

	r.pos += int(n)

	return b
}

func (r *WasmReader) name() string {
	return string(r.bytes(r.u32()))
}

func (r *WasmReader) limits() {
	flags := r.u32()

	r.leb(10)

	if flags&1 != 0 {
		r.leb(10)
	}
}

func (r *WasmReader) types() [][]byte {
	var out [][]byte

	for n := r.count(3); n > 0; n-- {
		start := r.pos

		if r.byte() != 0x60 {
			throwFmt("wasm: a type of no function")
		}

		r.bytes(r.u32())
		r.bytes(r.u32())

		out = append(out, r.data[start:r.pos])
	}

	return out
}

func (r *WasmReader) imports() []WasmImport {
	var out []WasmImport

	for n := r.count(4); n > 0; n-- {
		imp := WasmImport{module: r.name(), name: r.name(), kind: r.byte()}

		switch imp.kind {
		case 0:
			imp.typ = r.u32()
		case 1:
			r.byte()
			r.limits()
		case 2:
			r.limits()
		case 3:
			r.bytes(2)
		case 4:
			r.byte()
			r.u32()
		default:
			throwFmt("wasm: an import of kind %d", imp.kind)
		}

		out = append(out, imp)
	}

	return out
}
