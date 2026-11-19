//go:build linux && amd64

package main

// A FIFO of a server is a node and nothing more (bin/rootfs/rootfs.cpp):
// its pipe is the kernel's, kept by the FIFO's name while an end of it is
// open, the dev and ino of the stat its open is answered with (fileKey),
// and an open of the FIFO joins it as a reader, a writer or, with O_RDWR,
// both (pipe.go join). An open to read
// waits for a writer and one to write for a reader, as POSIX has it,
// unless O_NONBLOCK: then a reader goes on alone and a writer with no
// reader is ENXIO.

// fifoName is what a FIFO's pipe knows of the FIFO: where the kernel keeps
// it, its mode.
type fifoName struct {
	k    *Kernel
	key  fileKey
	mode uint32
}

// forget takes a FIFO's pipe off the kernel's table when it has no end.
func (n *fifoName) forget(pp *pipe) {
	n.k.mu.Lock()
	defer n.k.mu.Unlock()

	pp.mu.Lock()
	empty := pp.readers == 0 && pp.writers == 0
	pp.mu.Unlock()

	if empty && n.k.fifos[n.key] == pp {
		delete(n.k.fifos, n.key)
	}
}
