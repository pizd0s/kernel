//go:build linux && amd64

package main

import (
	"encoding/binary"
	"strconv"
	"strings"
	"syscall"
)

// A terminal is a service's open file (kernel_remote.go) whose OPEN's OK
// said openTerminal: an end of a pseudoterminal of bin/tools/pty.cpp, the
// system's only terminals. The kernel keeps nothing of one: its settings,
// its window, its foreground group and its discipline are its server's,
// and a terminal's call is a request to it (lib/protocol/protocol.h). A
// terminal has no offsets: seek and the positioned reads and writes are
// ESPIPE. The console is no terminal (kernel_console.go).
//
// What the kernel keeps is the processes': a session's controlling
// terminal, which its leader takes with TIOCSCTTY or by opening a slave
// with no controlling terminal and no O_NOCTTY, which /dev/tty opens, and
// which goes with the leader; and job control, as Linux's: a read of its
// controlling terminal from a group not in the foreground is SIGTTIN to
// the group, a write with TOSTOP or a change of the terminal SIGTTOU; the
// foreground group and TOSTOP are what the terminal's READY said last.
// dev and ino are the terminal's device's, the same for each open of it.
type remoteTerminal struct {
	remoteFile
	dev, ino uint64
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

// An OPEN's OK's arg: the open file is a terminal's; the open is of the
// opener's controlling terminal (/dev/tty), which the kernel opens.
const (
	openTerminal    = 2
	openControlling = 4
)

// The terminal's requests of ioctl, Linux's.
const (
	tiocsctty  = 0x540e
	tiocoutq   = 0x5411
	tiocnotty  = 0x5422
	tiocgsid   = 0x5429
	tiocgptn   = 0x80045430
	tiocsptlck = 0x40045431
)

func sameTerminal(a, b *remoteTerminal) bool {
	return a.dev == b.dev && a.ino == b.ino
}

// isCtty tells whether a terminal is the caller's controlling terminal.
func (p *Process) isCtty(t *remoteTerminal) bool {
	p.k.mu.Lock()
	c := p.k.cttys[p.sid]
	p.k.mu.Unlock()

	return c != nil && sameTerminal(c, t)
}

// openControlling opens the caller's controlling terminal anew, /dev/tty's
// open: ENXIO for a session that has none.
func (p *Process) openControlling(flags int) (File, error) {
	p.k.mu.Lock()
	c := p.k.cttys[p.sid]
	p.k.mu.Unlock()

	if c == nil {
		return nil, syscall.ENXIO
	}

	name, err := c.name(p, false)

	if err != nil {
		return nil, err
	}

	return p.openPath(name, flags|syscall.O_NOCTTY, 0)
}

// acquire makes a terminal opened without O_NOCTTY the controlling
// terminal of the caller's session when it leads one that has none, as
// Linux's open does; one that controls another stays as it is.
func (p *Process) acquire(t *remoteTerminal) {
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
func (p *Process) background(t *remoteTerminal, sig syscall.Signal) error {
	for {
		t.readiness()
		foreground := t.foreground.Load()
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

	return t.remoteFile.read(p, b, mode)
}

func (t *remoteTerminal) write(p *Process, b []byte, mode ioMode) (int, error) {
	if t.readiness(); t.tostop.Load() {
		if err := p.background(t, syscall.SIGTTOU); err != nil {
			return 0, err
		}
	}

	return t.remoteFile.write(p, b, mode)
}

// changeAt is the terminal of a descriptor for a call that changes it,
// past job control's check.
func (p *Process) changeAt(fd uint32) (*remoteTerminal, error) {
	t, err := p.terminalAt(fd)

	if err == nil {
		err = p.background(t, syscall.SIGTTOU)
	}

	return t, err
}

// leaveSession is the going of a session's leader: its controlling
// terminal controls no session any more, and its foreground group hears
// SIGHUP and SIGCONT, as Linux's does.
func (p *Process) leaveSession() {
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

	t.readiness()
	foreground := t.foreground.Load()

	var body [4]byte
	t.simple(p, rSetCtty, 0, body[:])

	if foreground > 0 {
		p.k.signalGroup(foreground, int(syscall.SIGHUP))
		p.k.signalGroup(foreground, int(syscall.SIGCONT))
	}

	t.unref()
}

func (t *remoteTerminal) seek(p *Process, off int64, whence int) (int64, error) {
	return 0, syscall.ESPIPE
}

func (t *remoteTerminal) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (t *remoteTerminal) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

// terminalAt is the terminal of a descriptor: EBADF for none open, ENOTTY
// for a file that is no terminal.
func (p *Process) terminalAt(fd uint32) (*remoteTerminal, error) {
	f, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	t, ok := f.(*remoteTerminal)

	if !ok {
		return nil, syscall.ENOTTY
	}

	return t, nil
}

// waits is a call that waits for the terminal's output to go: a signal
// cancels it, and it restarts when the handlers ask for that.
func (t *remoteTerminal) waits(p *Process, op, arg uint32, body []byte) error {
	for {
		_, cancelled, err := t.waiting(p, op, arg, body, nil)

		if err == syscall.EINTR && cancelled && p.interrupted() {
			continue
		}

		return err
	}
}

// count is a call answered with a count.
func (t *remoteTerminal) count(p *Process, op uint32) (uint32, error) {
	pk, err := t.on(p, op, 0, nil)

	if err == nil && len(pk.data) != 4 {
		err = syscall.EIO
	}

	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint32(pk.data), nil
}

// name is the path of the terminal's device, or with peer that of a
// master's slave: ENOTTY for a slave.
func (t *remoteTerminal) name(p *Process, peer bool) (string, error) {
	arg := uint32(0)

	if peer {
		arg = 1
	}

	pk, err := t.on(p, rName, arg, nil)

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

	pk, err := t.on(p, rTcgetattr, 0, nil)

	if err == nil && len(pk.data) != sizeofTermios {
		err = syscall.EIO
	}

	if err == nil {
		copy(p.span(at, sizeofTermios), pk.data)
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

	body := append([]byte(nil), p.span(at, sizeofTermios)...)

	if action == 0 {
		return t.simple(p, rTcsetattr, action, body)
	}

	return t.waits(p, rTcsetattr, action, body)
}

func (p *Process) libcTcflow(fd, action uint32) uint32 {
	return p.result(0, p.onTerminal(fd, rTcflow, action))
}

func (p *Process) libcTcflush(fd, queue uint32) uint32 {
	return p.result(0, p.onTerminal(fd, rTcflush, queue))
}

// onTerminal is a call that changes a terminal and answers nothing.
func (p *Process) onTerminal(fd, op, arg uint32) error {
	t, err := p.changeAt(fd)

	if err != nil {
		return err
	}

	return t.simple(p, op, arg, nil)
}

func (p *Process) libcTcdrain(fd uint32) uint32 {
	t, err := p.changeAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(0, t.waits(p, rTcdrain, 0, nil))
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
		if err == nil && !p.isCtty(t) && !t.master(p) {
			err = syscall.ENOTTY
		}

		if err != nil {
			return p.result(0, err)
		}

		group, err := t.count(p, rGetPgrp)

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
		return p.result(0, t.simple(p, rSetPgrp, pgrp, nil))
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

// master tells whether a terminal is a master, which has a slave.
func (t *remoteTerminal) master(p *Process) bool {
	_, err := t.name(p, true)

	return err == nil
}

// controls makes a terminal the controlling terminal of the caller's
// session, TIOCSCTTY's: the caller leads a session that has none, or has
// this one, and its group is the foreground group. EPERM when the
// terminal controls another session.
func (p *Process) controls(t *remoteTerminal) error {
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

	var body [4]byte
	binary.LittleEndian.PutUint32(body[:], uint32(pgid))

	if err := t.simple(p, rSetCtty, uint32(sid), body[:]); err != nil {
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
func (p *Process) notty(t *remoteTerminal) error {
	if !p.isCtty(t) {
		return syscall.ENOTTY
	}

	p.leaveSession()

	return nil
}

// terminalIoctl is ioctl's request on a terminal, and whether it is one
// of a terminal's.
func (p *Process) terminalIoctl(t *remoteTerminal, fd, req, va uint32) (uint32, bool) {
	arg := p.varArg(va, 0)

	switch req {
	case tcgets:
		return p.result(0, p.tcgetattr(fd, arg)), true
	case tcsets, tcsets + 1, tcsets + 2:
		return p.result(0, p.tcsetattr(fd, req-tcsets, arg)), true
	case tcflsh:
		return p.result(0, p.onTerminal(fd, rTcflush, arg)), true
	case tcxonc:
		return p.result(0, p.onTerminal(fd, rTcflow, arg)), true
	case tcsbrk:
		if err := p.background(t, syscall.SIGTTOU); err != nil || arg == 0 {
			return p.result(0, err), true
		}

		return p.result(0, t.waits(p, rTcdrain, 0, nil)), true
	case tiocgwinsz:
		pk, err := t.on(p, rGetWinsize, 0, nil)

		if err == nil && len(pk.data) != sizeofWinsize {
			err = syscall.EIO
		}

		if err == nil {
			copy(p.span(arg, sizeofWinsize), pk.data)
		}

		return p.result(0, err), true
	case tiocswinsz:
		body := append([]byte(nil), p.span(arg, sizeofWinsize)...)

		return p.result(0, t.simple(p, rSetWinsize, 0, body)), true
	case fionread, tiocoutq:
		op := rInq

		if req == tiocoutq {
			op = rOutq
		}

		n, err := t.count(p, op)

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
func (p *Process) slaveNumber(t *remoteTerminal) (int, error) {
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
		var t *remoteTerminal

		if t, err = p.terminalAt(uint32(slave)); err == nil {
			err = t.simple(p, rSetWinsize, 0, append([]byte(nil), p.span(winp, sizeofWinsize)...))
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

		var body [4]byte
		binary.LittleEndian.PutUint32(body[:], uint32(c.pid))

		if t.simple(p, rSetCtty, uint32(c.pid), body[:]) == nil {
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

	at := p.passwordAt
	pk, err := t.on(p, rTcgetattr, 0, nil)

	if err != nil || len(pk.data) != sizeofTermios {
		p.setErrno(syscall.EIO)

		return 0
	}

	saved := pk.data
	quiet := append([]byte(nil), saved...)
	le := binary.LittleEndian
	le.PutUint32(quiet, le.Uint32(quiet)&^(inlcr|igncr)|icrnl)
	le.PutUint32(quiet[12:], le.Uint32(quiet[12:])&^(echo|isig)|icanon)
	t.waits(p, rTcsetattr, 2, quiet)
	t.write(p, p.span(prompt, uint32(len(p.str(prompt)))), ioMode{})
	n, err := t.read(p, p.span(at, passwordSize), ioMode{})

	if err == nil {
		if n > 0 && p.span(at, passwordSize)[n-1] == '\n' || n == passwordSize {
			n--
		}

		p.span(at, passwordSize)[n] = 0
	}

	t.waits(p, rTcsetattr, 2, saved)
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
