//go:build linux && amd64

package main

type ProtocolEntry struct {
	name    string
	number  int
	aliases []string
}

func (e *ProtocolEntry) size(align uint32) uint32 {
	return (4-align%4)%4 + 4*uint32(len(e.aliases)+1) + stringsRoom(append([]string{e.name}, e.aliases...))
}
