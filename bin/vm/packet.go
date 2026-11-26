package main

const (
	packetData = iota
	packetEnd
)

type Packet struct {
	kind  uint8
	data  []byte
	files []*Description

	arg uint32
}
