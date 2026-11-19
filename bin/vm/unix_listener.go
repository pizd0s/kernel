//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"syscall"
)

// unixListener is a bound socket's: the connections that wait for accept,
// as many as listen's backlog lets, while its socket is open and listens,
// in a channel (msg_channel.go) whose messages are the connections' ends,
// of its socket's kind. The socket node of its name holds it too.
type unixListener struct {
	refs      atomic.Int32
	mu        sync.Mutex
	conns     *msgChannel
	listening bool
	closed    bool
	ino       uint64
	kind      int
}

func (l *unixListener) ref() {
	l.refs.Add(1)
}

func (l *unixListener) unref() {
	if l.refs.Add(-1) == 0 {
		l.shut()
	}
}

// A unixListener is a File, and itself to a call that wants one.
func (l *unixListener) asFile() File              { return l }
func (l *unixListener) asTransfer() Transfer      { return nil }
func (l *unixListener) asSeeker() Seeker          { return nil }
func (l *unixListener) asDir() Dir                { return nil }
func (l *unixListener) asTerminal() Terminal      { return nil }
func (l *unixListener) asWindowed() Windowed      { return nil }
func (l *unixListener) asLink() Link              { return nil }
func (l *unixListener) asAttrs() Attrs            { return nil }
func (l *unixListener) asSocket() Socket          { return nil }
func (l *unixListener) asIoctl() Ioctl            { return nil }
func (l *unixListener) asLocker() Locker          { return nil }
func (l *unixListener) asEpoll() *epoll           { return nil }
func (l *unixListener) asMemfd() *memfdFile       { return nil }
func (l *unixListener) asPair() *pairEnd          { return nil }
func (l *unixListener) asListener() *unixListener { return l }
func (l *unixListener) asConsole() *consoleEnd    { return nil }
func (l *unixListener) asVFS() *remoteDir         { return nil }
func (l *unixListener) asCall() *callEnd          { return nil }

// shut ends the listener: it refuses from then on, and the connections
// that waited are reset.
func (l *unixListener) shut() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.conns.finish()
	l.conns.nudge()
}

func (l *unixListener) listens() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.listening && !l.closed
}

// pop is the first connection that waits, or none.
func (l *unixListener) pop() *pairEnd {
	pk, _, ok, _ := l.conns.take()

	if !ok || len(pk.files) == 0 {
		return nil
	}

	return pk.files[0].asPair()
}

// push queues a connection: refused when the listener is shut or does
// not listen, not queued when its backlog is full.
func (l *unixListener) push(e *pairEnd) (queued, refused bool) {
	l.mu.Lock()
	listens := l.listening && !l.closed
	l.mu.Unlock()

	if !listens {
		return false, true
	}

	pk := packet{files: []Object{e}}
	full, err := l.conns.put(&pk)

	if err != nil {
		return false, true
	}

	return !full, false
}

func (l *unixListener) stat(p *Process, st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Ino: l.ino, Mode: syscall.S_IFSOCK | 0o777, Nlink: 1, Blksize: 4096}

	return nil
}

// poll is accept's input, a connection waits, and connect's output, the
// queue has room.
func (l *unixListener) poll(events int16) int16 {
	l.mu.Lock()
	shut := l.closed
	l.mu.Unlock()
	n, room := l.conns.count(), l.conns.room()
	mask := int16(0)

	if n > 0 {
		mask |= pollIn | pollRdnorm
	}

	if room || shut {
		mask |= pollOut | pollWrnorm
	}

	if shut {
		mask |= pollHup
	}

	return mask & (events | pollErr | pollHup)
}

// subscribe makes to learn of the connections that come, for accept's
// input, and of the room for them, for connect's output.
func (l *unixListener) subscribe(s *subscription, events int16) {
	in, out := l.sides(events)

	if in {
		l.conns.subscribe(&s.in, false)
	}

	if out {
		l.conns.subscribe(&s.out, true)
	}
}

func (l *unixListener) unsubscribe(s *subscription) {
	l.conns.unsubscribe(&s.in)
	l.conns.unsubscribe(&s.out)
}

// sides are what of the listener a wait for events is on: accept's input,
// connect's output, both when neither is asked.
func (l *unixListener) sides(events int16) (in, out bool) {
	in, out = events&(pollIn|pollRdnorm) != 0, events&(pollOut|pollWrnorm) != 0

	if !in && !out {
		in, out = true, true
	}

	return in, out
}

func (l *unixListener) flags() (int, error) {
	return syscall.O_RDWR, nil
}

func (l *unixListener) setFlags(fl int) error {
	return nil
}
