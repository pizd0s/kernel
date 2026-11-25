package main

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var hostTerminal struct {
	mu    sync.Mutex
	saved *syscall.Termios
}

type Console struct {
	out sync.WaitGroup

	stop   atomic.Bool
	movers []*Mover
}

type Mover struct {
	ready chan struct{}
	busy  atomic.Bool
	pipe  *Pipe

	sub Subscription
}

func newMover() *Mover {
	m := &Mover{ready: make(chan struct{}, 1)}

	m.sub.init(m, 0)

	return m
}

func (m *Mover) wake(*Hook) {
	select {
	case m.ready <- struct{}{}:
	default:
	}
}

func termiosOf(fd int, req uintptr, t *syscall.Termios) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(t))); e != 0 {
		return e
	}

	return nil
}

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

func restoreHostTerminal() {
	hostTerminal.mu.Lock()

	defer hostTerminal.mu.Unlock()

	if hostTerminal.saved != nil {
		termiosOf(0, syscall.TCSETS, hostTerminal.saved)
		hostTerminal.saved = nil
	}
}

func hostWindow() ([sizeofWinsize]byte, error) {
	var w [sizeofWinsize]byte
	var host struct{ row, col, xpixel, ypixel uint16 }

	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, 0, syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&host))); e != 0 {
		return w, ENOTTY
	}

	for i, v := range []uint16{host.row, host.col, host.xpixel, host.ypixel} {
		binary.LittleEndian.PutUint16(w[2*i:], v)
	}

	return w, nil
}

func (m *Mover) fill(host int, w *PipeEnd) {
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

func (m *Mover) put(pp *Pipe, b []byte) bool {
	pp.mu.Lock()

	for {
		full, gone := pp.put(b)

		if gone || !full {
			pp.mu.Unlock()

			return !gone
		}

		pp.watchers.add(&m.sub.out)
		pp.mu.Unlock()
		<-m.ready
		pp.mu.Lock()
		pp.watchers.remove(&m.sub.out)
	}
}

func (m *Mover) drain(r *PipeEnd, host int, c *Console) {
	defer c.out.Done()

	defer r.unref()

	pp := r.pipe
	buf := make([]byte, 65536)

	for {
		m.busy.Store(true)
		pp.mu.Lock()

		n, eof := pp.take(buf)
		idle := n == 0 && !eof && !c.stop.Load()

		if idle {
			pp.watchers.add(&m.sub.in)
		}

		pp.mu.Unlock()

		switch {
		case idle:
			m.busy.Store(false)
			<-m.ready
			pp.mu.Lock()
			pp.watchers.remove(&m.sub.in)
			pp.mu.Unlock()
		case n > 0:
			written := writeAll(host, buf[:n])

			m.busy.Store(false)

			if !written {
				return
			}
		default:
			m.busy.Store(false)

			return
		}
	}
}

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

func (c *Console) flush() {
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

func (c *Console) settle() {
	end := time.Now().Add(time.Second)

	for time.Now().Before(end) {
		moved := true

		for _, m := range c.movers {
			if m.pipe != nil && (m.busy.Load() || m.pipe.unread() != 0) {
				moved = false
			}
		}

		if moved {
			return
		}

		time.Sleep(time.Millisecond)
	}
}
