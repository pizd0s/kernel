//go:build linux && amd64

package main

func strtoulPart(s string) (uint64, bool) {
	base, digits := uint64(10), s

	switch {
	case len(s) > 2 && (s[:2] == "0x" || s[:2] == "0X"):
		base, digits = 16, s[2:]
	case len(s) > 1 && s[0] == '0':
		base, digits = 8, s[1:]
	}

	v := uint64(0)

	for _, c := range []byte(digits) {
		d := uint64(16)

		switch {
		case c >= '0' && c <= '9':
			d = uint64(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint64(c-'A') + 10
		}

		if d >= base {
			return 0, false
		}

		if v > (0xffffffff-d)/base {
			return 0, false
		}

		v = v*base + d
	}

	return v, true
}

func inetAton(s string) ([4]byte, bool) {
	var parts [4]uint64

	n := 0

	for {
		if n == 4 || s == "" || s[0] < '0' || s[0] > '9' {
			return [4]byte{}, false
		}

		end := 1

		for end < len(s) && s[end] != '.' {
			end++
		}

		v, ok := strtoulPart(s[:end])

		if !ok {
			return [4]byte{}, false
		}

		parts[n] = v
		n++

		if end == len(s) {
			break
		}

		s = s[end+1:]
	}

	var out [4]byte

	last := parts[n-1]
	room := 4 - (n - 1)

	if room < 4 && last>>(8*room) != 0 || room == 4 && last > 0xffffffff {
		return [4]byte{}, false
	}

	for i := range n - 1 {
		if parts[i] > 255 {
			return [4]byte{}, false
		}

		out[i] = byte(parts[i])
	}

	for i := 0; i < room; i++ {
		out[3-i] = byte(last >> (8 * i))
	}

	return out, true
}
