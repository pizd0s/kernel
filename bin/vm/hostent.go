//go:build linux && amd64

package main

import "net/netip"

type Hostent struct {
	name    string
	aliases []string
	family  int
	addrs   []netip.Addr
}

func (h *Hostent) length() uint32 {
	if h.family == AF_INET6 {
		return 16
	}

	return 4
}

func (h *Hostent) size(align uint32) uint32 {
	return (4-align%4)%4 + 4*uint32(len(h.aliases)+1) + 4*uint32(len(h.addrs)+1) + h.length()*uint32(len(h.addrs)) +
		stringsRoom(append([]string{h.name}, h.aliases...))
}
