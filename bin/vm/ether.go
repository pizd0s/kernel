//go:build linux && amd64

package main

import (
	"strconv"
	"strings"
)

func etherParse(s string) ([ETHER_ADDR_LEN]byte, string, bool) {
	var a [ETHER_ADDR_LEN]byte

	for i := range a {
		n := 0

		for n < len(s) && strings.IndexByte("0123456789abcdefABCDEF", s[n]) >= 0 {
			n++
		}

		v, err := strconv.ParseUint(s[:n], 16, 8)

		if n == 0 || err != nil {
			return a, s, false
		}

		a[i], s = byte(v), s[n:]

		if i < len(a)-1 {
			if s == "" || s[0] != ':' {
				return a, s, false
			}

			s = s[1:]
		}
	}

	return a, s, s == "" || strings.IndexByte(" \t\n", s[0]) >= 0
}
