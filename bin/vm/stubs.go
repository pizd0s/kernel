//go:build linux && amd64

package main

import "unsafe"

func morestackStub() uintptr

func interruptStub() uintptr

func landStub() uintptr

func framesStub() uintptr

func resumeStub() uintptr

func resumeReturnStub() uintptr

func asanRangeStub() uintptr

func moveMemory(dst, src []byte) {
	if len(src) > len(dst) {
		throwFmt("a copy of %d bytes into %d", len(src), len(dst))
	}

	if len(src) == 0 {
		return
	}

	strMove(strFunction(symStr+"memmove"), unsafe.Pointer(&dst[0]), unsafe.Pointer(&src[0]), uintptr(len(src)))
}

func strCall0(fn uintptr) uintptr

func strCall3(fn, a, b, c uintptr) uintptr

//go:noescape
func strMove(fn uintptr, dst, src unsafe.Pointer, n uintptr)
