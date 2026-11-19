//go:build linux && amd64

package main

import (
	"math"
	"syscall"
)

const pageSize = 65536

// LinearMemory is reserved at its maximum: it grows in place.
type LinearMemory struct {
	// data is in its instance's region: boundSlack bytes before it are
	// accessible, and past the maximum boundSlack bytes and a zero byte.
	data     []byte
	pages    uint64
	max      uint64
	tailUsed bool
	// shadow is the shadow map of an asan object's memory, a byte for each
	// byte of every 32-bit address and boundSlack past them, so an access
	// of a checked address with a static offset within boundSlack indexes
	// it without a check of its own (aot.cpp lowererAsanAddress); nil for
	// other objects. The kernel keeps it (process.go).
	shadow []byte
	// owner is the instance whose header describes it, the only one.
	owner *Instance
}

// shadowSize is the size of a shadow map.
const shadowSize = 1<<32 + 2*boundSlack

func mmap(size int, prot int) []byte {
	if chaosFail("mmap") {
		throw(syscall.ENOMEM)
	}

	return throw2(syscall.Mmap(-1, 0, size, prot, syscall.MAP_PRIVATE|syscall.MAP_ANON|syscall.MAP_NORESERVE))
}

// newRegion maps an instance and its memory, of at most most pages, if
// it has one: the instance's block at the start, the memory
// instanceDistance past it. The code keeps the memory's base in a register
// and finds the instance that far below it (aot.cpp instanceAt).
//
// The boundSlack bytes before the memory are accessible, and so are
// boundSlack bytes past its maximum: an access derived from a checked
// address reaches at most that far either way (aot.cpp lowererAddress).
// Then a byte that stays zero: past the accessible end the memory is zero
// at every size, as nothing writes there and growth clears the old tail,
// and lib/str's strlen, which the code calls before it checks where a
// string ended (lowererStrLength), stops there at the latest.
func newRegion(memory *MemoryMeta) []byte {
	size := uint64(instanceDistance)

	if memory != nil {
		if memory.max > 65536 || memory.min > memory.max {
			throwFmt("memory of %d..%d pages", memory.min, memory.max)
		}

		size += memory.max*pageSize + boundSlack + 1
	}

	return mmap(int(size), syscall.PROT_READ|syscall.PROT_WRITE)
}

// newMemory is the memory of data, reserved to most pages, with least.
func newMemory(data []byte, least, most uint64, shadow bool) *LinearMemory {
	m := &LinearMemory{data: data, pages: least, max: most}

	if shadow {
		// Zero, inaccessible everywhere, until the kernel marks what the
		// program may access.
		m.shadow = mmap(shadowSize, syscall.PROT_READ|syscall.PROT_WRITE)
	}

	return m
}

// grow adds pages and returns the old size, or MaxUint32 past the maximum.
func (m *LinearMemory) grow(delta uint32) uint32 {
	old := m.pages

	if old+uint64(delta) > m.max {
		return math.MaxUint32
	}

	if delta != 0 && m.tailUsed {
		// The old accessible tail becomes part of newly grown memory.
		// Writes to it must not survive the zero initialization of growth.
		clear(m.data[old*pageSize : old*pageSize+boundSlack])
	}

	m.pages += uint64(delta)
	m.owner.setMemory()

	return uint32(old)
}
