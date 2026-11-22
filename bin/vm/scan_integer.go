//go:build linux && amd64

package main

import "math"

func integerDigit(c byte, base uint64) (uint64, bool) {
	var d uint64

	switch {
	case c >= '0' && c <= '9':
		d = uint64(c - '0')
	case c|0x20 >= 'a' && c|0x20 <= 'z':
		d = uint64(c|0x20-'a') + 10
	default:
		return 0, false
	}

	return d, d < base
}

func scanInteger(s []byte, base int) (uint64, bool, int, bool) {
	at, negative := 0, false

	for at < len(s) && floatSpace(s[at]) {
		at++
	}

	if at < len(s) && (s[at] == '+' || s[at] == '-') {
		negative = s[at] == '-'
		at++
	}

	b := uint64(base)

	if (b == 0 || b == 16) && at+2 < len(s) && s[at] == '0' && s[at+1]|0x20 == 'x' {
		if _, ok := integerDigit(s[at+2], 16); ok {
			at, b = at+2, 16
		}
	}

	if b == 0 {
		b = 10

		if at < len(s) && s[at] == '0' {
			b = 8
		}
	}

	start, v, over := at, uint64(0), false

	for ; at < len(s); at++ {
		d, ok := integerDigit(s[at], b)

		if !ok {
			break
		}

		if v > (math.MaxUint64-d)/b {
			over = true
		}

		v = v*b + d
	}

	if at == start {
		return 0, false, 0, false
	}

	return v, negative, at, over
}

func scanWideInteger(s []uint32, base int) (uint64, bool, int, bool) {
	at := 0

	for at < len(s) && iswClass(wcSpace, s[at]) {
		at++
	}

	narrow := make([]byte, len(s)-at)

	for i, c := range s[at:] {
		narrow[i] = 0xff

		if c < 0x80 {
			narrow[i] = byte(c)
		}
	}

	v, negative, n, over := scanInteger(narrow, base)

	if n == 0 {
		return 0, false, 0, false
	}

	return v, negative, at + n, over
}

func integerIn(v uint64, negative, over, signed bool, bits int) (uint64, bool) {
	top := ^uint64(0) >> (64 - bits)

	if signed {
		top >>= 1

		switch {
		case negative && (over || v > top+1):
			return -(top + 1) & (^uint64(0) >> (64 - bits)), true
		case negative:
			return -v & (^uint64(0) >> (64 - bits)), false
		case over || v > top:
			return top, true
		}

		return v, false
	}

	if over || v > top {
		return top, true
	}

	if negative {
		return -v & top, false
	}

	return v, false
}
