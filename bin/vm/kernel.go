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

var hostSignals = map[syscall.Signal]int{
	syscall.SIGHUP:   SIGHUP,
	syscall.SIGINT:   SIGINT,
	syscall.SIGQUIT:  SIGQUIT,
	syscall.SIGUSR1:  SIGUSR1,
	syscall.SIGUSR2:  SIGUSR2,
	syscall.SIGALRM:  SIGALRM,
	syscall.SIGTERM:  SIGTERM,
	syscall.SIGCONT:  SIGCONT,
	syscall.SIGTSTP:  SIGTSTP,
	syscall.SIGWINCH: SIGWINCH,
}

type Kernel struct {
	root    Object
	user    Cred
	console *Console

	names   SystemNames
	mu      sync.Mutex
	procs   map[int32]*Process
	lastPid int32

	codes map[CodeKey]*CodeEntry

	fifos      map[FileKey]*Pipe
	sockets    map[FileKey]Object
	semaphores map[string]*Semaphore
	locks      LockTable

	fg        int32
	cttys     map[int32]Object
	first     *Process
	exit      chan int
	functions map[string]any
	compiler  func() *Code
	pool      *Pool
}

func newKernel(user Cred, memory uint64) *Kernel {
	k := &Kernel{procs: map[int32]*Process{}, codes: map[CodeKey]*CodeEntry{}, fifos: map[FileKey]*Pipe{}, sockets: map[FileKey]Object{},
		semaphores: map[string]*Semaphore{},
		exit:       make(chan int, 1), root: newNoRoot(), user: user, cttys: map[int32]Object{}, pool: newPool(memory)}

	k.locks.files = map[FileKey]*LockedFile{}
	k.names.host, k.names.domain = "pizdos", "(none)"
	k.functions = k.functionTable()

	k.compiler = sync.OnceValue(func() *Code {
		return link("aot", readObject(compilerBlob), k)
	})

	return k
}

func (k *Kernel) function(imp *ImportMeta) any {
	if imp.module == "kernel" {
		return k.functions[imp.name]
	}

	return nil
}

type Disk struct {
	name, path string
	mem        []byte
}

func (k *Kernel) newProcess(parent *Process) (*Process, error) {
	k.mu.Lock()

	defer k.mu.Unlock()

	k.lastPid++

	p := k.process(k.lastPid, parent)

	k.procs[p.pid] = p

	return p, nil
}

func (k *Kernel) process(pid int32, parent *Process) *Process {
	p := &Process{k: k, pid: pid, umask: 0o022, regexes: map[uint32]*PosixRegex{}, iconvs: map[uint32]*Iconv{}, seed: 0, syncs: map[int32]Object{},
		semOpens: map[string]*SemOpen{}}

	p.own.signal.kind, p.own.continued.kind = eventSignal, eventContinued
	p.own.child.kind, p.own.compiled.kind, p.own.pool.kind = eventChild, eventCompiled, eventPool
	p.own.signal.run = (*Process).takeSignals

	if parent == nil {
		p.pgid.Store(p.pid)
		p.sid.Store(p.pid)
		p.limits = hostLimits()
		p.root, p.cwd = k.root, k.root
		p.cred = k.user.clone()
	} else {
		p.ppid = parent.pid
		p.pgid.Store(parent.pgid.Load())
		p.sid.Store(parent.sid.Load())
		p.nice, p.policy = parent.nice, parent.policy
		p.comm, p.undumpable, p.noNewPrivs = parent.comm, parent.undumpable, parent.noNewPrivs
		p.cwd, p.umask, p.limits, p.seed = parent.cwd, parent.umask, parent.limits, parent.seed
		p.cred = parent.cred.clone()
		p.chaos = parent.chaos
		p.root = parent.root
		p.modes, p.classes, p.sink = parent.modes, parent.classes, parent.sink

		if p.sink != nil {
			p.sink.ref()
		}
	}

	p.root.ref()
	p.cwd.ref()

	return p
}

func (k *Kernel) code(p *Process, f Object, name string) (*Code, error) {
	exe, ok, err := programData(p, f, false)

	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, fmt.Errorf("%s: no executable of PZD1: %w", name, ENOEXEC)
	}

	return k.codeOf(p, exe, name, nil)
}

func programData(p *Process, f Object, module bool) (data []byte, ok bool, err error) {
	var st Stat

	if err := p.fileStat(f, &st); err == EBADF {
		return nil, false, EACCES
	} else if err != nil {
		return nil, false, err
	}

	if fl, _ := p.fileFlags(f); fl&O_PATH != 0 {
		return nil, false, EACCES
	}

	return readProgram(st.Size, func(b []byte, off int64) error { return readAt(p, f, b, off) }, module)
}

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

func readAt(p *Process, t Object, b []byte, off int64) error {
	for at := 0; at < len(b); {
		n, err := p.filePread(t, bytesFile(b[at:]), len(b)-at, off+int64(at))

		if err != nil {
			return err
		}

		if n == 0 {
			return ENOEXEC
		}

		at += n
	}

	return nil
}

type CodeKey struct {
	hash [sha256.Size]byte
	asan bool
}

type CodeEntry struct {
	done    bool
	waiting []*Process
	code    *Code
	err     error
	link    sync.Mutex
	traced  map[uint32]*Code
}

func (k *Kernel) codeOf(p *Process, data []byte, name string, obj *ElfObject) (*Code, error) {
	key := CodeKey{hash: sha256.Sum256(data), asan: p.modes&EXEC_ASAN != 0}

	k.mu.Lock()

	e, found := k.codes[key]

	if !found {
		e = &CodeEntry{}
		k.codes[key] = e

		go func() {
			code, err := k.compile(data, name, obj, key.asan)

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

	p.wait(time.Time{}, func(*Event) (bool, error) {
		k.mu.Lock()

		defer k.mu.Unlock()

		return e.done, nil
	})

	if e.err != nil {
		return nil, e.err
	}

	return k.traced(e, name, p.tracedCalls())
}

func (k *Kernel) traced(e *CodeEntry, name string, classes uint32) (*Code, error) {
	if classes == 0 {
		return e.code, nil
	}

	e.link.Lock()

	defer e.link.Unlock()

	if c := e.traced[classes]; c != nil {
		return c, nil
	}

	var c *Code

	if x := try(func() {
		c = link(name, e.code.obj, &TracedImports{k: k, classes: classes})
	}); x != nil {
		return nil, fmt.Errorf("%s: %s: %w", name, x, ENOEXEC)
	}

	if e.traced == nil {
		e.traced = map[uint32]*Code{}
	}

	e.traced[classes] = c

	return c, nil
}

func (k *Kernel) compile(data []byte, name string, obj *ElfObject, asan bool) (*Code, error) {
	if obj == nil {
		var err error

		if obj, err = k.compileExecutable(data, programOptions(asan)); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}

	var c *Code

	if e := try(func() {
		c = link(name, obj, k)
	}); e != nil {
		return nil, fmt.Errorf("%s: %s: %w", name, e, ENOEXEC)
	}

	return c, nil
}

func (k *Kernel) signalTargets(from *Process, pid int32) []*Process {
	return k.processes(func(q *Process) bool {
		switch {
		case pid > 0:
			return q.pid == pid
		case pid == 0:
			return q.pgid.Load() == from.pgid.Load()
		case pid == -1:
			return q.pid != 1 && q != from
		}

		return q.pgid.Load() == -pid
	})
}

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

func (k *Kernel) routeSignals() {
	c := make(chan os.Signal, 16)

	signal.Notify(c, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP, syscall.SIGWINCH, syscall.SIGTERM, syscall.SIGHUP,
		syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGCONT, syscall.SIGALRM)

	go func() {
		for s := range c {
			sig := hostSignals[s.(syscall.Signal)]

			switch s {
			case syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP, syscall.SIGWINCH:
				k.mu.Lock()

				fg := k.fg

				k.mu.Unlock()

				for _, q := range k.processes(func(q *Process) bool { return q.pgid.Load() == fg }) {
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

func (k *Kernel) orphaned(pgid int32) bool {
	k.mu.Lock()

	defer k.mu.Unlock()

	return k.orphanedLocked(pgid)
}

func (k *Kernel) orphanedLocked(pgid int32) bool {
	for _, q := range k.procs {
		if q.pgid.Load() != pgid || q.zombie {
			continue
		}

		if parent := k.procs[q.ppid]; parent != nil && parent.pgid.Load() != pgid && parent.sid.Load() == q.sid.Load() {
			return false
		}
	}

	return true
}

func (k *Kernel) orphans(groups []int32) {
	for i, pgid := range groups {
		if slices.Contains(groups[:i], pgid) {
			continue
		}

		k.mu.Lock()

		stopped := false

		for _, q := range k.procs {
			stopped = stopped || q.pgid.Load() == pgid && !q.zombie && q.halted
		}

		hangs := stopped && k.orphanedLocked(pgid)

		k.mu.Unlock()

		if hangs {
			k.signalGroup(pgid, SIGHUP)
			k.signalGroup(pgid, SIGCONT)
		}
	}
}

func (k *Kernel) signalGroup(pgid int32, sig int) {
	k.mu.Lock()

	var targets []*Process

	for _, q := range k.procs {
		if q.pgid.Load() == pgid && !q.zombie {
			targets = append(targets, q)
		}
	}

	k.mu.Unlock()

	for _, q := range targets {
		q.send(sig)
	}
}

func (k *Kernel) newConsole() []Object {
	c := &Console{}

	rawHostTerminal()
	var pipes [3]*PipeEnd

	for fd := range 3 {
		var st syscall.Stat_t

		if syscall.Fstat(fd, &st) != nil {
			continue
		}

		pp := newPipe()

		pp.readers, pp.writers = 1, 1

		reader, writer := &PipeEnd{pipe: pp, reads: true}, &PipeEnd{pipe: pp, writes: true}

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

	for fd, out := range []*PipeEnd{pipes[1], pipes[1], pipes[2]} {
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

func (k *Kernel) report(text string) {
	if k.console == nil || k.console.err == nil || !newMover().put(k.console.err, []byte(text)) {
		syscall.Write(2, []byte(text))
	}
}

func (k *Kernel) foreground() int32 {
	k.mu.Lock()

	defer k.mu.Unlock()

	return k.fg
}

type SystemNames struct {
	mu           sync.Mutex
	host, domain string
}

func (k *Kernel) functionTable() map[string]any {
	return map[string]any{
		"__kernel_start":                   libc__kernel_start,
		"__kernel_program":                 libc__kernel_program,
		"read":                             libcRead,
		"write":                            libcWrite,
		"readv":                            libcReadv,
		"writev":                           libcWritev,
		"preadv":                           libcPreadv,
		"pwritev":                          libcPwritev,
		"pread":                            libcPread,
		"pwrite":                           libcPwrite,
		"copy_file_range":                  libcCopy_file_range,
		"fdatasync":                        libcFdatasync,
		"sync":                             libcSync,
		"syncfs":                           libcSyncfs,
		"flock":                            libcFlock,
		"lockf":                            libcLockf,
		"posix_fadvise":                    libcPosix_fadvise,
		"posix_fallocate":                  libcPosix_fallocate,
		"select":                           libcSelect,
		"pselect":                          libcPselect,
		"lseek":                            libcLseek,
		"close":                            libcClose,
		"dup":                              libcDup,
		"dup2":                             libcDup2,
		"dup3":                             libcDup3,
		"pipe":                             libcPipe,
		"pipe2":                            libcPipe2,
		"fcntl":                            libcFcntl,
		"ioctl":                            libcIoctl,
		"isatty":                           libcIsatty,
		"tcgetattr":                        libcTcgetattr,
		"tcsetattr":                        libcTcsetattr,
		"cfmakeraw":                        libcCfmakeraw,
		"tcdrain":                          libcTcdrain,
		"tcsendbreak":                      libcTcsendbreak,
		"tcflow":                           libcTcflow,
		"tcflush":                          libcTcflush,
		"tcgetsid":                         libcTcgetsid,
		"cfgetospeed":                      libcCfgetospeed,
		"cfgetispeed":                      libcCfgetispeed,
		"cfsetospeed":                      libcCfsetospeed,
		"cfsetspeed":                       libcCfsetspeed,
		"cfsetispeed":                      libcCfsetispeed,
		"poll":                             libcPoll,
		"ppoll":                            libcPpoll,
		"epoll_create":                     libcEpoll_create,
		"epoll_create1":                    libcEpoll_create1,
		"epoll_ctl":                        libcEpoll_ctl,
		"epoll_wait":                       libcEpoll_wait,
		"epoll_pwait":                      libcEpoll_pwait,
		"fsync":                            libcFsync,
		"ftruncate":                        libcFtruncate,
		"fstat":                            libcFstat,
		"fchmod":                           libcFchmod,
		"fchown":                           libcFchown,
		"futimes":                          libcFutimes,
		"futimens":                         libcFutimens,
		"open":                             libcOpen,
		"openat":                           libcOpenat,
		"stat":                             libcStat,
		"lstat":                            libcLstat,
		"fstatat":                          libcFstatat,
		"access":                           libcAccess,
		"faccessat":                        libcFaccessat,
		"truncate":                         libcTruncate,
		"statvfs":                          libcStatvfs,
		"fstatvfs":                         libcFstatvfs,
		"fchdir":                           libcFchdir,
		"chroot":                           libcChroot,
		"attach":                           libcAttach,
		"watchdir":                         libcWatchdir,
		"chdir":                            libcChdir,
		"getcwd":                           libcGetcwd,
		"mkdir":                            libcMkdir,
		"mkdirat":                          libcMkdirat,
		"rmdir":                            libcRmdir,
		"unlink":                           libcUnlink,
		"unlinkat":                         libcUnlinkat,
		"mount":                            libcMount,
		"umount":                           libcUmount,
		"umount2":                          libcUmount2,
		"execmode":                         libcExecmode,
		"remove":                           libcRemove,
		"rename":                           libcRename,
		"renameat":                         libcRenameat,
		"link":                             libcLink,
		"linkat":                           libcLinkat,
		"symlink":                          libcSymlink,
		"symlinkat":                        libcSymlinkat,
		"readlink":                         libcReadlink,
		"readlinkat":                       libcReadlinkat,
		"canonicalize_file_name":           libcCanonicalize_file_name,
		"get_current_dir_name":             libcGet_current_dir_name,
		"creat":                            libcCreat,
		"eaccess":                          libcEaccess,
		"euidaccess":                       libcEuidaccess,
		"statfs":                           libcStatfs,
		"fstatfs":                          libcFstatfs,
		"acct":                             libcAcct,
		"realpath":                         libcRealpath,
		"chmod":                            libcChmod,
		"fchmodat":                         libcFchmodat,
		"chown":                            libcChown,
		"lchown":                           libcLchown,
		"fchownat":                         libcFchownat,
		"mknod":                            libcMknod,
		"mknodat":                          libcMknodat,
		"mkfifo":                           libcMkfifo,
		"mkfifoat":                         libcMkfifoat,
		"utimes":                           libcUtimes,
		"lutimes":                          libcLutimes,
		"utime":                            libcUtime,
		"utimensat":                        libcUtimensat,
		"opendir":                          libcOpendir,
		"fdopendir":                        libcFdopendir,
		"readdir":                          libcReaddir,
		"rewinddir":                        libcRewinddir,
		"closedir":                         libcClosedir,
		"dirfd":                            libcDirfd,
		"readdir_r":                        libcReaddir_r,
		"telldir":                          libcTelldir,
		"seekdir":                          libcSeekdir,
		"alphasort":                        libcAlphasort,
		"versionsort":                      libcVersionsort,
		"mkstemp":                          libcMkstemp,
		"mkostemp":                         libcMkostemp,
		"mkstemps":                         libcMkstemps,
		"mkdtemp":                          libcMkdtemp,
		"mktemp":                           libcMktemp,
		"malloc":                           libcMalloc,
		"free":                             libcFree,
		"calloc":                           libcCalloc,
		"realloc":                          libcRealloc,
		"mallopt":                          libcMallopt,
		"getpagesize":                      libcGetpagesize,
		"mmap":                             libcMmap,
		"memfd_create":                     libcMemfd_create,
		"munmap":                           libcMunmap,
		"msync":                            libcMsync,
		"madvise":                          libcMadvise,
		"mprotect":                         libcMprotect,
		"mlock":                            libcMlock,
		"munlock":                          libcMunlock,
		"mlockall":                         libcMlockall,
		"munlockall":                       libcMunlockall,
		"memalign":                         libcMemalign,
		"aligned_alloc":                    libcAligned_alloc,
		"valloc":                           libcValloc,
		"posix_memalign":                   libcPosix_memalign,
		"malloc_usable_size":               libcMalloc_usable_size,
		"malloc_trim":                      libcMalloc_trim,
		"reallocarray":                     libcReallocarray,
		"strncmp":                          libcStrncmp,
		"strcasecmp":                       libcStrcasecmp,
		"strncasecmp":                      libcStrncasecmp,
		"strchr":                           libcStrchr,
		"strchrnul":                        libcStrchrnul,
		"strrchr":                          libcStrrchr,
		"strcasestr":                       libcStrcasestr,
		"stpcpy":                           libcStpcpy,
		"strncpy":                          libcStrncpy,
		"stpncpy":                          libcStpncpy,
		"strdup":                           libcStrdup,
		"strndup":                          libcStrndup,
		"strtok_r":                         libcStrtok_r,
		"strsep":                           libcStrsep,
		"strverscmp":                       libcStrverscmp,
		"abs":                              libcAbs,
		"strtod":                           libcStrtod,
		"strtof":                           libcStrtof,
		"strtold":                          libcStrtold,
		"wcstod":                           libcWcstod,
		"wcstof":                           libcWcstof,
		"wcstold":                          libcWcstold,
		"a64l":                             libcA64l,
		"l64a":                             libcL64a,
		"gcvt":                             libcGcvt,
		"cuserid":                          libcCuserid,
		"sighold":                          libcSighold,
		"sigrelse":                         libcSigrelse,
		"sigignore":                        libcSigignore,
		"sigpause":                         libcSigpause,
		"sigset":                           libcSigset,
		"iconv_open":                       libcIconv_open,
		"iconv":                            libcIconv,
		"iconv_close":                      libcIconv_close,
		"__dtoa":                           libc__dtoa,
		"__ldtoa":                          libc__ldtoa,
		"__hdtoa":                          libc__hdtoa,
		"__hldtoa":                         libc__hldtoa,
		"isalnum":                          libcIsalnum,
		"isalpha":                          libcIsalpha,
		"isascii":                          libcIsascii,
		"isblank":                          libcIsblank,
		"iscntrl":                          libcIscntrl,
		"isdigit":                          libcIsdigit,
		"isgraph":                          libcIsgraph,
		"islower":                          libcIslower,
		"isprint":                          libcIsprint,
		"ispunct":                          libcIspunct,
		"isspace":                          libcIsspace,
		"isupper":                          libcIsupper,
		"isxdigit":                         libcIsxdigit,
		"tolower":                          libcTolower,
		"toupper":                          libcToupper,
		"toascii":                          libcToascii,
		"memmem":                           libcMemmem,
		"mempcpy":                          libcMempcpy,
		"strnlen":                          libcStrnlen,
		"strncat":                          libcStrncat,
		"strlcpy":                          libcStrlcpy,
		"strlcat":                          libcStrlcat,
		"strstr":                           libcStrstr,
		"strpbrk":                          libcStrpbrk,
		"explicit_bzero":                   libcExplicit_bzero,
		"bcopy":                            libcBcopy,
		"bzero":                            libcBzero,
		"memcpy":                           libcMemcpy,
		"memmove":                          libcMemmove,
		"memset":                           libcMemset,
		"memcmp":                           libcMemcmp,
		"bcmp":                             libcBcmp,
		"memchr":                           libcMemchr,
		"strlen":                           libcStrlen,
		"strcpy":                           libcStrcpy,
		"strcat":                           libcStrcat,
		"strcmp":                           libcStrcmp,
		"strspn":                           libcStrspn,
		"strcspn":                          libcStrcspn,
		"strerror":                         libcStrerror,
		"strsignal":                        libcStrsignal,
		"dirname":                          libcDirname,
		"fnmatch":                          libcFnmatch,
		"memrchr":                          libcMemrchr,
		"rawmemchr":                        libcRawmemchr,
		"memccpy":                          libcMemccpy,
		"swab":                             libcSwab,
		"strtok":                           libcStrtok,
		"strcoll":                          libcStrcoll,
		"strxfrm":                          libcStrxfrm,
		"strerror_r":                       libcStrerror_r,
		"basename":                         libcBasename,
		"ffs":                              libcFfs,
		"ffsl":                             libcFfsl,
		"ffsll":                            libcFfsll,
		"labs":                             libcLabs,
		"llabs":                            libcLlabs,
		"div":                              libcDiv,
		"lldiv":                            libcLldiv,
		"index":                            libcStrchr,
		"rindex":                           libcStrrchr,
		"ldiv":                             libcDiv,
		"imaxdiv":                          libcLldiv,
		"imaxabs":                          libcLlabs,
		"time":                             libcTime,
		"gettimeofday":                     libcGettimeofday,
		"clock_settime":                    libcClock_settime,
		"localtime":                        libcLocaltime,
		"localtime_r":                      libcLocaltime_r,
		"tzset":                            libcTzset,
		"asctime":                          libcAsctime,
		"asctime_r":                        libcAsctime_r,
		"ctime_r":                          libcCtime_r,
		"difftime":                         libcDifftime,
		"timegm":                           libcTimegm,
		"settimeofday":                     libcSettimeofday,
		"stime":                            libcStime,
		"adjtime":                          libcAdjtime,
		"usleep":                           libcUsleep,
		"ualarm":                           libcUalarm,
		"gmtime":                           libcGmtime,
		"gmtime_r":                         libcGmtime_r,
		"clock":                            libcClock,
		"clock_getres":                     libcClock_getres,
		"clock_nanosleep":                  libcClock_nanosleep,
		"setitimer":                        libcSetitimer,
		"getitimer":                        libcGetitimer,
		"alarm":                            libcAlarm,
		"mktime":                           libcMktime,
		"ctime":                            libcCtime,
		"strftime":                         libcStrftime,
		"strptime":                         libcStrptime,
		"times":                            libcTimes,
		"sleep":                            libcSleep,
		"clock_gettime":                    libcClock_gettime,
		"nanosleep":                        libcNanosleep,
		"regcomp":                          libcRegcomp,
		"regexec":                          libcRegexec,
		"regfree":                          libcRegfree,
		"regerror":                         libcRegerror,
		"signal":                           libcSignal,
		"sigaction":                        libcSigaction,
		"sigprocmask":                      libcSigprocmask,
		"pthread_sigmask":                  libcPthread_sigmask,
		"sigfillset":                       libcSigfillset,
		"sigemptyset":                      libcSigemptyset,
		"sigaddset":                        libcSigaddset,
		"sigdelset":                        libcSigdelset,
		"sigismember":                      libcSigismember,
		"sigisemptyset":                    libcSigisemptyset,
		"sigorset":                         libcSigorset,
		"sigandset":                        libcSigandset,
		"sigpending":                       libcSigpending,
		"sigwait":                          libcSigwait,
		"sigwaitinfo":                      libcSigwaitinfo,
		"sigtimedwait":                     libcSigtimedwait,
		"sigaltstack":                      libcSigaltstack,
		"siginterrupt":                     libcSiginterrupt,
		"pause":                            libcPause,
		"sigsuspend":                       libcSigsuspend,
		"raise":                            libcRaise,
		"kill":                             libcKill,
		"sigqueue":                         libcSigqueue,
		"killpg":                           libcKillpg,
		"abort":                            libcAbort,
		"bsd_signal":                       libcSignal,
		"getpid":                           libcGetpid,
		"getppid":                          libcGetppid,
		"getpgrp":                          libcGetpgrp,
		"setpgid":                          libcSetpgid,
		"tcgetpgrp":                        libcTcgetpgrp,
		"tcsetpgrp":                        libcTcsetpgrp,
		"getuid":                           libcGetuid,
		"geteuid":                          libcGeteuid,
		"getgid":                           libcGetgid,
		"getegid":                          libcGetegid,
		"getgroups":                        libcGetgroups,
		"uname":                            libcUname,
		"sysconf":                          libcSysconf,
		"umask":                            libcUmask,
		"rand":                             libcRand,
		"srand":                            libcSrand,
		"getenv":                           libcGetenv,
		"putenv":                           libcPutenv,
		"setenv":                           libcSetenv,
		"unsetenv":                         libcUnsetenv,
		"clearenv":                         libcClearenv,
		"execve":                           libcExecve,
		"execvp":                           libcExecvp,
		"execv":                            libcExecv,
		"execvpe":                          libcExecvpe,
		"execle":                           libcExecle,
		"fexecve":                          libcFexecve,
		"execl":                            libcExecl,
		"execlp":                           libcExeclp,
		"_exit":                            libc_exit,
		"_Exit":                            libc_Exit,
		"system":                           libcSystem,
		"setjmp":                           libcSetjmp,
		"sigsetjmp":                        libcSigsetjmp,
		"longjmp":                          libcLongjmp,
		"_setjmp":                          libcSetjmp,
		"_longjmp":                         libcLongjmp,
		"siglongjmp":                       libcLongjmp,
		"vfork":                            libcVfork,
		"fork":                             libcFork,
		"waitpid":                          libcWaitpid,
		"wait":                             libcWait,
		"wait3":                            libcWait3,
		"wait4":                            libcWait4,
		"waitid":                           libcWaitid,
		"setlocale":                        libcSetlocale,
		"localeconv":                       libcLocaleconv,
		"nl_langinfo":                      libcNl_langinfo,
		"towlower":                         libcTowlower,
		"towupper":                         libcTowupper,
		"wctype":                           libcWctype,
		"iswctype":                         libcIswctype,
		"wctrans":                          libcWctrans,
		"towctrans":                        libcTowctrans,
		"iswalnum":                         libcIswalnum,
		"iswalpha":                         libcIswalpha,
		"iswblank":                         libcIswblank,
		"iswcntrl":                         libcIswcntrl,
		"iswdigit":                         libcIswdigit,
		"iswgraph":                         libcIswgraph,
		"iswlower":                         libcIswlower,
		"iswprint":                         libcIswprint,
		"iswpunct":                         libcIswpunct,
		"iswspace":                         libcIswspace,
		"iswupper":                         libcIswupper,
		"iswxdigit":                        libcIswxdigit,
		"wcslen":                           libcWcslen,
		"wcsnlen":                          libcWcsnlen,
		"wcscpy":                           libcWcscpy,
		"wcsncpy":                          libcWcsncpy,
		"wcscat":                           libcWcscat,
		"wcscmp":                           libcWcscmp,
		"wcsncmp":                          libcWcsncmp,
		"wcscoll":                          libcWcscoll,
		"wcsxfrm":                          libcWcsxfrm,
		"wcschr":                           libcWcschr,
		"wcsrchr":                          libcWcsrchr,
		"wcsstr":                           libcWcsstr,
		"wcspbrk":                          libcWcspbrk,
		"wcsspn":                           libcWcsspn,
		"wcscspn":                          libcWcscspn,
		"wcstok":                           libcWcstok,
		"wcsdup":                           libcWcsdup,
		"wcsftime":                         libcWcsftime,
		"wmemchr":                          libcWmemchr,
		"wmemcmp":                          libcWmemcmp,
		"wmemcpy":                          libcWmemcpy,
		"wmemmove":                         libcWmemmove,
		"wmemset":                          libcWmemset,
		"wcwidth":                          libcWcwidth,
		"wcswidth":                         libcWcswidth,
		"setuid":                           libcSetuid,
		"seteuid":                          libcSeteuid,
		"setreuid":                         libcSetreuid,
		"setresuid":                        libcSetresuid,
		"setgid":                           libcSetgid,
		"setegid":                          libcSetegid,
		"setregid":                         libcSetregid,
		"setresgid":                        libcSetresgid,
		"getresuid":                        libcGetresuid,
		"getresgid":                        libcGetresgid,
		"setgroups":                        libcSetgroups,
		"initgroups":                       libcInitgroups,
		"setsid":                           libcSetsid,
		"getsid":                           libcGetsid,
		"getpgid":                          libcGetpgid,
		"setpgrp":                          libcSetpgrp,
		"nice":                             libcNice,
		"getpriority":                      libcGetpriority,
		"setpriority":                      libcSetpriority,
		"sched_yield":                      libcSched_yield,
		"sched_get_priority_max":           libcSched_get_priority_max,
		"sched_get_priority_min":           libcSched_get_priority_min,
		"sched_getscheduler":               libcSched_getscheduler,
		"sched_getparam":                   libcSched_getparam,
		"sched_setparam":                   libcSched_setparam,
		"sched_setscheduler":               libcSched_setscheduler,
		"sched_rr_get_interval":            libcSched_rr_get_interval,
		"openlog":                          libcOpenlog,
		"closelog":                         libcCloselog,
		"setlogmask":                       libcSetlogmask,
		"__kernel_syslog":                  libc__kernel_syslog,
		"sysinfo":                          libcSysinfo,
		"get_nprocs":                       libcGet_nprocs,
		"get_nprocs_conf":                  libcGet_nprocs_conf,
		"get_phys_pages":                   libcGet_phys_pages,
		"get_avphys_pages":                 libcGet_avphys_pages,
		"getloadavg":                       libcGetloadavg,
		"getrusage":                        libcGetrusage,
		"getrlimit":                        libcGetrlimit,
		"setrlimit":                        libcSetrlimit,
		"prlimit":                          libcPrlimit,
		"getdtablesize":                    libcGetdtablesize,
		"confstr":                          libcConfstr,
		"pathconf":                         libcPathconf,
		"fpathconf":                        libcFpathconf,
		"getlogin":                         libcGetlogin,
		"getlogin_r":                       libcGetlogin_r,
		"ctermid":                          libcCtermid,
		"ttyname":                          libcTtyname,
		"ttyname_r":                        libcTtyname_r,
		"getpwnam":                         libcGetpwnam,
		"getpwuid":                         libcGetpwuid,
		"getpwnam_r":                       libcGetpwnam_r,
		"getpwuid_r":                       libcGetpwuid_r,
		"getpwent":                         libcGetpwent,
		"setpwent":                         libcSetpwent,
		"endpwent":                         libcEndpwent,
		"getgrnam":                         libcGetgrnam,
		"getgrgid":                         libcGetgrgid,
		"getgrnam_r":                       libcGetgrnam_r,
		"getgrgid_r":                       libcGetgrgid_r,
		"getgrent":                         libcGetgrent,
		"setgrent":                         libcSetgrent,
		"endgrent":                         libcEndgrent,
		"getgrouplist":                     libcGetgrouplist,
		"posix_openpt":                     libcPosix_openpt,
		"grantpt":                          libcGrantpt,
		"unlockpt":                         libcUnlockpt,
		"ptsname":                          libcPtsname,
		"ptsname_r":                        libcPtsname_r,
		"openpty":                          libcOpenpty,
		"forkpty":                          libcForkpty,
		"glob":                             libcGlob,
		"globfree":                         libcGlobfree,
		"random":                           libcRandom,
		"srandom":                          libcSrandom,
		"initstate":                        libcInitstate,
		"setstate":                         libcSetstate,
		"rand_r":                           libcRand_r,
		"secure_getenv":                    libcSecure_getenv,
		"getsubopt":                        libcGetsubopt,
		"daemon":                           libcDaemon,
		"prctl":                            libcPrctl,
		"getpass":                          libcGetpass,
		"vhangup":                          libcVhangup,
		"getusershell":                     libcGetusershell,
		"setusershell":                     libcSetusershell,
		"endusershell":                     libcEndusershell,
		"gethostname":                      libcGethostname,
		"getentropy":                       libcGetentropy,
		"getdomainname":                    libcGetdomainname,
		"sethostname":                      libcSethostname,
		"setdomainname":                    libcSetdomainname,
		"gethostid":                        libcGethostid,
		"sethostid":                        libcSethostid,
		"psignal":                          libcPsignal,
		"psiginfo":                         libcPsiginfo,
		"__assert_fail":                    libc__assert_fail,
		"socket":                           libcSocket,
		"socketpair":                       libcSocketpair,
		"bind":                             libcBind,
		"connect":                          libcConnect,
		"listen":                           libcListen,
		"accept":                           libcAccept,
		"accept4":                          libcAccept4,
		"getsockname":                      libcGetsockname,
		"getpeername":                      libcGetpeername,
		"getsockopt":                       libcGetsockopt,
		"setsockopt":                       libcSetsockopt,
		"send":                             libcSend,
		"sendto":                           libcSendto,
		"recv":                             libcRecv,
		"recvfrom":                         libcRecvfrom,
		"sendmsg":                          libcSendmsg,
		"recvmsg":                          libcRecvmsg,
		"shutdown":                         libcShutdown,
		"sockatmark":                       libcSockatmark,
		"getaddrinfo":                      libcGetaddrinfo,
		"freeaddrinfo":                     libcFreeaddrinfo,
		"gai_strerror":                     libcGai_strerror,
		"getnameinfo":                      libcGetnameinfo,
		"gethostbyname":                    libcGethostbyname,
		"gethostbyname2":                   libcGethostbyname2,
		"gethostbyaddr":                    libcGethostbyaddr,
		"gethostent":                       libcGethostent,
		"sethostent":                       libcSethostent,
		"endhostent":                       libcEndhostent,
		"getservbyname":                    libcGetservbyname,
		"getservbyport":                    libcGetservbyport,
		"getprotobyname":                   libcGetprotobyname,
		"getprotobynumber":                 libcGetprotobynumber,
		"getnetbyname":                     libcGetnetbyname,
		"getnetbyaddr":                     libcGetnetbyaddr,
		"hstrerror":                        libcHstrerror,
		"herror":                           libcHerror,
		"inet_aton":                        libcInet_aton,
		"inet_addr":                        libcInet_addr,
		"inet_network":                     libcInet_network,
		"inet_ntoa":                        libcInet_ntoa,
		"inet_pton":                        libcInet_pton,
		"inet_ntop":                        libcInet_ntop,
		"inet_makeaddr":                    libcInet_makeaddr,
		"inet_lnaof":                       libcInet_lnaof,
		"inet_netof":                       libcInet_netof,
		"if_nametoindex":                   libcIf_nametoindex,
		"if_indextoname":                   libcIf_indextoname,
		"if_nameindex":                     libcIf_nameindex,
		"if_freenameindex":                 libcIf_freenameindex,
		"crypt":                            libcCrypt,
		"pthread_create":                   libcPthread_create,
		"pthread_join":                     libcPthread_join,
		"pthread_detach":                   libcPthread_detach,
		"pthread_self":                     libcPthread_self,
		"pthread_equal":                    libcPthread_equal,
		"pthread_cancel":                   libcPthread_cancel,
		"pthread_setcancelstate":           libcPthread_setcancelstate,
		"pthread_setcanceltype":            libcPthread_setcanceltype,
		"pthread_getschedparam":            libcPthread_getschedparam,
		"pthread_setschedparam":            libcPthread_setschedparam,
		"pthread_setschedprio":             libcPthread_setschedprio,
		"pthread_getconcurrency":           libcPthread_getconcurrency,
		"pthread_setconcurrency":           libcPthread_setconcurrency,
		"pthread_getcpuclockid":            libcPthread_getcpuclockid,
		"pthread_attr_init":                libcPthread_attr_init,
		"pthread_attr_destroy":             libcPthread_attr_destroy,
		"pthread_attr_setdetachstate":      libcPthread_attr_setdetachstate,
		"pthread_attr_getdetachstate":      libcPthread_attr_getdetachstate,
		"pthread_attr_setstacksize":        libcPthread_attr_setstacksize,
		"pthread_attr_getstacksize":        libcPthread_attr_getstacksize,
		"pthread_attr_setguardsize":        libcPthread_attr_setguardsize,
		"pthread_attr_getguardsize":        libcPthread_attr_getguardsize,
		"pthread_attr_setscope":            libcPthread_attr_setscope,
		"pthread_attr_getscope":            libcPthread_attr_getscope,
		"pthread_attr_setschedpolicy":      libcPthread_attr_setschedpolicy,
		"pthread_attr_getschedpolicy":      libcPthread_attr_getschedpolicy,
		"pthread_attr_setinheritsched":     libcPthread_attr_setinheritsched,
		"pthread_attr_getinheritsched":     libcPthread_attr_getinheritsched,
		"pthread_attr_setstack":            libcPthread_attr_setstack,
		"pthread_attr_getstack":            libcPthread_attr_getstack,
		"pthread_attr_setschedparam":       libcPthread_attr_setschedparam,
		"pthread_attr_getschedparam":       libcPthread_attr_getschedparam,
		"pthread_key_create":               libcPthread_key_create,
		"pthread_key_delete":               libcPthread_key_delete,
		"pthread_getspecific":              libcPthread_getspecific,
		"pthread_setspecific":              libcPthread_setspecific,
		"pthread_mutexattr_init":           libcPthread_mutexattr_init,
		"pthread_mutexattr_destroy":        libcPthread_mutexattr_destroy,
		"pthread_mutexattr_settype":        libcPthread_mutexattr_settype,
		"pthread_mutexattr_gettype":        libcPthread_mutexattr_gettype,
		"pthread_mutexattr_setprotocol":    libcPthread_mutexattr_setprotocol,
		"pthread_mutexattr_getprotocol":    libcPthread_mutexattr_getprotocol,
		"pthread_mutexattr_setrobust":      libcPthread_mutexattr_setrobust,
		"pthread_mutexattr_getrobust":      libcPthread_mutexattr_getrobust,
		"pthread_mutexattr_setpshared":     libcPthread_mutexattr_setpshared,
		"pthread_mutexattr_getpshared":     libcPthread_mutexattr_getpshared,
		"pthread_mutexattr_setprioceiling": libcPthread_mutexattr_setprioceiling,
		"pthread_mutexattr_getprioceiling": libcPthread_mutexattr_getprioceiling,
		"pthread_mutex_init":               libcPthread_mutex_init,
		"pthread_mutex_destroy":            libcPthread_mutex_destroy,
		"pthread_mutex_lock":               libcPthread_mutex_lock,
		"pthread_mutex_trylock":            libcPthread_mutex_trylock,
		"pthread_mutex_unlock":             libcPthread_mutex_unlock,
		"pthread_mutex_consistent":         libcPthread_mutex_consistent,
		"pthread_mutex_timedlock":          libcPthread_mutex_timedlock,
		"pthread_mutex_getprioceiling":     libcPthread_mutex_getprioceiling,
		"pthread_mutex_setprioceiling":     libcPthread_mutex_setprioceiling,
		"pthread_condattr_init":            libcPthread_condattr_init,
		"pthread_condattr_destroy":         libcPthread_condattr_destroy,
		"pthread_condattr_setclock":        libcPthread_condattr_setclock,
		"pthread_condattr_getclock":        libcPthread_condattr_getclock,
		"pthread_condattr_setpshared":      libcPthread_condattr_setpshared,
		"pthread_condattr_getpshared":      libcPthread_condattr_getpshared,
		"pthread_cond_init":                libcPthread_cond_init,
		"pthread_cond_destroy":             libcPthread_cond_destroy,
		"pthread_cond_signal":              libcPthread_cond_signal,
		"pthread_cond_broadcast":           libcPthread_cond_broadcast,
		"pthread_cond_wait":                libcPthread_cond_wait,
		"pthread_cond_timedwait":           libcPthread_cond_timedwait,
		"pthread_rwlockattr_init":          libcPthread_rwlockattr_init,
		"pthread_rwlockattr_destroy":       libcPthread_rwlockattr_destroy,
		"pthread_rwlockattr_setpshared":    libcPthread_rwlockattr_setpshared,
		"pthread_rwlockattr_getpshared":    libcPthread_rwlockattr_getpshared,
		"pthread_rwlock_init":              libcPthread_rwlock_init,
		"pthread_rwlock_destroy":           libcPthread_rwlock_destroy,
		"pthread_rwlock_rdlock":            libcPthread_rwlock_rdlock,
		"pthread_rwlock_tryrdlock":         libcPthread_rwlock_tryrdlock,
		"pthread_rwlock_wrlock":            libcPthread_rwlock_wrlock,
		"pthread_rwlock_trywrlock":         libcPthread_rwlock_trywrlock,
		"pthread_rwlock_unlock":            libcPthread_rwlock_unlock,
		"pthread_rwlock_timedrdlock":       libcPthread_rwlock_timedrdlock,
		"pthread_rwlock_timedwrlock":       libcPthread_rwlock_timedwrlock,
		"pthread_barrierattr_init":         libcPthread_barrierattr_init,
		"pthread_barrierattr_destroy":      libcPthread_barrierattr_destroy,
		"pthread_barrierattr_setpshared":   libcPthread_barrierattr_setpshared,
		"pthread_barrierattr_getpshared":   libcPthread_barrierattr_getpshared,
		"pthread_barrier_init":             libcPthread_barrier_init,
		"pthread_barrier_destroy":          libcPthread_barrier_destroy,
		"pthread_barrier_wait":             libcPthread_barrier_wait,
		"pthread_spin_init":                libcPthread_spin_init,
		"pthread_spin_destroy":             libcPthread_spin_destroy,
		"pthread_spin_lock":                libcPthread_spin_lock,
		"pthread_spin_trylock":             libcPthread_spin_trylock,
		"pthread_spin_unlock":              libcPthread_spin_unlock,
		"pthread_getaffinity_np":           libcPthread_getaffinity_np,
		"pthread_setaffinity_np":           libcPthread_setaffinity_np,
		"pthread_getattr_np":               libcPthread_getattr_np,
		"pthread_getattr_default_np":       libcPthread_getattr_default_np,
		"pthread_setattr_default_np":       libcPthread_setattr_default_np,
		"pthread_getname_np":               libcPthread_getname_np,
		"pthread_setname_np":               libcPthread_setname_np,
		"pthread_tryjoin_np":               libcPthread_tryjoin_np,
		"pthread_timedjoin_np":             libcPthread_timedjoin_np,
		"__pthread_once_begin":             libc__pthread_once_begin,
		"__pthread_once_end":               libc__pthread_once_end,
		"__pthread_cancelled":              libc__pthread_cancelled,
		"__pthread_key_take":               libc__pthread_key_take,
		"__sched_cpucount":                 libc__sched_cpucount,
		"sched_getaffinity":                libcSched_getaffinity,
		"sched_setaffinity":                libcSched_setaffinity,
		"sched_getcpu":                     libcSched_getcpu,
		"sem_init":                         libcSem_init,
		"sem_destroy":                      libcSem_destroy,
		"sem_wait":                         libcSem_wait,
		"sem_trywait":                      libcSem_trywait,
		"sem_post":                         libcSem_post,
		"sem_close":                        libcSem_close,
		"sem_timedwait":                    libcSem_timedwait,
		"sem_getvalue":                     libcSem_getvalue,
		"sem_unlink":                       libcSem_unlink,
		"pthread_kill":                     libcPthread_kill,
		"thrd_create":                      libcThrd_create,
		"thrd_join":                        libcThrd_join,
		"thrd_detach":                      libcThrd_detach,
		"thrd_current":                     libcThrd_current,
		"thrd_equal":                       libcThrd_equal,
		"thrd_sleep":                       libcThrd_sleep,
		"thrd_yield":                       libcThrd_yield,
		"mtx_init":                         libcMtx_init,
		"mtx_destroy":                      libcMtx_destroy,
		"mtx_lock":                         libcMtx_lock,
		"mtx_trylock":                      libcMtx_trylock,
		"mtx_unlock":                       libcMtx_unlock,
		"mtx_timedlock":                    libcMtx_timedlock,
		"cnd_init":                         libcCnd_init,
		"cnd_destroy":                      libcCnd_destroy,
		"cnd_broadcast":                    libcCnd_broadcast,
		"cnd_signal":                       libcCnd_signal,
		"cnd_timedwait":                    libcCnd_timedwait,
		"cnd_wait":                         libcCnd_wait,
		"tss_create":                       libcTss_create,
		"tss_delete":                       libcTss_delete,
		"tss_set":                          libcTss_set,
		"tss_get":                          libcTss_get,
		"gettid":                           libcGettid,
		"sem_open":                         libcSem_open,
		"clock_getcpuclockid":              libcClock_getcpuclockid,
		"timer_create":                     libcTimer_create,
		"timer_delete":                     libcTimer_delete,
		"timer_settime":                    libcTimer_settime,
		"timer_gettime":                    libcTimer_gettime,
		"timer_getoverrun":                 libcTimer_getoverrun,
		"eventfd":                          libcEventfd,
		"eventfd_read":                     libcEventfd_read,
		"eventfd_write":                    libcEventfd_write,
		"timerfd_create":                   libcTimerfd_create,
		"timerfd_settime":                  libcTimerfd_settime,
		"timerfd_gettime":                  libcTimerfd_gettime,
		"inotify_init":                     libcInotify_init,
		"inotify_init1":                    libcInotify_init1,
		"inotify_add_watch":                libcInotify_add_watch,
		"inotify_rm_watch":                 libcInotify_rm_watch,
		"mkostemps":                        libcMkostemps,
		"setkey":                           libcSetkey,
		"encrypt":                          libcEncrypt,
		"strerror_l":                       libcStrerror_l,
		"strcasecmp_l":                     libcStrcasecmp_l,
		"strncasecmp_l":                    libcStrncasecmp_l,
		"lchmod":                           libcLchmod,
		"posix_close":                      libcPosix_close,
		"brk":                              libcBrk,
		"sbrk":                             libcSbrk,
		"issetugid":                        libcIssetugid,
		"wcsncat":                          libcWcsncat,
		"wcpcpy":                           libcWcpcpy,
		"wcpncpy":                          libcWcpncpy,
		"wcscasecmp":                       libcWcscasecmp,
		"wcsncasecmp":                      libcWcsncasecmp,
		"wcscasecmp_l":                     libcWcscasecmp_l,
		"wcsncasecmp_l":                    libcWcsncasecmp_l,
		"wcswcs":                           libcWcswcs,
		"wcsftime_l":                       libcWcsftime_l,
		"towctrans_l":                      libcTowctrans_l,
		"wctrans_l":                        libcWctrans_l,
		"timespec_get":                     libcTimespec_get,
	}
}
