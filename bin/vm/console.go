//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// The console is vm's standard input, output and error, the first
// process's descriptors 0, 1 and 2: with the disks, all the system has of
// the host's. Each is a pipe of the kernel's (pipe.go), whose far
// end a goroutine of vm's, a mover, has: one moves what vm's standard
// input gives into its pipe, the others move what is written to theirs
// out to vm's standard output and error, as it comes. So a process waits
// on the console as on any pipe, a signal ends its wait, and no terminal's
// discipline is between: the bytes are as they come. A trap's report goes
// out with what the process wrote before it. The console has a foreground
// process group, the first process's at first, which tcsetpgrp sets: the
// signals the host makes for vm go to it (routeSignals).
//
// When vm's standard input is a terminal of the host's, vm makes it raw
// while it runs, as a terminal emulator does, and gives it back as it was
// when it ends: every key comes as its bytes, and the system's own
// terminal (bin/tools/console.cpp) is the discipline. The console has the
// window of that terminal then, TIOCGWINSZ's, which SIGWINCH says changed.
type console struct {
	// out are the movers of output, which flush waits for; err is the
	// pipe of the standard error, for the reports of traps.
	out sync.WaitGroup
	err *pipe
	// stop says the system has ended: a mover of output stops once its
	// pipe is empty.
	stop   atomic.Bool
	movers []*mover
}

// mover is a goroutine of vm's that moves the console's bytes: a waker
// whose wake is a token on ready, which it sleeps on for its pipe. busy
// says an output's mover holds bytes it has not yet written out, pipe is
// its pipe.
type mover struct {
	ready chan struct{}
	busy  atomic.Bool
	pipe  *pipe
	// sub is the mover's subscription to the pipe it moves through.
	sub subscription
}

func newMover() *mover {
	m := &mover{ready: make(chan struct{}, 1)}
	m.sub.init(m, 0)

	return m
}

func (m *mover) wake(*hook) {
	select {
	case m.ready <- struct{}{}:
	default:
	}
}

// The host's terminal of vm's standard input, as it was before vm made it
// raw; nil when it is none.
var hostTerminal struct {
	mu    sync.Mutex
	saved *syscall.Termios
}

func termiosOf(fd int, req uintptr, t *syscall.Termios) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(t))); e != 0 {
		return e
	}

	return nil
}

// rawHostTerminal makes vm's standard input raw, as cfmakeraw does, when
// it is a terminal.
func rawHostTerminal() {
	var t syscall.Termios

	if termiosOf(0, syscall.TCGETS, &t) != nil {
		return
	}

	saved := t
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN], t.Cc[syscall.VTIME] = 1, 0

	if termiosOf(0, syscall.TCSETS, &t) == nil {
		hostTerminal.mu.Lock()
		hostTerminal.saved = &saved
		hostTerminal.mu.Unlock()
	}
}

// restoreHostTerminal gives the host's terminal back as it was, before vm
// ends.
func restoreHostTerminal() {
	hostTerminal.mu.Lock()
	defer hostTerminal.mu.Unlock()

	if hostTerminal.saved != nil {
		termiosOf(0, syscall.TCSETS, hostTerminal.saved)
		hostTerminal.saved = nil
	}
}

// hostWindow is the size of the window of the host's terminal, a struct
// winsize: ENOTTY when vm's standard input is no terminal.
func hostWindow() ([sizeofWinsize]byte, error) {
	var w [sizeofWinsize]byte

	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, 0, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&w[0]))); e != 0 {
		return w, syscall.ENOTTY
	}

	return w, nil
}

// fill moves what vm's descriptor host gives into the pipe of w until
// host ends, or the pipe's readers have gone: then the pipe's data ends.
func (m *mover) fill(host int, w *pipeEnd) {
	defer w.unref()

	buf := make([]byte, pipeChunk)

	for {
		n, err := syscall.Read(host, buf)

		if err == syscall.EINTR {
			continue
		}

		if n <= 0 || !m.put(w.pipe, buf[:n]) {
			return
		}
	}
}

// put queues b in pp as one packet, of at most pipeChunk bytes, waiting
// for room: false when its readers have gone.
func (m *mover) put(pp *pipe, b []byte) bool {
	pk := packet{data: append([]byte(nil), b...)}

	for {
		pp.mu.Lock()
		ch := pp.ch
		full, err := ch.put(&pk)

		if err == nil && !full {
			pp.bytes.Add(int64(len(b)))
		}

		pp.mu.Unlock()

		if err != nil || !full {
			return err == nil
		}

		ch.subscribe(&m.sub.out, true)

		if ch.room() || ch.gone() {
			ch.unsubscribe(&m.sub.out)

			continue
		}

		<-m.ready
		ch.unsubscribe(&m.sub.out)
	}
}

// drain moves what is written to the pipe of r out to vm's descriptor
// host until its writers have gone, or the system has ended and the pipe
// is empty, or host takes no more: then the pipe's readers go, and a
// write to it is EPIPE.
func (m *mover) drain(r *pipeEnd, host int, c *console) {
	defer c.out.Done()
	defer r.unref()

	buf := make([]byte, 65536)

	for {
		m.busy.Store(true)
		n, eof, ok := r.pipe.take(buf)

		if ok && n > 0 {
			written := writeAll(host, buf[:n])
			m.busy.Store(false)

			if !written {
				return
			}

			continue
		}

		m.busy.Store(false)

		if eof || c.stop.Load() {
			return
		}

		ch := r.pipe.channel()
		ch.subscribe(&m.sub.in, false)

		if ch.count() == 0 && !ch.ended.Load() && !c.stop.Load() {
			<-m.ready
		}

		ch.unsubscribe(&m.sub.in)
	}
}

// writeAll writes b whole to vm's descriptor host: false when it takes no
// more.
func writeAll(host int, b []byte) bool {
	for len(b) > 0 {
		n, err := syscall.Write(host, b)

		if err == syscall.EINTR {
			continue
		}

		if err != nil {
			return false
		}

		b = b[n:]
	}

	return true
}

// flush waits, once the system has ended, for the movers of output to
// have moved what is queued: what the processes left of the system write
// from then on does not go out.
func (c *console) flush() {
	c.stop.Store(true)

	for _, m := range c.movers {
		m.wake(nil)
	}

	done := make(chan struct{})

	go func() {
		c.out.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

// settle waits, for a second at most, until what was written to the
// console is out: before vm stops with the first process, as the host's
// shell gets the terminal back, what it wrote before has gone to vm's
// standard output and error.
func (c *console) settle() {
	end := time.Now().Add(time.Second)

	for time.Now().Before(end) {
		moved := true

		for _, m := range c.movers {
			if m.pipe != nil && (m.busy.Load() || m.pipe.bytes.Load() != 0) {
				moved = false
			}
		}

		if moved {
			return
		}

		time.Sleep(time.Millisecond)
	}
}
