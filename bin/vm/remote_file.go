//go:build linux && amd64

package main

import ()

type RemoteFile struct {
	node RemoteNode
}

func (f *RemoteFile) ref() {
	f.node.ref()
}

func (f *RemoteFile) unref() {
	f.node.unref()
}

func (f *RemoteFile) asFile() File {
	return f
}

func (f *RemoteFile) asTransfer() Transfer {
	return f
}

func (f *RemoteFile) asSeeker() Seeker {
	return f
}

func (f *RemoteFile) asDir() Dir {
	return nil
}

func (f *RemoteFile) asTerminal() Terminal {
	return nil
}

func (f *RemoteFile) asWindowed() Windowed {
	return f
}

func (f *RemoteFile) asLink() Link {
	return nil
}

func (f *RemoteFile) asAttrs() Attrs {
	return f
}

func (f *RemoteFile) asSocket() Socket {
	return nil
}

func (f *RemoteFile) asIoctl() Ioctl {
	return nil
}

func (f *RemoteFile) asLocker() Locker {
	return f
}

func (f *RemoteFile) asEpoll() *Epoll {
	return nil
}

func (f *RemoteFile) asMemfd() *MemfdFile {
	return nil
}

func (f *RemoteFile) asPair() *PairEnd {
	return nil
}

func (f *RemoteFile) asListener() *UnixListener {
	return nil
}

func (f *RemoteFile) asConsole() *ConsoleEnd {
	return nil
}

func (f *RemoteFile) asVFS() *RemoteDir {
	return nil
}

func (f *RemoteFile) asCall() *CallEnd {
	return nil
}

func (f *RemoteFile) stat(p *Process, st *Stat) error {
	return f.node.stat(p, st)
}

func (f *RemoteFile) poll(events int16) int16 {
	return f.node.poll(events)
}

func (f *RemoteFile) subscribe(s *Subscription, events int16) {
	f.node.subscribe(s, events)
}

func (f *RemoteFile) unsubscribe(s *Subscription) {
	f.node.unsubscribe(s)
}

func (f *RemoteFile) flags() (int, error) {
	return f.node.flags()
}

func (f *RemoteFile) setFlags(fl int) error {
	return f.node.setFlags(fl)
}

func (f *RemoteFile) read(p *Process, b []byte, mode IoMode) (int, error) {
	return f.node.read(p, b, mode)
}

func (f *RemoteFile) write(p *Process, b []byte, mode IoMode) (int, error) {
	return f.node.write(p, b, mode)
}

func (f *RemoteFile) pread(p *Process, b []byte, off int64) (int, error) {
	return f.node.readAt(p, b, off)
}

func (f *RemoteFile) pwrite(p *Process, b []byte, off int64) (int, error) {
	return f.node.writeAt(p, b, off)
}

func (f *RemoteFile) seek(p *Process, off int64, whence int) (int64, error) {
	return f.node.seek(p, off, whence)
}

func (f *RemoteFile) readWindow(p *Process, w *MemfdFile, woff uint32, n int, from *int64) (int, error) {
	return f.node.readWindow(p, w, woff, n, from)
}

func (f *RemoteFile) writeWindow(p *Process, w *MemfdFile, woff uint32, n int, to *int64) (int, error) {
	return f.node.writeWindow(p, w, woff, n, to)
}

func (f *RemoteFile) truncate(p *Process, size int64) error {
	return f.node.truncate(p, size)
}

func (f *RemoteFile) sync(p *Process, data bool) error {
	return f.node.sync(p, data)
}

func (f *RemoteFile) chmod(p *Process, mode uint32) error {
	return f.node.chmod(p, mode)
}

func (f *RemoteFile) chown(p *Process, uid, gid int) error {
	return f.node.chown(p, uid, gid)
}

func (f *RemoteFile) utimes(p *Process, ts []Timespec) error {
	return f.node.utimes(p, ts)
}

func (f *RemoteFile) statfs(p *Process, st *Statfs) error {
	return f.node.statfs(p, st)
}

func (f *RemoteFile) flock(p *Process, op int) error {
	return f.node.flock(p, op)
}

func (f *RemoteFile) recordLock(p *Process, cmd int, lk *Flock) error {
	return f.node.recordLock(p, cmd, lk)
}
