package main

import (
	"encoding/binary"
	"unsafe"
)

type GoTables struct {
	nfunc   int
	end     uint32
	names   []byte
	pctab   []byte
	ftab    []GoFuncTab
	pcln    []byte
	buckets []byte
}

func readTables(data, names []byte, textSize int) *GoTables {
	le := binary.LittleEndian

	if len(data) < 32 {
		throwFmt("malformed .vm.pcln")
	}

	if uintptr(unsafe.Pointer(unsafe.SliceData(data)))%4 != 0 {
		data = append(make([]byte, 0, len(data)), data...)
	}

	word := func(k int) int {
		return int(le.Uint32(data[4*k:]))
	}

	part := func(at, n int) []byte {
		if at < 32 || at%8 != 0 || n < 0 || at > len(data) || n > len(data)-at {
			throwFmt("malformed .vm.pcln")
		}

		return data[at : at+n]
	}

	t := &GoTables{nfunc: word(0), end: uint32(word(1)), names: names}

	if t.nfunc <= 0 || int(t.end) > textSize {
		throwFmt("malformed .vm.pcln")
	}

	t.pctab = part(word(2), word(3))
	ftab := part(word(4), 8*(t.nfunc+1))
	t.ftab = unsafe.Slice((*GoFuncTab)(unsafe.Pointer(unsafe.SliceData(ftab))), t.nfunc+1)
	t.pcln = part(word(5), word(6))
	t.buckets = part(word(7), (int(t.end)+4095)/4096*20)

	for k := range t.nfunc {
		f := t.ftab[k]

		if f.entry > t.ftab[k+1].entry || uint64(f.offset)+52 > uint64(len(t.pcln)) || le.Uint32(t.pcln[f.offset+4:]) >= uint32(len(names)) {
			throwFmt("malformed .vm.pcln")
		}

		for _, at := range [3]uint32{16, 20, 24} {
			if le.Uint32(t.pcln[f.offset+at:]) >= uint32(len(t.pctab)) {
				throwFmt("malformed .vm.pcln")
			}
		}
	}

	if t.ftab[t.nfunc].entry != t.end {
		throwFmt("malformed .vm.pcln")
	}

	return t
}

func (t *GoTables) regions() []Region {
	out := make([]Region, t.nfunc)

	for k := range out {
		f := t.ftab[k]

		out[k] = Region{name: cstr(t.names, binary.LittleEndian.Uint32(t.pcln[f.offset+4:])), start: f.entry, end: t.ftab[k+1].entry}
	}

	return out
}
