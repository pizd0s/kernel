//go:build linux && amd64

package main

const (
	packetData = iota
	packetEnd
)

type Packet struct {
	kind  uint8
	data  []byte
	files []Object
	cred  *Ucred
	addr  []byte

	arg uint32
}

type Ucred struct {
	pid, uid, gid uint32
}

func dropFiles(files []Object) {
	for _, f := range files {
		f.unref()
	}
}
