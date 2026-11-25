package main

import (
	"time"
)

type SyncLock struct {
	thread int32
	try    bool
	shared bool
	end    time.Time
	run    bool
}

func (r *SyncLock) op() uint32 {
	return 0
}

func (r *SyncLock) unserved() error {
	return EINVAL
}

func (r *SyncLock) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, b, nil
}

func (r *SyncLock) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
