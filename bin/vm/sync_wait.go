//go:build linux && amd64

package main

type SyncWait struct {
	thread int32
	lock   Object
	at     uint32
}

func (r *SyncWait) op() uint32 {
	return 0
}

func (r *SyncWait) unserved() error {
	return EINVAL
}

func (r *SyncWait) ser(p *Process, b []byte) (uint32, []byte, []Object) {
	return 0, b, nil
}

func (r *SyncWait) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
