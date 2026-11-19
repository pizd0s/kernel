//go:build linux && amd64

package main

import (
	"encoding/binary"
	"net/netip"
	"strconv"
	"strings"
)

// numericHost parses a numeric address as musl does: IPv4 by inet_aton,
// IPv6 with a scope of a number or an interface.
func numericHost(s string) (netip.Addr, uint32, bool) {
	if a, ok := inetAton(s); ok {
		return netip.AddrFrom4(a), 0, true
	}

	host, zone, _ := strings.Cut(s, "%")
	addr, err := netip.ParseAddr(host)

	if err != nil || !addr.Is6() {
		return netip.Addr{}, 0, false
	}

	scope := uint32(0)

	if zone != "" {
		if n, err := strconv.ParseUint(zone, 10, 32); err == nil {
			scope = uint32(n)
		} else if index := interfaceIndex(zone); index != 0 {
			scope = index
		} else {
			return netip.Addr{}, 0, false
		}
	}

	return addr, scope, true
}

// sockaddr is the bytes of an address's struct sockaddr_in or _in6.
func sockaddr(addr netip.Addr, port int, scope uint32) []byte {
	if addr.Is4() {
		b := make([]byte, 16)
		binary.LittleEndian.PutUint16(b, afInet)
		binary.BigEndian.PutUint16(b[2:], uint16(port))
		a := addr.As4()
		copy(b[4:], a[:])

		return b
	}

	b := make([]byte, 28)
	binary.LittleEndian.PutUint16(b, afInet6)
	binary.BigEndian.PutUint16(b[2:], uint16(port))
	a := addr.As16()
	copy(b[8:], a[:])
	binary.LittleEndian.PutUint32(b[24:], scope)

	return b
}

// strtoulPart is strtoul's number of base 0 that a part of an address
// is: hexadecimal after 0x, octal after 0, decimal else. A value outside
// the 32-bit address range is invalid.
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

// inetAton is musl's: one to four numbers of strtoul's bases, the last
// filling the bytes left.
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

// The system's network interfaces: it has no network, and the loopback
// interface alone, lo, of index 1, as Linux always has.
var interfaces = []string{"lo"}

// interfaceIndex is the index of the interface of a name, 0 for none.
func interfaceIndex(name string) uint32 {
	for i, n := range interfaces {
		if n == name {
			return uint32(i + 1)
		}
	}

	return 0
}
