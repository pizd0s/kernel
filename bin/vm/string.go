//go:build linux && amd64

package main

import (
	"bytes"
	"fmt"
	"math/bits"
)

var signalNames = map[int]string{
	SIGHUP:    "SIGHUP",
	SIGINT:    "SIGINT",
	SIGQUIT:   "SIGQUIT",
	SIGILL:    "SIGILL",
	SIGTRAP:   "SIGTRAP",
	SIGABRT:   "SIGABRT",
	SIGBUS:    "SIGBUS",
	SIGFPE:    "SIGFPE",
	SIGKILL:   "SIGKILL",
	SIGUSR1:   "SIGUSR1",
	SIGSEGV:   "SIGSEGV",
	SIGUSR2:   "SIGUSR2",
	SIGPIPE:   "SIGPIPE",
	SIGALRM:   "SIGALRM",
	SIGTERM:   "SIGTERM",
	SIGSTKFLT: "SIGSTKFLT",
	SIGCHLD:   "SIGCHLD",
	SIGCONT:   "SIGCONT",
	SIGSTOP:   "SIGSTOP",
	SIGTSTP:   "SIGTSTP",
	SIGTTIN:   "SIGTTIN",
	SIGTTOU:   "SIGTTOU",
	SIGURG:    "SIGURG",
	SIGXCPU:   "SIGXCPU",
	SIGXFSZ:   "SIGXFSZ",
	SIGVTALRM: "SIGVTALRM",
	SIGPROF:   "SIGPROF",
	SIGWINCH:  "SIGWINCH",
	SIGIO:     "SIGIO",
	SIGPWR:    "SIGPWR",
	SIGSYS:    "SIGSYS",
}

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

func cmp(a, b int) uint32 {
	return uint32(int32(a - b))
}

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

func signalName(sig int) string {
	if s, ok := signalNames[sig]; ok {
		return s
	}

	return fmt.Sprintf("signal %d", sig)
}

func fnmatch(pat, s []byte, flags int, start bool) bool {
	hidden := func() bool {
		return start && flags&FNM_PERIOD != 0 && len(s) > 0 && s[0] == '.'
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

				if i < len(s) && flags&FNM_PATHNAME != 0 && s[i] == '/' {
					return false
				}
			}

			return false
		case '?':
			if len(s) == 0 || flags&FNM_PATHNAME != 0 && s[0] == '/' || hidden() {
				return false
			}

			c, pat = s[0], pat[1:]
		case '[':
			if len(s) == 0 || flags&FNM_PATHNAME != 0 && s[0] == '/' || hidden() {
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

			if lit == '\\' && flags&FNM_NOESCAPE == 0 && len(pat) > 0 {
				lit, pat = pat[0], pat[1:]
			}

			if len(s) == 0 || lit != s[0] && (flags&FNM_CASEFOLD == 0 || lower(lit) != lower(s[0])) {
				return false
			}

			c = s[0]
		}

		s = s[1:]
		start = flags&FNM_PATHNAME != 0 && c == '/'
	}

	return len(s) == 0 || flags&FNM_LEADING_DIR != 0 && s[0] == '/'
}

func bracket(pat []byte, c byte, flags int) (int, bool) {
	i := 1
	negate := false

	if i < len(pat) && (pat[i] == '!' || pat[i] == '^') {
		negate = true
		i++
	}

	fold := flags&FNM_CASEFOLD != 0
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

		if lo == '\\' && flags&FNM_NOESCAPE == 0 && i+1 < len(pat) {
			i++
			lo = pat[i]
		}

		i++

		hi := lo

		if i+1 < len(pat) && pat[i] == '-' && pat[i+1] != ']' {
			hi = pat[i+1]
			i += 2

			if hi == '\\' && flags&FNM_NOESCAPE == 0 && i < len(pat) {
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
