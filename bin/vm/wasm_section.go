//go:build linux && amd64

package main

type WasmSection struct {
	id   byte
	name string
	body []byte
}

func wasmSections(module []byte) []WasmSection {
	r := WasmReader{data: module, pos: len(wasmHeader)}

	var out []WasmSection

	for r.more() {
		s := WasmSection{id: r.byte()}

		s.body = r.bytes(r.u32())

		if s.id == 0 {
			n := WasmReader{data: s.body}

			s.name = n.name()
		}

		out = append(out, s)
	}

	return out
}

func lebWidth(v uint64) int {
	n := 1

	for v >= 0x80 {
		v >>= 7
		n++
	}

	return n
}

func appendLeb(b []byte, v uint64) []byte {
	return appendLebWide(b, v, lebWidth(v))
}

func appendLebWide(b []byte, v uint64, width int) []byte {
	for ; width > 1; width-- {
		b = append(b, byte(v&0x7f)|0x80)
		v >>= 7
	}

	return append(b, byte(v&0x7f))
}

func appendSleb(b []byte, v int64) []byte {
	for {
		c := byte(v & 0x7f)

		v >>= 7

		if v == 0 && c&0x40 == 0 || v == -1 && c&0x40 != 0 {
			return append(b, c)
		}

		b = append(b, c|0x80)
	}
}

func putLeb5(b []byte, v uint32) {
	for i := 0; i < 4; i++ {
		b[i] = byte(v>>(7*i))&0x7f | 0x80
	}

	b[4] = byte(v >> 28)
}

func putSleb5(b []byte, v int32) {
	for i := 0; i < 4; i++ {
		b[i] = byte(v>>(7*i))&0x7f | 0x80
	}

	b[4] = byte(v>>28) & 0x7f
}

func appendWasmName(b []byte, s string) []byte {
	return append(appendLeb(b, uint64(len(s))), s...)
}

func appendWasmSection(b []byte, id byte, payload []byte) []byte {
	return append(appendLeb(append(b, id), uint64(len(payload))), payload...)
}
