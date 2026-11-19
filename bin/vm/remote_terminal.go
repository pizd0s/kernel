//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/binary"
	"syscall"
)

// A terminal is a service's open file (remote_node.go) born of a served
// pair an OPEN's OK said a terminal's (openedTerminal): an end of a pseudoterminal of
// bin/tools/pty.cpp, the system's only terminals. The kernel keeps nothing
// of one: its settings, its window, its foreground group and its
// discipline are its server's. The kernel's calls see it as the Terminal
// role (object.go), which remoteTerminal answers by requests to its
// server (lib/protocol/protocol.h). A terminal has no offsets: seek and the
// positioned reads and writes are ESPIPE. The console is no terminal
// (console.go).
//
// What the kernel keeps is the processes': a session's controlling
// terminal, which its leader takes with TIOCSCTTY or by opening a slave
// with no controlling terminal and no O_NOCTTY, which /dev/tty opens, and
// which goes with the leader; and job control, as Linux's: a read of its
// controlling terminal from a group not in the foreground is SIGTTIN to
// the group, a write with TOSTOP or a change of the terminal SIGTTOU; the
// foreground group and TOSTOP are what the terminal's READY said last. A
// group a going orphans hears SIGHUP and SIGCONT when it has a stopped
// process, as POSIX says (orphans). dev and ino are the terminal's
// device's, the same for each open of it, which its server's struct
// served said.
type remoteTerminal struct {
	node remoteNode
	// key names the terminal: the dev and ino of its device, as its
	// server said them (fileKey).
	key fileKey
}

func (t *remoteTerminal) ref()   { t.node.ref() }
func (t *remoteTerminal) unref() { t.node.unref() }

// A remoteTerminal is a File, a Transfer, a Terminal, an Attrs and a
// Locker: its node answers but job control's reads and writes and the
// terminal's calls. It is no Seeker, and no Windowed: lseek is ESPIPE,
// copy_file_range EINVAL, as Linux's of a terminal.
func (t *remoteTerminal) asFile() File              { return t }
func (t *remoteTerminal) asTransfer() Transfer      { return t }
func (t *remoteTerminal) asSeeker() Seeker          { return nil }
func (t *remoteTerminal) asDir() Dir                { return nil }
func (t *remoteTerminal) asTerminal() Terminal      { return t }
func (t *remoteTerminal) asWindowed() Windowed      { return nil }
func (t *remoteTerminal) asLink() Link              { return nil }
func (t *remoteTerminal) asAttrs() Attrs            { return t }
func (t *remoteTerminal) asSocket() Socket          { return nil }
func (t *remoteTerminal) asIoctl() Ioctl            { return nil }
func (t *remoteTerminal) asLocker() Locker          { return t }
func (t *remoteTerminal) asEpoll() *epoll           { return nil }
func (t *remoteTerminal) asMemfd() *memfdFile       { return nil }
func (t *remoteTerminal) asPair() *pairEnd          { return nil }
func (t *remoteTerminal) asListener() *unixListener { return nil }
func (t *remoteTerminal) asConsole() *consoleEnd    { return nil }
func (t *remoteTerminal) asVFS() *remoteDir         { return nil }
func (t *remoteTerminal) asCall() *callEnd          { return nil }

func (t *remoteTerminal) stat(p *Process, st *syscall.Stat_t) error { return t.node.stat(p, st) }
func (t *remoteTerminal) poll(events int16) int16                   { return t.node.poll(events) }
func (t *remoteTerminal) subscribe(s *subscription, events int16)   { t.node.subscribe(s, events) }
func (t *remoteTerminal) unsubscribe(s *subscription)               { t.node.unsubscribe(s) }
func (t *remoteTerminal) flags() (int, error)                       { return t.node.flags() }
func (t *remoteTerminal) setFlags(fl int) error                     { return t.node.setFlags(fl) }

func (t *remoteTerminal) truncate(p *Process, size int64) error          { return t.node.truncate(p, size) }
func (t *remoteTerminal) sync(p *Process, data bool) error               { return t.node.sync(p, data) }
func (t *remoteTerminal) chmod(p *Process, mode uint32) error            { return t.node.chmod(p, mode) }
func (t *remoteTerminal) chown(p *Process, uid, gid int) error           { return t.node.chown(p, uid, gid) }
func (t *remoteTerminal) utimes(p *Process, ts []syscall.Timespec) error { return t.node.utimes(p, ts) }
func (t *remoteTerminal) statfs(p *Process, st *syscall.Statfs_t) error  { return t.node.statfs(p, st) }

func (t *remoteTerminal) flock(p *Process, op int) error { return t.node.flock(p, op) }
func (t *remoteTerminal) recordLock(p *Process, cmd int, lk *syscall.Flock_t) error {
	return t.node.recordLock(p, cmd, lk)
}

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

func (t *remoteTerminal) read(p *Process, b []byte, mode ioMode) (int, error) {
	if err := p.background(t, syscall.SIGTTIN); err != nil {
		return 0, err
	}

	return t.node.read(p, b, mode)
}

func (t *remoteTerminal) write(p *Process, b []byte, mode ioMode) (int, error) {
	if _, tostop, _ := t.jobs(); tostop {
		if err := p.background(t, syscall.SIGTTOU); err != nil {
			return 0, err
		}
	}

	return t.node.write(p, b, mode)
}

func (t *remoteTerminal) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (t *remoteTerminal) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

// count is a call answered with a count.
func (t *remoteTerminal) count(p *Process, op uint32) (uint32, error) {
	pk, err := t.node.on(p, op, 0, nil)

	if err == nil && len(pk.data) != 4 {
		err = syscall.EIO
	}

	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint32(pk.data), nil
}

// The Terminal role of a terminal's open file: each call is a request to
// its server (lib/protocol/protocol.h).

func (t *remoteTerminal) device() fileKey {
	return t.key
}

// jobs is what the terminal's READY said last.
func (t *remoteTerminal) jobs() (int32, bool, bool) {
	hungUp := t.node.readiness()&pollHup != 0

	return t.node.foreground.Load(), t.node.tostop.Load(), hungUp
}

func (t *remoteTerminal) getattr(p *Process) ([]byte, error) {
	pk, err := t.node.on(p, rTcgetattr, 0, nil)

	if err == nil && len(pk.data) != sizeofTermios {
		err = syscall.EIO
	}

	// The answer's bytes are its connection's: the caller's own outlive
	// the next call.
	return bytes.Clone(pk.data), err
}

func (t *remoteTerminal) setattr(p *Process, when uint32, termios []byte) error {
	if when == 0 {
		return t.node.simple(p, rTcsetattr, when, termios)
	}

	return t.node.waits(p, rTcsetattr, when, termios)
}

func (t *remoteTerminal) flush(p *Process, queue uint32) error {
	return t.node.simple(p, rTcflush, queue, nil)
}

func (t *remoteTerminal) flow(p *Process, action uint32) error {
	return t.node.simple(p, rTcflow, action, nil)
}

func (t *remoteTerminal) drain(p *Process) error {
	return t.node.waits(p, rTcdrain, 0, nil)
}

func (t *remoteTerminal) window(p *Process) ([]byte, error) {
	pk, err := t.node.on(p, rGetWinsize, 0, nil)

	if err == nil && len(pk.data) != sizeofWinsize {
		err = syscall.EIO
	}

	return bytes.Clone(pk.data), err
}

func (t *remoteTerminal) setWindow(p *Process, size []byte) error {
	return t.node.simple(p, rSetWinsize, 0, size)
}

func (t *remoteTerminal) pending(p *Process) (uint32, error) {
	return t.count(p, rInq)
}

func (t *remoteTerminal) unsent(p *Process) (uint32, error) {
	return t.count(p, rOutq)
}

func (t *remoteTerminal) foreground(p *Process) (uint32, error) {
	return t.count(p, rGetPgrp)
}

func (t *remoteTerminal) setForeground(p *Process, pgrp uint32) error {
	return t.node.simple(p, rSetPgrp, pgrp, nil)
}

func (t *remoteTerminal) control(p *Process, sid, pgrp int32) error {
	var body [4]byte
	binary.LittleEndian.PutUint32(body[:], uint32(pgrp))

	return t.node.simple(p, rSetCtty, uint32(sid), body[:])
}

// reopen is the server's other open of the slave, which the OK brings, a
// terminal's.
func (t *remoteTerminal) reopen(p *Process, flags int) (Terminal, error) {
	pk, err := t.node.call(p, rOpen, uint32(flags), nil, nil)

	if err != nil {
		return nil, err
	}

	if len(pk.files) != 1 {
		dropFiles(pk.files)

		return nil, syscall.EIO
	}

	f, err := p.opened(&pk)

	if err != nil {
		return nil, err
	}

	if r := f.asTerminal(); r != nil {
		return r, nil
	}

	f.unref()

	return nil, syscall.EIO
}

// name is the path of the terminal's device, or with peer that of a
// master's slave: ENOTTY for a slave.
func (t *remoteTerminal) name(p *Process, peer bool) (string, error) {
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
