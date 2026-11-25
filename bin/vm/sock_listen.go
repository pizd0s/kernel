package main

type SockListen struct {
	backlog int
}

func (r *SockListen) op() uint32 {
	return sListen
}

func (r *SockListen) unserved() error {
	return ENOTSOCK
}

func (r *SockListen) ser(p *Process, b []byte) (uint32, []byte, []*Description) {
	return uint32(r.backlog), b, nil
}

func (r *SockListen) des(p *Process, pk *Packet) error {
	dropFiles(pk.files)

	return nil
}
