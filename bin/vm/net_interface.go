//go:build linux && amd64

package main

type NetInterface struct {
	name  string
	index uint32
	flags uint32
	mac   [ETHER_ADDR_LEN]byte
	inet  bool
	addr  [4]byte
	mask  [4]byte
	named bool
	dns   [4]byte
}
