//go:build linux && amd64

package main

import (
	"bytes"
	"fmt"
	"math/bits"
	"syscall"
)

// String and character functions of the C locale. They are not system
// calls: they leave errno and pending signals alone.

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}

	return c
}

func lowerBytes(s []byte) []byte {
	out := make([]byte, len(s))

	for i, c := range s {
		out[i] = lower(c)
	}

	return out
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// cmp is a C comparison result.
func cmp(a, b int) uint32 {
	return uint32(int32(a - b))
}

// divide is C's division of two ints: as wasm's, it traps by zero and on
// overflow.
func divide(a, b, minimum int64) (int64, int64) {
	if b == 0 {
		panic(newTrap(trapDivByZero, 0))
	}

	if a == minimum && b == -1 {
		panic(newTrap(trapIntOverflow, 0))
	}

	return a / b, a % b
}

func ffs(x uint64) uint32 {
	if x == 0 {
		return 0
	}

	return uint32(bits.TrailingZeros64(x) + 1)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}

	return s
}

func capitalize(s string) string {
	if s != "" && s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}

	return s
}

func errorMessage(e syscall.Errno) string {
	if e == 0 {
		return "No error information"
	}

	s := e.Error()

	if len(s) > 6 && s[:6] == "errno " {
		return fmt.Sprintf("Unknown error %d", int(e))
	}

	return capitalize(s)
}

func signalMessage(sig int) string {
	if sig >= 34 && sig <= 64 {
		return fmt.Sprintf("Real-time signal %d", sig-34)
	}

	s := syscall.Signal(sig).String()

	if len(s) > 7 && s[:7] == "signal " {
		return fmt.Sprintf("Unknown signal %d", sig)
	}

	return capitalize(s)
}

// strverscmp is glibc's: digit runs compare as numbers, those with leading
// zeros as fractions.
func strverscmp(a, b []byte) int {
	const (
		sN = 0
		sI = 3
		sF = 6
		sZ = 9
		rC = 2
		rL = 3
	)
	next := [...]int{sN, sI, sZ, sN, sI, sI, sN, sF, sF, sN, sF, sZ}
	result := [...]int{
		rC, rC, rC, rC, rL, rC, rC, rC, rC,
		rC, -1, -1, 1, rL, rL, 1, rL, rL,
		rC, rC, rC, rC, rC, rC, rC, rC, rC,
		rC, 1, 1, -1, rC, rC, -1, rC, rC,
	}
	at := func(s []byte, i int) byte {
		if i < len(s) {
			return s[i]
		}

		return 0
	}
	kind := func(c byte) int {
		if c == '0' {
			return 2
		}

		if isDigit(c) {
			return 1
		}

		return 0
	}
	i := 0
	c1, c2 := at(a, 0), at(b, 0)
	state := sN + kind(c1)

	for c1 == c2 {
		if c1 == 0 {
			return 0
		}

		state = next[state]
		i++
		c1, c2 = at(a, i), at(b, i)
		state += kind(c1)
	}

	diff := int(c1) - int(c2)

	switch r := result[state*3+kind(c2)]; r {
	case rC:
		return diff
	case rL:
		j := i + 1

		for ; isDigit(at(a, j)); j++ {
			if !isDigit(at(b, j)) {
				return 1
			}
		}

		if isDigit(at(b, j)) {
			return -1
		}

		return diff
	default:
		return r
	}
}

const (
	fnmPathname   = 0x1
	fnmNoescape   = 0x2
	fnmPeriod     = 0x4
	fnmLeadingDir = 0x8
	fnmCasefold   = 0x10
)

// fnmatch matches a string against a shell pattern; start tells that s
// begins where a leading period must be matched explicitly.
func fnmatch(pat, s []byte, flags int, start bool) bool {
	hidden := func() bool {
		return start && flags&fnmPeriod != 0 && len(s) > 0 && s[0] == '.'
	}

	for len(pat) > 0 {
		var c byte

		switch pat[0] {
		case '*':
			for len(pat) > 0 && pat[0] == '*' {
				pat = pat[1:]
			}

			if hidden() {
				return false
			}

			for i := 0; i <= len(s); i++ {
				if fnmatch(pat, s[i:], flags, false) {
					return true
				}

				if i < len(s) && flags&fnmPathname != 0 && s[i] == '/' {
					return false
				}
			}

			return false
		case '?':
			if len(s) == 0 || flags&fnmPathname != 0 && s[0] == '/' || hidden() {
				return false
			}

			c, pat = s[0], pat[1:]
		case '[':
			if len(s) == 0 || flags&fnmPathname != 0 && s[0] == '/' || hidden() {
				return false
			}

			n, ok := bracket(pat, s[0], flags)

			if n == 0 {
				if s[0] != '[' {
					return false
				}

				pat = pat[1:]
			} else if !ok {
				return false
			} else {
				pat = pat[n:]
			}

			c = s[0]
		default:
			lit := pat[0]
			pat = pat[1:]

			if lit == '\\' && flags&fnmNoescape == 0 && len(pat) > 0 {
				lit, pat = pat[0], pat[1:]
			}

			if len(s) == 0 || lit != s[0] && (flags&fnmCasefold == 0 || lower(lit) != lower(s[0])) {
				return false
			}

			c = s[0]
		}

		s = s[1:]
		start = flags&fnmPathname != 0 && c == '/'
	}

	return len(s) == 0 || flags&fnmLeadingDir != 0 && s[0] == '/'
}

// bracket matches a byte against the bracket expression pat starts with:
// it returns the expression's length, 0 when it is not one, and whether
// it matched.
func bracket(pat []byte, c byte, flags int) (int, bool) {
	i := 1
	negate := false

	if i < len(pat) && (pat[i] == '!' || pat[i] == '^') {
		negate = true
		i++
	}

	fold := flags&fnmCasefold != 0
	matched := false
	first := true

	for {
		if i >= len(pat) {
			return 0, false
		}

		if pat[i] == ']' && !first {
			break
		}

		first = false

		if pat[i] == '[' && i+1 < len(pat) && pat[i+1] == ':' {
			end := bytes.Index(pat[i+2:], []byte(":]"))

			if end >= 0 {
				if inClass(string(pat[i+2:i+2+end]), c) || fold && (inClass(string(pat[i+2:i+2+end]), lower(c)) || inClass(string(pat[i+2:i+2+end]), upper(c))) {
					matched = true
				}

				i += end + 4

				continue
			}
		}

		lo := pat[i]

		if lo == '\\' && flags&fnmNoescape == 0 && i+1 < len(pat) {
			i++
			lo = pat[i]
		}

		i++
		hi := lo

		if i+1 < len(pat) && pat[i] == '-' && pat[i+1] != ']' {
			hi = pat[i+1]
			i += 2

			if hi == '\\' && flags&fnmNoescape == 0 && i < len(pat) {
				hi = pat[i]
				i++
			}
		}

		if lo <= c && c <= hi || fold && (lo <= lower(c) && lower(c) <= hi || lo <= upper(c) && upper(c) <= hi) {
			matched = true
		}
	}

	return i + 1, matched != negate
}

func upper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 'a' + 'A'
	}

	return c
}

// inClass tells whether a byte is in a character class of the C locale.
func inClass(class string, c byte) bool {
	switch class {
	case "alnum":
		return isDigit(c) || lower(c) >= 'a' && lower(c) <= 'z'
	case "alpha":
		return lower(c) >= 'a' && lower(c) <= 'z'
	case "blank":
		return c == ' ' || c == '\t'
	case "cntrl":
		return c < 0x20 || c == 0x7f
	case "digit":
		return isDigit(c)
	case "graph":
		return c > 0x20 && c < 0x7f
	case "lower":
		return c >= 'a' && c <= 'z'
	case "print":
		return c >= 0x20 && c < 0x7f
	case "punct":
		return c > 0x20 && c < 0x7f && !isDigit(c) && !(lower(c) >= 'a' && lower(c) <= 'z')
	case "space":
		return c == ' ' || c >= '\t' && c <= '\r'
	case "upper":
		return c >= 'A' && c <= 'Z'
	case "xdigit":
		return isDigit(c) || lower(c) >= 'a' && lower(c) <= 'f'
	}

	return false
}
