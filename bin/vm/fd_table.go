//go:build linux && amd64

package main

import (
	"math/bits"
	"slices"
)

const fullWord = ^uint64(0)

type FdTable struct {
	slots []Descriptor
	used  FdTree
}

func (t *FdTable) size() int {
	return len(t.slots)
}

func (t *FdTable) at(fd int) (Descriptor, bool) {
	if fd < 0 || fd >= len(t.slots) || t.slots[fd].obj == nil {
		return Descriptor{}, false
	}

	return t.slots[fd], true
}

func (t *FdTable) put(fd int, obj Object, cloexec bool) {
	if fd >= len(t.slots) {
		t.slots = append(t.slots, make([]Descriptor, fd+1-len(t.slots))...)
	}

	t.slots[fd] = Descriptor{obj: obj, cloexec: cloexec}
	t.used.set(fd)
}

func (t *FdTable) take(fd int) Object {
	if fd < 0 || fd >= len(t.slots) || t.slots[fd].obj == nil {
		return nil
	}

	obj := t.slots[fd].obj

	t.slots[fd] = Descriptor{}
	t.used.clear(fd)

	return obj
}

func (t *FdTable) lowest(min int) int {
	return t.used.lowestFree(min)
}

type FdTree struct {
	levels [][]uint64
}

func (t *FdTree) set(n int) {
	t.grow(n)

	for _, words := range t.levels {
		w := n >> 6

		words[w] |= 1 << uint(n&63)

		if words[w] != fullWord {
			return
		}

		n = w
	}
}

func (t *FdTree) clear(n int) {
	for _, words := range t.levels {
		w := n >> 6

		if w >= len(words) {
			return
		}

		full := words[w] == fullWord

		words[w] &^= 1 << uint(n&63)

		if !full {
			return
		}

		n = w
	}
}

func (t *FdTree) lowestFree(min int) int {
	pos, level := min, 0

	for {
		if level == len(t.levels) || pos>>6 >= len(t.levels[level]) {
			return pos << (6 * level)
		}

		w := pos >> 6
		room := ^t.levels[level][w] &^ (1<<uint(pos&63) - 1)

		if room != 0 {
			pos = w<<6 | bits.TrailingZeros64(room)

			break
		}

		pos, level = w+1, level+1
	}

	for level > 0 {
		level--

		if pos >= len(t.levels[level]) {
			return pos << (6 * (level + 1))
		}

		pos = pos<<6 | bits.TrailingZeros64(^t.levels[level][pos])
	}

	return pos
}

func (t *FdTree) grow(n int) {
	words := n>>6 + 1

	if len(t.levels) > 0 && len(t.levels[0]) >= words {
		return
	}

	var bottom []uint64

	if len(t.levels) > 0 {
		bottom = t.levels[0]
	}

	grown := make([]uint64, max(words, 2*len(bottom), 1))

	copy(grown, bottom)
	t.levels = append(t.levels[:0], grown)

	for below := grown; len(below) > 1; {
		above := make([]uint64, (len(below)+63)>>6)

		for i, word := range below {
			if word == fullWord {
				above[i>>6] |= 1 << uint(i&63)
			}
		}

		t.levels = append(t.levels, above)
		below = above
	}
}

func (t *FdTree) clone() FdTree {
	c := FdTree{levels: make([][]uint64, len(t.levels))}

	for i, words := range t.levels {
		c.levels[i] = slices.Clone(words)
	}

	return c
}

type Descriptor struct {
	obj     Object
	cloexec bool
}
