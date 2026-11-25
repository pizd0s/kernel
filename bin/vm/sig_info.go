package main

const sigQueueMax = 1024

type SigInfo struct {
	code    int32
	pid     int32
	uid     uint32
	status  int32
	value   uint32
	timer   int32
	overrun int32
}
