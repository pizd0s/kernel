//go:build linux && amd64

package main

import ()

type RemotePath struct {
	node RemoteNode
	id   uint64
}

func (h *RemotePath) ref() {
	h.node.ref()
}

func (h *RemotePath) unref() {
	h.node.unref()
}

func (h *RemotePath) asFile() File {
	return h
}

func (h *RemotePath) asTransfer() Transfer {
	return nil
}

func (h *RemotePath) asSeeker() Seeker {
	return nil
}

func (h *RemotePath) asDir() Dir {
	return nil
}

func (h *RemotePath) asTerminal() Terminal {
	return nil
}

func (h *RemotePath) asWindowed() Windowed {
	return nil
}

func (h *RemotePath) asLink() Link {
	return h
}

func (h *RemotePath) asAttrs() Attrs {
	return h
}

func (h *RemotePath) asSocket() Socket {
	return nil
}

func (h *RemotePath) asIoctl() Ioctl {
	return nil
}

func (h *RemotePath) asLocker() Locker {
	return nil
}

func (h *RemotePath) asEpoll() *Epoll {
	return nil
}

func (h *RemotePath) asMemfd() *MemfdFile {
	return nil
}

func (h *RemotePath) asPair() *PairEnd {
	return nil
}

func (h *RemotePath) asListener() *UnixListener {
	return nil
}

func (h *RemotePath) asConsole() *ConsoleEnd {
	return nil
}

func (h *RemotePath) asVFS() *RemoteDir {
	return nil
}

func (h *RemotePath) stat(p *Process, st *Stat) error {
	return h.node.stat(p, st)
}

func (h *RemotePath) poll(events int16) int16 {
	return h.node.poll(events)
}

func (h *RemotePath) subscribe(s *Subscription, events int16) {
	h.node.subscribe(s, events)
}

func (h *RemotePath) unsubscribe(s *Subscription) {
	h.node.unsubscribe(s)
}

func (h *RemotePath) flags() (int, error) {
	return h.node.flags()
}

func (h *RemotePath) setFlags(fl int) error {
	return h.node.setFlags(fl)
}

func (h *RemotePath) truncate(p *Process, size int64) error {
	return h.node.truncate(p, size)
}

func (h *RemotePath) sync(p *Process, data bool) error {
	return h.node.sync(p, data)
}

func (h *RemotePath) chmod(p *Process, mode uint32) error {
	return h.node.chmod(p, mode)
}

func (h *RemotePath) chown(p *Process, uid, gid int) error {
	return h.node.chown(p, uid, gid)
}

func (h *RemotePath) utimes(p *Process, ts []Timespec) error {
	return h.node.utimes(p, ts)
}

func (h *RemotePath) statfs(p *Process, st *Statfs) error {
	return h.node.statfs(p, st)
}

func (h *RemotePath) readlink(p *Process) (string, error) {
	pk, err := h.node.vfs(p, h.id, vReadlink, 0, nil)

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}
