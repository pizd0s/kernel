//go:build linux && amd64

package main

import (
	"fmt"
)

// Trap is the panic of a trapping instance, with the stack of the trap
// (trace.go). An asan trap tells the access the shadow map refused: its
// address, width and kind.
type Trap struct {
	code  uint32
	addr  uint64
	width uint8
	kind  uint8
	stack []uintptr
}

// newTrap is a trap of code with the stack from the caller up, less skip
// frames above the caller.
func newTrap(code uint32, skip int) *Trap {
	return &Trap{code: code, stack: callers(skip + 1)}
}

// trapOf runs f and returns the code of its Wasm trap, or -1.
func trapOf(f func()) int {
	if trap := tryAs[*Trap](f); trap != nil {
		return int(trap.code)
	}

	return -1
}

var trapNames = map[uint32]string{
	trapUnreachable: "unreachable", trapMemoryOOB: "out of bounds memory access", trapIntOverflow: "integer overflow",
	trapDivByZero: "integer divide by zero", trapConversion: "invalid conversion to integer",
	trapIndirectType: "indirect call type mismatch", trapUndefinedElem: "undefined element",
	trapUninitElem: "uninitialized element", trapStack: "call stack exhausted", trapTableOOB: "out of bounds table access",
	trapNullRef: "null reference", trapAsan: "inaccessible memory",
}

func (t *Trap) Error() string {
	if t.code == trapAsan {
		return "wasm trap: " + trapNames[t.code] + ": " + t.access()
	}

	if name, ok := trapNames[t.code]; ok {
		return "wasm trap: " + name
	}

	return fmt.Sprintf("wasm trap %d", t.code)
}

// access describes the access of an asan trap.
func (t *Trap) access() string {
	switch t.kind {
	case asanLoad:
		return fmt.Sprintf("%d-byte load at %#x", t.width, t.addr)
	case asanStore:
		return fmt.Sprintf("%d-byte store at %#x", t.width, t.addr)
	}

	return fmt.Sprintf("access of a range at %#x", t.addr)
}
