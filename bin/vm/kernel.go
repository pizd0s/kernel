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

type Kernel struct {
	FunctionImports

	root    Dir
	user    Cred
	console *Console

	names   SystemNames
	mu      sync.Mutex
	procs   map[int32]*Process
	lastPid int32

	codes   map[[sha256.Size]byte]*CodeEntry
	options CompileOptions

	fifos map[FileKey]*Pipe
	locks LockTable

	fg        int32
	cttys     map[int32]Terminal
	first     *Process
	exit      chan int
	functions map[string]any
}

func newKernel(options CompileOptions, user Cred) *Kernel {
	k := &Kernel{procs: map[int32]*Process{}, codes: map[[sha256.Size]byte]*CodeEntry{}, options: options, fifos: map[FileKey]*Pipe{},
		exit: make(chan int, 1), root: newNoRoot(), user: user, cttys: map[int32]Terminal{}}

	k.locks.files = map[FileKey]*LockedFile{}
	k.names.host, k.names.domain = "pizdos", "(none)"
	k.functions = k.functionTable()

	syscall.Umask(0)
	k.routeSignals()

	return k
}

func (k *Kernel) function(imp *ImportMeta) any {
	switch imp.module {
	case "kernel":
		return k.functions[imp.name]
	case wasiModule:
		return wasiFunctions[imp.name]
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

	p := &Process{k: k, pid: k.lastPid, umask: 0o022, regexes: map[uint32]*PosixRegex{}, seed: 0,
		events: EventQueue{bell: make(chan struct{}, 1)}}

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

type CodeEntry struct {
	done    bool
	waiting []*Process
	code    *Code
	err     error
}

func (k *Kernel) codeOf(p *Process, data []byte, name string, obj *ElfObject) (*Code, error) {
	key := sha256.Sum256(data)

	k.mu.Lock()

	e, found := k.codes[key]

	if !found {
		e = &CodeEntry{}
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

	p.wait(time.Time{}, func(*Event) (bool, error) {
		k.mu.Lock()

		defer k.mu.Unlock()

		return e.done, nil
	})

	return e.code, e.err
}

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
