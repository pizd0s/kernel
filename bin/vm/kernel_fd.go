//go:build linux && amd64

package main

import (
	"math/bits"
	"slices"
)

// fdTable is a process's descriptors by number: a slot each, by value,
// and the numbers in use, in a tree (fdTree) that gives the lowest free
// one, which open and the rest take, as POSIX has it.
type fdTable struct {
	slots []Descriptor
	used  fdTree
}

// size is past the last slot: the numbers to look at for the open ones.
func (t *fdTable) size() int {
	return len(t.slots)
}

// at is the descriptor of a number, ok false for none.
func (t *fdTable) at(fd int) (Descriptor, bool) {
	if fd < 0 || fd >= len(t.slots) || t.slots[fd].obj == nil {
		return Descriptor{}, false
	}

	return t.slots[fd], true
}

// put makes a free number refer to obj, which takes the reference.
func (t *fdTable) put(fd int, obj File, cloexec bool) {
	if fd >= len(t.slots) {
		t.slots = append(t.slots, make([]Descriptor, fd+1-len(t.slots))...)
	}

	t.slots[fd] = Descriptor{obj: obj, cloexec: cloexec}
	t.used.set(fd)
}

// take frees a number and gives what it referred to, nil for nothing.
func (t *fdTable) take(fd int) File {
	if fd < 0 || fd >= len(t.slots) || t.slots[fd].obj == nil {
		return nil
	}

	obj := t.slots[fd].obj
	t.slots[fd] = Descriptor{}
	t.used.clear(fd)

	return obj
}

// lowest is the lowest free number from min.
func (t *fdTable) lowest(min int) int {
	return t.used.lowestFree(min)
}

// fdTree is the numbers in use: a tree of bitmaps 64 ways wide, in flat
// arrays. levels[0] has a bit for each number, set while it is in use;
// each level above has a bit for each word of the one below, set while
// that word is full; the top level is one word. The lowest free number
// from some is a walk up from its word to the first one with room, and
// down that one's free side.
type fdTree struct {
	levels [][]uint64
}

const fullWord = ^uint64(0)

// set marks a number in use.
func (t *fdTree) set(n int) {
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

// clear marks a number free.
func (t *fdTree) clear(n int) {
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

// lowestFree is the lowest number from min not in use: past the tree
// every number is free.
func (t *fdTree) lowestFree(min int) int {
	pos, level := min, 0

	// Up: the first bit not set from pos in its word, else the next word,
	// a bit of the level above.
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

	// Down: the first word with room below a bit not set, its first free
	// bit; a word past the level is free from its first number.
	for level > 0 {
		level--

		if pos >= len(t.levels[level]) {
			return pos << (6 * (level + 1))
		}

		pos = pos<<6 | bits.TrailingZeros64(^t.levels[level][pos])
	}

	return pos
}

// grow makes room for a number: the bottom twice as long at least, the
// levels above it made again from it, as they must tell its full words.
func (t *fdTree) grow(n int) {
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

// clone is a copy, a child's of fork.
func (t *fdTree) clone() fdTree {
	c := fdTree{levels: make([][]uint64, len(t.levels))}

	for i, words := range t.levels {
		c.levels[i] = slices.Clone(words)
	}

	return c
}
