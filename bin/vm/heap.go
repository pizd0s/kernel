//go:build linux && amd64

package main

import (
	"math/bits"
	"slices"
)

// The heap is the memory from __heap_base up, and it holds the program's
// data alone: what the allocator knows of it, which block is in use and how
// large, is the kernel's, in Go, where the program can neither read nor
// change it, and every pointer it frees is checked against it. The memory
// is pages of 64 KiB, the wasm page, in segments of 32 the heap grows by,
// as trustme's allocator has them. A block of up to 16 KiB is a slot of a
// page of its size class: the class hands out its current page's slots,
// those freed last first, then the ones the page never handed out, then
// takes a page with free slots or a free page, and a page goes back to its
// segment when its last slot is freed. A larger block is a run of pages.
// A segment's mask has a bit for each free page. All of the heap is in
// memory, so a fork copies it with the process, and the child gets a copy
// of what the kernel knows of it.

const (
	heapPageShift = 16
	heapPageSize  = 1 << heapPageShift
	segmentPages  = 32
	segmentSize   = segmentPages * heapPageSize
	smallLimit    = 16384
	smallClasses  = 40
	// heapMax is the largest allocation.
	heapMax = 1 << 31
	// A page's kind is its class, or one of these.
	pageFree = 0xff
	pageRun  = 0xfe
	pageTail = 0xfd
	noPage   = -1
	// segmentHops is how many segments with free pages a run looks
	// through before the heap grows.
	segmentHops = 8
)

// Heap is what the kernel knows of a process's heap.
type Heap struct {
	// base is the address of the first page, top the end of the pages
	// ever handed out, the heap's end for fork.
	base     uint32
	top      uint32
	pages    []heapPage
	segments []heapSegment
	// free is the first segment with free pages.
	free    int32
	classes [smallClasses]heapClass
}

// heapPage is a page: of a class, the slots in use and their number, the
// first slot it never handed out and those freed, the last freed last; of
// a run, its first page has the run's length.
type heapPage struct {
	kind uint8
	// listed is whether the page is in its class's list of pages with
	// free slots, by prev and next.
	listed bool
	live   uint16
	bump   uint16
	run    uint32
	prev   int32
	next   int32
	freed  []uint16
	used   []uint64
}

// heapSegment is 32 pages: a bit of mask for each free one, and its place
// in the list of segments with free pages.
type heapSegment struct {
	mask   uint32
	linked bool
	prev   int32
	next   int32
}

// heapClass is a size class: the page it hands out slots of and the first
// of its other pages with free slots.
type heapClass struct {
	current int32
	partial int32
}

// classSizes and classSlots are each class's block size and the blocks a
// page has of it.
var classSizes, classSlots = func() (sizes [smallClasses]uint32, slots [smallClasses]uint16) {
	for i := range sizes {
		if i < 16 {
			sizes[i] = uint32(i+1) * 16
		} else {
			base := uint32(1) << (8 + (i-16)/4)
			sizes[i] = base + base/4*uint32((i-16)%4+1)
		}

		slots[i] = uint16(heapPageSize / sizes[i])
	}

	return
}()

// classOf is the class of a block of n bytes, at most smallLimit: steps of
// 16 up to 256, then four per power of two.
func classOf(n uint32) int {
	if n <= 256 {
		return max(int(n+15)/16-1, 0)
	}

	e := bits.Len32(n-1) - 1

	return 16 + (e-8)*4 + int((n-1)>>(e-2)&3)
}

// pagesFor is how many pages n bytes take.
func pagesFor(n uint64) uint32 {
	return uint32((n + heapPageSize - 1) >> heapPageShift)
}

// newHeap is an empty heap whose first page is the first at base or after
// it.
func newHeap(base uint32) *Heap {
	first := (uint64(base) + heapPageSize - 1) &^ (heapPageSize - 1)
	h := &Heap{base: uint32(first), top: uint32(first), free: noPage}

	for i := range h.classes {
		h.classes[i] = heapClass{noPage, noPage}
	}

	return h
}

// address is the address of a page.
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

// releasePages gives count pages from first back to their segments.
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

// markRun makes count pages from first a run.
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

// clone is a copy of what the kernel knows of the heap, for a child of
// fork.
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

// alignment checks an alignment: a power of two, for posix_memalign also
// a multiple of a pointer's size.
func alignment(align uint32, pointer bool) bool {
	return align != 0 && align&(align-1) == 0 && (!pointer || align%4 == 0)
}
