package main

import (
	"encoding/binary"
	"strings"
	"sync"
)

const (
	objectMagic  = "PZO1"
	objectPage   = 4096
	objectHeader = 96
)

type CodeObject struct {
	text   []byte
	rodata []byte

	defined map[string]uint32
	got     []string
	meta    *Meta
	tables  *GoTables
	regions func() []Region
}

func readObject(data []byte) *CodeObject {
	le := binary.LittleEndian

	if len(data) < objectPage || string(data[:4]) != objectMagic || le.Uint64(data[8:]) != uint64(len(data)) {
		throwFmt("not an object of this kernel's compiler")
	}

	text, rodata := le.Uint64(data[16:]), le.Uint64(data[24:])

	if text >= 1<<31 || rodata >= 1<<31 {
		throwFmt("malformed object")
	}

	textEnd := objectPage + (max(text, 1)+objectPage-1)&^(objectPage-1)
	imageEnd := textEnd + rodata

	if imageEnd > uint64(len(data)) {
		throwFmt("malformed object")
	}

	part := func(k int) []byte {
		at, n := le.Uint64(data[32+16*k:]), le.Uint64(data[40+16*k:])

		if at < imageEnd || at > uint64(len(data)) || n > uint64(len(data))-at {
			throwFmt("malformed object")
		}

		return data[at : at+n]
	}

	obj := &CodeObject{text: data[objectPage : objectPage+text], rodata: data[textEnd:imageEnd]}
	obj.tables = readTables(part(0), part(1), int(text))

	if got := part(2); len(got) > 0 {
		if got[len(got)-1] != 0 {
			throwFmt("malformed object")
		}

		obj.got = strings.Split(string(got[:len(got)-1]), "\x00")
	}

	obj.meta = decodeNote(part(3))
	obj.defined = obj.meta.defined
	obj.regions = sync.OnceValue(obj.tables.regions)
	obj.describe()

	return obj
}

func (obj *CodeObject) describe() {
	m := obj.meta

	m.init = symInit

	for _, name := range m.symbols {
		if obj.defined[name] >= uint32(len(obj.text)) {
			throwFmt("symbol %s out of the text", name)
		}

		if names, t, ok := parseFuncSymbol(name, symExport, 1); ok {
			m.exports = append(m.exports, ExportMeta{kind: externFunc, name: names[0], sym: name, params: t.params, results: t.results})
		}
	}

	if _, ok := obj.defined[symInit]; !ok {
		throwFmt("no %s", symInit)
	}

	for _, name := range obj.got {
		if names, t, ok := parseFuncSymbol(name, symImport, 2); ok {
			m.imports = append(m.imports, ImportMeta{kind: externFunc, module: names[0], name: names[1], sym: name, params: t.params, results: t.results})
		}
	}
}
