//go:build linux && amd64

package main

import (
	_ "embed"
	"encoding/binary"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	//go:embed libstr.o
	strBlob  []byte
	strSlots = []string{"memcpy", "memmove", "memset", "memcmp", "strlen", "strcpy", "strcat", "strcmp", "strspn", "strcspn", "memchr", "strnlen"}
	strLib   = sync.OnceValue(linkStr)
)

var strDerived = []string{"bcmp", "bcopy", "bzero", "explicit_bzero", "mempcpy", "strncat", "strlcpy", "strlcat", "strpbrk", "strsep",
	"strtok_r", "memmem", "strstr"}

const strConfigTable = 16

type StrLibrary struct {
	code  []byte
	slots map[string]uintptr
}

func strFunction(sym string) uintptr {
	at, ok := strLib().slots[strings.TrimPrefix(sym, symStr)]

	if !ok {
		throwFmt("no function of lib/str for %s", sym)
	}

	return at
}

func linkStr() *StrLibrary {
	f := parseELF(strBlob)
	eh := f.find(".eh_frame")
	text, rodata := f.sections[f.text].data, f.sections[f.rodata].data
	defined := f.functions()
	regions := readEhFrame(f.sections[eh].data, f.relocations(eh), defined)

	page := func(n int) int {
		return (n + 4095) &^ 4095
	}

	rodataAt := page(max(len(text), 1))
	code := mmap(rodataAt+page(max(len(rodata), 1)), syscall.PROT_READ|syscall.PROT_WRITE)

	copy(code, text)
	copy(code[rodataAt:], rodata)

	base := uintptr(unsafe.Pointer(unsafe.SliceData(code)))

	for _, section := range []struct{ index, at int }{{f.text, 0}, {f.rodata, rodataAt}} {
		for _, r := range f.relocations(section.index) {
			target := uint64(base) + r.value

			switch r.in {
			case inRodata:
				target += uint64(rodataAt)
			}

			at := section.at + int(r.at)

			switch r.typ {
			case rX86_64_PC32:
				put32rel(code[at:], int64(target)+r.addend-int64(uint64(base)+uint64(at)))
			case rX86_64_64:
				binary.LittleEndian.PutUint64(code[at:], target+uint64(r.addend))
			}
		}
	}

	throw(syscall.Mprotect(code[:rodataAt], syscall.PROT_READ|syscall.PROT_EXEC))
	registerCode("str", base, uintptr(len(text)), regions, true, nil, nil)

	function := func(name string) uintptr {
		at, ok := defined[name]

		if !ok {
			throwFmt("lib/str has no %s", name)
		}

		return base + uintptr(at)
	}

	strCall0(function("str_dispatch"))
	throw(syscall.Mprotect(code[rodataAt:], syscall.PROT_READ))

	at := strCall0(function("str_choose")) - base
	lib := &StrLibrary{code: code, slots: map[string]uintptr{}}

	for k, slot := range strSlots {
		lib.slots[slot] = uintptr(binary.LittleEndian.Uint64(code[at+strConfigTable+8*uintptr(k):]))
	}

	for _, name := range strDerived {
		lib.slots[name] = function("str_" + name)
	}

	return lib
}
