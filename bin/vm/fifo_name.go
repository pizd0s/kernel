package main

type FifoName struct {
	k    *Kernel
	key  FileKey
	mode uint32
}

func (n *FifoName) forget(pp *Pipe) {
	n.k.mu.Lock()

	defer n.k.mu.Unlock()

	pp.mu.Lock()

	empty := pp.readers == 0 && pp.writers == 0

	pp.mu.Unlock()

	if empty && n.k.fifos[n.key] == pp {
		delete(n.k.fifos, n.key)
	}
}
