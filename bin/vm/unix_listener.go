//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
)

type UnixListener struct {
	refs      atomic.Int32
	mu        sync.Mutex
	conns     *MsgChannel
	listening bool
	closed    bool
	ino       uint64
	kind      int
}

func (l *UnixListener) ref() {
	l.refs.Add(1)
}

func (l *UnixListener) unref() {
	if l.refs.Add(-1) == 0 {
		l.shut()
	}
}

func (l *UnixListener) asFile() File {
	return l
}

func (l *UnixListener) asTransfer() Transfer {
	return nil
}

func (l *UnixListener) asSeeker() Seeker {
	return nil
}

func (l *UnixListener) asDir() Dir {
	return nil
}

func (l *UnixListener) asTerminal() Terminal {
	return nil
}

func (l *UnixListener) asWindowed() Windowed {
	return nil
}

func (l *UnixListener) asLink() Link {
	return nil
}

func (l *UnixListener) asAttrs() Attrs {
	return nil
}

func (l *UnixListener) asSocket() Socket {
	return nil
}

func (l *UnixListener) asIoctl() Ioctl {
	return nil
}

func (l *UnixListener) asLocker() Locker {
	return nil
}

func (l *UnixListener) asEpoll() *Epoll {
	return nil
}

func (l *UnixListener) asMemfd() *MemfdFile {
	return nil
}

func (l *UnixListener) asPair() *PairEnd {
	return nil
}

func (l *UnixListener) asListener() *UnixListener {
	return l
}

func (l *UnixListener) asConsole() *ConsoleEnd {
	return nil
}

func (l *UnixListener) asVFS() *RemoteDir {
	return nil
}

func (l *UnixListener) asCall() *CallEnd {
	return nil
}

func (l *UnixListener) shut() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.conns.finish()
	l.conns.nudge()
}

func (l *UnixListener) listens() bool {
	l.mu.Lock()

	defer l.mu.Unlock()

	return l.listening && !l.closed
}

func (l *UnixListener) pop() *PairEnd {
	pk, _, ok, _ := l.conns.take()

	if !ok || len(pk.files) == 0 {
		return nil
	}

	return pk.files[0].asPair()
}

func (l *UnixListener) push(e *PairEnd) (queued, refused bool) {
	l.mu.Lock()

	listens := l.listening && !l.closed

	l.mu.Unlock()

	if !listens {
		return false, true
	}

	pk := Packet{files: []Object{e}}
	full, err := l.conns.put(&pk)

	if err != nil {
		return false, true
	}

	return !full, false
}

func (l *UnixListener) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: l.ino, Mode: S_IFSOCK | 0o777, Nlink: 1, Blksize: 4096}

	return nil
}

func (l *UnixListener) poll(events int16) int16 {
	l.mu.Lock()

	shut := l.closed

	l.mu.Unlock()

	n, room := l.conns.count(), l.conns.room()
	mask := int16(0)

	if n > 0 {
		mask |= POLLIN | POLLRDNORM
	}

	if room || shut {
		mask |= POLLOUT | POLLWRNORM
	}

	if shut {
		mask |= POLLHUP
	}

	return mask & (events | POLLERR | POLLHUP)
}

func (l *UnixListener) subscribe(s *Subscription, events int16) {
	in, out := l.sides(events)

	if in {
		l.conns.subscribe(&s.in, false)
	}

	if out {
		l.conns.subscribe(&s.out, true)
	}
}

func (l *UnixListener) unsubscribe(s *Subscription) {
	l.conns.unsubscribe(&s.in)
	l.conns.unsubscribe(&s.out)
}

func (l *UnixListener) sides(events int16) (in, out bool) {
	in, out = events&(POLLIN|POLLRDNORM) != 0, events&(POLLOUT|POLLWRNORM) != 0

	if !in && !out {
		in, out = true, true
	}

	return in, out
}

func (l *UnixListener) flags() (int, error) {
	return O_RDWR, nil
}

func (l *UnixListener) setFlags(fl int) error {
	return nil
}
