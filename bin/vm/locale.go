//go:build linux && amd64

package main

import (
	"unicode"
)

const (
	wcAlnum = 1 + iota
	wcAlpha
	wcBlank
	wcCntrl
	wcDigit
	wcGraph
	wcLower
	wcPrint
	wcPunct
	wcSpace
	wcUpper
	wcXdigit
)

func noBreakSpace(c rune) bool {
	return c == 0xa0 || c == 0x2007 || c == 0x202f
}

func towlower(c uint32) uint32 {
	if c > unicode.MaxRune {
		return c
	}

	return uint32(unicode.ToLower(rune(c)))
}

func towupper(c uint32) uint32 {
	if c > unicode.MaxRune {
		return c
	}

	return uint32(unicode.ToUpper(rune(c)))
}

func iswprint(c uint32) bool {
	switch {
	case c < 0xff:
		return (c+1)&0x7f >= 0x21
	case c < 0x2028 || c-0x202a < 0xd800-0x202a || c-0xe000 < 0xfff9-0xe000:
		return true
	case c-0xfffc > 0x10ffff-0xfffc || c&0xfffe == 0xfffe:
		return false
	}

	return true
}

func iswClass(class int, c uint32) bool {
	r := rune(c)
	valid := c <= unicode.MaxRune

	switch class {
	case wcAlnum:
		return c-'0' < 10 || valid && unicode.In(r, unicode.L, unicode.Nl, unicode.Other_Alphabetic)
	case wcAlpha:
		return valid && unicode.In(r, unicode.L, unicode.Nl, unicode.Other_Alphabetic)
	case wcBlank:
		return c == '\t' || valid && unicode.Is(unicode.Zs, r) && !noBreakSpace(r)
	case wcCntrl:
		return c < 32 || c-0x7f < 33 || c-0x2028 < 2 || c-0xfff9 < 3
	case wcDigit:
		return c-'0' < 10
	case wcGraph:
		return iswprint(c) && !iswClass(wcSpace, c)
	case wcLower:
		return towupper(c) != c || c == 0xdf
	case wcPrint:
		return iswprint(c)
	case wcPunct:
		return iswprint(c) && !iswClass(wcSpace, c) && !iswClass(wcAlnum, c)
	case wcSpace:
		return c != 0 && valid && unicode.Is(unicode.White_Space, r) && !noBreakSpace(r)
	case wcUpper:
		return towlower(c) != c
	case wcXdigit:
		return c-'0' < 10 || c|32-'a' < 6
	}

	return false
}

func ctype(class string, c uint32) uint32 {
	return bit(c < 0x80 && inClass(class, byte(c)))
}
