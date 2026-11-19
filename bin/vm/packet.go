//go:build linux && amd64

package main

// A packet is bytes, files and the sender's credentials, and the address
// of a datagram's sender, the path it is bound to (unix_dgram.go).
type packet struct {
	data  []byte
	files []Object
	cred  *ucred
	addr  []byte
	// arg is a server's reply's arg, of its header (replyOf).
	arg uint32
}

// ucred is SCM_CREDENTIALS' struct ucred: the pid, uid and gid of a
// sender.
type ucred struct {
	pid, uid, gid uint32
}

// dropFiles lets go of the files of packets nobody takes.
func dropFiles(files []Object) {
	for _, f := range files {
		f.unref()
	}
}
