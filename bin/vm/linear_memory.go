//go:build linux && amd64

package main

import (
	"math"
	"syscall"
)

const (
	pageSize   = 65536
	shadowSize = 1<<32 + 2*boundSlack
)

type LinearMemory struct {
	data     []byte
	pages    uint64
	max      uint64
	tailUsed bool

	shadow []byte

	owner *Instance
}

func mmap(size int, prot int) []byte {
	if chaosFail("mmap") {
		throw(eNomem)
	}

	return throw2(syscall.Mmap(-1, 0, size, prot, syscall.MAP_PRIVATE|syscall.MAP_ANON|syscall.MAP_NORESERVE))
}

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

func newMemory(data []byte, least, most uint64, shadow bool) *LinearMemory {
	m := &LinearMemory{data: data, pages: least, max: most}

	if shadow {
		m.shadow = mmap(shadowSize, syscall.PROT_READ|syscall.PROT_WRITE)
	}

	return m
}

func (m *LinearMemory) grow(delta uint32) uint32 {
	old := m.pages

	if old+uint64(delta) > m.max {
		return math.MaxUint32
	}

	if delta != 0 && m.tailUsed {
		clear(m.data[old*pageSize : old*pageSize+boundSlack])
	}

	m.pages += uint64(delta)
	m.owner.setMemory()

	return uint32(old)
}
