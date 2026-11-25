package main

type SocketName struct {
	k   *Kernel
	key FileKey
}

func (n *SocketName) forget(held Object) {
	n.k.mu.Lock()

	if n.k.sockets[n.key] == held {
		delete(n.k.sockets, n.key)
	} else {
		held = nil
	}

	n.k.mu.Unlock()

	if held != nil {
		held.unref()
	}
}
