//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/binary"
)

const (
	rTcgetattr uint32 = iota + 112
	rTcsetattr
	rTcflush
	rTcflow
	rTcdrain
	rGetWinsize
	rSetWinsize
	rInq
	rOutq
	rName
	rGetPgrp
	rSetPgrp
	rSetCtty
)

type RemoteTerminal struct {
	node RemoteNode

	key FileKey
}

func (t *RemoteTerminal) ref() {
	t.node.ref()
}

func (t *RemoteTerminal) unref() {
	t.node.unref()
}

func (t *RemoteTerminal) asFile() File {
	return t
}

func (t *RemoteTerminal) asTransfer() Transfer {
	return t
}

func (t *RemoteTerminal) asSeeker() Seeker {
	return nil
}

func (t *RemoteTerminal) asDir() Dir {
	return nil
}

func (t *RemoteTerminal) asTerminal() Terminal {
	return t
}

func (t *RemoteTerminal) asWindowed() Windowed {
	return nil
}

func (t *RemoteTerminal) asLink() Link {
	return nil
}

func (t *RemoteTerminal) asAttrs() Attrs {
	return t
}

func (t *RemoteTerminal) asSocket() Socket {
	return nil
}

func (t *RemoteTerminal) asIoctl() Ioctl {
	return nil
}

func (t *RemoteTerminal) asLocker() Locker {
	return t
}

func (t *RemoteTerminal) asEpoll() *Epoll {
	return nil
}

func (t *RemoteTerminal) asMemfd() *MemfdFile {
	return nil
}

func (t *RemoteTerminal) asPair() *PairEnd {
	return nil
}

func (t *RemoteTerminal) asListener() *UnixListener {
	return nil
}

func (t *RemoteTerminal) asConsole() *ConsoleEnd {
	return nil
}

func (t *RemoteTerminal) asVFS() *RemoteDir {
	return nil
}

func (t *RemoteTerminal) stat(p *Process, st *Stat) error {
	return t.node.stat(p, st)
}

func (t *RemoteTerminal) poll(events int16) int16 {
	return t.node.poll(events)
}

func (t *RemoteTerminal) subscribe(s *Subscription, events int16) {
	t.node.subscribe(s, events)
}

func (t *RemoteTerminal) unsubscribe(s *Subscription) {
	t.node.unsubscribe(s)
}

func (t *RemoteTerminal) flags() (int, error) {
	return t.node.flags()
}

func (t *RemoteTerminal) setFlags(fl int) error {
	return t.node.setFlags(fl)
}

func (t *RemoteTerminal) truncate(p *Process, size int64) error {
	return t.node.truncate(p, size)
}

func (t *RemoteTerminal) sync(p *Process, data bool) error {
	return t.node.sync(p, data)
}

func (t *RemoteTerminal) chmod(p *Process, mode uint32) error {
	return t.node.chmod(p, mode)
}

func (t *RemoteTerminal) chown(p *Process, uid, gid int) error {
	return t.node.chown(p, uid, gid)
}

func (t *RemoteTerminal) utimes(p *Process, ts []Timespec) error {
	return t.node.utimes(p, ts)
}

func (t *RemoteTerminal) statfs(p *Process, st *Statfs) error {
	return t.node.statfs(p, st)
}

func (t *RemoteTerminal) flock(p *Process, op int) error {
	return t.node.flock(p, op)
}

func (t *RemoteTerminal) recordLock(p *Process, cmd int, lk *Flock) error {
	return t.node.recordLock(p, cmd, lk)
}

func (t *RemoteTerminal) read(p *Process, b []byte, mode IoMode) (int, error) {
	if err := p.background(t, SIGTTIN); err != nil {
		return 0, err
	}

	return t.node.read(p, b, mode)
}

func (t *RemoteTerminal) write(p *Process, b []byte, mode IoMode) (int, error) {
	if _, tostop, _ := t.jobs(); tostop {
		if err := p.background(t, SIGTTOU); err != nil {
			return 0, err
		}
	}

	return t.node.write(p, b, mode)
}

func (t *RemoteTerminal) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (t *RemoteTerminal) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, ESPIPE
}

func (t *RemoteTerminal) count(p *Process, op uint32) (uint32, error) {
	pk, err := t.node.on(p, op, 0, nil)

	if err == nil && len(pk.data) != 4 {
		err = EIO
	}

	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint32(pk.data), nil
}

func (t *RemoteTerminal) device() FileKey {
	return t.key
}

func (t *RemoteTerminal) jobs() (int32, bool, bool) {
	hungUp := t.node.readiness()&POLLHUP != 0

	return t.node.foreground.Load(), t.node.tostop.Load(), hungUp
}

func (t *RemoteTerminal) getattr(p *Process) ([]byte, error) {
	pk, err := t.node.on(p, rTcgetattr, 0, nil)

	if err == nil && len(pk.data) != sizeofTermios {
		err = EIO
	}

	return bytes.Clone(pk.data), err
}

func (t *RemoteTerminal) setattr(p *Process, when uint32, termios []byte) error {
	if when == 0 {
		return t.node.simple(p, rTcsetattr, when, termios)
	}

	return t.node.waits(p, rTcsetattr, when, termios)
}

func (t *RemoteTerminal) flush(p *Process, queue uint32) error {
	return t.node.simple(p, rTcflush, queue, nil)
}

func (t *RemoteTerminal) flow(p *Process, action uint32) error {
	return t.node.simple(p, rTcflow, action, nil)
}

func (t *RemoteTerminal) drain(p *Process) error {
	return t.node.waits(p, rTcdrain, 0, nil)
}

func (t *RemoteTerminal) window(p *Process) ([]byte, error) {
	pk, err := t.node.on(p, rGetWinsize, 0, nil)

	if err == nil && len(pk.data) != sizeofWinsize {
		err = EIO
	}

	return bytes.Clone(pk.data), err
}

func (t *RemoteTerminal) setWindow(p *Process, size []byte) error {
	return t.node.simple(p, rSetWinsize, 0, size)
}

func (t *RemoteTerminal) pending(p *Process) (uint32, error) {
	return t.count(p, rInq)
}

func (t *RemoteTerminal) unsent(p *Process) (uint32, error) {
	return t.count(p, rOutq)
}

func (t *RemoteTerminal) foreground(p *Process) (uint32, error) {
	return t.count(p, rGetPgrp)
}

func (t *RemoteTerminal) setForeground(p *Process, pgrp uint32) error {
	return t.node.simple(p, rSetPgrp, pgrp, nil)
}

func (t *RemoteTerminal) control(p *Process, sid, pgrp int32) error {
	var body [4]byte
	binary.LittleEndian.PutUint32(body[:], uint32(pgrp))

	return t.node.simple(p, rSetCtty, uint32(sid), body[:])
}

func (t *RemoteTerminal) reopen(p *Process, flags int) (Terminal, error) {
	pk, err := t.node.call(p, rOpen, uint32(flags), nil, nil)

	if err != nil {
		return nil, err
	}

	if len(pk.files) != 1 {
		dropFiles(pk.files)

		return nil, EIO
	}

	f, err := p.opened(&pk)

	if err != nil {
		return nil, err
	}

	if r := f.asTerminal(); r != nil {
		return r, nil
	}

	f.unref()

	return nil, EIO
}

func (t *RemoteTerminal) name(p *Process, peer bool) (string, error) {
	arg := uint32(0)

	if peer {
		arg = 1
	}

	pk, err := t.node.on(p, rName, arg, nil)

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}
