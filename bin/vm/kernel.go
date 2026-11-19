//go:build linux && amd64

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"
)

// The kernel runs programs linked against lib/c, whose system is the module
// "kernel" (lib/c/include declares its functions). A process is a goroutine
// running an instance; the kernel keeps its pid, descriptors, working
// directory, signals and children. Paths resolve inside the root directory
// `vm run` is given. A program is a WebAssembly module, which the kernel
// compiles itself, once in its life for a content (Kernel.codeOf): exec
// runs modules and scripts, nothing else.
//
// A function of the kernel finds its process through the instance. Pointers
// are offsets in the process's memory: one out of it traps, as the guest's
// own access would. A system call sets errno and returns through result,
// which delivers pending signals.

type Kernel struct {
	FunctionImports
	// root is the first process's root, none: rootfs, the VFS, serves the
	// roots of the others; user is its credentials, `vm run --user`'s.
	root    Dir
	user    cred
	console *console
	// names are uname's.
	names   systemNames
	mu      sync.Mutex
	procs   map[int32]*Process
	lastPid int32
	// codes are the modules compiled, by the digest of their content;
	// options are what they are compiled with, `vm run --asan` among them:
	// then the processes run with the shadow map (process.go).
	codes   map[[sha256.Size]byte]*codeEntry
	options CompileOptions
	// fifos are the pipes of the FIFOs of servers with an end open
	// (fifo_name.go); locks the locks of flock and fcntl on the servers'
	// files (lock_table.go).
	fifos map[fileKey]*pipe
	locks lockTable
	// fg is the foreground process group of the console; cttys are the
	// sessions' controlling terminals (process.go).
	fg        int32
	cttys     map[int32]Terminal
	first     *Process
	exit      chan int
	functions map[string]any
}

// newKernel makes a kernel whose first process has a directory of the
// host for its root, or with no root at all when root is empty: then it
// is a server, or a program of none. Its programs compile with options.
func newKernel(options CompileOptions, user cred) *Kernel {
	k := &Kernel{procs: map[int32]*Process{}, codes: map[[sha256.Size]byte]*codeEntry{}, options: options, fifos: map[fileKey]*pipe{},
		exit: make(chan int, 1), root: newNoRoot(), user: user, cttys: map[int32]Terminal{}}
	k.locks.files = map[fileKey]*lockedFile{}
	k.names.host, k.names.domain = "pizdos", "(none)"

	k.functions = k.functionTable()
	// The kernel applies the processes' umasks itself.
	syscall.Umask(0)
	k.routeSignals()

	return k
}

// Imports: the kernel's functions, and WASI's (wasi.go).
func (k *Kernel) function(imp *ImportMeta) any {
	switch imp.module {
	case "kernel":
		return k.functions[imp.name]
	case wasiModule:
		return wasiFunctions[imp.name]
	}

	return nil
}

// A disk is a file of the host the first process gets as a descriptor
// (diskFile): read and written as it is, its name the first process's to
// give. A disk of mem is no file's: the bytes that follow the program in
// its file (hostProgram), a memfd of them, read only.
type disk struct {
	name, path string
	mem        []byte
}

// newProcess makes a process: a child of parent, or the first, the leader
// of its group and session.
func (k *Kernel) newProcess(parent *Process) (*Process, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.lastPid++
	p := &Process{k: k, pid: k.lastPid, umask: 0o022, regexes: map[uint32]*posixRegex{}, seed: 0,
		events: eventQueue{bell: make(chan struct{}, 1)}}
	p.own.signal.kind, p.own.continued.kind = eventSignal, eventContinued
	p.own.child.kind, p.own.compiled.kind = eventChild, eventCompiled
	p.own.signal.run = (*Process).takeSignals

	if parent == nil {
		p.pgid, p.sid = p.pid, p.pid
		p.limits = hostLimits()
		p.root, p.cwd = k.root, k.root
		p.cred = k.user.clone()
	} else {
		p.ppid, p.pgid, p.sid = parent.pid, parent.pgid, parent.sid
		p.nice, p.policy = parent.nice, parent.policy
		p.comm, p.undumpable, p.noNewPrivs = parent.comm, parent.undumpable, parent.noNewPrivs
		p.cwd, p.umask, p.limits, p.seed = parent.cwd, parent.umask, parent.limits, parent.seed
		p.cred = parent.cred.clone()
		p.chaos = parent.chaos
		p.root = parent.root
	}

	p.root.ref()
	p.cwd.ref()
	k.procs[p.pid] = p

	return p, nil
}

// code is the code of the executable a file holds: the error is ENOEXEC
// when the file is no executable of a program of the kernel, EACCES when it
// cannot be read at an offset.
func (k *Kernel) code(p *Process, f Object, name string) (*Code, error) {
	exe, ok, err := programData(p, f, false)

	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, fmt.Errorf("%s: no executable of PZD1: %w", name, syscall.ENOEXEC)
	}

	return k.codeOf(p, exe, name, nil)
}

// programData is the executable a file begins with, read at offsets to
// its length and no further, as exec reads a program (readProgram).
// EACCES when it cannot be read at an offset.
func programData(p *Process, f Object, module bool) (data []byte, ok bool, err error) {
	file := f.asFile()

	if file == nil {
		return nil, false, syscall.EACCES
	}

	var st syscall.Stat_t

	if err := file.stat(p, &st); err != nil {
		return nil, false, err
	}

	t := f.asTransfer()

	if t == nil {
		return nil, false, syscall.EACCES
	}

	return readProgram(st.Size, func(b []byte, off int64) error { return readAt(p, t, b, off) }, module)
}

// readProgram is the executable a file of size bytes begins with, which
// readAt reads at offsets to its length and no further, as exec reads a
// program and vm run its first. A file that begins with none is not read
// on; for vm run, module, it is read whole, a module of a WASI command,
// and ok is false. ENOEXEC when it ends before its size.
func readProgram(size int64, readAt func(b []byte, off int64) error, module bool) (data []byte, ok bool, err error) {
	header := make([]byte, min(exeHeader, max(size, 0)))

	if err := readAt(header, 0); err != nil {
		return nil, false, err
	}

	length, err := exeLength(header, size)

	if err != nil || length == 0 && !module {
		return nil, false, err
	}

	if length > 0 {
		size = int64(length)
	}

	data = make([]byte, size)
	copy(data, header)

	if err := readAt(data[len(header):], int64(len(header))); err != nil {
		return nil, false, err
	}

	return data, length > 0, nil
}

// readAt fills b from a file at off: ENOEXEC when the file ends first.
func readAt(p *Process, t Transfer, b []byte, off int64) error {
	for at := 0; at < len(b); {
		n, err := t.pread(p, b[at:], off+int64(at))

		if err != nil {
			return err
		}

		if n == 0 {
			return syscall.ENOEXEC
		}

		at += n
	}

	return nil
}

// A codeEntry is the code of a module, or why it has none; done says
// either is known, and waiting are the processes that wait for it, whom
// the end of the compilation wakes, so a process that asks for a module
// another is compiling waits for that compilation: both with the kernel's
// lock.
type codeEntry struct {
	done    bool
	waiting []*Process
	code    *Code
	err     error
}

// codeOf is the code of an executable, data, the part of a file to its
// length: its module unpacked, compiled with the kernel's options and
// linked to it once in the kernel's life for those bytes, which every
// process that runs the module shares; what a file has past the length
// is no part of them. obj is the module compiled already, or nil. A
// failure is not kept: the next exec tries again.
//
// The compiler runs on a goroutine of its own, and the process p waits
// for its end, taking its signals meanwhile as in any wait: in the frames
// of the process's code, a compiler that ends in a panic, a module it
// refuses, would leave the goroutine's instance its own (aot.cpp's entry
// thunk restores it only on return).
func (k *Kernel) codeOf(p *Process, data []byte, name string, obj *ElfObject) (*Code, error) {
	key := sha256.Sum256(data)
	k.mu.Lock()
	e, found := k.codes[key]

	if !found {
		e = &codeEntry{}
		k.codes[key] = e

		go func() {
			code, err := k.compile(data, name, obj)
			k.mu.Lock()
			e.code, e.err, e.done = code, err, true
			waiting := e.waiting
			e.waiting = nil

			if err != nil && k.codes[key] == e {
				delete(k.codes, key)
			}

			k.mu.Unlock()

			for _, w := range waiting {
				w.post(&w.own.compiled)
			}
		}()
	}

	if !e.done {
		e.waiting = append(e.waiting, p)
	}

	k.mu.Unlock()
	p.wait(time.Time{}, func(*event) (bool, error) {
		k.mu.Lock()
		defer k.mu.Unlock()

		return e.done, nil
	})

	return e.code, e.err
}

// compile compiles a module, unless obj is its object already, and links
// it to the kernel: a program of lib/c's or a WASI command, whose imports
// are the kernel's or WASI's (wasi.go); ENOEXEC, with why, for one
// that imports anything else.
func (k *Kernel) compile(data []byte, name string, obj *ElfObject) (*Code, error) {
	if obj == nil {
		var err error

		if obj, err = compileExecutable(data, k.options); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}

	var c *Code

	if e := try(func() {
		c = link(name, obj, k)
	}); e != nil {
		return nil, fmt.Errorf("%s: %s: %w", name, e, syscall.ENOEXEC)
	}

	return c, nil
}

// signalTargets are the processes kill sends to: one pid, a group (pid 0
// is the sender's, -1 every process but the first and the sender).
func (k *Kernel) signalTargets(from *Process, pid int32) []*Process {
	return k.processes(func(q *Process) bool {
		switch {
		case pid > 0:
			return q.pid == pid
		case pid == 0:
			return q.pgid == from.pgid
		case pid == -1:
			return q.pid != 1 && q != from
		}

		return q.pgid == -pid
	})
}

// processes are those that match.
func (k *Kernel) processes(match func(*Process) bool) []*Process {
	k.mu.Lock()
	defer k.mu.Unlock()

	var out []*Process

	for _, q := range k.procs {
		if match(q) {
			out = append(out, q)
		}
	}

	return out
}

// routeSignals takes the host's signals for the processes.
func (k *Kernel) routeSignals() {
	c := make(chan os.Signal, 16)
	signal.Notify(c, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP, syscall.SIGWINCH, syscall.SIGTERM, syscall.SIGHUP,
		syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGCONT, syscall.SIGALRM)

	go func() {
		for s := range c {
			sig := int(s.(syscall.Signal))

			switch s {
			case syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP, syscall.SIGWINCH:
				k.mu.Lock()
				fg := k.fg
				k.mu.Unlock()

				for _, q := range k.processes(func(q *Process) bool { return q.pgid == fg }) {
					q.send(sig)
				}
			default:
				if k.first != nil {
					k.first.send(sig)
				}
			}
		}
	}()
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

// The system's names, uname's, the kernel's own: none of the host's. The
// host and domain names are root's to set.
type systemNames struct {
	mu           sync.Mutex
	host, domain string
}

// The kernel's functions by their names in lib/c: each takes the process
// from ctx and calls the method that does the work.

func (k *Kernel) functionTable() map[string]any {
	return map[string]any{
		"__kernel_start":         libc__kernel_start,
		"__kernel_program":       libc__kernel_program,
		"read":                   libcRead,
		"write":                  libcWrite,
		"readv":                  libcReadv,
		"writev":                 libcWritev,
		"preadv":                 libcPreadv,
		"pwritev":                libcPwritev,
		"pread":                  libcPread,
		"pwrite":                 libcPwrite,
		"copy_file_range":        libcCopy_file_range,
		"fdatasync":              libcFdatasync,
		"sync":                   libcSync,
		"syncfs":                 libcSyncfs,
		"flock":                  libcFlock,
		"lockf":                  libcLockf,
		"posix_fadvise":          libcPosix_fadvise,
		"posix_fallocate":        libcPosix_fallocate,
		"select":                 libcSelect,
		"pselect":                libcPselect,
		"lseek":                  libcLseek,
		"close":                  libcClose,
		"dup":                    libcDup,
		"dup2":                   libcDup2,
		"dup3":                   libcDup3,
		"pipe":                   libcPipe,
		"pipe2":                  libcPipe2,
		"fcntl":                  libcFcntl,
		"ioctl":                  libcIoctl,
		"isatty":                 libcIsatty,
		"tcgetattr":              libcTcgetattr,
		"tcsetattr":              libcTcsetattr,
		"cfmakeraw":              libcCfmakeraw,
		"tcdrain":                libcTcdrain,
		"tcsendbreak":            libcTcsendbreak,
		"tcflow":                 libcTcflow,
		"tcflush":                libcTcflush,
		"tcgetsid":               libcTcgetsid,
		"cfgetospeed":            libcCfgetospeed,
		"cfgetispeed":            libcCfgetispeed,
		"cfsetospeed":            libcCfsetospeed,
		"cfsetspeed":             libcCfsetspeed,
		"cfsetispeed":            libcCfsetispeed,
		"poll":                   libcPoll,
		"ppoll":                  libcPpoll,
		"epoll_create":           libcEpoll_create,
		"epoll_create1":          libcEpoll_create1,
		"epoll_ctl":              libcEpoll_ctl,
		"epoll_wait":             libcEpoll_wait,
		"epoll_pwait":            libcEpoll_pwait,
		"fsync":                  libcFsync,
		"ftruncate":              libcFtruncate,
		"fstat":                  libcFstat,
		"fchmod":                 libcFchmod,
		"fchown":                 libcFchown,
		"futimes":                libcFutimes,
		"futimens":               libcFutimens,
		"open":                   libcOpen,
		"openat":                 libcOpenat,
		"stat":                   libcStat,
		"lstat":                  libcLstat,
		"fstatat":                libcFstatat,
		"access":                 libcAccess,
		"faccessat":              libcFaccessat,
		"truncate":               libcTruncate,
		"statvfs":                libcStatvfs,
		"fstatvfs":               libcFstatvfs,
		"fchdir":                 libcFchdir,
		"chroot":                 libcChroot,
		"chdir":                  libcChdir,
		"getcwd":                 libcGetcwd,
		"mkdir":                  libcMkdir,
		"mkdirat":                libcMkdirat,
		"rmdir":                  libcRmdir,
		"unlink":                 libcUnlink,
		"unlinkat":               libcUnlinkat,
		"mount":                  libcMount,
		"umount":                 libcUmount,
		"umount2":                libcUmount2,
		"remove":                 libcRemove,
		"rename":                 libcRename,
		"renameat":               libcRenameat,
		"link":                   libcLink,
		"linkat":                 libcLinkat,
		"symlink":                libcSymlink,
		"symlinkat":              libcSymlinkat,
		"readlink":               libcReadlink,
		"readlinkat":             libcReadlinkat,
		"canonicalize_file_name": libcCanonicalize_file_name,
		"get_current_dir_name":   libcGet_current_dir_name,
		"creat":                  libcCreat,
		"eaccess":                libcEaccess,
		"euidaccess":             libcEuidaccess,
		"statfs":                 libcStatfs,
		"fstatfs":                libcFstatfs,
		"acct":                   libcAcct,
		"realpath":               libcRealpath,
		"chmod":                  libcChmod,
		"fchmodat":               libcFchmodat,
		"chown":                  libcChown,
		"lchown":                 libcLchown,
		"fchownat":               libcFchownat,
		"mknod":                  libcMknod,
		"mknodat":                libcMknodat,
		"mkfifo":                 libcMkfifo,
		"mkfifoat":               libcMkfifoat,
		"utimes":                 libcUtimes,
		"lutimes":                libcLutimes,
		"utime":                  libcUtime,
		"utimensat":              libcUtimensat,
		"opendir":                libcOpendir,
		"fdopendir":              libcFdopendir,
		"readdir":                libcReaddir,
		"rewinddir":              libcRewinddir,
		"closedir":               libcClosedir,
		"dirfd":                  libcDirfd,
		"readdir_r":              libcReaddir_r,
		"telldir":                libcTelldir,
		"seekdir":                libcSeekdir,
		"alphasort":              libcAlphasort,
		"versionsort":            libcVersionsort,
		"mkstemp":                libcMkstemp,
		"mkostemp":               libcMkostemp,
		"mkstemps":               libcMkstemps,
		"mkdtemp":                libcMkdtemp,
		"mktemp":                 libcMktemp,
		"malloc":                 libcMalloc,
		"free":                   libcFree,
		"calloc":                 libcCalloc,
		"realloc":                libcRealloc,
		"mallopt":                libcMallopt,
		"getpagesize":            libcGetpagesize,
		"mmap":                   libcMmap,
		"memfd_create":           libcMemfd_create,
		"munmap":                 libcMunmap,
		"msync":                  libcMsync,
		"madvise":                libcMadvise,
		"mprotect":               libcMprotect,
		"mlock":                  libcMlock,
		"munlock":                libcMunlock,
		"mlockall":               libcMlockall,
		"munlockall":             libcMunlockall,
		"memalign":               libcMemalign,
		"aligned_alloc":          libcAligned_alloc,
		"valloc":                 libcValloc,
		"posix_memalign":         libcPosix_memalign,
		"malloc_usable_size":     libcMalloc_usable_size,
		"malloc_trim":            libcMalloc_trim,
		"reallocarray":           libcReallocarray,
		"strncmp":                libcStrncmp,
		"strcasecmp":             libcStrcasecmp,
		"strncasecmp":            libcStrncasecmp,
		"strchr":                 libcStrchr,
		"strchrnul":              libcStrchrnul,
		"strrchr":                libcStrrchr,
		"strcasestr":             libcStrcasestr,
		"stpcpy":                 libcStpcpy,
		"strncpy":                libcStrncpy,
		"stpncpy":                libcStpncpy,
		"strdup":                 libcStrdup,
		"strndup":                libcStrndup,
		"strtok_r":               libcStrtok_r,
		"strsep":                 libcStrsep,
		"strverscmp":             libcStrverscmp,
		"abs":                    libcAbs,
		"isalnum":                libcIsalnum,
		"isalpha":                libcIsalpha,
		"isascii":                libcIsascii,
		"isblank":                libcIsblank,
		"iscntrl":                libcIscntrl,
		"isdigit":                libcIsdigit,
		"isgraph":                libcIsgraph,
		"islower":                libcIslower,
		"isprint":                libcIsprint,
		"ispunct":                libcIspunct,
		"isspace":                libcIsspace,
		"isupper":                libcIsupper,
		"isxdigit":               libcIsxdigit,
		"tolower":                libcTolower,
		"toupper":                libcToupper,
		"toascii":                libcToascii,
		"memmem":                 libcMemmem,
		"mempcpy":                libcMempcpy,
		"strnlen":                libcStrnlen,
		"strncat":                libcStrncat,
		"strlcpy":                libcStrlcpy,
		"strlcat":                libcStrlcat,
		"strstr":                 libcStrstr,
		"strpbrk":                libcStrpbrk,
		"explicit_bzero":         libcExplicit_bzero,
		"bcopy":                  libcBcopy,
		"bzero":                  libcBzero,
		"memcpy":                 libcMemcpy,
		"memmove":                libcMemmove,
		"memset":                 libcMemset,
		"memcmp":                 libcMemcmp,
		"bcmp":                   libcBcmp,
		"memchr":                 libcMemchr,
		"strlen":                 libcStrlen,
		"strcpy":                 libcStrcpy,
		"strcat":                 libcStrcat,
		"strcmp":                 libcStrcmp,
		"strspn":                 libcStrspn,
		"strcspn":                libcStrcspn,
		"strerror":               libcStrerror,
		"strsignal":              libcStrsignal,
		"dirname":                libcDirname,
		"fnmatch":                libcFnmatch,
		"memrchr":                libcMemrchr,
		"rawmemchr":              libcRawmemchr,
		"memccpy":                libcMemccpy,
		"swab":                   libcSwab,
		"strtok":                 libcStrtok,
		"strcoll":                libcStrcoll,
		"strxfrm":                libcStrxfrm,
		"strerror_r":             libcStrerror_r,
		"basename":               libcBasename,
		"ffs":                    libcFfs,
		"ffsl":                   libcFfsl,
		"ffsll":                  libcFfsll,
		"labs":                   libcLabs,
		"llabs":                  libcLlabs,
		"div":                    libcDiv,
		"lldiv":                  libcLldiv,
		"index":                  libcStrchr,
		"rindex":                 libcStrrchr,
		"ldiv":                   libcDiv,
		"imaxdiv":                libcLldiv,
		"imaxabs":                libcLlabs,
		"time":                   libcTime,
		"gettimeofday":           libcGettimeofday,
		"clock_settime":          libcClock_settime,
		"localtime":              libcLocaltime,
		"localtime_r":            libcLocaltime_r,
		"tzset":                  libcTzset,
		"asctime":                libcAsctime,
		"asctime_r":              libcAsctime_r,
		"ctime_r":                libcCtime_r,
		"difftime":               libcDifftime,
		"timegm":                 libcTimegm,
		"settimeofday":           libcSettimeofday,
		"stime":                  libcStime,
		"adjtime":                libcAdjtime,
		"usleep":                 libcUsleep,
		"ualarm":                 libcUalarm,
		"gmtime":                 libcGmtime,
		"gmtime_r":               libcGmtime_r,
		"clock":                  libcClock,
		"clock_getres":           libcClock_getres,
		"clock_nanosleep":        libcClock_nanosleep,
		"setitimer":              libcSetitimer,
		"getitimer":              libcGetitimer,
		"alarm":                  libcAlarm,
		"mktime":                 libcMktime,
		"ctime":                  libcCtime,
		"strftime":               libcStrftime,
		"strptime":               libcStrptime,
		"times":                  libcTimes,
		"sleep":                  libcSleep,
		"clock_gettime":          libcClock_gettime,
		"nanosleep":              libcNanosleep,
		"regcomp":                libcRegcomp,
		"regexec":                libcRegexec,
		"regfree":                libcRegfree,
		"regerror":               libcRegerror,
		"signal":                 libcSignal,
		"sigaction":              libcSigaction,
		"sigprocmask":            libcSigprocmask,
		"pthread_sigmask":        libcPthread_sigmask,
		"sigfillset":             libcSigfillset,
		"sigemptyset":            libcSigemptyset,
		"sigaddset":              libcSigaddset,
		"sigdelset":              libcSigdelset,
		"sigismember":            libcSigismember,
		"sigisemptyset":          libcSigisemptyset,
		"sigorset":               libcSigorset,
		"sigandset":              libcSigandset,
		"sigpending":             libcSigpending,
		"sigwait":                libcSigwait,
		"sigwaitinfo":            libcSigwaitinfo,
		"sigtimedwait":           libcSigtimedwait,
		"sigaltstack":            libcSigaltstack,
		"siginterrupt":           libcSiginterrupt,
		"pause":                  libcPause,
		"sigsuspend":             libcSigsuspend,
		"raise":                  libcRaise,
		"kill":                   libcKill,
		"sigqueue":               libcSigqueue,
		"killpg":                 libcKillpg,
		"abort":                  libcAbort,
		"bsd_signal":             libcSignal,
		"getpid":                 libcGetpid,
		"getppid":                libcGetppid,
		"getpgrp":                libcGetpgrp,
		"setpgid":                libcSetpgid,
		"tcgetpgrp":              libcTcgetpgrp,
		"tcsetpgrp":              libcTcsetpgrp,
		"getuid":                 libcGetuid,
		"geteuid":                libcGeteuid,
		"getgid":                 libcGetgid,
		"getegid":                libcGetegid,
		"getgroups":              libcGetgroups,
		"uname":                  libcUname,
		"sysconf":                libcSysconf,
		"umask":                  libcUmask,
		"rand":                   libcRand,
		"srand":                  libcSrand,
		"getenv":                 libcGetenv,
		"putenv":                 libcPutenv,
		"setenv":                 libcSetenv,
		"unsetenv":               libcUnsetenv,
		"clearenv":               libcClearenv,
		"execve":                 libcExecve,
		"execvp":                 libcExecvp,
		"execv":                  libcExecv,
		"execvpe":                libcExecvpe,
		"execle":                 libcExecle,
		"fexecve":                libcFexecve,
		"execl":                  libcExecl,
		"execlp":                 libcExeclp,
		"_exit":                  libc_exit,
		"_Exit":                  libc_Exit,
		"system":                 libcSystem,
		"setjmp":                 libcSetjmp,
		"sigsetjmp":              libcSigsetjmp,
		"longjmp":                libcLongjmp,
		"_setjmp":                libcSetjmp,
		"_longjmp":               libcLongjmp,
		"siglongjmp":             libcLongjmp,
		"vfork":                  libcVfork,
		"fork":                   libcFork,
		"waitpid":                libcWaitpid,
		"wait":                   libcWait,
		"wait3":                  libcWait3,
		"wait4":                  libcWait4,
		"waitid":                 libcWaitid,
		"setlocale":              libcSetlocale,
		"localeconv":             libcLocaleconv,
		"nl_langinfo":            libcNl_langinfo,
		"towlower":               libcTowlower,
		"towupper":               libcTowupper,
		"wctype":                 libcWctype,
		"iswctype":               libcIswctype,
		"wctrans":                libcWctrans,
		"towctrans":              libcTowctrans,
		"iswalnum":               libcIswalnum,
		"iswalpha":               libcIswalpha,
		"iswblank":               libcIswblank,
		"iswcntrl":               libcIswcntrl,
		"iswdigit":               libcIswdigit,
		"iswgraph":               libcIswgraph,
		"iswlower":               libcIswlower,
		"iswprint":               libcIswprint,
		"iswpunct":               libcIswpunct,
		"iswspace":               libcIswspace,
		"iswupper":               libcIswupper,
		"iswxdigit":              libcIswxdigit,
		"wcslen":                 libcWcslen,
		"wcsnlen":                libcWcsnlen,
		"wcscpy":                 libcWcscpy,
		"wcsncpy":                libcWcsncpy,
		"wcscat":                 libcWcscat,
		"wcscmp":                 libcWcscmp,
		"wcsncmp":                libcWcsncmp,
		"wcscoll":                libcWcscoll,
		"wcsxfrm":                libcWcsxfrm,
		"wcschr":                 libcWcschr,
		"wcsrchr":                libcWcsrchr,
		"wcsstr":                 libcWcsstr,
		"wcspbrk":                libcWcspbrk,
		"wcsspn":                 libcWcsspn,
		"wcscspn":                libcWcscspn,
		"wcstok":                 libcWcstok,
		"wcsdup":                 libcWcsdup,
		"wcsftime":               libcWcsftime,
		"wmemchr":                libcWmemchr,
		"wmemcmp":                libcWmemcmp,
		"wmemcpy":                libcWmemcpy,
		"wmemmove":               libcWmemmove,
		"wmemset":                libcWmemset,
		"wcwidth":                libcWcwidth,
		"wcswidth":               libcWcswidth,
		"setuid":                 libcSetuid,
		"seteuid":                libcSeteuid,
		"setreuid":               libcSetreuid,
		"setresuid":              libcSetresuid,
		"setgid":                 libcSetgid,
		"setegid":                libcSetegid,
		"setregid":               libcSetregid,
		"setresgid":              libcSetresgid,
		"getresuid":              libcGetresuid,
		"getresgid":              libcGetresgid,
		"setgroups":              libcSetgroups,
		"initgroups":             libcInitgroups,
		"setsid":                 libcSetsid,
		"getsid":                 libcGetsid,
		"getpgid":                libcGetpgid,
		"setpgrp":                libcSetpgrp,
		"nice":                   libcNice,
		"getpriority":            libcGetpriority,
		"setpriority":            libcSetpriority,
		"sched_yield":            libcSched_yield,
		"sched_get_priority_max": libcSched_get_priority_max,
		"sched_get_priority_min": libcSched_get_priority_min,
		"sched_getscheduler":     libcSched_getscheduler,
		"sched_getparam":         libcSched_getparam,
		"sched_setparam":         libcSched_setparam,
		"sched_setscheduler":     libcSched_setscheduler,
		"sched_rr_get_interval":  libcSched_rr_get_interval,
		"openlog":                libcOpenlog,
		"closelog":               libcCloselog,
		"setlogmask":             libcSetlogmask,
		"__kernel_syslog":        libc__kernel_syslog,
		"sysinfo":                libcSysinfo,
		"get_nprocs":             libcGet_nprocs,
		"get_nprocs_conf":        libcGet_nprocs_conf,
		"get_phys_pages":         libcGet_phys_pages,
		"get_avphys_pages":       libcGet_avphys_pages,
		"getloadavg":             libcGetloadavg,
		"getrusage":              libcGetrusage,
		"getrlimit":              libcGetrlimit,
		"setrlimit":              libcSetrlimit,
		"prlimit":                libcPrlimit,
		"getdtablesize":          libcGetdtablesize,
		"confstr":                libcConfstr,
		"pathconf":               libcPathconf,
		"fpathconf":              libcFpathconf,
		"getlogin":               libcGetlogin,
		"getlogin_r":             libcGetlogin_r,
		"ctermid":                libcCtermid,
		"ttyname":                libcTtyname,
		"ttyname_r":              libcTtyname_r,
		"getpwnam":               libcGetpwnam,
		"getpwuid":               libcGetpwuid,
		"getpwnam_r":             libcGetpwnam_r,
		"getpwuid_r":             libcGetpwuid_r,
		"getpwent":               libcGetpwent,
		"setpwent":               libcSetpwent,
		"endpwent":               libcEndpwent,
		"getgrnam":               libcGetgrnam,
		"getgrgid":               libcGetgrgid,
		"getgrnam_r":             libcGetgrnam_r,
		"getgrgid_r":             libcGetgrgid_r,
		"getgrent":               libcGetgrent,
		"setgrent":               libcSetgrent,
		"endgrent":               libcEndgrent,
		"getgrouplist":           libcGetgrouplist,
		"posix_openpt":           libcPosix_openpt,
		"grantpt":                libcGrantpt,
		"unlockpt":               libcUnlockpt,
		"ptsname":                libcPtsname,
		"ptsname_r":              libcPtsname_r,
		"openpty":                libcOpenpty,
		"forkpty":                libcForkpty,
		"glob":                   libcGlob,
		"globfree":               libcGlobfree,
		"random":                 libcRandom,
		"srandom":                libcSrandom,
		"initstate":              libcInitstate,
		"setstate":               libcSetstate,
		"rand_r":                 libcRand_r,
		"secure_getenv":          libcSecure_getenv,
		"getsubopt":              libcGetsubopt,
		"daemon":                 libcDaemon,
		"prctl":                  libcPrctl,
		"getpass":                libcGetpass,
		"vhangup":                libcVhangup,
		"getusershell":           libcGetusershell,
		"setusershell":           libcSetusershell,
		"endusershell":           libcEndusershell,
		"gethostname":            libcGethostname,
		"getentropy":             libcGetentropy,
		"getdomainname":          libcGetdomainname,
		"sethostname":            libcSethostname,
		"setdomainname":          libcSetdomainname,
		"gethostid":              libcGethostid,
		"sethostid":              libcSethostid,
		"psignal":                libcPsignal,
		"psiginfo":               libcPsiginfo,
		"__assert_fail":          libc__assert_fail,
		"socket":                 libcSocket,
		"socketpair":             libcSocketpair,
		"bind":                   libcBind,
		"connect":                libcConnect,
		"listen":                 libcListen,
		"accept":                 libcAccept,
		"accept4":                libcAccept4,
		"getsockname":            libcGetsockname,
		"getpeername":            libcGetpeername,
		"getsockopt":             libcGetsockopt,
		"setsockopt":             libcSetsockopt,
		"send":                   libcSend,
		"sendto":                 libcSendto,
		"recv":                   libcRecv,
		"recvfrom":               libcRecvfrom,
		"sendmsg":                libcSendmsg,
		"recvmsg":                libcRecvmsg,
		"shutdown":               libcShutdown,
		"sockatmark":             libcSockatmark,
		"getaddrinfo":            libcGetaddrinfo,
		"freeaddrinfo":           libcFreeaddrinfo,
		"gai_strerror":           libcGai_strerror,
		"getnameinfo":            libcGetnameinfo,
		"gethostbyname":          libcGethostbyname,
		"gethostbyname2":         libcGethostbyname2,
		"gethostbyaddr":          libcGethostbyaddr,
		"gethostent":             libcGethostent,
		"sethostent":             libcSethostent,
		"endhostent":             libcEndhostent,
		"getservbyname":          libcGetservbyname,
		"getservbyport":          libcGetservbyport,
		"getprotobyname":         libcGetprotobyname,
		"getprotobynumber":       libcGetprotobynumber,
		"getnetbyname":           libcGetnetbyname,
		"getnetbyaddr":           libcGetnetbyaddr,
		"hstrerror":              libcHstrerror,
		"herror":                 libcHerror,
		"inet_aton":              libcInet_aton,
		"inet_addr":              libcInet_addr,
		"inet_network":           libcInet_network,
		"inet_ntoa":              libcInet_ntoa,
		"inet_pton":              libcInet_pton,
		"inet_ntop":              libcInet_ntop,
		"inet_makeaddr":          libcInet_makeaddr,
		"inet_lnaof":             libcInet_lnaof,
		"inet_netof":             libcInet_netof,
		"if_nametoindex":         libcIf_nametoindex,
		"if_indextoname":         libcIf_indextoname,
		"if_nameindex":           libcIf_nameindex,
		"if_freenameindex":       libcIf_freenameindex,
		"crypt":                  libcCrypt,
	}
}
