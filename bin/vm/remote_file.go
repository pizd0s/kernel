//go:build linux && amd64

package main

import (
	"syscall"
)

// remoteFile is a server's file.
type remoteFile struct {
	node remoteNode
}

func (f *remoteFile) ref()   { f.node.ref() }
func (f *remoteFile) unref() { f.node.unref() }

// A remoteFile is a File, a Transfer, a Seeker, a Windowed, an Attrs and a
// Locker: its node answers them all.
func (f *remoteFile) asFile() File              { return f }
func (f *remoteFile) asTransfer() Transfer      { return f }
func (f *remoteFile) asSeeker() Seeker          { return f }
func (f *remoteFile) asDir() Dir                { return nil }
func (f *remoteFile) asTerminal() Terminal      { return nil }
func (f *remoteFile) asWindowed() Windowed      { return f }
func (f *remoteFile) asLink() Link              { return nil }
func (f *remoteFile) asAttrs() Attrs            { return f }
func (f *remoteFile) asSocket() Socket          { return nil }
func (f *remoteFile) asIoctl() Ioctl            { return nil }
func (f *remoteFile) asLocker() Locker          { return f }
func (f *remoteFile) asEpoll() *epoll           { return nil }
func (f *remoteFile) asMemfd() *memfdFile       { return nil }
func (f *remoteFile) asPair() *pairEnd          { return nil }
func (f *remoteFile) asListener() *unixListener { return nil }
func (f *remoteFile) asConsole() *consoleEnd    { return nil }
func (f *remoteFile) asVFS() *remoteDir         { return nil }
func (f *remoteFile) asCall() *callEnd          { return nil }

func (f *remoteFile) stat(p *Process, st *syscall.Stat_t) error { return f.node.stat(p, st) }
func (f *remoteFile) poll(events int16) int16                   { return f.node.poll(events) }
func (f *remoteFile) subscribe(s *subscription, events int16)   { f.node.subscribe(s, events) }
func (f *remoteFile) unsubscribe(s *subscription)               { f.node.unsubscribe(s) }
func (f *remoteFile) flags() (int, error)                       { return f.node.flags() }
func (f *remoteFile) setFlags(fl int) error                     { return f.node.setFlags(fl) }

func (f *remoteFile) read(p *Process, b []byte, mode ioMode) (int, error) {
	return f.node.read(p, b, mode)
}
func (f *remoteFile) write(p *Process, b []byte, mode ioMode) (int, error) {
	return f.node.write(p, b, mode)
}
func (f *remoteFile) pread(p *Process, b []byte, off int64) (int, error) {
	return f.node.readAt(p, b, off)
}
func (f *remoteFile) pwrite(p *Process, b []byte, off int64) (int, error) {
	return f.node.writeAt(p, b, off)
}
func (f *remoteFile) seek(p *Process, off int64, whence int) (int64, error) {
	return f.node.seek(p, off, whence)
}
func (f *remoteFile) readWindow(p *Process, w *memfdFile, woff uint32, n int, from *int64) (int, error) {
	return f.node.readWindow(p, w, woff, n, from)
}
func (f *remoteFile) writeWindow(p *Process, w *memfdFile, woff uint32, n int, to *int64) (int, error) {
	return f.node.writeWindow(p, w, woff, n, to)
}

func (f *remoteFile) truncate(p *Process, size int64) error          { return f.node.truncate(p, size) }
func (f *remoteFile) sync(p *Process, data bool) error               { return f.node.sync(p, data) }
func (f *remoteFile) chmod(p *Process, mode uint32) error            { return f.node.chmod(p, mode) }
func (f *remoteFile) chown(p *Process, uid, gid int) error           { return f.node.chown(p, uid, gid) }
func (f *remoteFile) utimes(p *Process, ts []syscall.Timespec) error { return f.node.utimes(p, ts) }
func (f *remoteFile) statfs(p *Process, st *syscall.Statfs_t) error  { return f.node.statfs(p, st) }

func (f *remoteFile) flock(p *Process, op int) error { return f.node.flock(p, op) }
func (f *remoteFile) recordLock(p *Process, cmd int, lk *syscall.Flock_t) error {
	return f.node.recordLock(p, cmd, lk)
}
