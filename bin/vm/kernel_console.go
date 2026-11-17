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
// the host's. Each is a pipe of the kernel's (kernel_pipe.go), whose far
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

// consoleEnd is a descriptor of the console, open to read and write, as a
// terminal's are: it reads the pipe of the input, in, and writes the pipe
// of its output, out, the standard output's or the standard error's;
// either is nil when vm has no such descriptor.
type consoleEnd struct {
	in, out  *pipeEnd
	refs     atomic.Int32
	nonblock atomic.Bool
}

func newConsoleEnd(in, out *pipeEnd) *consoleEnd {
	for _, e := range []*pipeEnd{in, out} {
		if e != nil {
			e.ref()
		}
	}

	c := &consoleEnd{in: in, out: out}
	c.refs.Store(1)

	return c
}

func (c *consoleEnd) ref() {
	c.refs.Add(1)
}

func (c *consoleEnd) unref() {
	if c.refs.Add(-1) != 0 {
		return
	}

	for _, e := range []*pipeEnd{c.in, c.out} {
		if e != nil {
			e.unref()
		}
	}
}

// A consoleEnd is a File, a Transfer and an Ioctl, and itself to a call
// that wants one.
func (c *consoleEnd) asFile() File              { return c }
func (c *consoleEnd) asTransfer() Transfer      { return c }
func (c *consoleEnd) asSeeker() Seeker          { return nil }
func (c *consoleEnd) asDir() Dir                { return nil }
func (c *consoleEnd) asTerminal() Terminal      { return nil }
func (c *consoleEnd) asWindowed() Windowed      { return nil }
func (c *consoleEnd) asLink() Link              { return nil }
func (c *consoleEnd) asAttrs() Attrs            { return nil }
func (c *consoleEnd) asSocket() Socket          { return nil }
func (c *consoleEnd) asIoctl() Ioctl            { return c }
func (c *consoleEnd) asLocker() Locker          { return nil }
func (c *consoleEnd) asEpoll() *epoll           { return nil }
func (c *consoleEnd) asMemfd() *memfdFile       { return nil }
func (c *consoleEnd) asPair() *pairEnd          { return nil }
func (c *consoleEnd) asListener() *unixListener { return nil }
func (c *consoleEnd) asConsole() *consoleEnd    { return c }
func (c *consoleEnd) asVFS() *remoteDir         { return nil }

// stat is a character device's, as a terminal's.
func (c *consoleEnd) stat(p *Process, st *syscall.Stat_t) error {
	*st = syscall.Stat_t{Ino: 1, Mode: syscall.S_IFCHR | 0o620, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Blksize: 4096}

	return nil
}

const (
	inEvents  = pollIn | pollRdnorm | pollHup
	outEvents = pollOut | pollWrnorm | pollErr
)

func (c *consoleEnd) poll(events int16) int16 {
	var ready int16

	if c.in != nil {
		ready |= c.in.poll(events&inEvents) & inEvents
	}

	if c.out != nil {
		ready |= c.out.poll(events&outEvents) & outEvents
	}

	return ready
}

func (c *consoleEnd) subscribe(s *subscription, events int16) {
	if c.in != nil && (events == 0 || events&inEvents != 0) {
		c.in.subscribe(s, events&inEvents)
	}

	if c.out != nil && (events == 0 || events&outEvents != 0) {
		c.out.subscribe(s, events&outEvents)
	}
}

func (c *consoleEnd) unsubscribe(s *subscription) {
	for _, e := range []*pipeEnd{c.in, c.out} {
		if e != nil {
			e.unsubscribe(s)
		}
	}
}

func (c *consoleEnd) flags() (int, error) {
	fl := syscall.O_RDWR

	if c.nonblock.Load() {
		fl |= syscall.O_NONBLOCK
	}

	return fl, nil
}

func (c *consoleEnd) setFlags(fl int) error {
	c.nonblock.Store(fl&syscall.O_NONBLOCK != 0)

	return nil
}

func (c *consoleEnd) read(p *Process, b []byte, mode ioMode) (int, error) {
	if c.in == nil {
		return 0, syscall.EBADF
	}

	mode.nonblock = mode.nonblock || c.nonblock.Load()

	return c.in.read(p, b, mode)
}

func (c *consoleEnd) write(p *Process, b []byte, mode ioMode) (int, error) {
	if c.out == nil {
		return 0, syscall.EBADF
	}

	mode.nonblock = mode.nonblock || c.nonblock.Load()

	return c.out.write(p, b, mode)
}

func (c *consoleEnd) pread(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

func (c *consoleEnd) pwrite(p *Process, b []byte, off int64) (int, error) {
	return 0, syscall.ESPIPE
}

// ioctl knows FIONREAD, what the input has.
func (c *consoleEnd) ioctl(req uintptr, arg unsafe.Pointer) error {
	if c.in == nil {
		return syscall.ENOTTY
	}

	return c.in.ioctl(req, arg)
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

// newConsole makes the console of the descriptors vm has of 0, 1 and 2,
// nil for one it has not, and starts its movers: the first process's 0 and
// 1 write the standard output, its 2 the standard error, and all three
// read the standard input.
func (k *Kernel) newConsole() []Object {
	c := &console{}
	rawHostTerminal()
	var pipes [3]*pipeEnd

	for fd := range 3 {
		var st syscall.Stat_t

		if syscall.Fstat(fd, &st) != nil {
			continue
		}

		pp := newPipe()
		pp.readers, pp.writers = 1, 1
		reader, writer := &pipeEnd{pipe: pp, reads: true}, &pipeEnd{pipe: pp, writes: true}
		reader.refs.Store(1)
		writer.refs.Store(1)
		m := newMover()
		c.movers = append(c.movers, m)

		if fd == 0 {
			pipes[fd] = reader
			go m.fill(fd, writer)

			continue
		}

		if fd == 2 {
			c.err = pp
		}

		pipes[fd] = writer
		m.pipe = pp
		c.out.Add(1)
		go m.drain(reader, fd, c)
	}

	k.console = c
	files := make([]Object, 3)

	for fd, out := range []*pipeEnd{pipes[1], pipes[1], pipes[2]} {
		if pipes[fd] != nil {
			files[fd] = newConsoleEnd(pipes[0], out)
		}
	}

	for _, e := range pipes {
		if e != nil {
			e.unref()
		}
	}

	return files
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

		ch.writers.add(&m.sub.out)

		if ch.room() || ch.gone() {
			ch.writers.remove(&m.sub.out)

			continue
		}

		<-m.ready
		ch.writers.remove(&m.sub.out)
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
		ch.readers.add(&m.sub.in)

		if ch.count() == 0 && !ch.ended.Load() && !c.stop.Load() {
			<-m.ready
		}

		ch.readers.remove(&m.sub.in)
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

// report writes a report of the kernel's, a trap's, to the console's
// standard error after what was written to it before: to vm's own when
// there is none.
func (k *Kernel) report(text string) {
	if k.console == nil || k.console.err == nil || !newMover().put(k.console.err, []byte(text)) {
		syscall.Write(2, []byte(text))
	}
}

// foreground is the console's foreground process group, tcgetpgrp's.
func (k *Kernel) foreground() int32 {
	k.mu.Lock()
	defer k.mu.Unlock()

	return k.fg
}

// isConsole tells whether a descriptor is the console's.
func (p *Process) isConsole(fd uint32) (bool, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return false, err
	}

	return obj.asConsole() != nil, nil
}

// diskFile is a disk of vm's, a file of the host (`vm run --disk`): read
// and written at offsets as it is, its size the file's, a block device to
// the system, which vm's own calls move the bytes of. A memfd's copy to
// and from it goes straight between the file and the memfd's memory
// (kernel_memfd.go).
type diskFile struct {
	fd   int
	refs atomic.Int32
	pos  atomic.Int64
	ino  uint64
}

func newDiskFile(fd int) *diskFile {
	f := &diskFile{fd: fd, ino: pipeInodes.Add(1)}
	f.refs.Store(1)

	return f
}

func (f *diskFile) ref() {
	f.refs.Add(1)
}

func (f *diskFile) unref() {
	if f.refs.Add(-1) == 0 {
		syscall.Close(f.fd)
	}
}

// A diskFile is a File, a Transfer, a Seeker, a Windowed and an Attrs.
func (f *diskFile) asFile() File              { return f }
func (f *diskFile) asTransfer() Transfer      { return f }
func (f *diskFile) asSeeker() Seeker          { return f }
func (f *diskFile) asDir() Dir                { return nil }
func (f *diskFile) asTerminal() Terminal      { return nil }
func (f *diskFile) asWindowed() Windowed      { return f }
func (f *diskFile) asLink() Link              { return nil }
func (f *diskFile) asAttrs() Attrs            { return f }
func (f *diskFile) asSocket() Socket          { return nil }
func (f *diskFile) asIoctl() Ioctl            { return nil }
func (f *diskFile) asLocker() Locker          { return nil }
func (f *diskFile) asEpoll() *epoll           { return nil }
func (f *diskFile) asMemfd() *memfdFile       { return nil }
func (f *diskFile) asPair() *pairEnd          { return nil }
func (f *diskFile) asListener() *unixListener { return nil }
func (f *diskFile) asConsole() *consoleEnd    { return nil }
func (f *diskFile) asVFS() *remoteDir         { return nil }

// size is the file's size now.
func (f *diskFile) size() (int64, error) {
	var st syscall.Stat_t

	if err := syscall.Fstat(f.fd, &st); err != nil {
		return 0, syscall.EIO
	}

	return st.Size, nil
}

func (f *diskFile) stat(p *Process, st *syscall.Stat_t) error {
	size, err := f.size()

	if err != nil {
		return err
	}

	*st = syscall.Stat_t{Ino: f.ino, Mode: syscall.S_IFBLK | 0o660, Nlink: 1, Uid: p.cred.euid(), Gid: p.cred.egid(), Size: size,
		Blksize: 4096, Blocks: (size + 511) / 512}

	return nil
}

func (f *diskFile) poll(events int16) int16 {
	return events & (pollIn | pollRdnorm | pollOut | pollWrnorm)
}

func (f *diskFile) subscribe(s *subscription, events int16) {
}

func (f *diskFile) unsubscribe(s *subscription) {
}

func (f *diskFile) flags() (int, error) {
	return syscall.O_RDWR, nil
}

func (f *diskFile) setFlags(fl int) error {
	return nil
}

// at reads into b, or writes it with write, at off: what the host's file
// gives, a short read at its end.
func (f *diskFile) at(b []byte, off int64, write bool) (int, error) {
	if off < 0 {
		return 0, syscall.EINVAL
	}

	for {
		var n int
		var err error

		if write {
			n, err = syscall.Pwrite(f.fd, b, off)
		} else {
			n, err = syscall.Pread(f.fd, b, off)
		}

		if err == syscall.EINTR {
			continue
		}

		if err != nil {
			return 0, syscall.EIO
		}

		return n, nil
	}
}

func (f *diskFile) pread(p *Process, b []byte, off int64) (int, error) {
	return f.at(b, off, false)
}

func (f *diskFile) pwrite(p *Process, b []byte, off int64) (int, error) {
	return f.at(b, off, true)
}

func (f *diskFile) read(p *Process, b []byte, mode ioMode) (int, error) {
	n, err := f.at(b, f.pos.Load(), false)
	f.pos.Add(int64(n))

	return n, err
}

func (f *diskFile) write(p *Process, b []byte, mode ioMode) (int, error) {
	n, err := f.at(b, f.pos.Load(), true)
	f.pos.Add(int64(n))

	return n, err
}

func (f *diskFile) seek(p *Process, off int64, whence int) (int64, error) {
	base := int64(0)

	switch whence {
	case 0:
	case 1:
		base = f.pos.Load()
	case 2:
		size, err := f.size()

		if err != nil {
			return 0, err
		}

		base = size
	default:
		return 0, syscall.EINVAL
	}

	if base+off < 0 {
		return 0, syscall.EINVAL
	}

	f.pos.Store(base + off)

	return base + off, nil
}

func (f *diskFile) truncate(p *Process, size int64) error {
	return syscall.EINVAL
}

func (f *diskFile) sync(p *Process, data bool) error {
	sync := syscall.Fsync

	if data {
		sync = syscall.Fdatasync
	}

	if err := sync(f.fd); err != nil {
		return syscall.EIO
	}

	return nil
}

func (f *diskFile) chmod(p *Process, mode uint32) error {
	return syscall.EPERM
}

func (f *diskFile) chown(p *Process, uid, gid int) error {
	return syscall.EPERM
}

func (f *diskFile) utimes(p *Process, ts []syscall.Timespec) error {
	return syscall.EPERM
}

func (f *diskFile) statfs(p *Process, st *syscall.Statfs_t) error {
	*st = syscall.Statfs_t{Bsize: 4096, Frsize: 4096, Namelen: 255}

	return nil
}
