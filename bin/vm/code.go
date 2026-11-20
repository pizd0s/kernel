//go:build linux && amd64

package main

import (
	"encoding/binary"
	"math"
	"reflect"
	"strings"
	"syscall"
	"unsafe"
)

const thunkSize = 16

type Code struct {
	name string
	obj  *ElfObject
	text []byte
	base uintptr

	keep    []any
	thunks  []byte
	thunkOf map[string]uintptr
}

func helperTrap(code uint32) {
	panic(newTrap(code, 1))
}

func helperTrapAt(code uint32, addr uint64, info uint32) {
	t := newTrap(code, 1)

	t.addr, t.width, t.kind = addr, uint8(info), uint8(info>>8)
	panic(t)
}

func helperMemoryGrow(delta uint32) uint32 {
	i := current()

	if _, compiler := i.host.(*CompilerIO); !compiler && i.proc.chaosFail("memory-grow") {
		return math.MaxUint32
	}

	return i.memory.grow(delta)
}

type Interruptible interface {
	interrupt()
}

func helperInterrupt() {
	if h, ok := current().host.(Interruptible); ok {
		h.interrupt()
	}
}

func helperTableGrow(table, delta uint32, init uintptr) uint32 {
	return math.MaxUint32
}

func funcval(f any) uintptr {
	return (*[2]uintptr)(unsafe.Pointer(&f))[1]
}

func goType(t ValType) reflect.Type {
	switch t {
	case I32:
		return reflect.TypeFor[uint32]()
	case I64:
		return reflect.TypeFor[uint64]()
	case F32:
		return reflect.TypeFor[float32]()
	case F64:
		return reflect.TypeFor[float64]()
	case FuncRef, ExternRef:
		return reflect.TypeFor[uintptr]()
	}

	throwFmt("unsupported value type v128")

	return nil
}

func goFuncType(params, results []ValType) reflect.Type {
	return funcType(nil, params, results)
}

func exportType(params, results []ValType) reflect.Type {
	return funcType([]reflect.Type{reflect.TypeFor[uintptr]()}, params, results)
}

func funcType(in []reflect.Type, params, results []ValType) reflect.Type {
	for _, t := range params {
		in = append(in, goType(t))
	}

	var out []reflect.Type

	for _, t := range results {
		out = append(out, goType(t))
	}

	return reflect.FuncOf(in, out, false)
}

func (c *Code) keepClosure(sym string, f any) uintptr {
	if c.thunks == nil {
		c.thunks = mmap((thunkSize*len(c.obj.meta.imports)+4095)&^4095, syscall.PROT_READ|syscall.PROT_WRITE)[:0]
		c.thunkOf = map[string]uintptr{}
	}

	c.keep = append(c.keep, f)

	at := uintptr(unsafe.Pointer(unsafe.SliceData(c.thunks))) + uintptr(len(c.thunks))

	c.thunks = append(c.thunks, 0x48, 0xba)
	c.thunks = binary.LittleEndian.AppendUint64(c.thunks, uint64(funcval(f)))
	c.thunks = append(c.thunks, 0xff, 0x22, 0xcc, 0xcc, 0xcc, 0xcc)
	c.thunkOf[sym] = at

	return at
}

func (c *Code) keepFunc(f any) uintptr {
	c.keep = append(c.keep, f)

	return *(*uintptr)(unsafe.Pointer(funcval(f)))
}

func (c *Code) resolve(sym string, imports Imports) uintptr {
	switch sym {
	case symMorestackStub:
		return morestackStub()
	case symInterruptCall:
		return interruptStub()
	case symTrap:
		return c.keepFunc(helperTrap)
	case symTrapAt:
		return c.keepFunc(helperTrapAt)
	case symAsanRange:
		return asanRangeStub()
	case symMemoryGrow:
		return c.keepFunc(helperMemoryGrow)
	case symTableGrow:
		return c.keepFunc(helperTableGrow)
	}

	if strings.HasPrefix(sym, symStr) {
		return strFunction(sym)
	}

	if at, ok := c.thunkOf[sym]; ok {
		return at
	}

	for k := range c.obj.meta.imports {
		imp := &c.obj.meta.imports[k]

		if imp.kind != externFunc || imp.sym != sym {
			continue
		}

		f := imports.function(imp)

		if f == nil {
			throwFmt("unresolved import %s.%s", imp.module, imp.name)
		}

		if code, ok := f.(uintptr); ok {
			return code
		}

		wrapped, closure := f.(Closure)

		if closure {
			f = wrapped.f
		}

		if want := goFuncType(imp.params, imp.results); reflect.TypeOf(f) != want {
			throwFmt("import %s.%s is %T, not %s", imp.module, imp.name, f, want)
		}

		if closure {
			return c.keepClosure(sym, f)
		}

		return c.keepFunc(f)
	}

	throwFmt("unresolved symbol %s", sym)

	return 0
}

func link(name string, obj *ElfObject, imports Imports) *Code {
	c := &Code{name: name, obj: obj}

	page := func(n int) int {
		return (n + 4095) &^ 4095
	}

	got := map[string]int{}

	for _, r := range obj.relocs {
		if r.in == inExternal {
			if _, ok := got[r.sym]; !ok {
				got[r.sym] = len(got)
			}
		}
	}

	textSize := page(max(len(obj.text), 1))
	gotAt := textSize + int(alignTo(int32(len(obj.rodata)), 16))

	c.text = mmap(textSize+page(gotAt-textSize+8*len(got)), syscall.PROT_READ|syscall.PROT_WRITE)
	copy(c.text, obj.text)
	copy(c.text[textSize:], obj.rodata)
	c.base = uintptr(unsafe.Pointer(unsafe.SliceData(c.text)))

	for sym, k := range got {
		binary.LittleEndian.PutUint64(c.text[gotAt+8*k:], uint64(c.resolve(sym, imports)))
	}

	for _, r := range obj.relocs {
		var target uint64

		switch r.in {
		case inText:
			target = uint64(c.base) + r.value
		case inRodata:
			target = uint64(c.base) + uint64(textSize) + r.value
		}

		place := uint64(c.base) + uint64(r.at)

		switch r.typ {
		case rX86_64_PC32:
			if r.in == inExternal {
				throwFmt("pc-relative relocation to external %s", r.sym)
			}

			put32rel(c.text[r.at:], int64(target)+r.addend-int64(place))
		case rX86_64_GOTPCREL, rX86_64_GOTPCRELX, rX86_64_REX_GOTPCRELX:
			if r.in != inExternal {
				throwFmt("GOT relocation to a symbol of the object at %#x", r.at)
			}

			put32rel(c.text[r.at:], int64(c.base)+int64(gotAt+8*got[r.sym])+r.addend-int64(place))
		case rX86_64_64:
			if r.in == inExternal {
				target = uint64(c.resolve(r.sym, imports))
			}

			binary.LittleEndian.PutUint64(c.text[r.at:], target+uint64(r.addend))
		default:
			throwFmt("unsupported relocation type %d at %#x", r.typ, r.at)
		}
	}

	throw(syscall.Mprotect(c.text[:textSize], syscall.PROT_READ|syscall.PROT_EXEC))
	throw(syscall.Mprotect(c.text[textSize:], syscall.PROT_READ))

	if c.thunks != nil {
		throw(syscall.Mprotect(c.thunks[:cap(c.thunks)], syscall.PROT_READ|syscall.PROT_EXEC))
	}

	registerCode(name, c.base, uintptr(len(obj.text)), obj.meta.regions, false, obj.meta.files, obj.meta.lines)

	return c
}

func put32rel(at []byte, v int64) {
	if v != int64(int32(v)) {
		throwFmt("relocation out of range")
	}

	binary.LittleEndian.PutUint32(at, uint32(v))
}
