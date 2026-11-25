//go:build linux && amd64

package main

import (
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
