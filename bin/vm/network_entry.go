//go:build linux && amd64

package main

import (
	"strconv"
	"strings"
)

type NetworkEntry struct {
	name    string
	net     uint32
	aliases []string
}

func (e *NetworkEntry) size(align uint32) uint32 {
	return (4-align%4)%4 + 4*uint32(len(e.aliases)+1) + stringsRoom(append([]string{e.name}, e.aliases...))
}

func networkNumber(s string) (uint32, bool) {
	parts := strings.Split(s, ".")

	if len(parts) > 4 {
		return 0, false
	}

	n := uint32(0)

	for _, part := range parts {
		v, err := strconv.ParseUint(part, 0, 8)

		if err != nil {
			return 0, false
		}

		n = n<<8 | uint32(v)
	}

	return n, true
}
