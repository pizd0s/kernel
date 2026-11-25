package main

import (
	"sync"
	"sync/atomic"
	"time"
)

type UnixListener struct {
	ObjectIdent

	mu        sync.Mutex
	conns     MsgChannel[*PairEnd]
	backlog   int
	watchers  Subscribers
	listening bool
	gone      bool
	ino       uint64
	kind      int
}

func (l *UnixListener) closed() {
}

func (l *UnixListener) shut() {
	var dropped []*PairEnd

	l.mu.Lock()
	l.gone = true

	for e, ok := l.conns.take(); ok; e, ok = l.conns.take() {
		dropped = append(dropped, e)
	}

	l.watchers.tell()
	l.mu.Unlock()

	for _, e := range dropped {
		e.closed()
	}
}

func (l *UnixListener) listens() bool {
	l.mu.Lock()

	defer l.mu.Unlock()

	return l.listening && !l.gone
}

func (l *UnixListener) open(backlog int) {
	l.mu.Lock()
	l.listening, l.backlog = true, min(max(backlog, 1), SOMAXCONN)
	l.watchers.tell()
	l.mu.Unlock()
}

func (l *UnixListener) next(p *Process, nonblock bool) (*PairEnd, error) {
	l.mu.Lock()

	for {
		e, ok := l.conns.take()

		var err error

		switch {
		case !l.listening || l.gone:
			err = EINVAL
		case ok:
			l.watchers.tellOut()
		case nonblock:
			err = EAGAIN
		default:
			_, err = p.watch(&l.mu, &l.watchers, false, time.Time{}, restarts)

			if err == nil {
				continue
			}
		}

		l.mu.Unlock()

		if err != nil && ok {
			e.closed()
		}

		if err != nil {
			return nil, err
		}

		return e, nil
	}
}

func (l *UnixListener) push(e *PairEnd) (queued, refused bool) {
	l.mu.Lock()

	defer l.mu.Unlock()

	if !l.listening || l.gone {
		return false, true
	}

	if l.conns.len() >= l.backlog {
		return false, false
	}

	l.conns.put(e)
	l.watchers.tellIn()

	return true, false
}

func (l *UnixListener) awaitRoom(p *Process, end time.Time, how Signalled) (timedOut bool, err error) {
	l.mu.Lock()

	if l.listening && !l.gone && l.conns.len() >= l.backlog {
		timedOut, err = p.watch(&l.mu, &l.watchers, true, end, how)
	}

	l.mu.Unlock()

	return timedOut, err
}

func (l *UnixListener) stat(p *Process, st *Stat) error {
	*st = Stat{Ino: l.ino, Mode: S_IFSOCK | 0o777, Nlink: 1, Blksize: 4096}

	return nil
}

func (l *UnixListener) poll(events int16, through []*Epoll) int16 {
	l.mu.Lock()

	shut, n, room := l.gone, l.conns.len(), l.conns.len() < l.backlog

	l.mu.Unlock()

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

	l.mu.Lock()

	if in {
		l.watchers.add(&s.in)
	}

	if out {
		l.watchers.add(&s.out)
	}

	l.mu.Unlock()
}

func (l *UnixListener) unsubscribe(s *Subscription) {
	l.mu.Lock()
	l.watchers.remove(&s.in)
	l.watchers.remove(&s.out)
	l.mu.Unlock()
}

func (l *UnixListener) sides(events int16) (in, out bool) {
	in, out = events&(POLLIN|POLLRDNORM) != 0, events&(POLLOUT|POLLWRNORM) != 0

	if !in && !out {
		in, out = true, true
	}

	return in, out
}

func (l *UnixListener) flags(p *Process) (int, error) {
	return O_RDWR, nil
}

func (l *UnixListener) setFlags(p *Process, fl int) error {
	return nil
}

func (l *UnixListener) call(p *Process, r Request, group uint32, waits bool) (bool, error) {
	switch r := r.(type) {
	case *FileStat:
		return false, l.stat(p, r.st)
	case *SockKnock:
		var err error

		r.conn, err = l.knock(p, r.kind, r.nonblock, r.timeout)

		return false, err
	case *FileGetfl:
		var err error

		r.flags, err = l.flags(p)

		return false, err
	case *FileSetfl:
		return false, l.setFlags(p, r.flags)
	case *FileRead:
		return false, EBADF
	case *FileWrite:
		return false, EBADF
	case *FileCopyTo, *FileCopyFrom:
		return false, EBADF
	}

	return false, errUnserved
}

func (l *UnixListener) knock(p *Process, kind int, nonblock bool, timeout *atomic.Int64) (*PairEnd, error) {
	if l.kind != kind {
		return nil, EPROTOTYPE
	}

	mine, theirs := newPairOf(kind)

	for {
		queued, refused := l.push(theirs)

		if queued {
			return mine, nil
		}

		var err error

		if !refused && !nonblock {
			end, how := until(timeout)

			var timedOut bool

			if timedOut, err = l.awaitRoom(p, end, how); err == nil && !timedOut {
				continue
			} else if err == nil {
				err = EAGAIN
			}
		} else if refused {
			err = ECONNREFUSED
		} else {
			err = EAGAIN
		}

		mine.closed()
		theirs.closed()

		return nil, err
	}
}
