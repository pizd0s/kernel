//go:build linux && amd64

package main

import (
	"math"
	"runtime"
	"sync"
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

	gate    sync.Mutex
	readers int
	moving  bool
}

func mmap(size int, prot int) []byte {
	if chaosFail("mmap") {
		throw(ENOMEM)
	}

	return throw2(syscall.Mmap(-1, 0, size, prot, syscall.MAP_PRIVATE|syscall.MAP_ANON|syscall.MAP_NORESERVE))
}

func distanceOf(size uint32) uint64 {
	return (uint64(size) + 8 + 4095) &^ 4095
}

func profileBytes(meta *Meta) uint64 {
	if meta.memory == nil || meta.memory.profile == 0 {
		return 0
	}

	return (uint64(len(meta.memory.sites))*profileCounterBytes + 4095) &^ 4095
}

func newMemory(data []byte, least, most uint64, shadow bool) *LinearMemory {
	m := &LinearMemory{data: data, pages: least, max: most}

	if shadow {
		m.shadow = mmap(shadowSize, syscall.PROT_READ|syscall.PROT_WRITE)
	}

	return m
}

func (m *LinearMemory) bytes() []byte {
	if m.owner != nil {
		return m.owner.mem
	}

	return m.data
}

func (m *LinearMemory) enter() {
	m.gate.Lock()

	for m.moving {
		m.gate.Unlock()
		runtime.Gosched()
		m.gate.Lock()
	}

	m.readers++
	m.gate.Unlock()
}

func (m *LinearMemory) leave() {
	m.gate.Lock()
	m.readers--
	m.gate.Unlock()
}

func (m *LinearMemory) shut() {
	for {
		m.gate.Lock()

		if m.readers == 0 {
			m.moving = true
			m.gate.Unlock()

			return
		}

		m.gate.Unlock()
		runtime.Gosched()
	}
}

func (m *LinearMemory) open() {
	m.gate.Lock()
	m.moving = false
	m.gate.Unlock()
}

func (m *LinearMemory) grow(delta uint32, by *Process) uint32 {
	old := m.pages

	if old+uint64(delta) > m.max || !m.owner.reach(old+uint64(delta), by, true) {
		if q := m.owner.proc; q != nil {
			q.starved = true
		}

		return math.MaxUint32
	}

	if delta != 0 && m.tailUsed {
		clear(m.data[old*pageSize : old*pageSize+boundSlack])
	}

	m.pages += uint64(delta)
	m.owner.setMemory()

	return uint32(old)
}
