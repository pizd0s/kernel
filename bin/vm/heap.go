//go:build linux && amd64

package main

import (
	"math/bits"
	"slices"
)

var classSizes, classSlots, classPages = func() (sizes [smallClasses]uint32, slots [smallClasses]uint16, pages [smallClasses]uint8) {
	for i := range sizes {
		if i < 16 {
			sizes[i] = uint32(i+1) * 16
		} else {
			base := uint32(1) << (8 + (i-16)/4)

			sizes[i] = base + base/4*uint32((i-16)%4+1)
		}

		n := uint32(1)

		for n*heapPageSize < sizes[i] || n*heapPageSize%sizes[i] > n*heapPageSize/8 {
			n++
		}

		pages[i], slots[i] = uint8(n), uint16(n*heapPageSize/sizes[i])
	}

	return
}()

const (
	heapPageShift = 12
	heapPageSize  = 1 << heapPageShift
	segmentPages  = 32
	segmentSize   = segmentPages * heapPageSize
	smallLimit    = 16384
	smallClasses  = 40
	heapMax       = 1 << 31
	pageFree      = 0xff
	pageRun       = 0xfe
	pageTail      = 0xfd
	pageSlab      = 0xfc
	noPage        = -1
	segmentHops   = 8
)

type Heap struct {
	base     uint32
	top      uint32
	pages    []HeapPage
	segments []HeapSegment

	free    int32
	classes [smallClasses]HeapClass
}

type HeapPage struct {
	kind uint8

	listed bool
	live   uint16
	bump   uint16
	run    uint32
	prev   int32
	next   int32
	freed  []uint16
	used   []uint64
}

type HeapSegment struct {
	mask   uint32
	linked bool
	prev   int32
	next   int32
}

type HeapClass struct {
	current int32
	partial int32
}

func classOf(n uint32) int {
	if n <= 256 {
		return max(int(n+15)/16-1, 0)
	}

	e := bits.Len32(n-1) - 1

	return 16 + (e-8)*4 + int((n-1)>>(e-2)&3)
}

func pagesFor(n uint64) uint32 {
	return uint32((n + heapPageSize - 1) >> heapPageShift)
}

func newHeap(base uint32) *Heap {
	first := (uint64(base) + heapPageSize - 1) &^ (heapPageSize - 1)
	h := &Heap{base: uint32(first), top: uint32(first), free: noPage}

	for i := range h.classes {
		h.classes[i] = HeapClass{noPage, noPage}
	}

	return h
}

func (h *Heap) address(page int32) uint32 {
	return h.base + uint32(page)<<heapPageShift
}

func (h *Heap) link(s int32) {
	seg := &h.segments[s]

	seg.linked, seg.prev, seg.next = true, noPage, h.free

	if h.free != noPage {
		h.segments[h.free].prev = s
	}

	h.free = s
}

func (h *Heap) unlink(s int32) {
	seg := &h.segments[s]

	seg.linked = false

	if seg.prev != noPage {
		h.segments[seg.prev].next = seg.next
	} else {
		h.free = seg.next
	}

	if seg.next != noPage {
		h.segments[seg.next].prev = seg.prev
	}
}

func (h *Heap) releasePages(first int32, count uint32) {
	for q := first; q < first+int32(count); q++ {
		h.pages[q].kind = pageFree

		s := q / segmentPages

		h.segments[s].mask |= 1 << (q % segmentPages)

		if !h.segments[s].linked {
			h.link(s)
		}
	}
}

func (h *Heap) markRun(first int32, count uint32) {
	h.pages[first].kind, h.pages[first].run = pageRun, count

	for q := first + 1; q < first+int32(count); q++ {
		h.pages[q].kind = pageTail
	}
}

func (h *Heap) list(cls int, page int32) {
	pg, c := &h.pages[page], &h.classes[cls]

	pg.listed, pg.prev, pg.next = true, noPage, c.partial

	if c.partial != noPage {
		h.pages[c.partial].prev = page
	}

	c.partial = page
}

func (h *Heap) unlist(cls int, page int32) {
	pg, c := &h.pages[page], &h.classes[cls]

	pg.listed = false

	if pg.prev != noPage {
		h.pages[pg.prev].next = pg.next
	} else {
		c.partial = pg.next
	}

	if pg.next != noPage {
		h.pages[pg.next].prev = pg.prev
	}
}

func pageRound(n uint32) uint64 {
	return (uint64(n) + 4095) &^ 4095
}

func (h *Heap) clone() *Heap {
	if h == nil {
		return nil
	}

	c := *h

	c.pages = slices.Clone(h.pages)

	for i := range c.pages {
		c.pages[i].freed = slices.Clone(c.pages[i].freed)
		c.pages[i].used = slices.Clone(c.pages[i].used)
	}

	c.segments = slices.Clone(h.segments)

	return &c
}

func alignment(align uint32, pointer bool) bool {
	return align != 0 && align&(align-1) == 0 && (!pointer || align%4 == 0)
}
