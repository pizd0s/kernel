package main

type FileStat struct {
	st *Stat
}

func (r *FileStat) op() uint32 {
	return rFstat
}

func (r *FileStat) unserved() error {
	return EBADF
}

func (r *FileStat) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return 0, b, nil
}

func (r *FileStat) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return decodeStat(pk.data, r.st)
}
