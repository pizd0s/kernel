package main

import (
	"fmt"
)

var trapNames = map[uint32]string{
	trapUnreachable: "unreachable", trapMemoryOOB: "out of bounds memory access", trapIntOverflow: "integer overflow",
	trapDivByZero: "integer divide by zero", trapConversion: "invalid conversion to integer",
	trapIndirectType: "indirect call type mismatch", trapUndefinedElem: "undefined element",
	trapUninitElem: "uninitialized element", trapStack: "call stack exhausted", trapTableOOB: "out of bounds table access",
	trapNullRef: "null reference", trapAsan: "inaccessible memory",
}

type Trap struct {
	code  uint32
	addr  uint64
	width uint8
	kind  uint8
	stack []uintptr
}

func newTrap(code uint32, skip int) *Trap {
	return &Trap{code: code, stack: callers(skip + 1)}
}

func trapOf(f func()) int {
	if trap := tryAs[*Trap](f); trap != nil {
		return int(trap.code)
	}

	return -1
}

func (t *Trap) Error() string {
	return t.error()
}

func (t *Trap) error() string {
	if t.code == trapAsan {
		return "wasm trap: " + trapNames[t.code] + ": " + t.access()
	}

	if name, ok := trapNames[t.code]; ok {
		return "wasm trap: " + name
	}

	return fmt.Sprintf("wasm trap %d", t.code)
}

func (t *Trap) access() string {
	switch t.kind {
	case asanLoad:
		return fmt.Sprintf("%d-byte load at %#x", t.width, t.addr)
	case asanStore:
		return fmt.Sprintf("%d-byte store at %#x", t.width, t.addr)
	}

	return fmt.Sprintf("access of a range at %#x", t.addr)
}
