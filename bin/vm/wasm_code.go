//go:build linux && amd64

package main

func wasmFunction(funcs []uint32, v uint64) uint32 {
	if v >= uint64(len(funcs)) {
		throwFmt("wasm: function %d of %d", v, len(funcs))
	}

	return funcs[v]
}

func remapBody(body []byte, funcs []uint32) []byte {
	r := WasmReader{data: body}
	out := make([]byte, 0, len(body))
	last := 0

	for n := r.count(2); n > 0; n-- {
		r.u32()

		if t := r.byte(); t == 0x63 || t == 0x64 {
			throwFmt("wasm: a local of a typed reference")
		}
	}

	for r.more() {
		op := r.byte()

		if op != 0x10 && op != 0x12 && op != 0xd2 {
			skipImmediates(&r, op)

			continue
		}

		at := r.pos
		v, width := r.leb(5)
		to := uint64(wasmFunction(funcs, v))

		out = appendLebWide(append(out, body[last:at]...), to, max(width, lebWidth(to)))
		last = r.pos
	}

	return append(out, body[last:]...)
}

func remapExpr(r *WasmReader, out []byte, funcs []uint32) []byte {
	start := r.pos

	for {
		switch op := r.byte(); op {
		case 0x0b:
			return append(out, r.data[start:r.pos]...)
		case 0xd2:
			at := r.pos
			v, _ := r.leb(5)

			if funcs != nil {
				out = appendLeb(append(out, r.data[start:at]...), uint64(wasmFunction(funcs, v)))
				start = r.pos
			}
		case 0x41, 0xd0:
			r.leb(5)
		case 0x42:
			r.leb(10)
		case 0x43:
			r.bytes(4)
		case 0x44:
			r.bytes(8)
		case 0x23:
			r.u32()
		case 0x6a, 0x6b, 0x6c, 0x7c, 0x7d, 0x7e:
		default:
			throwFmt("wasm: an instruction 0x%02x in a constant", op)
		}
	}
}

func skipImmediates(r *WasmReader, op byte) {
	switch {
	case op <= 0x01, op == 0x05, op == 0x0b, op == 0x0f, op == 0x1a, op == 0x1b, op == 0xd1, op >= 0x45 && op <= 0xc4:
	case op >= 0x02 && op <= 0x04, op == 0xd0:
		r.leb(5)
	case op == 0x0c, op == 0x0d, op >= 0x20 && op <= 0x26, op == 0x3f, op == 0x40:
		r.u32()
	case op == 0x0e:
		for n := r.count(1); n >= 0; n-- {
			r.u32()
		}
	case op == 0x11, op == 0x13:
		r.u32()
		r.u32()
	case op == 0x1c:
		r.bytes(r.u32())
	case op >= 0x28 && op <= 0x3e:
		if r.u32()&0x40 != 0 {
			r.u32()
		}

		r.leb(10)
	case op == 0x41:
		r.leb(5)
	case op == 0x42:
		r.leb(10)
	case op == 0x43:
		r.bytes(4)
	case op == 0x44:
		r.bytes(8)
	case op == 0xfc:
		switch sub := r.u32(); {
		case sub <= 7:
		case sub == 8, sub == 10, sub == 12, sub == 14:
			r.u32()
			r.u32()
		case sub <= 17:
			r.u32()
		default:
			throwFmt("wasm: an instruction 0xfc %d", sub)
		}
	default:
		throwFmt("wasm: an instruction 0x%02x", op)
	}
}
