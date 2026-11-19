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

// Code is an object linked in memory: the text, then .rodata and the GOT.
// The code reaches its instance through the context, and the Go functions
// of the GOT take the instance first, so every instance of the object runs
// the same code.
type Code struct {
	name string
	obj  *ElfObject
	text []byte
	base uintptr
	// keep holds the Go function values the code calls.
	keep []any
}

// bindContext returns code of Go's convention that calls code with ctx as
// its first argument, the others as they are: an export thunk of another
// instance with that instance, say. The stub lives as long as the process.
//
//	movabs $ctx, %rax
//	jmp *target(%rip)
//	target: .quad code
func bindContext(code, ctx uintptr) uintptr {
	stub := mmap(4096, syscall.PROT_READ|syscall.PROT_WRITE)
	stub = append(stub[:0], 0x48, 0xb8)
	stub = binary.LittleEndian.AppendUint64(stub, uint64(ctx))
	stub = append(stub, 0xff, 0x25, 0, 0, 0, 0)
	stub = binary.LittleEndian.AppendUint64(stub, uint64(code))
	throw(syscall.Mprotect(stub[:4096], syscall.PROT_READ|syscall.PROT_EXEC))

	return uintptr(unsafe.Pointer(unsafe.SliceData(stub)))
}

func helperTrap(ctx uintptr, code uint32) {
	panic(newTrap(code, 1))
}

// helperTrapAt is the trap of an asan check: the access's address, and its
// width in the low byte of info with its kind above.
func helperTrapAt(ctx uintptr, code uint32, addr uint64, info uint32) {
	t := newTrap(code, 1)
	t.addr, t.width, t.kind = addr, uint8(info), uint8(info>>8)
	panic(t)
}

// helperMemoryGrow is memory.grow; its chaos point is a program's, not
// the compiler's, which the kernel runs for exec.
func helperMemoryGrow(ctx uintptr, delta uint32) uint32 {
	i := instanceAt(ctx)

	if _, compiler := i.host.(*CompilerIO); !compiler && i.proc.chaosFail("memory-grow") {
		return math.MaxUint32
	}

	return i.memory.grow(delta)
}

// interruptible is a host whose code Go interrupts (lib/goruntime).
type interruptible interface {
	interrupt()
}

// helperInterrupt is the call Go injects into interrupted code: the host
// does what it asked the interruption for.
func helperInterrupt(ctx uintptr) {
	if h, ok := instanceAt(ctx).host.(interruptible); ok {
		h.interrupt()
	}
}

// Tables do not grow.
func helperTableGrow(ctx uintptr, table, delta uint32, init uintptr) uint32 {
	return math.MaxUint32
}

// funcval returns a Go function value's closure: the address of its code
// pointer, followed by what it captured. Function values are pointer
// shaped, held in an interface's data word. Its argument is a fixed helper
// or an import whose signature resolve checked before keepFunc.
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

	// parseTypes admits these six types and v128 at function boundaries.
	throwFmt("unsupported value type v128")

	return nil
}

// goFuncType is the Go type of a function of the module at the boundary:
// func(instance uintptr, params...) (results...).
func goFuncType(params, results []ValType) reflect.Type {
	in := []reflect.Type{reflect.TypeFor[uintptr]()}

	for _, t := range params {
		in = append(in, goType(t))
	}

	var out []reflect.Type

	for _, t := range results {
		out = append(out, goType(t))
	}

	return reflect.FuncOf(in, out, false)
}

// keepFunc returns the code address of a Go function, the GOT's entry
// for it: the code calls it as Go does, with the instance in AX. A
// closure would miss its context; none of the VM's functions is one.
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

		if want := goFuncType(imp.params, imp.results); reflect.TypeOf(f) != want {
			throwFmt("import %s.%s is %T, not %s", imp.module, imp.name, f, want)
		}

		return c.keepFunc(f)
	}

	throwFmt("unresolved symbol %s", sym)

	return 0
}

// link links an object in place: the text, executable, then on pages of
// their own .rodata and a GOT entry per external symbol, read-only. It
// registers the code with Go; code lives as long as the process.
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
	registerCode(name, c.base, uintptr(len(obj.text)), obj.meta.regions, false, obj.meta.files, obj.meta.lines)

	return c
}

func put32rel(at []byte, v int64) {
	if v != int64(int32(v)) {
		throwFmt("relocation out of range")
	}

	binary.LittleEndian.PutUint32(at, uint32(v))
}
