//go:build linux && amd64

package main

import (
	"syscall"
)

// remotePath is a handle of rootfs's of what O_PATH opened that is no
// directory: its calls are those of what it is, by an empty path, and no
// path walks from it.
type remotePath struct {
	node remoteNode
	id   uint64
}

func (h *remotePath) ref()   { h.node.ref() }
func (h *remotePath) unref() { h.node.unref() }

// A remotePath is a File, a Link, an Attrs and a Locker: its node answers
// but readlink.
func (h *remotePath) asFile() File              { return h }
func (h *remotePath) asTransfer() Transfer      { return nil }
func (h *remotePath) asSeeker() Seeker          { return nil }
func (h *remotePath) asDir() Dir                { return nil }
func (h *remotePath) asTerminal() Terminal      { return nil }
func (h *remotePath) asWindowed() Windowed      { return nil }
func (h *remotePath) asLink() Link              { return h }
func (h *remotePath) asAttrs() Attrs            { return h }
func (h *remotePath) asSocket() Socket          { return nil }
func (h *remotePath) asIoctl() Ioctl            { return nil }
func (h *remotePath) asLocker() Locker          { return nil }
func (h *remotePath) asEpoll() *epoll           { return nil }
func (h *remotePath) asMemfd() *memfdFile       { return nil }
func (h *remotePath) asPair() *pairEnd          { return nil }
func (h *remotePath) asListener() *unixListener { return nil }
func (h *remotePath) asConsole() *consoleEnd    { return nil }
func (h *remotePath) asVFS() *remoteDir         { return nil }
func (h *remotePath) asCall() *callEnd          { return nil }

func (h *remotePath) stat(p *Process, st *syscall.Stat_t) error { return h.node.stat(p, st) }
func (h *remotePath) poll(events int16) int16                   { return h.node.poll(events) }
func (h *remotePath) subscribe(s *subscription, events int16)   { h.node.subscribe(s, events) }
func (h *remotePath) unsubscribe(s *subscription)               { h.node.unsubscribe(s) }
func (h *remotePath) flags() (int, error)                       { return h.node.flags() }
func (h *remotePath) setFlags(fl int) error                     { return h.node.setFlags(fl) }

func (h *remotePath) truncate(p *Process, size int64) error          { return h.node.truncate(p, size) }
func (h *remotePath) sync(p *Process, data bool) error               { return h.node.sync(p, data) }
func (h *remotePath) chmod(p *Process, mode uint32) error            { return h.node.chmod(p, mode) }
func (h *remotePath) chown(p *Process, uid, gid int) error           { return h.node.chown(p, uid, gid) }
func (h *remotePath) utimes(p *Process, ts []syscall.Timespec) error { return h.node.utimes(p, ts) }
func (h *remotePath) statfs(p *Process, st *syscall.Statfs_t) error  { return h.node.statfs(p, st) }

// readlink is the target of the link a handle of O_PATH is, by an empty
// path.
func (h *remotePath) readlink(p *Process) (string, error) {
	pk, err := h.node.vfs(p, h.id, vReadlink, 0, nil)

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}
