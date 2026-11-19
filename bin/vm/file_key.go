//go:build linux && amd64

package main

// fileKey names to the kernel what a server serves, a file, a directory,
// a FIFO or a terminal: the dev and ino the server says, its file
// system's own, as its stat has them. A dev is a number of the kernel's
// each file system takes once (a pair's), so the two are unique while
// servers say their own, and they pass by value: rootfs says a
// directory's as its file system does. The kernel takes a server's word
// for them, as for the rest of a stat. The locks of flock and fcntl are
// kept by it (lock_table.go), a FIFO's pipe (fifo_name.go), and two
// opens of a terminal are one terminal by it (remote_terminal.go).
type fileKey struct {
	dev, ino uint64
}
