//go:build linux && amd64

package main

import (
	"encoding/binary"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// A terminal is a service's open file (kernel_remote.go) born of a served
// pair of SERVED_TERMINAL: an end of a pseudoterminal of
// bin/tools/pty.cpp, the system's only terminals. The kernel keeps nothing
// of one: its settings, its window, its foreground group and its
// discipline are its server's. The kernel's calls see it as the Terminal
// role (kernel_object.go), which remoteTerminal answers by requests to its
// server (lib/protocol/protocol.h). A terminal has no offsets: seek and the
// positioned reads and writes are ESPIPE. The console is no terminal
// (kernel_console.go).
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
	// key names the terminal: its server, and the dev and ino of its
	// device the server said (fileKey).
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

// A service's OPEN, and on a slave's open file another open of the slave.
const rOpen uint32 = 96

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

// An OPEN's OK's arg: the open is of the opener's controlling terminal
// (/dev/tty), which the kernel opens.
const openControlling = 4

// The terminal's requests of ioctl, Linux's.
const (
	tiocsctty  = 0x540e
	tiocoutq   = 0x5411
	tiocnotty  = 0x5422
	tiocgsid   = 0x5429
	tiocgptn   = 0x80045430
	tiocsptlck = 0x40045431
)

// sameTerminal tells whether two opens are of one terminal.
func sameTerminal(a, b Terminal) bool {
	return a.device() == b.device()
}

// isCtty tells whether a terminal is the caller's controlling terminal.
func (p *Process) isCtty(t Terminal) bool {
	p.k.mu.Lock()
	c := p.k.cttys[p.sid]
	p.k.mu.Unlock()

	return c != nil && sameTerminal(c, t)
}

// openControlling opens the caller's controlling terminal anew, /dev/tty's
// open: another open of the terminal, which no owner's permission keeps
// out, as Linux's does not; ENXIO for a session that has none, or whose
// terminal hung up, which controls it no more.
func (p *Process) openControlling(flags int) (Object, error) {
	p.k.mu.Lock()
	c := p.k.cttys[p.sid]

	if c != nil {
		c.ref()
	}

	p.k.mu.Unlock()

	if c == nil {
		return nil, syscall.ENXIO
	}

	defer c.unref()

	if _, _, hungUp := c.jobs(); hungUp {
		return nil, syscall.ENXIO
	}

	return c.reopen(p, flags)
}

// acquire makes a terminal opened without O_NOCTTY the controlling
// terminal of the caller's session when it leads one that has none, as
// Linux's open does; one that controls another stays as it is.
func (p *Process) acquire(t Terminal) {
	p.k.mu.Lock()
	_, has := p.k.cttys[p.sid]
	leads := p.sid == p.pid
	p.k.mu.Unlock()

	if leads && !has {
		p.controls(t)
	}
}

// shrugs tells whether a process ignores or blocks a signal.
func (p *Process) shrugs(sig int) bool {
	p.sig.mu.Lock()
	defer p.sig.mu.Unlock()

	return p.sig.actions[sig].handler == sigIgn || p.sig.blocked&sigBit(sig) != 0
}

// orphaned tells whether a process group is orphaned: no member has a
// parent in another group of its session, which could continue it.
func (k *Kernel) orphaned(pgid int32) bool {
	k.mu.Lock()
	defer k.mu.Unlock()

	return k.orphanedLocked(pgid)
}

func (k *Kernel) orphanedLocked(pgid int32) bool {
	for _, q := range k.procs {
		if q.pgid != pgid || q.zombie {
			continue
		}

		if parent := k.procs[q.ppid]; parent != nil && parent.pgid != pgid && parent.sid == q.sid {
			return false
		}
	}

	return true
}

// orphans is what a process's going does to the groups it joined to its
// session, its own and its children's, of a parent in another group of
// it: one orphaned now, with a stopped process, which no one could
// continue, hears SIGHUP and SIGCONT, as POSIX says.
func (k *Kernel) orphans(groups []int32) {
	for i, pgid := range groups {
		if slices.Contains(groups[:i], pgid) {
			continue
		}

		k.mu.Lock()
		stopped := false

		for _, q := range k.procs {
			stopped = stopped || q.pgid == pgid && !q.zombie && q.halted
		}

		hangs := stopped && k.orphanedLocked(pgid)
		k.mu.Unlock()

		if hangs {
			k.signalGroup(pgid, int(syscall.SIGHUP))
			k.signalGroup(pgid, int(syscall.SIGCONT))
		}
	}
}

// signalGroup is the kernel's signal to a process group, which no
// permission keeps out.
func (k *Kernel) signalGroup(pgid int32, sig int) {
	k.mu.Lock()

	var targets []*Process

	for _, q := range k.procs {
		if q.pgid == pgid && !q.zombie {
			targets = append(targets, q)
		}
	}

	k.mu.Unlock()

	for _, q := range targets {
		q.send(sig)
	}
}

// background is job control's check of a call on a terminal: a process
// whose controlling terminal it is, of a group not in its foreground,
// stops by sig, SIGTTIN for a read, SIGTTOU for a write with TOSTOP or a
// change of the terminal. A read that ignores or blocks SIGTTIN is EIO,
// and one of an orphaned group, which no one would continue; a write or
// a change that ignores or blocks SIGTTOU goes on, one of an orphaned
// group is EIO. Else the group hears sig, and the call goes on once the
// process does, when its handlers ask for that: EINTR else.
func (p *Process) background(t Terminal, sig syscall.Signal) error {
	for {
		foreground, _, _ := t.jobs()
		p.k.mu.Lock()
		pgid := p.pgid
		p.k.mu.Unlock()

		if foreground == 0 || foreground == pgid || !p.isCtty(t) {
			return nil
		}

		if p.shrugs(int(sig)) {
			if sig == syscall.SIGTTIN {
				return syscall.EIO
			}

			return nil
		}

		if p.k.orphaned(pgid) {
			return syscall.EIO
		}

		p.k.signalGroup(pgid, int(sig))

		if !p.interrupted() {
			return syscall.EINTR
		}
	}
}

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

// changeAt is the terminal of a descriptor for a call that changes it,
// past job control's check.
func (p *Process) changeAt(fd uint32) (Terminal, error) {
	t, err := p.terminalAt(fd)

	if err == nil {
		err = p.background(t, syscall.SIGTTOU)
	}

	return t, err
}

// leaveSession is the going of a session's leader, or its TIOCNOTTY: its
// controlling terminal controls no session any more, and its foreground
// group hears SIGHUP, and SIGCONT after for a TIOCNOTTY, as Linux's
// pseudoterminals do.
func (p *Process) leaveSession(going bool) {
	p.k.mu.Lock()
	t := p.k.cttys[p.sid]
	leads := p.sid == p.pid

	if leads && t != nil {
		delete(p.k.cttys, p.sid)
	}

	p.k.mu.Unlock()

	if !leads || t == nil {
		return
	}

	foreground, _, _ := t.jobs()
	t.control(p, 0, 0)

	if foreground > 0 {
		p.k.signalGroup(foreground, int(syscall.SIGHUP))

		if !going {
			p.k.signalGroup(foreground, int(syscall.SIGCONT))
		}
	}

	t.unref()
}

func (t *remoteTerminal) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (t *remoteTerminal) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

// terminalAt is the terminal of a descriptor: EBADF for none open, ENOTTY
// for a file that is no terminal.
func (p *Process) terminalAt(fd uint32) (Terminal, error) {
	f, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	t := f.asTerminal()

	if t == nil {
		return nil, syscall.ENOTTY
	}

	return t, nil
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

	return pk.data, err
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

	return pk.data, err
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

	if len(pk.files) == 1 {
		if r := pk.files[0].asTerminal(); r != nil {
			return r, nil
		}
	}

	dropFiles(pk.files)

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

// The calls of termios.h.

func (p *Process) libcIsatty(fd uint32) uint32 {
	if _, err := p.terminalAt(fd); err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	return 1
}

func (p *Process) libcTcgetattr(fd, at uint32) uint32 {
	return p.result(0, p.tcgetattr(fd, at))
}

func (p *Process) tcgetattr(fd, at uint32) error {
	t, err := p.terminalAt(fd)

	if err != nil {
		return err
	}

	settings, err := t.getattr(p)

	if err == nil {
		copy(p.span(at, sizeofTermios), settings)
	}

	return err
}

// tcsetattr's action is checked first, as musl checks it: EINVAL past
// TCSAFLUSH. One that drains waits for the output to go.
func (p *Process) libcTcsetattr(fd, action, at uint32) uint32 {
	return p.result(0, p.tcsetattr(fd, action, at))
}

func (p *Process) tcsetattr(fd, action, at uint32) error {
	if action > 2 {
		return syscall.EINVAL
	}

	t, err := p.changeAt(fd)

	if err != nil {
		return err
	}

	return t.setattr(p, action, append([]byte(nil), p.span(at, sizeofTermios)...))
}

func (p *Process) libcTcflow(fd, action uint32) uint32 {
	t, err := p.changeAt(fd)

	if err == nil {
		err = t.flow(p, action)
	}

	return p.result(0, err)
}

func (p *Process) libcTcflush(fd, queue uint32) uint32 {
	t, err := p.changeAt(fd)

	if err == nil {
		err = t.flush(p, queue)
	}

	return p.result(0, err)
}

func (p *Process) libcTcdrain(fd uint32) uint32 {
	t, err := p.changeAt(fd)

	if err == nil {
		err = t.drain(p)
	}

	return p.result(0, err)
}

// A pseudoterminal has no line to break.
func (p *Process) libcTcsendbreak(fd, duration uint32) uint32 {
	_, err := p.changeAt(fd)

	return p.result(0, err)
}

// tcgetpgrp and tcsetpgrp are the foreground group of the caller's
// controlling terminal, or of the console (kernel_console.go); a master
// tells its slave's. The group set is one of the caller's session.
func (p *Process) libcTcgetpgrp(fd uint32) uint32 {
	if t, err := p.terminalAt(fd); err != syscall.ENOTTY {
		if err == nil && !p.isCtty(t) && !isMaster(p, t) {
			err = syscall.ENOTTY
		}

		if err != nil {
			return p.result(0, err)
		}

		group, err := t.foreground(p)

		return p.result(int(group), err)
	}

	if err := p.consoleAt(fd); err != nil {
		return p.result(0, err)
	}

	return p.result(int(p.k.foreground()), nil)
}

func (p *Process) libcTcsetpgrp(fd, pgrp uint32) uint32 {
	t, err := p.terminalAt(fd)

	switch {
	case err == syscall.ENOTTY:
		err = p.consoleAt(fd)
		t = nil
	case err == nil && !p.isCtty(t):
		err = syscall.ENOTTY
	case err == nil:
		err = p.background(t, syscall.SIGTTOU)
	}

	switch {
	case err != nil:
		return p.result(0, err)
	case int32(pgrp) < 0:
		return p.result(0, syscall.EINVAL)
	case !p.groupExists(int32(pgrp)):
		return p.result(0, syscall.EPERM)
	case t != nil:
		return p.result(0, t.setForeground(p, pgrp))
	}

	p.k.mu.Lock()
	p.k.fg = int32(pgrp)
	p.k.mu.Unlock()

	return p.result(0, nil)
}

// tcgetsid is the session of the caller's controlling terminal.
func (p *Process) libcTcgetsid(fd uint32) uint32 {
	t, err := p.terminalAt(fd)

	if err == nil && !p.isCtty(t) {
		err = syscall.ENOTTY
	}

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	return p.result(int(p.sid), nil)
}

// isMaster tells whether a terminal is a master, which has a slave.
func isMaster(p *Process, t Terminal) bool {
	_, err := t.name(p, true)

	return err == nil
}

// controls makes a terminal the controlling terminal of the caller's
// session, TIOCSCTTY's: the caller leads a session that has none, or has
// this one, and its group is the foreground group. EPERM when the
// terminal controls another session.
func (p *Process) controls(t Terminal) error {
	p.k.mu.Lock()
	sid, pgid, leads := p.sid, p.pgid, p.sid == p.pid
	c := p.k.cttys[sid]
	p.k.mu.Unlock()

	switch {
	case leads && c != nil && sameTerminal(c, t):
		return nil
	case !leads || c != nil:
		return syscall.EPERM
	}

	if err := t.control(p, sid, pgid); err != nil {
		return err
	}

	t.ref()
	p.k.mu.Lock()
	p.k.cttys[sid] = t
	p.k.mu.Unlock()

	return nil
}

// notty is TIOCNOTTY: the caller gives up its controlling terminal; a
// leader, for its session, as it does going.
func (p *Process) notty(t Terminal) error {
	if !p.isCtty(t) {
		return syscall.ENOTTY
	}

	p.leaveSession(false)

	return nil
}

// terminalIoctl is ioctl's request on a terminal, and whether it is one
// of a terminal's.
func (p *Process) terminalIoctl(t Terminal, fd, req, va uint32) (uint32, bool) {
	arg := p.varArg(va, 0)

	switch req {
	case tcgets:
		return p.result(0, p.tcgetattr(fd, arg)), true
	case tcsets, tcsets + 1, tcsets + 2:
		return p.result(0, p.tcsetattr(fd, req-tcsets, arg)), true
	case tcflsh:
		return p.libcTcflush(fd, arg), true
	case tcxonc:
		return p.libcTcflow(fd, arg), true
	case tcsbrk:
		if err := p.background(t, syscall.SIGTTOU); err != nil || arg == 0 {
			return p.result(0, err), true
		}

		return p.result(0, t.drain(p)), true
	case tiocgwinsz:
		size, err := t.window(p)

		if err == nil {
			copy(p.span(arg, sizeofWinsize), size)
		}

		return p.result(0, err), true
	case tiocswinsz:
		return p.result(0, t.setWindow(p, append([]byte(nil), p.span(arg, sizeofWinsize)...))), true
	case fionread, tiocoutq:
		count := t.pending

		if req == tiocoutq {
			count = t.unsent
		}

		n, err := count(p)

		if err == nil {
			p.put32(arg, n)
		}

		return p.result(0, err), true
	case tiocgptn:
		n, err := p.slaveNumber(t)

		if err == nil {
			p.put32(arg, uint32(n))
		}

		return p.result(0, err), true
	case tiocsptlck:
		_, err := t.name(p, true)

		return p.result(0, err), true
	case tiocsctty:
		return p.result(0, p.controls(t)), true
	case tiocnotty:
		return p.result(0, p.notty(t)), true
	case tiocgsid:
		r := p.libcTcgetsid(fd)

		if int32(r) >= 0 {
			p.put32(arg, r)
			r = 0
		}

		return r, true
	}

	return 0, false
}

// slaveNumber is the number of a master's slave, /dev/pts/N's.
func (p *Process) slaveNumber(t Terminal) (int, error) {
	name, err := t.name(p, true)

	if err != nil {
		return 0, err
	}

	n, err := strconv.Atoi(strings.TrimPrefix(name, "/dev/pts/"))

	if err != nil {
		return 0, syscall.EIO
	}

	return n, nil
}

// terminalName is ttyname's: the path of a terminal's device.
func (p *Process) terminalName(fd uint32) (string, syscall.Errno) {
	t, err := p.terminalAt(fd)

	if err == nil {
		var name string

		if name, err = t.name(p, false); err == nil {
			return name, 0
		}
	}

	return "", errnoOf(err)
}

// The pseudoterminals: posix_openpt opens /dev/pty, a new terminal's
// master; its slave is /dev/pts/N. grantpt has nothing to do, as musl's,
// and unlockpt nothing but to find a master: a slave opens whenever.

func (p *Process) libcPosix_openpt(flags uint32) uint32 {
	return p.result(p.open(atFdcwd, "/dev/pty", flags, 0))
}

func (p *Process) libcGrantpt(fd uint32) uint32 {
	_, err := p.fd(fd)

	return p.result(0, err)
}

func (p *Process) libcUnlockpt(fd uint32) uint32 {
	t, err := p.terminalAt(fd)

	if err == nil {
		_, err = t.name(p, true)
	}

	return p.result(0, err)
}

// ptsname is the slave's name in a buffer of its own, NULL with errno;
// ptsname_r the error number.
func (p *Process) libcPtsname(fd uint32) uint32 {
	name, err := p.ptsname(fd)

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	return p.constant(name)
}

func (p *Process) libcPtsname_r(fd, buf, size uint32) uint32 {
	name, err := p.ptsname(fd)

	switch {
	case err != nil:
		return uint32(errnoOf(err))
	case uint32(len(name)) >= size:
		return uint32(syscall.ERANGE)
	}

	p.putString(buf, name)

	return 0
}

func (p *Process) ptsname(fd uint32) (string, error) {
	t, err := p.terminalAt(fd)

	if err != nil {
		return "", err
	}

	return t.name(p, true)
}

// openPty opens a new terminal's master and its slave, as descriptors,
// the slave set to termios at termp and its window to winsize at winp
// when they are not NULL: the descriptors, and the slave's name.
func (p *Process) openPty(termp, winp uint32) (int, int, string, error) {
	master, err := p.open(atFdcwd, "/dev/pty", syscall.O_RDWR|syscall.O_NOCTTY, 0)

	if err != nil {
		return 0, 0, "", err
	}

	name, err := p.ptsname(uint32(master))
	slave := 0

	if err == nil {
		slave, err = p.open(atFdcwd, name, syscall.O_RDWR|syscall.O_NOCTTY, 0)
	}

	if err == nil && termp != 0 {
		err = p.tcsetattr(uint32(slave), 0, termp)
	}

	if err == nil && winp != 0 {
		var t Terminal

		if t, err = p.terminalAt(uint32(slave)); err == nil {
			err = t.setWindow(p, append([]byte(nil), p.span(winp, sizeofWinsize)...))
		}
	}

	if err != nil {
		p.closeFd(master)

		if slave != 0 {
			p.closeFd(slave)
		}

		return 0, 0, "", err
	}

	return master, slave, name, nil
}

func (p *Process) libcOpenpty(amaster, aslave, name, termp, winp uint32) uint32 {
	master, slave, path, err := p.openPty(termp, winp)

	if err != nil {
		return p.result(0, err)
	}

	p.put32(amaster, uint32(master))
	p.put32(aslave, uint32(slave))

	if name != 0 {
		p.putString(name, path)
	}

	return 0
}

// forkpty forks a child whose standard input, output and error are a new
// terminal's slave, the terminal of a session it leads, its group the
// foreground group; the parent keeps the master.
func (p *Process) libcForkpty(amaster, name, termp, winp uint32) uint32 {
	master, slave, path, err := p.openPty(termp, winp)

	if err != nil {
		return p.result(0, err)
	}

	t, err := p.terminalAt(uint32(slave))

	if err != nil {
		return p.result(0, err)
	}

	pid, err := p.fork(func(c *Process) {
		c.k.mu.Lock()
		c.sid, c.pgid = c.pid, c.pid
		c.k.mu.Unlock()

		if t.control(p, c.pid, c.pid) == nil {
			t.ref()
			c.k.mu.Lock()
			c.k.cttys[c.pid] = t
			c.k.mu.Unlock()
		}

		for fd := range 3 {
			c.closeFd(fd)
			t.ref()
			c.files.put(fd, t, false)
		}

		c.closeFd(master)
		c.closeFd(slave)
	})

	p.closeFd(slave)

	if err != nil {
		p.closeFd(master)

		return p.result(0, err)
	}

	if amaster != 0 {
		p.put32(amaster, uint32(master))
	}

	if name != 0 {
		p.putString(name, path)
	}

	return uint32(pid)
}

// getpass reads a password from the caller's controlling terminal, as
// musl's: /dev/tty, its echo and signals off for the line, the prompt
// written to it; NULL with open's error when there is none. The password
// is in a buffer of the process's, the same for each call.
func (p *Process) libcGetpass(prompt uint32) uint32 {
	fd, err := p.open(atFdcwd, "/dev/tty", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	defer p.closeFd(fd)

	t, err := p.terminalAt(uint32(fd))

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	if p.passwordAt == 0 {
		p.passwordAt = p.malloc(passwordSize + 1)
	}

	if p.passwordAt == 0 {
		p.setErrno(syscall.ENOMEM)

		return 0
	}

	at := p.passwordAt
	saved, err := t.getattr(p)

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	quiet := append([]byte(nil), saved...)
	le := binary.LittleEndian
	le.PutUint32(quiet, le.Uint32(quiet)&^(inlcr|igncr)|icrnl)
	le.PutUint32(quiet[12:], le.Uint32(quiet[12:])&^(echo|isig)|icanon)
	t.setattr(p, 2, quiet)
	t.write(p, p.span(prompt, uint32(len(p.str(prompt)))), ioMode{})
	n, err := t.read(p, p.span(at, passwordSize), ioMode{})

	if err == nil {
		if n > 0 && p.span(at, passwordSize)[n-1] == '\n' || n == passwordSize {
			n--
		}

		p.span(at, passwordSize)[n] = 0
	}

	t.setattr(p, 2, saved)
	newline := p.span(at+passwordSize, 1)
	newline[0] = '\n'
	t.write(p, newline, ioMode{})

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	return at
}

// passwordSize is getpass's buffer's, musl's; a byte past it holds the
// newline written after the password.
const passwordSize = 128
