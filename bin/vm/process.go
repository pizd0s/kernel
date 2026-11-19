//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/bits"
	mathrand "math/rand/v2"
	"net"
	"net/netip"
	"os"
	"path"
	"regexp"
	"regexp/syntax"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

type Process struct {
	k    *Kernel
	pid  int32
	ppid int32
	pgid int32
	sid  int32
	// nice and policy are the scheduling of the process,
	// pdeathsig the signal of prctl it gets when its parent ends, all under
	// the kernel's lock.
	nice      int32
	policy    int32
	pdeathsig int
	// comm, undumpable and noNewPrivs are prctl's.
	comm       *string
	undumpable bool
	noNewPrivs bool
	inst       *Instance
	// image is what the instance runs: its code, arguments and environment.
	image *execImage
	files fdTable
	// root is the directory absolute paths start at and ".." stops at;
	// cwd is the working directory, which relative paths start at. Both
	// are directories the process holds, not names.
	root  Dir
	cwd   Dir
	umask uint32
	// errnoAt and environAt are where the crt keeps errno and environ, tzAt
	// tzname, timezone and daylight, herrnoAt h_errno; passwordAt is
	// getpass's buffer.
	errnoAt    uint32
	environAt  uint32
	tzAt       [3]uint32
	herrnoAt   uint32
	passwordAt uint32
	// heap is what the kernel knows of the heap, static the block of the
	// results libc keeps in static storage, env the environ array the kernel
	// allocated last.
	heap   *Heap
	static uint32
	env    uint32
	seed   uint64
	limits [rlimitCount]rlimit
	cred   cred
	// wasiRoot is the descriptor of a WASI command's preopened root, -1 for
	// none (wasi.go); chaos is the rules of the points of failure of
	// the process's tree, of a build for chaos only (chaos_on.go).
	wasiRoot  int
	chaos     *chaosRulesOf
	regexes   map[uint32]*posixRegex
	nextRegex uint32
	// constants are strings of the heap the kernel returns and the program
	// does not free: zone names for tm_zone, locale names and items;
	// siginfo is the siginfo_t of handlers.
	constants map[string]uint32
	siginfo   uint32
	// locale is the name of each category's locale, "" for C;
	// envStrings are the strings of environ setenv made, mappings the
	// lengths of mmap's blocks.
	locale     [lcAll]string
	zone       zoneCache
	envStrings map[uint32]bool
	mappings   map[uint32]mapping
	// timers are the interval timers, the process's own.
	timers [3]itimer
	// jumps are the setjmps of the image by their jmp_buf, stackAt where
	// the instance keeps __stack_pointer.
	jumps   map[uint32]*jump
	stackAt uint32
	sig     signalState
	// vfork is set while the process runs on its parent's frames, from vfork
	// until it execs or ends.
	vfork *vforkFrame
	// zombie tells an ended process its parent has not reaped, status its
	// wait status; ppid, zombie and status are the kernel's lock's.
	zombie bool
	status int
	// halted is set while the process is stopped; report is the wait status
	// of a stop or continue its parent has not seen, under the same lock.
	halted bool
	report int
	// childEvents counts the changes of the children, for a blocked wait.
	childEvents atomic.Uint64
	// token is the goroutine running the process's code, for Go to
	// interrupt it, chasing set while a goroutine repeats that.
	token   atomic.Uintptr
	chasing atomic.Bool
	// events is the queue the process sleeps on (event_queue.go):
	// whatever may change for a wait of its puts an event there; own are
	// its events of its own, spare its subscriptions spare, receives and
	// turns its spare waiters, for a packet and for a turn.
	events   eventQueue
	own      ownEvents
	spare    *subscription
	receives spareWaiters[received]
	turns    spareWaiters[turn]
	// unwound says the process's image ended, by its exit or an exec, and
	// no handler of its runs until its next image starts.
	unwound bool
	// asking is the request the process has at a server now, by the end
	// its reply comes on, nil for none; cancelled says a CANCEL went for
	// it, as the process took a signal (takeSignals).
	asking    *callEnd
	cancelled bool
	// kept are the connections to servers' open files the process keeps
	// for its next calls on them, the last used first (remote_node.go);
	// it keeps none once it ended, disconnected.
	kept         [keptConnections]keptConnection
	disconnected bool
	// interruption is what the signals the process took in its waits said
	// since its call began: none taken, some, or one whose handler did not
	// ask to restart calls. A wait of the call that a signal ends asks it
	// (interrupts): the call may have taken the signal in another of its
	// waits, for a server's answer.
	interruption uint64
}

// execImage is a program a process runs; panicked by exec, it unwinds the
// old image.
type execImage struct {
	code *Code
	argv []string
	envp []string
	// path is the guest's absolute path of the program's file, through its
	// links, or empty when the file is outside the root.
	path string
}

// procExit is the panic of a process that ends; status is a wait status.
type procExit struct {
	status int
}

// proc is the process of the instance ctx, as a call of the kernel's
// begins: every call takes its process so, once, and what the signals
// taken in the call before said (interruption) is not this call's.
func proc(ctx uintptr) *Process {
	p := instanceAt(ctx).proc
	p.interruption = signalNone

	return p
}

// chaosStart makes the process's rules of the environment its program
// starts with, unless it has some (chaos_on.go).
func (p *Process) chaosStart(env []string) {
	if p.chaos == nil {
		p.chaos = chaosRulesIn(env)
	}
}

// chaosFail is a visit of a point of the process's: whether its rules
// fail it.
func (p *Process) chaosFail(point string) bool {
	return p != nil && p.chaos.fail(point)
}

// firstCwd is the first process's working directory, held, and its path:
// its root, which is none.
func (p *Process) firstCwd() (Dir, string) {
	p.root.ref()

	return p.root, "/"
}

// run runs the process's programs, one after another as it execs, then
// ends it.
func (p *Process) run(img *execImage) {
	p.runFrom(img, nil, nil)
}

// runFrom is run with the first program already in i, entered by enter:
// in a child of fork.
func (p *Process) runFrom(img *execImage, i *Instance, enter func()) {
	status := 0

	if err := try(func() {
		for img != nil {
			p.image = img
			img, status = p.runImage(img, i, enter)
			i, enter = nil, nil

			if img != nil {
				p.execReset()
			}
		}
	}); err != nil {
		p.crash(err.message(), nil)
		status = 1 << 8
	}

	p.terminate(status)
}

func (p *Process) runImage(img *execImage, i *Instance, enter func()) (next *execImage, status int) {
	if i == nil {
		i = newInstance(img.code, p.k, runStack)
		i.attach(p)
		p.heap, p.static, p.env, p.errnoAt, p.environAt, p.siginfo = nil, 0, 0, 0, 0, 0
		p.tzAt, p.herrnoAt, p.passwordAt = [3]uint32{}, 0, 0
		p.regexes, p.constants, p.locale = map[uint32]*posixRegex{}, map[string]uint32{}, [lcAll]string{}
		p.envStrings, p.mappings = map[uint32]bool{}, map[uint32]mapping{}
		p.jumps, p.stackAt = map[uint32]*jump{}, 0
		enter = func() {
			p.chaosStart(img.envp)
			p.wasiRoot = -1

			if isWASI(img.code.obj) {
				p.wasiPreopen()
			}

			p.asanStart()
			i.start()
			p.asanInit()
			i.startCommand()
		}
	}

	p.setInstance(i)
	p.image, p.unwound = img, false
	funcs, ok := i.functionCode()

	if ok {
		p.token.Store(runtime.SetInterrupt(uintptr(funcs[0]), uintptr(funcs[1]), interruptStub()))
	}

	defer func() {
		runtime.SetInterrupt(0, 0, 0)
		p.token.Store(0)
		p.unwound = true
		e := recover()
		who := i.host.(*Process)

		// A trap's report is the trapping process's, a borrower's of vfork
		// too, on its standard error before the borrowers end and their
		// descriptors with them.
		if t, ok := e.(*Trap); ok {
			msg := t.Error()

			if t.code == trapAsan {
				msg = who.asanReport(t)
			}

			who.crash(msg, t.stack)
		}

		p.endBorrowers(i, e)

		switch e := e.(type) {
		case nil:
		case *procExit:
			status = e.status
		case *execImage:
			next = e
		case *Trap:
			status = trapSignal(e.code)
		default:
			panic(e)
		}

		runtime.KeepAlive(i)
		p.unmapAll()
		p.setInstance(nil)
		i.release()
	}()

	// A signal can arrive, and a timer fire, before this image has an
	// interrupt token.
	p.raiseInterrupt()

	if p.interruptDue() {
		p.chase()
	}

	enter()

	return nil, 0
}

// trapSignal is the signal a trap ends a process with.
func trapSignal(code uint32) int {
	switch code {
	case trapMemoryOOB, trapTableOOB, trapNullRef, trapUndefinedElem, trapUninitElem, trapAsan:
		return int(syscall.SIGSEGV)
	case trapIntOverflow, trapDivByZero, trapConversion:
		return int(syscall.SIGFPE)
	}

	return int(syscall.SIGILL)
}

// terminate ends the process with a wait status: its descriptors close, its
// children go to the first process, it becomes a zombie its parent reaps,
// or nothing when the parent ignores SIGCHLD, and the groups it joined to
// its session may be orphaned (orphans); with the first process vm ends.
func (p *Process) terminate(status int) {
	p.unwound = true
	p.stopTimers()
	p.leaveSession(true)

	for fd := range p.files.size() {
		p.closeFd(fd)
	}

	p.disconnect()
	p.root.unref()
	p.cwd.unref()
	p.sig.mu.Lock()
	p.sig.dead = true
	p.sig.mu.Unlock()
	k := p.k
	k.mu.Lock()
	parent := k.procs[p.ppid]
	adopted := false

	var bereaved []*Process
	var joined []int32

	if parent != nil && parent.pgid != p.pgid && parent.sid == p.sid {
		joined = append(joined, p.pgid)
	}

	for _, q := range k.procs {
		if q.ppid == p.pid {
			q.ppid = k.first.pid
			adopted = adopted || q.zombie

			if q.pdeathsig != 0 && !q.zombie {
				bereaved = append(bereaved, q)
			}

			if !q.zombie && q.pgid != p.pgid && q.sid == p.sid {
				joined = append(joined, q.pgid)
			}
		}
	}

	notify := parent != nil && !parent.discardsChildren()

	if notify {
		p.zombie, p.status = true, status
	} else {
		delete(k.procs, p.pid)
	}

	k.mu.Unlock()
	k.orphans(joined)

	if adopted && p != k.first {
		k.first.childChanged(int(syscall.SIGCHLD))
	}

	for _, q := range bereaved {
		q.send(q.pdeathsig)
	}

	if notify {
		parent.childChanged(int(syscall.SIGCHLD))
	}

	if p == k.first {
		k.exit <- hostCode(status)
	}
}

// discardsChildren tells that the process's ended children are not kept for
// it: it ignores SIGCHLD, or asked so with SA_NOCLDWAIT.
func (p *Process) discardsChildren() bool {
	p.sig.mu.Lock()
	defer p.sig.mu.Unlock()

	act := p.sig.actions[syscall.SIGCHLD]

	return act.handler == sigIgn || act.flags&saNocldwait != 0
}

// childChanged tells the process that a child changed, with a signal or
// none: its event of a child.
func (p *Process) childChanged(sig int) {
	p.childEvents.Add(1)

	if sig != 0 {
		p.send(sig)
	}

	p.post(&p.own.child)
}

// hostCode is vm's exit code for a wait status.
func hostCode(status int) int {
	if status&0x7f != 0 {
		return 128 + status&0x7f
	}

	return status >> 8 & 0xff
}

// exitWith ends the process from inside one of its calls. A child of vfork
// gives its parent's frames back.
func (p *Process) exitWith(status int) {
	if v := p.vfork; v != nil {
		p.vfork = nil
		p.terminate(status)
		v.resume(p.pid)
	}

	panic(&procExit{status})
}

func (p *Process) libc__kernel_start(errnoAt, environAt, argcAt, argvAt, tznameAt, timezoneAt, daylightAt, herrnoAt uint32) {
	p.errnoAt, p.environAt = errnoAt, environAt
	p.tzAt, p.herrnoAt = [3]uint32{tznameAt, timezoneAt, daylightAt}, herrnoAt
	argv := p.stringArray(p.image.argv)
	p.env = p.stringArray(p.image.envp)

	if argv == 0 || p.env == 0 {
		p.exitWith(int(syscall.SIGABRT))
	}

	p.put32(argcAt, uint32(len(p.image.argv)))
	p.put32(argvAt, argv)
	p.put32(environAt, p.env)
}

// libc__kernel_program writes the guest's path of the program to buf, as
// getcwd writes the working directory's: what lib/c's dladdr names.
func (p *Process) libc__kernel_program(buf, size uint32) uint32 {
	path := p.image.path

	if path == "" {
		return p.pointer(0, syscall.ENOENT)
	}

	if uint32(len(path)) >= size {
		return p.pointer(0, syscall.ERANGE)
	}

	p.putString(buf, path)

	return p.pointer(buf, nil)
}

// stringArray copies strings to the heap as a null-terminated array of
// pointers, or returns 0 when the heap is exhausted.
func (p *Process) stringArray(list []string) uint32 {
	arr := p.malloc(uint32(4 * (len(list) + 1)))

	if arr == 0 {
		return 0
	}

	for i, s := range list {
		at := p.newString(s)

		if at == 0 {
			return 0
		}

		p.put32(arr+uint32(4*i), at)
	}

	p.put32(arr+uint32(4*len(list)), 0)

	return arr
}

// newString copies a string to the heap, or returns 0.
func (p *Process) newString(s string) uint32 {
	at := p.malloc(uint32(len(s) + 1))

	if at != 0 {
		b := p.span(at, uint32(len(s)+1))
		copy(b, s)
		b[len(s)] = 0
	}

	return at
}

// memory

func (p *Process) mem() []byte {
	return p.inst.mem
}

func oob() {
	panic(newTrap(trapMemoryOOB, 1))
}

// span is n bytes of memory at at, which the shadow map of an asan object
// must allow.
func (p *Process) span(at, n uint32) []byte {
	m := p.mem()

	if uint64(at)+uint64(n) > uint64(len(m)) {
		oob()
	}

	if p.inst.memory.shadow != nil {
		p.asanCheck(at, n)
	}

	return m[at : at+n]
}

func (p *Process) u8(at uint32) byte {
	return p.span(at, 1)[0]
}

func (p *Process) u16(at uint32) uint16 {
	return binary.LittleEndian.Uint16(p.span(at, 2))
}

func (p *Process) u32(at uint32) uint32 {
	return binary.LittleEndian.Uint32(p.span(at, 4))
}

func (p *Process) u64(at uint32) uint64 {
	return binary.LittleEndian.Uint64(p.span(at, 8))
}

func (p *Process) put16(at uint32, v uint16) {
	binary.LittleEndian.PutUint16(p.span(at, 2), v)
}

func (p *Process) put32(at, v uint32) {
	binary.LittleEndian.PutUint32(p.span(at, 4), v)
}

func (p *Process) put64(at uint32, v uint64) {
	binary.LittleEndian.PutUint64(p.span(at, 8), v)
}

// cstr is the string at at, without its terminator.
func (p *Process) cstr(at uint32) []byte {
	m := p.mem()

	if uint64(at) >= uint64(len(m)) {
		oob()
	}

	n := bytes.IndexByte(m[at:], 0)

	if n < 0 {
		oob()
	}

	if p.inst.memory.shadow != nil {
		p.asanCheck(at, uint32(n)+1)
	}

	return m[at : at+uint32(n)]
}

func (p *Process) str(at uint32) string {
	return string(p.cstr(at))
}

// strings reads a null-terminated array of string pointers.
func (p *Process) strings(at uint32) []string {
	var out []string

	for ; ; at += 4 {
		s := p.u32(at)

		if s == 0 {
			return out
		}

		out = append(out, p.str(s))
	}
}

// putString copies s and a terminator to at, which has room for them.
func (p *Process) putString(at uint32, s string) {
	b := p.span(at, uint32(len(s)+1))
	copy(b, s)
	b[len(s)] = 0
}

// constant is a string of the heap for results the program reads but
// does not free; the process keeps one copy of each.
func (p *Process) constant(s string) uint32 {
	if at, ok := p.constants[s]; ok {
		return at
	}

	at := p.newString(s)

	if at == 0 {
		p.exitWith(int(syscall.SIGABRT))
	}

	p.constants[s] = at

	return at
}

// errors

// errnoOf unwraps a kernel error: syscall.Errno, an os/net wrapper of one,
// or code's diagnostic wrapping ENOEXEC. Kernel helpers do not return other
// errors; directory enumeration uses Readdirnames(-1), which has no EOF error.
func errnoOf(err error) syscall.Errno {
	var e syscall.Errno
	errors.As(err, &e)

	return e
}

func (p *Process) setErrno(e syscall.Errno) {
	p.put32(p.errnoAt, uint32(e))
}

func (p *Process) errno() syscall.Errno {
	return syscall.Errno(p.u32(p.errnoAt))
}

// result returns r, or -1 with errno set when err is not nil: a system
// call returns through it. The process's signals are none of its
// business: the interrupt word takes them on the way out of the import.
func (p *Process) result(r int, err error) uint32 {
	if err != nil {
		p.setErrno(errnoOf(err))
		r = -1
	}

	return uint32(r)
}

// result64 is result for a 64-bit return value.
func (p *Process) result64(r int64, err error) uint64 {
	if err != nil {
		p.setErrno(errnoOf(err))
		r = -1
	}

	return uint64(r)
}

// pointer returns a pointer result: 0 with errno set when err is not nil.
func (p *Process) pointer(at uint32, err error) uint32 {
	if err != nil {
		p.setErrno(errnoOf(err))
		at = 0
	}

	return at
}

// staticArea is the guest block of libc's static results, made at first
// use: offsets staticDot.. below.
const (
	staticDot        = 0
	staticSlash      = 2
	staticEmpty      = 4
	staticTm         = 8
	staticCtime      = 56
	staticStrerror   = 88
	staticStrsignal  = 184
	staticLconv      = 256
	staticStrtok     = 312
	staticPasswd     = 316
	staticGroup      = 320
	staticPwent      = 324
	staticGrent      = 328
	staticShell      = 332
	staticLogIdent   = 336
	staticLogOption  = 368
	staticLogMask    = 372
	staticShellIndex = 376
	staticPass       = 380
	staticRandom     = 384
	staticRandomInit = 388
	staticHostent    = 516
	staticHostIndex  = 520
	staticServent    = 524
	staticProtoent   = 528
	staticNtoa       = 532
	staticCrypt      = 548
	staticSize       = 560
)

func (p *Process) staticAt(off uint32) uint32 {
	if p.static == 0 {
		p.static = p.malloc(staticSize)

		if p.static == 0 {
			p.exitWith(int(syscall.SIGABRT))
		}

		clear(p.span(p.static, staticSize))
		p.putString(p.static+staticDot, ".")
		p.putString(p.static+staticSlash, "/")
	}

	return p.static + off
}

// Processes: identity, limits, the environment and exec. exec runs a
// module, which the kernel compiles and links once per content of its
// file, compressed or not, or a script through its #! interpreter.

const (
	rlimitCount  = 16
	rlimitNofile = 7
	rlimitNproc  = 6
)

type rlimit struct {
	cur, max uint64
}

func hostLimits() [rlimitCount]rlimit {
	var out [rlimitCount]rlimit

	for i := range out {
		var l syscall.Rlimit

		if syscall.Getrlimit(i, &l) == nil {
			out[i] = rlimit{l.Cur, l.Max}
		} else {
			out[i] = rlimit{^uint64(0), ^uint64(0)}
		}
	}

	return out
}

// exec replaces the process's program; it returns only an error.
func (p *Process) exec(path string, argv, envp []string) error {
	return p.execFrom(path, nil, argv, envp)
}

// execFrom is exec of a file, when one is given: fexecve's.
func (p *Process) execFrom(path string, f Object, argv, envp []string) error {
	img, err := p.loadFrom(path, f, argv)

	if err != nil {
		return err
	}

	img.envp = envp
	p.execImage(img)

	return syscall.ELOOP
}

// loadProgram is exec's search: a module, or the interpreter of a #!
// script, with argv rewritten for it; the image has no environment yet.
func (p *Process) loadProgram(path string, argv []string) (*execImage, error) {
	return p.loadFrom(path, nil, argv)
}

// loadFrom is loadProgram of a file, when one is given: fexecve's. A
// program is a regular file the process may execute.
func (p *Process) loadFrom(path string, first Object, argv []string) (*execImage, error) {
	for range 4 {
		f, program := first, ""

		if first != nil {
			first.ref()
			first = nil
		} else {
			var err error

			if f, program, err = p.openProgram(path); err != nil {
				return nil, err
			}
		}

		var st syscall.Stat_t
		t := f.asTransfer()

		if err := statFile(p, f, &st); err != nil {
			f.unref()

			return nil, err
		}

		if t == nil || st.Mode&syscall.S_IFMT != syscall.S_IFREG {
			f.unref()

			return nil, syscall.EACCES
		}

		// Execute permission: a file of a server shows its mode, which
		// says it for the process's class.
		if !p.permits(&st, 1) {
			f.unref()

			return nil, syscall.EACCES
		}

		head := make([]byte, 256)
		n, _ := t.pread(p, head, 0)
		head = head[:max(n, 0)]

		if bytes.HasPrefix(head, []byte("#!")) {
			f.unref()
			line, _, _ := bytes.Cut(head[2:], []byte("\n"))
			fields := strings.Fields(string(line))

			if len(fields) == 0 {
				return nil, syscall.ENOEXEC
			}

			script := []string{fields[0]}

			if len(fields) > 1 {
				script = append(script, strings.Join(fields[1:], " "))
			}

			if len(argv) > 0 {
				argv = argv[1:]
			}

			argv = append(append(script, path), argv...)
			path = fields[0]

			continue
		}

		code, err := p.k.code(p, f, path)
		f.unref()

		if err != nil {
			return nil, err
		}

		return &execImage{code: code, argv: argv, path: program}, nil
	}

	return nil, syscall.ELOOP
}

// openProgram opens the file a path names for exec, a last link followed,
// and says its path from the root, as Linux's /proc/self/exe does.
func (p *Process) openProgram(path string) (Object, string, error) {
	var f Object
	var program string
	err := p.onPath(atFdcwd, path, func(d Dir) error {
		var err error

		if f, err = d.openPath(p, path, syscall.O_RDONLY, 0); err != nil {
			return err
		}

		if real, err := d.realpathOf(p, path); err == nil {
			program = real
		}

		return nil
	})

	return f, program, err
}

// execPath is exec with a search of PATH when file has no slash.
func (p *Process) execPath(file string, argv, envp []string) error {
	if file == "" {
		return syscall.ENOENT
	}

	if strings.Contains(file, "/") {
		return p.exec(file, argv, envp)
	}

	path, ok := p.getenv("PATH")

	if !ok {
		path = "/usr/local/bin:/bin:/usr/bin"
	}

	err := error(syscall.ENOENT)

	for _, dir := range strings.Split(path, ":") {
		if dir == "" {
			dir = "."
		}

		switch e := p.exec(dir+"/"+file, argv, envp); e {
		case syscall.ENOENT, syscall.ENOTDIR:
		case syscall.EACCES:
			err = e
		default:
			return e
		}
	}

	return err
}

// execReset is what a new program starts with: descriptors marked
// close-on-exec are closed, handled signals are back to their defaults and
// there is no alternate signal stack.
func (p *Process) execReset() {
	for fd, d := range p.files.slots {
		if d.obj != nil && d.cloexec {
			p.closeFd(fd)
		}
	}

	p.sig.mu.Lock()

	for i, a := range p.sig.actions {
		if a.handler != sigDfl && a.handler != sigIgn {
			p.sig.actions[i] = sigAction{}
		}
	}

	p.sig.altstack.sp, p.sig.altstack.size = 0, 0
	p.sig.mu.Unlock()
	p.comm = nil
}

// environment is the process's environ as it is now.
func (p *Process) environment() []string {
	if p.environAt == 0 {
		return nil
	}

	env := p.u32(p.environAt)

	if env == 0 {
		return nil
	}

	return p.strings(env)
}

// lookupEnv finds a variable: the address of its entry in environ and of
// its value.
func (p *Process) lookupEnv(name []byte) (uint32, uint32, bool) {
	if p.environAt == 0 {
		return 0, 0, false
	}

	env := p.u32(p.environAt)

	if env == 0 {
		return 0, 0, false
	}

	for at := env; ; at += 4 {
		s := p.u32(at)

		if s == 0 {
			return 0, 0, false
		}

		entry := p.cstr(s)

		if len(entry) > len(name) && entry[len(name)] == '=' && bytes.Equal(entry[:len(name)], name) {
			return at, s + uint32(len(name)) + 1, true
		}
	}
}

func (p *Process) getenv(name string) (string, bool) {
	_, value, ok := p.lookupEnv([]byte(name))

	if !ok {
		return "", false
	}

	return p.str(value), true
}

// setEnviron makes a new environ array of pointers.
func (p *Process) setEnviron(entries []uint32) bool {
	arr := p.malloc(uint32(4 * (len(entries) + 1)))

	if arr == 0 {
		return false
	}

	for i, s := range entries {
		p.put32(arr+uint32(4*i), s)
	}

	p.put32(arr+uint32(4*len(entries)), 0)
	old := p.u32(p.environAt)
	p.put32(p.environAt, arr)

	if old != 0 && old == p.env {
		p.free(old)
	}

	p.env = arr

	return true
}

// setVariable replaces the variables of environ named name by entry, or
// removes them for 0. A string setenv made is freed when it goes.
func (p *Process) setVariable(name string, entry uint32) bool {
	var entries, gone []uint32

	if env := p.u32(p.environAt); env != 0 {
		for at := env; p.u32(at) != 0; at += 4 {
			s := p.u32(at)

			if e := p.cstr(s); len(e) > len(name) && e[len(name)] == '=' && string(e[:len(name)]) == name {
				gone = append(gone, s)
			} else {
				entries = append(entries, s)
			}
		}
	}

	if entry != 0 {
		entries = append(entries, entry)
	}

	if !p.setEnviron(entries) {
		return false
	}

	for _, s := range gone {
		if s != entry && p.envStrings[s] {
			delete(p.envStrings, s)
			p.free(s)
		}
	}

	return true
}

// variableName is the name setenv and unsetenv take: not empty, no '='.
func (p *Process) variableName(at uint32) (string, bool) {
	name := p.str(at)

	if name == "" || strings.IndexByte(name, '=') >= 0 {
		p.setErrno(syscall.EINVAL)

		return "", false
	}

	return name, true
}

func (p *Process) libcGetpid() uint32 {
	return uint32(p.pid)
}

func (p *Process) libcGetppid() uint32 {
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	return uint32(p.ppid)
}

func (p *Process) libcGetpgrp() uint32 {
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	return uint32(p.pgid)
}

func (p *Process) libcGetuid() uint32 {
	return p.cred.uid()
}

func (p *Process) libcGeteuid() uint32 {
	return p.cred.euid()
}

func (p *Process) libcGetgid() uint32 {
	return p.cred.gid()
}

func (p *Process) libcGetegid() uint32 {
	return p.cred.egid()
}

func (p *Process) libcGetgroups(n, list uint32) uint32 {
	groups := p.cred.groups

	if n == 0 {
		return p.result(len(groups), nil)
	}

	if int(int32(n)) < len(groups) {
		return p.result(0, syscall.EINVAL)
	}

	for i, g := range groups {
		p.put32(list+uint32(4*i), g)
	}

	return p.result(len(groups), nil)
}

// sysconf's -1 for timezone names and the buffers of the user and group
// databases means no limit. Open streams share the descriptor limit.
func (p *Process) libcSysconf(name uint32) uint32 {
	switch name {
	case 85:
		return uint32(pages(false))
	case 86:
		return uint32(pages(true))
	}

	values := map[uint32]int64{
		0: 131072, 1: int64(min64(p.limits[rlimitNproc].cur, 1<<30)), 2: 100, 3: 65536,
		4: int64(min64(p.limits[rlimitNofile].cur, 1<<30)), 5: int64(min64(p.limits[rlimitNofile].cur, 1<<30)), 6: -1,
		7: 1, 8: 1, 29: 200809, 30: 4096, 43: 2048, 60: 1024,
		69: -1, 70: -1, 71: 256, 72: 32, 83: int64(runtime.NumCPU()), 84: int64(runtime.NumCPU()), 149: 200809, 173: 40, 180: 64,
	}

	if v, ok := values[name]; ok {
		return uint32(v)
	}

	return p.result(0, syscall.EINVAL)
}

func (p *Process) libcUmask(mask uint32) uint32 {
	old := p.umask
	p.umask = mask & 0o777

	return old
}

func (p *Process) libcRand() uint32 {
	p.seed = 6364136223846793005*p.seed + 1

	return uint32(p.seed >> 33)
}

func (p *Process) libcSrand(seed uint32) {
	p.seed = uint64(seed) - 1
}

func (p *Process) libcGetenv(name uint32) uint32 {
	_, value, _ := p.lookupEnv(p.cstr(name))

	return value
}

// putenv without a '=' removes the variable, as musl's.
func (p *Process) libcPutenv(s uint32) uint32 {
	name, _, set := strings.Cut(p.str(s), "=")

	if name == "" {
		p.setErrno(syscall.EINVAL)

		return math.MaxUint32
	}

	if !set {
		s = 0
	}

	if !p.setVariable(name, s) {
		p.setErrno(syscall.ENOMEM)

		return math.MaxUint32
	}

	return 0
}

func (p *Process) libcSetenv(at, value, overwrite uint32) uint32 {
	name, ok := p.variableName(at)

	if !ok {
		return math.MaxUint32
	}

	if _, _, set := p.lookupEnv([]byte(name)); set && overwrite == 0 {
		return 0
	}

	s := p.newString(name + "=" + p.str(value))

	if s == 0 || !p.setVariable(name, s) {
		p.free(s)
		p.setErrno(syscall.ENOMEM)

		return math.MaxUint32
	}

	p.envStrings[s] = true

	return 0
}

func (p *Process) libcUnsetenv(at uint32) uint32 {
	name, ok := p.variableName(at)

	if !ok {
		return math.MaxUint32
	}

	if !p.setVariable(name, 0) {
		p.setErrno(syscall.ENOMEM)

		return math.MaxUint32
	}

	return 0
}

func (p *Process) libcClearenv() uint32 {
	old := p.u32(p.environAt)
	p.put32(p.environAt, 0)

	if old != 0 && old == p.env {
		p.free(old)
	}

	p.env = 0

	return 0
}

func (p *Process) libcExecve(path, argv, envp uint32) uint32 {
	return p.result(0, p.exec(p.str(path), p.strings(argv), p.strings(envp)))
}

func (p *Process) libcExecvp(file, argv uint32) uint32 {
	return p.result(0, p.execPath(p.str(file), p.strings(argv), p.environment()))
}

func (p *Process) libcExecv(path, argv uint32) uint32 {
	return p.result(0, p.exec(p.str(path), p.strings(argv), p.environment()))
}

func (p *Process) libcExecvpe(file, argv, envp uint32) uint32 {
	return p.result(0, p.execPath(p.str(file), p.strings(argv), p.strings(envp)))
}

// execle's environment follows the null pointer that ends the
// arguments.
func (p *Process) libcExecle(path, arg0, va uint32) uint32 {
	argv := p.argList(arg0, va)
	envp := p.varArg(va, uint32(max(len(argv), 1)))

	if arg0 == 0 {
		envp = p.varArg(va, 0)
	}

	return p.result(0, p.exec(p.str(path), argv, p.strings(envp)))
}

// fexecve runs the file of a descriptor; a script's interpreter gets
// its path.
func (p *Process) libcFexecve(fd, argv, envp uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	// A memfd or a server's file has no path but its descriptor's.
	path := fmt.Sprintf("/dev/fd/%d", fd)

	return p.result(0, p.execFrom(path, obj, p.strings(argv), p.strings(envp)))
}

func (p *Process) libcExecl(path, arg0, va uint32) uint32 {
	return p.result(0, p.exec(p.str(path), p.argList(arg0, va), p.environment()))
}

func (p *Process) libcExeclp(file, arg0, va uint32) uint32 {
	return p.result(0, p.execPath(p.str(file), p.argList(arg0, va), p.environment()))
}

func (p *Process) libc_exit(code uint32) {
	p.exitWith(int(code&0xff) << 8)
}

func (p *Process) libc_Exit(code uint32) {
	p.exitWith(int(code&0xff) << 8)
}

// system runs a command in the shell of the root and waits for it:
// a child of the process with its descriptors, its wait status
// shifted as the shell's system reports it. The shell missing is
// 127, the way a child that cannot run ends.
func (p *Process) libcSystem(cmd uint32) uint32 {
	if cmd == 0 {
		_, err := p.loadProgram("/bin/sh", []string{"sh", "-c", "exit 0"})

		return bit(err == nil)
	}

	argv := []string{"sh", "-c", string(p.cstr(cmd))}
	img, err := p.loadProgram("/bin/sh", argv)

	if err != nil {
		return uint32(int32(127 << 8))
	}

	img.envp = p.environment()

	c, err := p.k.newProcess(p)

	if err != nil {
		return p.result(0, err)
	}

	c.inherit(p)
	c.execReset()
	var ended atomic.Bool

	// The child's end is an event of p's, which waits for it as POSIX has
	// system: SIGINT and SIGQUIT ignored and SIGCHLD blocked while the
	// command runs, its other signals taken.
	go func() {
		c.run(img)
		ended.Store(true)
		p.post(&p.own.child)
	}()

	intr := p.setAction(int(syscall.SIGINT), sigAction{handler: sigIgn})
	quit := p.setAction(int(syscall.SIGQUIT), sigAction{handler: sigIgn})
	mask := p.setMask(p.mask() | sigBit(int(syscall.SIGCHLD)))
	p.wait(time.Time{}, func(*event) (bool, error) {
		return ended.Load(), nil
	})
	p.setAction(int(syscall.SIGINT), intr)
	p.setAction(int(syscall.SIGQUIT), quit)
	p.setMask(mask)

	status := c.status
	// its parent never waits for it: the child is reaped here
	p.k.mu.Lock()
	delete(p.k.procs, c.pid)
	p.k.mu.Unlock()

	return uint32(int32(status))
}

// argList reads the arguments of execl: the first, then the variadic ones
// up to a null pointer.
func (p *Process) argList(arg0, va uint32) []string {
	if arg0 == 0 {
		return nil
	}

	args := []string{p.str(arg0)}

	for i := uint32(0); ; i++ {
		s := p.varArg(va, i)

		if s == 0 {
			return args
		}

		args = append(args, p.str(s))
	}
}

func (p *Process) groupExists(pgid int32) bool {
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	for _, q := range p.k.procs {
		if q.pgid == pgid && q.sid == p.sid {
			return true
		}
	}

	return false
}

func (p *Process) libcSetpgid(pid, pgid uint32) uint32 {
	target := p

	if pid != 0 && int32(pid) != p.pid {
		p.k.mu.Lock()
		q, ok := p.k.procs[int32(pid)]
		ok = ok && q.ppid == p.pid
		p.k.mu.Unlock()

		if !ok {
			return p.result(0, syscall.ESRCH)
		}

		target = q
	}

	group := int32(pgid)

	if group == 0 {
		group = target.pid
	}

	if group < 0 || group != target.pid && !p.groupExists(group) {
		return p.result(0, syscall.EPERM)
	}

	p.k.mu.Lock()
	target.pgid = group
	p.k.mu.Unlock()

	return p.result(0, nil)
}

// consoleAt is nil for a descriptor of the console's, else EBADF or
// ENOTTY.
func (p *Process) consoleAt(fd uint32) error {
	ok, err := p.isConsole(fd)

	if err == nil && !ok {
		err = syscall.ENOTTY
	}

	return err
}

// Who a process is: its credentials, session and group, priority and
// scheduling. The credentials are the kernel's own (cred), none of the
// host's. A priority is a nice value, a policy SCHED_OTHER, SCHED_BATCH or
// SCHED_IDLE: the kernel keeps them, Go's scheduler does not know them,
// and the real time policies need privilege.

const (
	prioProcess = 0
	prioPgrp    = 1
	prioUser    = 2
	niceMin     = -20
	niceMax     = 19
	schedOther  = 0
	schedFifo   = 1
	schedRr     = 2
	schedBatch  = 3
	schedIdle   = 5
)

// setIds is setresuid's and setresgid's change of a process's ids, with
// the kernel's lock: r, e and s, each kept by -1.
func (p *Process) setIds(ids *[3]uint32, r, e, s uint32) error {
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	if !p.cred.privileged() && !(may(r, ids[0], ids[1], ids[2]) && may(e, ids[0], ids[1], ids[2]) && may(s, ids[0], ids[1], ids[2])) {
		return syscall.EPERM
	}

	for i, id := range [3]uint32{r, e, s} {
		if id != keep {
			ids[i] = id
		}
	}

	return nil
}

// setId is setuid's and setgid's: a privileged process's sets all three, an
// unprivileged one's the effective, to the real or the saved id.
func (p *Process) setId(ids *[3]uint32, id uint32) error {
	if p.cred.privileged() {
		return p.setIds(ids, id, id, id)
	}

	if id != ids[0] && id != ids[2] {
		return syscall.EPERM
	}

	return p.setIds(ids, keep, id, keep)
}

// setReIds is setreuid's and setregid's: the real to the real or the
// effective, the effective to any of the three; a change of the real, or of
// the effective to other than the real, makes the saved the new effective.
func (p *Process) setReIds(ids *[3]uint32, r, e uint32) error {
	if !p.cred.privileged() && (r != keep && r != ids[0] && r != ids[1] || !may(e, ids[0], ids[1], ids[2])) {
		return syscall.EPERM
	}

	saved := keep

	if r != keep || e != keep && e != ids[0] {
		saved = e

		if e == keep {
			saved = ids[1]
		}
	}

	return p.setIds(ids, r, e, saved)
}

// process is the process of a pid, the caller for 0.
func (p *Process) process(pid int32) (*Process, error) {
	if pid == 0 || pid == p.pid {
		return p, nil
	}

	p.k.mu.Lock()
	q, ok := p.k.procs[pid]
	p.k.mu.Unlock()

	if !ok || q.zombie {
		return nil, syscall.ESRCH
	}

	return q, nil
}

// prioTargets are the processes getpriority and setpriority name.
func (p *Process) prioTargets(which, who uint32) ([]*Process, error) {
	switch which {
	case prioProcess:
		q, err := p.process(int32(who))

		if err != nil {
			return nil, err
		}

		return []*Process{q}, nil
	case prioPgrp:
		pgid := int32(who)

		if pgid == 0 {
			pgid = p.pgid
		}

		return p.k.processes(func(q *Process) bool { return q.pgid == pgid && !q.zombie }), nil
	case prioUser:
		if who == 0 {
			who = p.cred.uid()
		}

		return p.k.processes(func(q *Process) bool { return !q.zombie && q.cred.uid() == who }), nil
	}

	return nil, syscall.EINVAL
}

func (p *Process) libcSetuid(uid uint32) uint32 {
	return p.result(0, p.setId(&p.cred.uids, uid))
}

func (p *Process) libcSeteuid(uid uint32) uint32 {
	return p.result(0, p.setIds(&p.cred.uids, keep, uid, keep))
}

func (p *Process) libcSetreuid(r, e uint32) uint32 {
	return p.result(0, p.setReIds(&p.cred.uids, r, e))
}

func (p *Process) libcSetresuid(r, e, s uint32) uint32 {
	return p.result(0, p.setIds(&p.cred.uids, r, e, s))
}

func (p *Process) libcSetgid(gid uint32) uint32 {
	return p.result(0, p.setId(&p.cred.gids, gid))
}

func (p *Process) libcSetegid(gid uint32) uint32 {
	return p.result(0, p.setIds(&p.cred.gids, keep, gid, keep))
}

func (p *Process) libcSetregid(r, e uint32) uint32 {
	return p.result(0, p.setReIds(&p.cred.gids, r, e))
}

func (p *Process) libcSetresgid(r, e, s uint32) uint32 {
	return p.result(0, p.setIds(&p.cred.gids, r, e, s))
}

func (p *Process) libcGetresuid(r, e, s uint32) uint32 {
	for i, at := range []uint32{r, e, s} {
		p.put32(at, p.cred.uids[i])
	}

	return p.result(0, nil)
}

func (p *Process) libcGetresgid(r, e, s uint32) uint32 {
	for i, at := range []uint32{r, e, s} {
		p.put32(at, p.cred.gids[i])
	}

	return p.result(0, nil)
}

func (p *Process) libcSetsid() uint32 {
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	for _, q := range p.k.procs {
		if q.pgid == p.pid {
			return p.result(0, syscall.EPERM)
		}
	}

	p.sid, p.pgid = p.pid, p.pid

	return p.result(int(p.pid), nil)
}

func (p *Process) libcGetsid(pid uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	return p.result(int(q.sid), nil)
}

func (p *Process) libcGetpgid(pid uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	return p.result(int(q.pgid), nil)
}

func (p *Process) libcSetpgrp() uint32 {
	return p.libcSetpgid(0, 0)
}

// nice returns the new nice value; lowering it needs privilege.
func (p *Process) libcNice(inc uint32) uint32 {
	p.k.mu.Lock()
	n := min(max(p.nice+int32(inc), niceMin), niceMax)

	if n < p.nice && !p.cred.privileged() {
		p.k.mu.Unlock()

		return p.result(0, syscall.EPERM)
	}

	p.nice = n
	p.k.mu.Unlock()

	return p.result(int(n), nil)
}

// getpriority returns the lowest nice value of the processes named.
func (p *Process) libcGetpriority(which, who uint32) uint32 {
	targets, err := p.prioTargets(which, who)

	if err == nil && len(targets) == 0 {
		err = syscall.ESRCH
	}

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()
	n := int32(niceMax)

	for _, q := range targets {
		n = min(n, q.nice)
	}

	p.k.mu.Unlock()

	return p.result(int(n), nil)
}

func (p *Process) libcSetpriority(which, who, prio uint32) uint32 {
	targets, err := p.prioTargets(which, who)

	if err == nil && len(targets) == 0 {
		err = syscall.ESRCH
	}

	if err != nil {
		return p.result(0, err)
	}

	n := min(max(int32(prio), niceMin), niceMax)
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	for _, q := range targets {
		if n < q.nice && !p.cred.privileged() {
			return p.result(0, syscall.EACCES)
		}
	}

	for _, q := range targets {
		q.nice = n
	}

	return p.result(0, nil)
}

func (p *Process) libcSched_yield() uint32 {
	runtime.Gosched()

	return 0
}

func (p *Process) libcSched_get_priority_max(policy uint32) uint32 {
	switch policy {
	case schedFifo, schedRr:
		return p.result(99, nil)
	case schedOther, schedBatch, schedIdle:
		return p.result(0, nil)
	}

	return p.result(0, syscall.EINVAL)
}

func (p *Process) libcSched_get_priority_min(policy uint32) uint32 {
	switch policy {
	case schedFifo, schedRr:
		return p.result(1, nil)
	case schedOther, schedBatch, schedIdle:
		return p.result(0, nil)
	}

	return p.result(0, syscall.EINVAL)
}

func (p *Process) libcSched_getscheduler(pid uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	return p.result(int(q.policy), nil)
}

func (p *Process) libcSched_getparam(pid, param uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return p.result(0, err)
	}

	p.put32(param, 0)

	return p.result(0, nil)
}

// The priority of the policies the kernel keeps is 0.
func (p *Process) libcSched_setparam(pid, param uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return p.result(0, err)
	}

	if p.u32(param) != 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(0, nil)
}

func (p *Process) libcSched_setscheduler(pid, policy, param uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	switch policy {
	case schedOther, schedBatch, schedIdle:
		if p.u32(param) != 0 {
			return p.result(0, syscall.EINVAL)
		}

		p.k.mu.Lock()
		q.policy = int32(policy)
		p.k.mu.Unlock()

		return p.result(0, nil)
	case schedFifo, schedRr:
		return p.result(0, syscall.EPERM)
	}

	return p.result(0, syscall.EINVAL)
}

// sched_rr_get_interval is the quantum of Linux's SCHED_RR.
func (p *Process) libcSched_rr_get_interval(pid, ts uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return p.result(0, err)
	}

	p.putTimespec(ts, 100*time.Millisecond)

	return p.result(0, nil)
}

// setgroups and initgroups need privilege: initgroups's groups are gid
// and those of the group database that name user a member.
func (p *Process) libcSetgroups(n, list uint32) uint32 {
	if n > ngroupsMax {
		return p.result(0, syscall.EINVAL)
	}

	groups := make([]uint32, n)

	for i := range groups {
		groups[i] = p.u32(list + uint32(4*i))
	}

	return p.result(0, p.setGroups(groups))
}

func (p *Process) libcInitgroups(user, gid uint32) uint32 {
	name := p.str(user)
	groups := []uint32{gid}

	for _, e := range p.groupEntries() {
		if slices.Contains(e.members, name) && !slices.Contains(groups, e.gid) {
			groups = append(groups, e.gid)
		}
	}

	return p.result(0, p.setGroups(groups))
}

// ngroupsMax is NGROUPS_MAX, Linux's.
const ngroupsMax = 65536

func (p *Process) setGroups(groups []uint32) error {
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	if !p.cred.privileged() {
		return syscall.EPERM
	}

	p.cred.groups = groups

	return nil
}

// Children. A child of vfork runs on its parent's goroutine, in its frames
// and memory, as the instance's process, until it execs, when it goes on
// on a goroutine of its own, or ends; then the parent returns from vfork a
// second time, with the child's pid (instance.go). The parent is suspended
// meanwhile because its goroutine runs the child.
//
// An ended process is a zombie until its parent waits for it; the children
// of an ended process go to the first one.

const (
	wNohang    = 1
	wUntraced  = 2
	wExited    = 4
	wContinued = 8
	wNowait    = 0x1000000
	pAll       = 0
	pPid       = 1
	pPgid      = 2
	cldExited  = 1
	cldKilled  = 2
	cldStopped = 5
	cldCont    = 6
	// the wait statuses of a stop by sig, sig<<8|stoppedBy, and a continue
	stoppedBy = 0x7f
	continued = 0xffff
)

// vforkFrame is the parent's side of a vfork: the frame to return to and
// its stack pointer.
type vforkFrame struct {
	parent *Process
	jump   Jump
	stack  uint32
}

// resume returns the parent from vfork with pid.
func (v *vforkFrame) resume(pid int32) {
	p := v.parent
	p.inst.attach(p)
	p.raiseInterrupt()
	binary.LittleEndian.PutUint32(p.inst.block[p.stackPointer():], v.stack)
	p.inst.longjmp(&v.jump, uint32(pid))
	p.fatal("vfork: the function that called vfork has returned")
}

func (p *Process) startVfork() uint32 {
	v := &vforkFrame{parent: p}

	if !p.inst.setjmp(&v.jump) {
		p.fatal("vfork: called through a pointer")
	}

	v.stack = binary.LittleEndian.Uint32(p.inst.block[p.stackPointer():])
	c, err := p.k.newProcess(p)

	if err != nil {
		return p.result(0, err)
	}

	c.setInstance(p.inst)
	c.image, c.vfork = p.image, v
	c.heap, c.static, c.env, c.errnoAt, c.environAt, c.siginfo = p.heap, p.static, p.env, p.errnoAt, p.environAt, p.siginfo
	c.regexes, c.nextRegex, c.constants, c.jumps, c.stackAt = p.regexes, p.nextRegex, p.constants, p.jumps, p.stackAt
	c.locale, c.envStrings, c.mappings, c.tzAt, c.herrnoAt, c.passwordAt = p.locale, p.envStrings, p.mappings, p.tzAt, p.herrnoAt, p.passwordAt
	c.token.Store(p.token.Load())
	c.inherit(p)
	p.inst.attach(c)

	return c.result(0, nil)
}

// fork makes a child that is a copy of the process: its memory in use,
// from the stack pointer to the top of the heap, its instance and its
// frames up to _start, on a goroutine of its own; setup changes it before
// it runs. The call fork is in returns 0 in the child. A signal handler
// cannot fork: Go's frames are in the way.
func (p *Process) fork(setup func(c *Process)) (int32, error) {
	if p.chaosFail("fork") {
		return 0, syscall.EAGAIN
	}

	j := new(Jump)

	if !p.inst.setjmp(j) {
		p.fatal("fork: called through a pointer")
	}

	frames, ok := p.inst.frames(j, "_start")

	if !ok {
		return 0, syscall.ENOSYS
	}

	from := uint64(binary.LittleEndian.Uint32(p.inst.block[p.stackPointer():])) &^ 4095
	to := p.inst.memory.pages * pageSize

	if p.heap != nil {
		to = uint64(p.heap.top)
	}

	c, err := p.k.newProcess(p)

	if err != nil {
		return 0, err
	}

	inst := p.inst.clone(p.k, runStack, from, to)
	inst.attach(c)
	c.image = p.image
	c.heap, c.static, c.env, c.errnoAt, c.environAt, c.siginfo = p.heap.clone(), p.static, p.env, p.errnoAt, p.environAt, p.siginfo
	c.nextRegex, c.stackAt, c.locale, c.tzAt, c.herrnoAt, c.passwordAt = p.nextRegex, p.stackAt, p.locale, p.tzAt, p.herrnoAt, p.passwordAt
	c.regexes, c.constants, c.jumps = map[uint32]*posixRegex{}, map[string]uint32{}, map[uint32]*jump{}

	for id, r := range p.regexes {
		copied := *r
		c.regexes[id] = &copied
	}

	for s, at := range p.constants {
		c.constants[s] = at
	}

	c.envStrings, c.mappings = maps.Clone(p.envStrings), map[uint32]mapping{}

	for at, m := range p.mappings {
		if m.file != nil {
			m.file.ref()
			m.read = bytes.Clone(m.read)
		}

		c.mappings[at] = m
	}

	// The jumps of frames deeper than the call have no frames in the child.
	for buf, o := range p.jumps {
		if o.sp() <= j.sp() {
			copied := *o
			c.jumps[buf] = &copied
		}
	}

	c.inherit(p)

	if setup != nil {
		setup(c)
	}

	go c.runFrom(p.image, inst, func() { inst.resume(frames) })

	return c.pid, nil
}

// inherit gives a child its parent's descriptors, signal actions, mask and
// alternate stack.
func (c *Process) inherit(p *Process) {
	c.files = fdTable{slots: slices.Clone(p.files.slots), used: p.files.used.clone()}

	for _, d := range c.files.slots {
		if d.obj != nil {
			d.obj.ref()
		}
	}

	p.sig.mu.Lock()
	c.sig.actions, c.sig.blocked, c.sig.altstack = p.sig.actions, p.sig.blocked, p.sig.altstack
	p.sig.mu.Unlock()
}

// execImage starts a program exec found: it unwinds the old one, or a
// child of vfork goes on with it on a goroutine of its own and gives its
// parent's frames back.
func (p *Process) execImage(img *execImage) {
	v := p.vfork

	if v == nil {
		panic(img)
	}

	p.vfork = nil
	p.unwound = true
	p.execReset()
	p.setInstance(nil)
	go p.run(img)
	v.resume(p.pid)
}

// endBorrowers ends the children of vfork still on the frames an image
// unwound, as a trap in one of them does: they go with it.
func (p *Process) endBorrowers(i *Instance, e any) {
	for q := i.host.(*Process); q != p; {
		v := q.vfork
		q.vfork = nil
		status := int(syscall.SIGKILL)

		if t, ok := e.(*Trap); ok {
			status = trapSignal(t.code)
		}

		q.terminate(status)
		q = v.parent
	}

	i.attach(p)
}

// pidSelector is what waitpid's pid selects of the children: one, the
// caller's group (0), all (-1) or a group.
func (p *Process) pidSelector(pid int32) func(q *Process) bool {
	switch {
	case pid > 0:
		return func(q *Process) bool { return q.pid == pid }
	case pid == 0:
		pgid := p.pgid

		return func(q *Process) bool { return q.pgid == pgid }
	case pid == -1:
		return func(*Process) bool { return true }
	}

	return func(q *Process) bool { return q.pgid == -pid }
}

// reap takes a child selected that ended, stopped or continued, as options
// ask: it returns its pid and status, or 0 and whether any child is
// selected. With wNowait the child stays as it was.
func (p *Process) reap(selects func(*Process) bool, options uint32) (int32, int, bool) {
	k := p.k
	k.mu.Lock()
	defer k.mu.Unlock()

	found := false

	for _, q := range k.procs {
		if q.ppid != p.pid || !selects(q) {
			continue
		}

		found = true

		if q.zombie && options&wExited != 0 {
			if options&wNowait == 0 {
				delete(k.procs, q.pid)
			}

			return q.pid, q.status, true
		}

		if r := q.report; r&0xff == stoppedBy && options&wUntraced != 0 || r == continued && options&wContinued != 0 {
			if options&wNowait == 0 {
				q.report = 0
			}

			return q.pid, r, true
		}
	}

	return 0, 0, found
}

// waitChild waits until a selected child changes as options ask and
// returns its pid and wait status, or 0 with wNohang while none has.
func (p *Process) waitChild(selects func(*Process) bool, options uint32) (int32, int, error) {
	for {
		seen := p.childEvents.Load()
		q, status, found := p.reap(selects, options)

		if q != 0 {
			return q, status, nil
		}

		if !found {
			return 0, 0, syscall.ECHILD
		}

		if options&wNohang != 0 {
			return 0, 0, nil
		}

		if err := p.waitEvent(func() bool { return p.childEvents.Load() != seen }); err != nil {
			return 0, 0, err
		}
	}
}

// waitpid is wait4 without the usage of the child.
func (p *Process) waitpid(pid int32, at, options uint32) (int, error) {
	const (
		clone     = uint32(1 << 31)
		all       = uint32(1 << 30)
		notThread = uint32(1 << 29)
		basic     = wNohang | wUntraced | wContinued
	)

	if options&^(basic|clone|all|notThread) != 0 {
		return 0, syscall.EINVAL
	}

	// Every child is a fork child with SIGCHLD, and each process has one thread.
	if options&clone != 0 && options&all == 0 {
		return 0, syscall.ECHILD
	}

	q, status, err := p.waitChild(p.pidSelector(pid), options&basic|wExited)

	if err == nil && q != 0 && at != 0 {
		p.put32(at, uint32(status))
	}

	return int(q), err
}

// wait4 is waitpid with the child's struct rusage, which is the kernel's:
// all zero.
func (p *Process) wait4(pid int32, at, options, usage uint32) uint32 {
	q, err := p.waitpid(pid, at, options)

	if err == nil && q != 0 && usage != 0 {
		p.putRusage(usage, nil)
	}

	return p.result(q, err)
}

func (p *Process) waitid(idtype, id, info, options uint32) uint32 {
	var selects func(*Process) bool

	switch {
	case idtype == pAll:
		selects = func(*Process) bool { return true }
	case idtype == pPid && int32(id) > 0:
		selects = p.pidSelector(int32(id))
	case idtype == pPgid && int32(id) >= 0:
		pgid := int32(id)

		if pgid == 0 {
			pgid = p.pgid
		}

		selects = func(q *Process) bool { return q.pgid == pgid }
	default:
		return p.result(0, syscall.EINVAL)
	}

	if options&(wExited|wUntraced|wContinued) == 0 || options&^(wNohang|wExited|wUntraced|wContinued|wNowait) != 0 {
		return p.result(0, syscall.EINVAL)
	}

	q, status, err := p.waitChild(selects, options)

	if err != nil {
		return p.result(0, err)
	}

	if info != 0 {
		clear(p.span(info, 36))

		if q != 0 {
			code, value := cldExited, status>>8&0xff

			switch {
			case status&0xff == stoppedBy:
				code = cldStopped
			case status == continued:
				code, value = cldCont, int(syscall.SIGCONT)
			case status&0x7f != 0:
				code, value = cldKilled, status&0x7f
			}

			p.put32(info, uint32(syscall.SIGCHLD))
			p.put32(info+8, uint32(code))
			p.put32(info+12, uint32(q))
			// The kernel keeps no ids of a reaped child: its real uid is
			// taken to be its parent's, which a child has but by setuid.
			p.put32(info+16, p.cred.uid())
			p.put32(info+20, uint32(value))
		}
	}

	return p.result(0, nil)
}

// stopped stops the process by sig: its parent learns it, and it waits
// until it is continued or a SIGKILL is pending; its other signals wait
// with it (takeSignals).
func (p *Process) stopped(sig int) {
	p.k.mu.Lock()
	p.halted, p.report = true, sig<<8|stoppedBy
	p.k.mu.Unlock()
	p.tellParent(saNocldstop)
	p.wait(time.Time{}, func(*event) (bool, error) {
		p.k.mu.Lock()
		halted := p.halted
		p.k.mu.Unlock()
		p.sig.mu.Lock()
		killed := p.sig.pending&sigBit(int(syscall.SIGKILL)) != 0
		p.sig.mu.Unlock()

		return !halted || killed, nil
	})
}

// cont continues the process if it is stopped.
func (p *Process) cont() {
	p.k.mu.Lock()
	was := p.halted

	if was {
		p.halted, p.report = false, continued
	}

	p.k.mu.Unlock()

	if was {
		p.post(&p.own.continued)
		p.tellParent(saNocldstop)
	}
}

// tellParent tells the parent of a change of the process: SIGCHLD unless
// the parent's action has flag.
func (p *Process) tellParent(flag uint32) {
	p.k.mu.Lock()
	parent := p.k.procs[p.ppid]
	p.k.mu.Unlock()

	if parent == nil {
		return
	}

	parent.sig.mu.Lock()
	quiet := parent.sig.actions[syscall.SIGCHLD].flags&flag != 0
	parent.sig.mu.Unlock()

	if quiet {
		parent.childChanged(0)
	} else {
		parent.childChanged(int(syscall.SIGCHLD))
	}
}

// waitEvent waits until ready holds, which the event of a child comes
// for, or a signal interrupts: then the error is EINTR, after the
// handlers ran when they all restart calls. Ready comes first, as a call
// that has its result returns it.
func (p *Process) waitEvent(ready func() bool) error {
	_, err := p.wait(time.Time{}, func(*event) (bool, error) {
		if ready() {
			return true, nil
		}

		return p.interrupts(true), syscall.EINTR
	})

	return err
}

func (p *Process) libcFork() uint32 {
	pid, err := p.fork(nil)

	return p.result(int(pid), err)
}

func (p *Process) libcWaitpid(pid, status, options uint32) uint32 {
	return p.result(p.waitpid(int32(pid), status, options))
}

func (p *Process) libcWait(status uint32) uint32 {
	return p.result(p.waitpid(-1, status, 0))
}

func (p *Process) libcWait4(pid, status, options, usage uint32) uint32 {
	return p.wait4(int32(pid), status, options, usage)
}

func (p *Process) libcVfork() uint32 {
	return p.startVfork()
}

func (p *Process) libcWait3(status, options, usage uint32) uint32 {
	return p.wait4(-1, status, options, usage)
}

func (p *Process) libcWaitid(idtype, id, info, options uint32) uint32 {
	return p.waitid(idtype, id, info, options)
}

// Signals are the kernel's: a process has its actions, blocked mask and
// pending set; sending one sets it pending and puts the process's signal
// event in its queue (event_queue.go), whose callback takes the signals:
// a wait runs it as it takes the event, and the process's own code as it
// is interrupted, which the interrupt word of its instance asks for on
// its way out of an import and Go as it runs (lib/goruntime). No call of
// the kernel's looks at the signals on its way out. A handler
// is a function of the guest's table, called through the exports of the
// crt. Signals of the host's terminal go to its foreground group, the
// host's others to the first process.

const (
	sigDfl      = 0
	sigIgn      = ^uint32(1)
	saNocldstop = 1
	saNocldwait = 2
	saSiginfo   = 4
	saOnstack   = 0x08000000
	saRestart   = 0x10000000
	saNodefer   = 0x40000000
	saResethand = 0x80000000
	sigBlock    = 0
	sigUnblock  = 1
	sigSetmask  = 2
	nsig        = 65
	ssOnstack   = 1
	ssDisable   = 2
	minSigstksz = 2048
)

// What a signal does by default.
const (
	actTerminate = iota
	actIgnore
	actStop
	actContinue
)

func defaultAction(sig int) int {
	switch syscall.Signal(sig) {
	case syscall.SIGCHLD, syscall.SIGURG, syscall.SIGWINCH:
		return actIgnore
	case syscall.SIGCONT:
		return actContinue
	case syscall.SIGSTOP, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU:
		return actStop
	}

	return actTerminate
}

func sigBit(sig int) uint64 {
	return 1 << (sig - 1)
}

// stopSignals stop a process by default; SIGCONT and they discard each
// other when sent.
const stopSignals = 1<<(syscall.SIGSTOP-1) | 1<<(syscall.SIGTSTP-1) | 1<<(syscall.SIGTTIN-1) | 1<<(syscall.SIGTTOU-1)

// sent discards what a signal sent cancels: SIGCONT the pending stops, a
// stop a pending SIGCONT; the caller holds the lock.
func (s *signalState) sent(sig int) {
	switch {
	case sig == int(syscall.SIGCONT):
		s.pending &^= stopSignals
	case sigBit(sig)&stopSignals != 0:
		s.pending &^= sigBit(int(syscall.SIGCONT))
	}
}

// unblockable is what no mask holds: SIGKILL and SIGSTOP.
const unblockable = 1<<(syscall.SIGKILL-1) | 1<<(syscall.SIGSTOP-1)

type sigAction struct {
	handler uint32
	flags   uint32
	mask    uint64
}

type signalState struct {
	mu      sync.Mutex
	actions [nsig]sigAction
	blocked uint64
	pending uint64
	// dead is set when the process ended and closed its wake descriptor.
	dead bool
	// altstack is the stack of sigaltstack, none when its size is 0.
	altstack struct{ sp, size uint32 }
}

func (s *signalState) deliverable() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.pending &^ s.blocked
}

// ignored tells whether a signal is discarded when sent.
func (a sigAction) ignored(sig int) bool {
	return a.handler == sigIgn || a.handler == sigDfl && defaultAction(sig) == actIgnore
}

// send makes a signal pending for the process and wakes it. A fatal signal
// for the first process ends vm at once: the process may be running
// code that takes no signals. SIGCONT continues a stopped process even
// when it ignores it.
func (p *Process) send(sig int) {
	if sig == int(syscall.SIGCONT) {
		p.cont()
	}

	p.sig.mu.Lock()
	p.sig.sent(sig)
	act := p.sig.actions[sig]
	blocked := p.sig.blocked&sigBit(sig) != 0

	if act.ignored(sig) && !blocked {
		p.sig.mu.Unlock()

		return
	}

	if p == p.k.first && !blocked && act.handler == sigDfl && defaultAction(sig) == actTerminate {
		restoreHostTerminal()
		os.Exit(128 + sig)
	}

	p.sig.pending |= sigBit(sig)
	token := p.token.Load()

	if p.sig.dead || blocked {
		token = 0
	} else {
		p.setInterrupt(1)
	}

	p.sig.mu.Unlock()
	p.post(&p.own.signal)

	if token != 0 {
		runtime.Interrupt(token)
		p.chase()
	}
}

// chase interrupts the process again until it takes what it has, the
// events of its queue and its deliverable signals: Go's signal may find
// it outside its own code, in a stub, or in a long call of the kernel. It
// gives up when the process stops or leaves its image, and after 32 ms it
// tries every 10 ms.
func (p *Process) chase() {
	if !p.chasing.CompareAndSwap(false, true) {
		return
	}

	go func() {
		for k := 0; ; k++ {
			wait := time.Millisecond

			if k >= 32 {
				wait = 10 * time.Millisecond
			}

			time.Sleep(wait)
			token := p.token.Load()
			p.k.mu.Lock()
			halted := p.halted
			p.k.mu.Unlock()

			if token != 0 && !halted && p.interruptDue() {
				runtime.Interrupt(token)

				continue
			}

			p.chasing.Store(false)

			// a signal sent meanwhile found the flag set
			if token == 0 || halted || !p.interruptDue() || !p.chasing.CompareAndSwap(false, true) {
				return
			}
		}
	}()
}

// interruptDue tells whether the process has something to take where its
// code is interrupted: an event in its queue, or a deliverable signal.
func (p *Process) interruptDue() bool {
	return p.events.count.Load() != 0 || p.sig.deliverable() != 0
}

// nudge has the process take the events of its queue though it runs its
// own code, for one put there that does not wait for a wait of its, a
// timer's: its interrupt word, and Go's interrupt of the code, as for a
// signal sent. The event is in the queue before: the word is cleared only
// with the queue empty (interrupt).
func (p *Process) nudge() {
	p.sig.mu.Lock()
	token := p.token.Load()

	if p.sig.dead {
		token = 0
	} else {
		p.setInterrupt(1)
	}

	p.sig.mu.Unlock()

	if token != 0 {
		runtime.Interrupt(token)
		p.chase()
	}
}

// setInstance sets the instance the process runs, under the lock of its
// signals, which write the instance's interrupt word.
func (p *Process) setInstance(i *Instance) {
	p.sig.mu.Lock()
	p.inst = i
	p.sig.mu.Unlock()
}

// setInterrupt sets the interrupt word of the process's instance: while it
// is not zero, every return from an import interrupts the code. The caller
// holds the lock of the signals.
func (p *Process) setInterrupt(v uint32) {
	if p.inst != nil && !p.sig.dead {
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&p.inst.block[offInterrupt])), v)
	}
}

// raiseInterrupt is the process coming to the code of an instance whose
// interrupt word says nothing of it: a new image's, or the one a child of
// vfork ran on. A signal deliverable then may have lost its event to a
// wait that could run no handler, and gets it again; and with an event in
// the queue the word is set.
func (p *Process) raiseInterrupt() {
	if p.sig.deliverable() != 0 {
		p.post(&p.own.signal)
	}

	p.sig.mu.Lock()

	if p.events.count.Load() != 0 {
		p.setInterrupt(1)
	}

	p.sig.mu.Unlock()
}

// redZone is how far below __stack_pointer a leaf function keeps its frame
// without moving it, as LLVM's wasm code does.
const redZone = 128

// interrupt is the process taking the events of its queue where its own
// code was interrupted, by Go or on its way out of an import, as a wait
// takes them, but that it asks nothing and never sleeps: the oldest
// first, each one's callback run. So a timer that fired sends its signal,
// and the signal's event, behind it, takes the signals. An event with no
// callback names a change for a wait, and none waits here: a wait asks
// what it waits for before it sleeps, and one whose handler this is in
// asks all of it again after the handler. A handler's frames on the
// shadow stack go below the red zone of the function interrupted. With
// the queue empty and no signal deliverable the process has nothing more
// to take, and the interrupt word is cleared: here only.
func (p *Process) interrupt() {
	at, shadow := p.inst.globalAt("__stack_pointer")
	var sp uint32

	if shadow {
		sp = binary.LittleEndian.Uint32(p.inst.block[at:])
		binary.LittleEndian.PutUint32(p.inst.block[at:], sp-redZone)
	}

	for e := p.events.next(); e != nil; e = p.events.next() {
		if e.run != nil {
			e.run(p)
		}
	}

	if shadow {
		binary.LittleEndian.PutUint32(p.inst.block[at:], sp)
	}

	p.sig.mu.Lock()

	if p.sig.pending&^p.sig.blocked == 0 && p.events.count.Load() == 0 {
		p.setInterrupt(0)
	}

	p.sig.mu.Unlock()
}

// sendSelf sends a signal the process raised itself; it takes it on its
// way out of the call, as its interrupt word asks.
func (p *Process) sendSelf(sig int) {
	p.sig.mu.Lock()
	p.sig.sent(sig)
	act := p.sig.actions[sig]

	if !act.ignored(sig) || p.sig.blocked&sigBit(sig) != 0 {
		p.sig.pending |= sigBit(sig)
	}

	p.sig.mu.Unlock()
	p.signalled()
}

// signalled puts the process's signal event when a signal of its is
// deliverable, and sets its interrupt word: after a signal it sent itself,
// or a change of its mask.
func (p *Process) signalled() {
	p.sig.mu.Lock()
	ready := p.sig.pending&^p.sig.blocked != 0

	if ready {
		p.setInterrupt(1)
	}

	p.sig.mu.Unlock()

	if ready {
		p.post(&p.own.signal)
	}
}

// What the signals a call took say of it (Process.interruption): none
// was taken; some were, and every handler run asked to restart calls; a
// handler ran that did not.
const (
	signalNone = iota
	signalTaken
	signalFinal
)

// interrupts tells whether the signals the call took end a wait of its:
// any taken ends one that does not restart, a handler that did not ask to
// restart ends any. Told so, the wait ends with EINTR, and the call's
// signals are told of: what the call does after, to put back what it
// changed, they interrupt no more.
func (p *Process) interrupts(restart bool) bool {
	if p.interruption == signalFinal || p.interruption == signalTaken && !restart {
		p.interruption = signalNone

		return true
	}

	return false
}

// takeSignals is the callback of the process's signal event, which the
// wait that takes the event runs, any wait, and the process's own code
// interrupted: it delivers the signals the process has, their handlers
// and default actions, and notes for the call what it took (interruption):
// the calls a handler makes are calls of their own. With a request
// at a server, the process sends CANCEL for it first, once, and takes its
// signals at once after, not waiting for the server's word: a process
// stopped, or in a handler, has asked nothing of a server that the next
// to ask would wait behind, and its call finds on its return what the
// server said, its answer or that it took the request back. A process
// whose image ended, by its exit or an exec, has no code to run a handler
// of, and a stopped one runs none: their signals stay pending, for the
// next image or the end of the stop, which SIGKILL is.
func (p *Process) takeSignals() {
	if p.unwound || p.inst == nil {
		return
	}

	p.k.mu.Lock()
	halted := p.halted
	p.k.mu.Unlock()

	if halted {
		return
	}

	if r := p.asking; r != nil && !p.cancelled && p.sig.deliverable() != 0 {
		p.cancelled = true
		r.cancel(p)
	}

	before := p.interruption
	taken, restart := p.handleSignals()

	switch {
	case !taken:
		p.interruption = before
	case restart:
		p.interruption = max(before, signalTaken)
	default:
		p.interruption = signalFinal
	}
}

// ask notes the request the process has at a server from now on, by the
// end its reply comes on; it returns what was noted before, the request
// of a call a handler of whose wait makes this one, for unask to put back
// as this one ends.
func (p *Process) ask(r *callEnd) (*callEnd, bool) {
	outer, was := p.asking, p.cancelled
	p.asking, p.cancelled = r, false

	return outer, was
}

func (p *Process) unask(outer *callEnd, was bool) {
	p.asking, p.cancelled = outer, was
}

// take removes the lowest deliverable signal and returns it with its
// action, or 0.
func (p *Process) take() (int, sigAction) {
	p.sig.mu.Lock()
	defer p.sig.mu.Unlock()

	ready := p.sig.pending &^ p.sig.blocked

	if ready == 0 {
		return 0, sigAction{}
	}

	sig := bits.TrailingZeros64(ready) + 1
	p.sig.pending &^= sigBit(sig)
	act := p.sig.actions[sig]

	if act.handler != sigDfl && act.handler != sigIgn && act.flags&saResethand != 0 {
		p.sig.actions[sig] = sigAction{}
	}

	return sig, act
}

// deliver takes the pending signals: it runs their handlers and their
// default actions.
func (p *Process) deliver() {
	p.handleSignals()
}

// interrupted takes the signals the process has now, those it sent its
// own group, and tells whether its call restarts: when every handler run
// asked for SA_RESTART.
func (p *Process) interrupted() bool {
	_, restart := p.handleSignals()

	return restart
}

// handleSignals takes the deliverable signals, each by its action: taken
// says there was one, restart that no handler run did not ask for
// SA_RESTART.
func (p *Process) handleSignals() (taken, restart bool) {
	restart = true

	for {
		sig, act := p.take()

		if sig == 0 {
			return taken, restart
		}

		taken = true

		switch act.handler {
		case sigIgn:
		case sigDfl:
			switch defaultAction(sig) {
			case actTerminate:
				p.exitWith(sig)
			case actStop:
				p.stop(sig)
			}
		default:
			if act.flags&saRestart == 0 {
				restart = false
			}

			p.runHandler(sig, act)
		}
	}
}

// stop stops the process until SIGCONT or SIGKILL. The
// first process stops vm, so the host's shell gets the terminal back,
// once what it wrote to the console is out.
func (p *Process) stop(sig int) {
	if p == p.k.first {
		if p.k.console != nil {
			p.k.console.settle()
		}

		// A process-directed stop can reach another thread after this one
		// resumes the guest. Stop this thread before returning to its code.
		runtime.LockOSThread()

		defer runtime.UnlockOSThread()

		throw(syscall.Tgkill(os.Getpid(), syscall.Gettid(), syscall.SIGSTOP))

		return
	}

	p.stopped(sig)
}

// runHandler calls a handler with the action's mask added for its time;
// errno is the interrupted code's again after it. A handler may leave by
// longjmp, and then nothing after the call runs: here,
// nor in the kernel's frames above.
func (p *Process) runHandler(sig int, act sigAction) {
	p.sig.mu.Lock()
	saved := p.sig.blocked
	p.sig.blocked |= act.mask &^ unblockable

	if act.flags&saNodefer == 0 {
		p.sig.blocked |= sigBit(sig)
	}

	alt := p.sig.altstack
	p.sig.mu.Unlock()
	errno := p.errno()
	ctx := p.inst.ctx()

	// A handler of SA_ONSTACK runs on the alternate stack, unless it is
	// on it already; a longjmp out of it restores the stack pointer.
	sp := binary.LittleEndian.Uint32(p.inst.block[p.stackPointer():])
	moved := act.flags&saOnstack != 0 && alt.size != 0 && sp-alt.sp > alt.size

	if moved {
		binary.LittleEndian.PutUint32(p.inst.block[p.stackPointer():], (alt.sp+alt.size)&^15)
	}

	if act.flags&saSiginfo != 0 {
		if p.siginfo == 0 {
			p.siginfo = p.malloc(36)
		}

		info := p.span(p.siginfo, 36)
		clear(info)
		p.put32(p.siginfo, uint32(sig))
		p.put32(p.siginfo+12, uint32(p.pid))
		f, _ := p.inst.export("__call_signal_action")
		f.Interface().(func(uintptr, uint32, uint32, uint32, uint32))(ctx, act.handler, uint32(sig), p.siginfo, 0)
	} else {
		f, _ := p.inst.export("__call_signal_handler")
		f.Interface().(func(uintptr, uint32, uint32))(ctx, act.handler, uint32(sig))
	}

	if moved {
		binary.LittleEndian.PutUint32(p.inst.block[p.stackPointer():], sp)
	}

	p.setErrno(errno)
	p.sig.mu.Lock()
	p.sig.blocked = saved
	p.sig.mu.Unlock()
}

func (p *Process) sigprocmask(how, set, old uint32) error {
	mask := p.mask()

	if old != 0 {
		p.put64(old, mask)
	}

	if set != 0 {
		s := p.u64(set)

		switch how {
		case sigBlock:
			mask |= s
		case sigUnblock:
			mask &^= s
		case sigSetmask:
			mask = s
		default:
			return syscall.EINVAL
		}

		p.setMask(mask)
	}

	return nil
}

// sigwait waits until a signal of set is pending and takes it, for at most
// timeout when that is not negative, else EAGAIN. Another signal the process
// would deliver interrupts the wait: its handler runs and the error is
// EINTR.
func (p *Process) sigwait(set uint64, timeout time.Duration) (int, error) {
	set &^= unblockable
	var end time.Time

	if timeout >= 0 {
		end = time.Now().Add(timeout)
	}

	// The signals of set are the wait's to take: blocked while it waits,
	// so the signal event's callback leaves them, and takes the others.
	old := p.setMask(p.mask() | set)
	defer p.setMask(old)

	sig := 0
	timedOut, err := p.wait(end, func(*event) (bool, error) {
		p.sig.mu.Lock()

		if ready := p.sig.pending & set; ready != 0 {
			sig = bits.TrailingZeros64(ready) + 1
			p.sig.pending &^= sigBit(sig)
		}

		p.sig.mu.Unlock()

		if sig != 0 {
			return true, nil
		}

		return p.interrupts(false), syscall.EINTR
	})

	if timedOut {
		return 0, syscall.EAGAIN
	}

	return sig, err
}

// putSiginfo writes the siginfo_t of a signal sigwaitinfo takes.
func (p *Process) putSiginfo(at uint32, sig int) {
	if at != 0 {
		clear(p.span(at, 36))
		p.put32(at, uint32(sig))
	}
}

// setMember checks a signal a sigset_t takes: not 32 and 33, which
// sigfillset leaves out as musl's.
func (p *Process) setMember(sig uint32) bool {
	if !validSignal(sig) || sig == 32 || sig == 33 {
		p.setErrno(syscall.EINVAL)

		return false
	}

	return true
}

func (p *Process) setAction(sig int, act sigAction) sigAction {
	p.sig.mu.Lock()
	defer p.sig.mu.Unlock()

	old := p.sig.actions[sig]
	p.sig.actions[sig] = act

	if act.ignored(sig) {
		p.sig.pending &^= sigBit(sig)
	}

	return old
}

// setMask sets the blocked signals: a pending one the new mask lets
// through is deliverable from then on (signalled).
func (p *Process) setMask(mask uint64) uint64 {
	p.sig.mu.Lock()
	old := p.sig.blocked
	p.sig.blocked = mask &^ unblockable
	ready := p.sig.pending&^p.sig.blocked != 0

	if ready {
		p.setInterrupt(1)
	}

	p.sig.mu.Unlock()

	if ready {
		p.post(&p.own.signal)
	}

	return old
}

func (p *Process) mask() uint64 {
	p.sig.mu.Lock()
	defer p.sig.mu.Unlock()

	return p.sig.blocked
}

func validSignal(sig uint32) bool {
	return sig >= 1 && sig < nsig
}

func (p *Process) kill(pid int32, sig uint32) error {
	if sig != 0 && !validSignal(sig) {
		return syscall.EINVAL
	}

	targets := p.k.signalTargets(p, pid)

	if len(targets) == 0 {
		return syscall.ESRCH
	}

	if sig != 0 {
		for _, q := range targets {
			if q == p {
				q.sendSelf(int(sig))
			} else {
				q.send(int(sig))
			}
		}
	}

	return nil
}

func (p *Process) libcSignal(sig, handler uint32) uint32 {
	if !validSignal(sig) || sig == uint32(syscall.SIGKILL) || sig == uint32(syscall.SIGSTOP) {
		p.setErrno(syscall.EINVAL)

		return ^uint32(0)
	}

	return p.setAction(int(sig), sigAction{handler: handler, flags: saRestart}).handler
}

func (p *Process) libcSigaction(sig, act, old uint32) uint32 {
	if !validSignal(sig) || act != 0 && (sig == uint32(syscall.SIGKILL) || sig == uint32(syscall.SIGSTOP)) {
		return p.result(0, syscall.EINVAL)
	}

	p.sig.mu.Lock()
	prev := p.sig.actions[sig]
	p.sig.mu.Unlock()

	if act != 0 {
		p.setAction(int(sig), sigAction{handler: p.u32(act), mask: p.u64(act + 8), flags: p.u32(act + 16)})
	}

	if old != 0 {
		clear(p.span(old, 24))
		p.put32(old, prev.handler)
		p.put64(old+8, prev.mask)
		p.put32(old+16, prev.flags)
	}

	return p.result(0, nil)
}

func (p *Process) libcSigprocmask(how, set, old uint32) uint32 {
	return p.result(0, p.sigprocmask(how, set, old))
}

// pthread_sigmask is sigprocmask of the process's one thread; it
// returns an error number.
func (p *Process) libcPthread_sigmask(how, set, old uint32) uint32 {
	err := p.sigprocmask(how, set, old)

	if err != nil {
		return uint32(errnoOf(err))
	}

	return 0
}

func (p *Process) libcSigfillset(set uint32) uint32 {
	p.put64(set, ^uint64(0)&^(3<<31))

	return 0
}

func (p *Process) libcSigemptyset(set uint32) uint32 {
	p.put64(set, 0)

	return 0
}

func (p *Process) libcSigaddset(set, sig uint32) uint32 {
	if !p.setMember(sig) {
		return math.MaxUint32
	}

	p.put64(set, p.u64(set)|sigBit(int(sig)))

	return 0
}

func (p *Process) libcSigdelset(set, sig uint32) uint32 {
	if !p.setMember(sig) {
		return math.MaxUint32
	}

	p.put64(set, p.u64(set)&^sigBit(int(sig)))

	return 0
}

func (p *Process) libcSigismember(set, sig uint32) uint32 {
	return bit(validSignal(sig) && p.u64(set)&sigBit(int(sig)) != 0)
}

func (p *Process) libcSigisemptyset(set uint32) uint32 {
	return bit(p.u64(set) == 0)
}

func (p *Process) libcSigorset(d, a, b uint32) uint32 {
	p.put64(d, p.u64(a)|p.u64(b))

	return 0
}

func (p *Process) libcSigandset(d, a, b uint32) uint32 {
	p.put64(d, p.u64(a)&p.u64(b))

	return 0
}

func (p *Process) libcSigpending(set uint32) uint32 {
	p.sig.mu.Lock()
	pending := p.sig.pending
	p.sig.mu.Unlock()
	p.put64(set, pending)

	return p.result(0, nil)
}

// sigwait returns an error number.
func (p *Process) libcSigwait(set, at uint32) uint32 {
	for {
		sig, err := p.sigwait(p.u64(set), -1)

		if err == nil {
			p.put32(at, uint32(sig))

			return 0
		}
	}
}

func (p *Process) libcSigwaitinfo(set, info uint32) uint32 {
	sig, err := p.sigwait(p.u64(set), -1)

	if err == nil {
		p.putSiginfo(info, sig)
	}

	return p.result(sig, err)
}

func (p *Process) libcSigtimedwait(set, info, timeout uint32) uint32 {
	d := time.Duration(-1)

	if timeout != 0 {
		var err error

		if d, err = p.timespec(timeout); err != nil {
			return p.result(0, err)
		}
	}

	sig, err := p.sigwait(p.u64(set), d)

	if err == nil {
		p.putSiginfo(info, sig)
	}

	return p.result(sig, err)
}

func (p *Process) libcSigaltstack(ss, old uint32) uint32 {
	sp := binary.LittleEndian.Uint32(p.inst.block[p.stackPointer():])
	p.sig.mu.Lock()
	alt := p.sig.altstack
	p.sig.mu.Unlock()
	on := alt.size != 0 && sp-alt.sp <= alt.size

	if old != 0 {
		flags := uint32(0)

		switch {
		case alt.size == 0:
			flags = ssDisable
		case on:
			flags = ssOnstack
		}

		p.put32(old, alt.sp)
		p.put32(old+4, flags)
		p.put32(old+8, alt.size)
	}

	if ss == 0 {
		return p.result(0, nil)
	}

	flags := p.u32(ss + 4)

	switch {
	case on:
		return p.result(0, syscall.EPERM)
	case flags&^(ssOnstack|ssDisable|1<<31) != 0:
		return p.result(0, syscall.EINVAL)
	case flags&ssDisable != 0:
		alt.sp, alt.size = 0, 0
	case p.u32(ss+8) < minSigstksz:
		return p.result(0, syscall.ENOMEM)
	default:
		alt.sp, alt.size = p.u32(ss), p.u32(ss+8)
		p.span(alt.sp, alt.size)
	}

	p.sig.mu.Lock()
	p.sig.altstack = alt
	p.sig.mu.Unlock()

	return p.result(0, nil)
}

// siginterrupt makes a signal's handler interrupt calls, or restart
// them.
func (p *Process) libcSiginterrupt(sig, flag uint32) uint32 {
	if !validSignal(sig) {
		return p.result(0, syscall.EINVAL)
	}

	p.sig.mu.Lock()

	if flag != 0 {
		p.sig.actions[sig].flags &^= saRestart
	} else {
		p.sig.actions[sig].flags |= saRestart
	}

	p.sig.mu.Unlock()

	return p.result(0, nil)
}

func (p *Process) libcPause() uint32 {
	for {
		if p.idle(-1, false) != nil {
			break
		}
	}

	return p.result(0, syscall.EINTR)
}

func (p *Process) libcSigsuspend(set uint32) uint32 {
	old := p.setMask(p.u64(set))

	for {
		if p.idle(-1, false) != nil {
			break
		}
	}

	p.handleSignals()
	p.setMask(old)

	return p.result(0, syscall.EINTR)
}

func (p *Process) libcRaise(sig uint32) uint32 {
	if !validSignal(sig) {
		return p.result(0, syscall.EINVAL)
	}

	p.sendSelf(int(sig))

	return p.result(0, nil)
}

func (p *Process) libcKill(pid, sig uint32) uint32 {
	return p.result(0, p.kill(int32(pid), sig))
}

// sigqueue sends a signal as kill does; its value does not reach
// the handler.
func (p *Process) libcSigqueue(pid, sig, value uint32) uint32 {
	if int32(pid) <= 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(0, p.kill(int32(pid), sig))
}

func (p *Process) libcKillpg(pgrp, sig uint32) uint32 {
	if int32(pgrp) <= 1 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(0, p.kill(-int32(pgrp), sig))
}

// abort raises SIGABRT, unblocked, and when a handler returns from it ends
// the process by it.
func (p *Process) abort() {
	abrt := int(syscall.SIGABRT)
	p.setMask(p.mask() &^ sigBit(abrt))
	p.sendSelf(abrt)
	p.deliver()
	p.setAction(abrt, sigAction{})
	p.exitWith(abrt)
}

func (p *Process) libcAbort() {
	p.abort()
}

// setjmp and longjmp (instance.go). The process keeps its jumps by the address
// of their jmp_buf, which stays unused: the guest cannot forge a frame. A
// jump also restores the module's stack pointer, which the program exports
// as __stack_pointer, and sigsetjmp's mask when it saved one.
//
// longjmp may leave a signal handler, dropping the kernel's frames down to
// it: what runs a handler holds no lock, defers nothing and leaves nothing
// half done for after it.

type jump struct {
	Jump
	stack uint32
	mask  uint64
	saved bool
}

// stackPointer is where the instance keeps __stack_pointer.
func (p *Process) stackPointer() uint32 {
	if p.stackAt == 0 {
		at, ok := p.inst.globalAt("__stack_pointer")

		if !ok {
			p.fatal("setjmp: no __stack_pointer")
		}

		p.stackAt = at
	}

	return p.stackAt
}

func (p *Process) setjmp(buf uint32, saveMask bool) uint32 {
	j := p.jumps[buf]

	if j == nil {
		j = new(jump)
	}

	if !p.inst.setjmp(&j.Jump) {
		p.fatal("setjmp: called through a pointer")
	}

	j.stack = binary.LittleEndian.Uint32(p.inst.block[p.stackPointer():])
	j.mask, j.saved = p.mask(), saveMask
	p.jumps[buf] = j

	return 0
}

func (p *Process) longjmp(buf, val uint32) {
	j := p.jumps[buf]

	if j == nil {
		p.fatal("longjmp: the jmp_buf was not set")
	}

	if val == 0 {
		val = 1
	}

	// The frames the jump leaves take their jumps with them.
	for b, o := range p.jumps {
		if o.sp() > j.sp() {
			delete(p.jumps, b)
		}
	}

	binary.LittleEndian.PutUint32(p.inst.block[p.stackPointer():], j.stack)

	if j.saved {
		p.setMask(j.mask)
		p.deliver()
	}

	p.inst.longjmp(&j.Jump, val)
	p.fatal("longjmp: the function that set the jmp_buf has returned")
}

func (p *Process) libcSetjmp(buf uint32) uint32 {
	return p.setjmp(buf, false)
}

func (p *Process) libcSigsetjmp(buf, saveMask uint32) uint32 {
	return p.setjmp(buf, saveMask != 0)
}

func (p *Process) libcLongjmp(buf, val uint32) {
	p.longjmp(buf, val)
}

// post puts an event at the end of the process's queue, unless it is
// there, and rings the bell; it never blocks.
func (p *Process) post(e *event) {
	q := &p.events
	q.mu.Lock()

	if !e.queued {
		e.queued, e.prev, e.next = true, q.last, nil

		if q.last != nil {
			q.last.next = e
		} else {
			q.first = e
		}

		q.last = e
		q.count.Add(1)
	}

	q.mu.Unlock()
	p.ring()
}

// ring rings the process's bell: a wait of its that sleeps looks at the
// queue, and with nothing there asks its predicate. It never blocks.
func (p *Process) ring() {
	select {
	case p.events.bell <- struct{}{}:
	default:
	}
}

// wake is the process as the waker of its subscriptions: the event of the
// subscription.
func (p *Process) wake(l *hook) {
	p.post(&l.sub.event)
}

// subscription is a spare subscription of the process's, or a new one,
// which wakes the process with id.
func (p *Process) subscription(id uint64) *subscription {
	s := p.spare

	if s == nil {
		s = &subscription{}
	} else {
		p.spare, s.spare = s.spare, nil
	}

	s.init(p, id)

	return s
}

// release gives back a subscription of the process's that nothing has any
// more: its event goes out of the queue.
func (p *Process) release(s *subscription) {
	p.events.drop(&s.event)
	s.spare, p.spare = p.spare, s
}

// wait is every wait of the process. done says whether the wait is over,
// and with what error: first on no event, nil, then on each event of the
// queue, the oldest first, after its callback ran. With the queue empty
// the process sleeps on its bell, and asks done on no event again when
// the bell rang and brought none: a waiter of its was answered, or was
// before the wait. A deadline, end, zero for none, is an
// event of the wait's, which a timer puts in the queue: on it the wait
// ends, timedOut. Another's, an earlier wait's or one a wait in a signal
// handler of this one's took, it passes over; so the wait looks at the
// time too before it sleeps.
func (p *Process) wait(end time.Time, done func(e *event) (bool, error)) (timedOut bool, err error) {
	if over, err := done(nil); over {
		return false, err
	}

	var deadline *event

	if !end.IsZero() {
		deadline = &event{kind: eventDeadline}
		t := time.AfterFunc(time.Until(end), func() { p.post(deadline) })

		defer func() {
			t.Stop()
			p.events.drop(deadline)
		}()
	}

	rung := false

	for {
		e := p.events.next()

		switch {
		case e == nil:
			if rung {
				rung = false

				if over, err := done(nil); over {
					return false, err
				}
			}

			if deadline != nil && !time.Now().Before(end) {
				return true, nil
			}

			<-p.events.bell
			rung = true

			continue
		case e.kind == eventDeadline:
			if e == deadline {
				return true, nil
			}

			continue
		case e.run != nil:
			e.run(p)
		}

		if over, err := done(e); over {
			return false, err
		}
	}
}

// await waits until a waiter of the process's has its answer, done, or the
// deadline, end, zero for none: timedOut then; a signal ends it as how
// says. The waiter is at its object still after either, for the caller
// to take back.
func (p *Process) await(done *atomic.Bool, end time.Time, how signalled) (timedOut bool, err error) {
	return p.wait(end, func(*event) (bool, error) {
		if done.Load() {
			return true, nil
		}

		if how == waitsOn {
			return false, nil
		}

		return p.interrupts(how == restarts), syscall.EINTR
	})
}

// ensure grows memory to hold end bytes.
func (p *Process) ensure(end uint64) bool {
	m := p.inst.memory
	size := m.pages * pageSize

	if end <= size {
		return true
	}

	need := (end - size + pageSize - 1) / pageSize
	more := max(need, min(m.pages/8, 256))

	if m.grow(uint32(more)) != math.MaxUint32 {
		return true
	}

	return m.grow(uint32(need)) != math.MaxUint32
}

// heapInit makes the heap from __heap_base.
func (p *Process) heapInit() *Heap {
	base, ok := p.inst.globalExport("__heap_base")

	if !ok {
		p.fatal("no __heap_base")
	}

	p.heap = newHeap(base)

	return p.heap
}

// grow adds a segment of free pages at the end of the heap.
func (p *Process) heapGrow(h *Heap) bool {
	end := uint64(h.base) + uint64(len(h.segments)+1)*segmentSize

	if end > math.MaxUint32 || !p.ensure(end) {
		return false
	}

	h.segments = append(h.segments, heapSegment{mask: math.MaxUint32})

	for range segmentPages {
		h.pages = append(h.pages, heapPage{kind: pageFree})
	}

	h.link(int32(len(h.segments) - 1))

	return true
}

// takePages takes count free pages in a row, from a segment with free
// pages or one the heap grows by.
func (p *Process) takePages(h *Heap, count uint32) (int32, bool) {
	if count > segmentPages {
		return p.takeSegments(h, count)
	}

	need := uint32(math.MaxUint32)

	if count < segmentPages {
		need = 1<<count - 1
	}

	for {
		hops := 0

		for s := h.free; s != noPage && hops < segmentHops; s, hops = h.segments[s].next, hops+1 {
			seg := &h.segments[s]
			runs := seg.mask

			for i := uint32(1); i < count && runs != 0; i++ {
				runs &= runs >> 1
			}

			if runs == 0 {
				continue
			}

			start := bits.TrailingZeros32(runs)
			seg.mask &^= need << start

			if seg.mask == 0 {
				h.unlink(s)
			}

			first := s*segmentPages + int32(start)
			h.top = max(h.top, h.address(first+int32(count)))

			return first, true
		}

		if !p.heapGrow(h) {
			return 0, false
		}
	}
}

// takeSegments takes the segments a run of more than a segment's pages
// needs, free in a row, those at the end of the heap or new ones; the
// last keeps the pages the run leaves.
func (p *Process) takeSegments(h *Heap, count uint32) (int32, bool) {
	k := int((count + segmentPages - 1) / segmentPages)
	at, row := -1, 0

	for i := range h.segments {
		if h.segments[i].mask != math.MaxUint32 {
			row = 0

			continue
		}

		if row++; row == k {
			at = i - k + 1

			break
		}
	}

	if at < 0 {
		for range k - row {
			if !p.heapGrow(h) {
				return 0, false
			}
		}

		at = len(h.segments) - k
	}

	left := count

	for s := at; s < at+k; s++ {
		n := min(left, segmentPages)
		seg := &h.segments[s]

		if n == segmentPages {
			seg.mask = 0
		} else {
			seg.mask &^= 1<<n - 1
		}

		if seg.mask == 0 && seg.linked {
			h.unlink(int32(s))
		}

		left -= n
	}

	first := int32(at * segmentPages)
	h.top = max(h.top, h.address(first+int32(count)))

	return first, true
}

// refill gives a class a current page: one of its pages with free slots,
// or a free page.
func (p *Process) refill(h *Heap, cls int) bool {
	c := &h.classes[cls]

	if page := c.partial; page != noPage {
		h.unlist(cls, page)
		c.current = page

		return true
	}

	page, ok := p.takePages(h, 1)

	if !ok {
		return false
	}

	pg := &h.pages[page]
	words := (int(classSlots[cls]) + 63) / 64
	pg.kind, pg.live, pg.bump, pg.freed = uint8(cls), 0, 0, pg.freed[:0]

	if cap(pg.used) >= words {
		pg.used = pg.used[:words]
		clear(pg.used)
	} else {
		pg.used = make([]uint64, words)
	}

	c.current = page

	return true
}

// mallocSmall allocates a block of a class.
func (p *Process) mallocSmall(h *Heap, cls int) uint32 {
	c := &h.classes[cls]

	for {
		if page := c.current; page != noPage {
			pg := &h.pages[page]
			slot := uint16(0)

			if n := len(pg.freed); n > 0 {
				slot = pg.freed[n-1]
				pg.freed = pg.freed[:n-1]
			} else if pg.bump < classSlots[cls] {
				slot = pg.bump
				pg.bump++
			} else {
				slot = math.MaxUint16
			}

			if slot != math.MaxUint16 {
				pg.used[slot/64] |= 1 << (slot % 64)
				pg.live++

				return h.address(page) + uint32(slot)*classSizes[cls]
			}
		}

		if !p.refill(h, cls) {
			return 0
		}
	}
}

// mallocRun allocates a run of pages.
func (p *Process) mallocRun(h *Heap, n uint64) uint32 {
	count := pagesFor(n)
	first, ok := p.takePages(h, count)

	if !ok {
		return 0
	}

	h.markRun(first, count)

	return h.address(first)
}

// malloc allocates n bytes of the heap, or returns 0.
func (p *Process) malloc(n uint32) uint32 {
	h := p.heap

	if h == nil {
		h = p.heapInit()
	}

	if n <= smallLimit {
		cls := classOf(n)
		at := p.mallocSmall(h, cls)

		if at != 0 {
			p.shadowBlock(at, n, classSizes[cls])
		}

		return at
	}

	if n > heapMax {
		return 0
	}

	at := p.mallocRun(h, uint64(n))

	if at != 0 {
		p.shadowBlock(at, n, pagesFor(uint64(n))<<heapPageShift)
	}

	return at
}

// usedBlock finds the block ptr is the start of: its page, slot and size,
// or false for another pointer.
func (p *Process) usedBlock(ptr uint32) (page int32, slot, size uint32, ok bool) {
	h := p.heap

	if h == nil || ptr < h.base || uint64(ptr-h.base)>>heapPageShift >= uint64(len(h.pages)) {
		return 0, 0, 0, false
	}

	page = int32((ptr - h.base) >> heapPageShift)
	off := (ptr - h.base) & (heapPageSize - 1)
	pg := &h.pages[page]

	switch {
	case pg.kind < smallClasses:
		size = classSizes[pg.kind]
		slot = off / size

		if off%size != 0 || slot >= uint32(pg.bump) || pg.used[slot/64]&(1<<(slot%64)) == 0 {
			return 0, 0, 0, false
		}
	case pg.kind == pageRun && off == 0:
		size = pg.run << heapPageShift
	default:
		return 0, 0, 0, false
	}

	return page, slot, size, true
}

// block is usedBlock of a pointer the program gave what; another pointer
// ends the process.
func (p *Process) block(ptr uint32, what string) (int32, uint32, uint32) {
	page, slot, size, ok := p.usedBlock(ptr)

	if !ok {
		p.fatal(fmt.Sprintf("%s(): invalid pointer %#x", what, ptr))
	}

	return page, slot, size
}

func (p *Process) free(ptr uint32) {
	if ptr == 0 {
		return
	}

	page, slot, size := p.block(ptr, "free")
	h := p.heap
	pg := &h.pages[page]
	p.shadowFree(ptr, size)

	if pg.kind == pageRun {
		h.releasePages(page, pg.run)

		return
	}

	cls := int(pg.kind)
	pg.used[slot/64] &^= 1 << (slot % 64)
	pg.live--
	pg.freed = append(pg.freed, uint16(slot))

	if page == h.classes[cls].current {
		return
	}

	if pg.live == 0 {
		if pg.listed {
			h.unlist(cls, page)
		}

		h.releasePages(page, 1)
	} else if !pg.listed {
		h.list(cls, page)
	}
}

// resizeRun makes a run hold n bytes in place: the pages past them go
// back, or the free pages after it, those the heap grows by at its end
// included, join it. It returns false when they are not free.
func (p *Process) resizeRun(page int32, n uint64) bool {
	h := p.heap
	run := h.pages[page].run
	count := pagesFor(n)

	if count <= run {
		p.shadowFree(h.address(page+int32(count)), (run-count)<<heapPageShift)
		h.releasePages(page+int32(count), run-count)
		h.pages[page].run = count

		return true
	}

	end := page + int32(count)

	for q := page + int32(run); q < end; q++ {
		if int(q) >= len(h.pages) {
			break
		}

		if h.pages[q].kind != pageFree {
			return false
		}
	}

	for int(end) > len(h.pages) {
		if !p.heapGrow(h) {
			return false
		}
	}

	for q := page + int32(run); q < end; q++ {
		s := q / segmentPages
		h.segments[s].mask &^= 1 << (q % segmentPages)

		if h.segments[s].mask == 0 && h.segments[s].linked {
			h.unlink(s)
		}
	}

	h.markRun(page, count)
	h.top = max(h.top, h.address(end))

	return true
}

func (p *Process) realloc(ptr, n uint32) uint32 {
	if ptr == 0 {
		return p.malloc(n)
	}

	if n == 0 {
		p.free(ptr)

		return 0
	}

	page, _, size := p.block(ptr, "realloc")
	run := p.heap.pages[page].kind == pageRun
	// What the program asked for, which the copy keeps to; without the
	// shadow map, the block.
	asked := p.blockAsked(ptr, size)

	if n <= size && !run {
		p.shadowBlock(ptr, n, size)

		return ptr
	}

	if run && n > smallLimit && n <= heapMax && p.resizeRun(page, uint64(n)) {
		p.shadowBlock(ptr, n, pagesFor(uint64(n))<<heapPageShift)

		return ptr
	}

	moved := p.malloc(n)

	if moved != 0 {
		keep := min(asked, n)
		copy(p.span(moved, keep), p.span(ptr, keep))
		p.free(ptr)
	}

	return moved
}

// fatal ends the process as abort does, with a message and the trace of its
// caller on the host's standard error.
func (p *Process) fatal(msg string) {
	p.crash(msg, callers(1))
	p.exitWith(int(syscall.SIGABRT))
}

// crash reports what ends the process on its standard error, where its
// own errors go, or the console's (report) when it has none it can write:
// the message, then the trace (trace.go), each line marked with the
// program and the pid, as the lines of processes interleave.
func (p *Process) crash(msg string, stack []uintptr) {
	name := "?"

	if p.image != nil && len(p.image.argv) > 0 {
		name = p.image.argv[0]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s[%d]: %s\n", name, p.pid, msg)

	for _, line := range traceLines(stack) {
		fmt.Fprintf(&b, "%s[%d]:     %s\n", name, p.pid, line)
	}

	if t, err := p.transferAt(2, false); err == nil {
		if _, err := t.write(p, []byte(b.String()), ioMode{quiet: true}); err == nil {
			return
		}
	}

	p.k.report(b.String())
}

const (
	mapShared    = 0x01
	mapAnonymous = 0x20
	protWrite    = 0x2
	madvDontneed = 4
)

// memalign allocates n bytes at a multiple of align, a power of two above
// 16: a slot of the class of the power of two that holds them, whose slots
// are at multiples of its size, a run, at multiples of a page, or a run
// with room to align its start, the pages before and after it given back.
func (p *Process) memalign(align, n uint32) uint32 {
	if align <= 16 {
		return p.malloc(n)
	}

	h := p.heap

	if h == nil {
		h = p.heapInit()
	}

	if n > heapMax {
		return 0
	}

	if size := max(n, align); size <= smallLimit {
		cls := classOf(1 << bits.Len32(size-1))
		at := p.mallocSmall(h, cls)

		if at != 0 {
			p.shadowBlock(at, n, classSizes[cls])
		}

		return at
	}

	count := pagesFor(uint64(n))

	if align <= heapPageSize {
		at := p.mallocRun(h, uint64(n))

		if at != 0 {
			p.shadowBlock(at, n, count<<heapPageShift)
		}

		return at
	}

	slack := align/heapPageSize - 1
	first, ok := p.takePages(h, count+slack)

	if !ok {
		return 0
	}

	at := uint64(h.address(first))
	aligned := (at + uint64(align) - 1) &^ (uint64(align) - 1)
	lead := uint32((aligned - at) >> heapPageShift)
	h.releasePages(first, lead)
	h.releasePages(first+int32(lead+count), slack-lead)
	h.markRun(first+int32(lead), count)
	p.shadowBlock(uint32(aligned), n, count<<heapPageShift)

	return uint32(aligned)
}

// mapping is a block mmap made: its length and, of a file, the file and
// its offset. A shared mapping that may be written keeps the contents it
// read, which msync, munmap and the end of the image compare with to write
// back what changed.
type mapping struct {
	length uint32
	file   Transfer
	offset int64
	read   []byte
}

// writeBack writes what changed of a shared mapping's range to its file,
// up to the file's end.
func (p *Process) writeBack(at uint32, m mapping, from, n uint32) error {
	if m.read == nil {
		return nil
	}

	var st syscall.Stat_t

	if err := statFile(p, m.file, &st); err != nil {
		return err
	}

	n = uint32(max(min(int64(n), st.Size-m.offset-int64(from)), 0))
	now := p.span(at+from, n)
	was := m.read[from : from+n]

	for i := 0; i < len(now); {
		if now[i] == was[i] {
			i++

			continue
		}

		j := i

		for j < len(now) && now[j] != was[j] {
			j++
		}

		if _, err := m.file.pwrite(p, now[i:j], m.offset+int64(from)+int64(i)); err != nil {
			return err
		}

		copy(was[i:j], now[i:j])
		i = j
	}

	return nil
}

// unmap writes a mapping back and frees it.
func (p *Process) unmap(at uint32) {
	m := p.mappings[at]
	p.writeBack(at, m, 0, m.length)
	delete(p.mappings, at)

	if m.file != nil {
		m.file.unref()
	}

	p.free(at)
}

// unmapAll ends the mappings of an image that ends: shared ones go back to
// their files. The memory goes with the image. runImage calls it before
// clearing the instance it installed, including after unwinding vfork.
func (p *Process) unmapAll() {
	for at, m := range p.mappings {
		p.writeBack(at, m, 0, m.length)

		if m.file != nil {
			m.file.unref()
		}
	}

	clear(p.mappings)
}

// mappingOf finds the mapping an address is in.
func (p *Process) mappingOf(addr uint32) (uint32, mapping, bool) {
	for at, m := range p.mappings {
		if addr-at < m.length {
			return at, m, true
		}
	}

	return 0, mapping{}, false
}

func (p *Process) libcMalloc(n uint32) uint32 {
	at := p.malloc(n)

	if at == 0 {
		p.setErrno(syscall.ENOMEM)
	}

	return at
}

func (p *Process) libcCalloc(n, size uint32) uint32 {
	total := uint64(n) * uint64(size)

	if total > math.MaxUint32 {
		p.setErrno(syscall.ENOMEM)

		return 0
	}

	at := p.malloc(uint32(total))

	if at == 0 {
		p.setErrno(syscall.ENOMEM)

		return 0
	}

	clear(p.span(at, uint32(total)))

	return at
}

func (p *Process) libcRealloc(ptr, n uint32) uint32 {
	at := p.realloc(ptr, n)

	if at == 0 && n != 0 {
		p.setErrno(syscall.ENOMEM)
	}

	return at
}

func (p *Process) libcMallopt(param, value uint32) uint32 {
	return 1
}

func (p *Process) libcGetpagesize() uint32 {
	return 4096
}

// mmap maps from the heap: anonymous memory zeroed, a file read in
// as a private copy. The process keeps the length of each mapping
// for munmap.
func (p *Process) libcMmap(addr, n, prot, flags, fd uint32, off uint64) uint32 {
	const failed = math.MaxUint32

	if n == 0 || off&4095 != 0 || flags&15 < 1 || flags&15 > 3 {
		p.setErrno(syscall.EINVAL)

		return failed
	}

	rounded := pageRound(n)

	if rounded > math.MaxUint32 {
		p.setErrno(syscall.ENOMEM)

		return failed
	}

	n = uint32(rounded)
	at := p.memalign(4096, n)

	if at == 0 {
		p.setErrno(syscall.ENOMEM)

		return failed
	}

	b := p.span(at, n)
	clear(b)
	m := mapping{length: n}

	if flags&mapAnonymous == 0 {
		// A file that is mapped is read at an offset: one that cannot be
		// is no device for it, as a pipe is none to Linux.
		obj, err := p.fd(fd)
		var f Transfer

		if err == nil {
			t := obj.asTransfer()

			if t == nil || obj.asSeeker() == nil {
				err = syscall.ENODEV
			} else {
				f = t
				_, err = f.pread(p, b, int64(off))
			}
		}

		if err != nil {
			p.free(at)
			p.setErrno(errnoOf(err))

			return failed
		}

		f.ref()
		m.file, m.offset = f, int64(off)

		if flags&mapShared != 0 && prot&protWrite != 0 {
			m.read = bytes.Clone(b)
		}
	}

	p.mappings[at] = m

	return at
}

// munmap frees a mapping it covers whole; of part of one it frees
// nothing, and the rest of the mapping stays.
func (p *Process) libcMunmap(addr, n uint32) uint32 {
	if n == 0 {
		return p.result(0, syscall.EINVAL)
	}

	if m, ok := p.mappings[addr]; ok && pageRound(n) >= pageRound(m.length) {
		p.unmap(addr)
	}

	return p.result(0, nil)
}

func (p *Process) libcMsync(addr, n, flags uint32) uint32 {
	const (
		async      = 1
		invalidate = 2
		sync       = 4
	)

	if addr&4095 != 0 || flags&^(async|invalidate|sync) != 0 || flags&(async|sync) == async|sync {
		return p.result(0, syscall.EINVAL)
	}

	at, m, ok := p.mappingOf(addr)

	if !ok {
		return p.result(0, syscall.ENOMEM)
	}

	n = min(n, m.length-(addr-at))

	return p.result(0, p.writeBack(at, m, addr-at, n))
}

// madvise's MADV_DONTNEED reads a private mapping of a file again,
// leaves a shared one and zeroes other memory, as Linux's; other
// advice changes nothing.
func (p *Process) libcMadvise(addr, n, advice uint32) uint32 {
	b := p.span(addr, n)

	if advice != madvDontneed {
		return p.result(0, nil)
	}

	at, m, ok := p.mappingOf(addr)

	switch {
	case !ok || m.file == nil:
		clear(b)
	case m.read == nil:
		in := b[:min(n, m.length-(addr-at))]
		clear(in)
		m.file.pread(p, in, m.offset+int64(addr-at))
	}

	return p.result(0, nil)
}

// Memory has no protection nor pages to lock.
func (p *Process) libcMprotect(addr, n, prot uint32) uint32 {
	p.span(addr, n)

	return p.result(0, nil)
}

func (p *Process) libcMemalign(align, n uint32) uint32 {
	if !alignment(align, false) {
		p.setErrno(syscall.EINVAL)

		return 0
	}

	at := p.memalign(align, n)

	if at == 0 {
		p.setErrno(syscall.ENOMEM)
	}

	return at
}

func (p *Process) libcAligned_alloc(align, n uint32) uint32 {
	if !alignment(align, false) {
		p.setErrno(syscall.EINVAL)

		return 0
	}

	at := p.memalign(align, n)

	if at == 0 {
		p.setErrno(syscall.ENOMEM)
	}

	return at
}

func (p *Process) libcValloc(n uint32) uint32 {
	at := p.memalign(4096, n)

	if at == 0 {
		p.setErrno(syscall.ENOMEM)
	}

	return at
}

// posix_memalign returns an error number.
func (p *Process) libcPosix_memalign(out, align, n uint32) uint32 {
	if !alignment(align, true) {
		return uint32(syscall.EINVAL)
	}

	at := p.memalign(align, n)

	if at == 0 {
		return uint32(syscall.ENOMEM)
	}

	p.put32(out, at)

	return 0
}

func (p *Process) libcMalloc_usable_size(ptr uint32) uint32 {
	if ptr == 0 {
		return 0
	}

	_, _, size := p.block(ptr, "malloc_usable_size")

	// Under the shadow map the usable bytes are the ones asked for: the
	// rest is a red zone.
	return p.blockAsked(ptr, size)
}

// The heap gives no memory back.
func (p *Process) libcMalloc_trim(pad uint32) uint32 {
	return 0
}

func (p *Process) libcReallocarray(ptr, n, size uint32) uint32 {
	total := uint64(n) * uint64(size)

	if total > math.MaxUint32 {
		p.setErrno(syscall.ENOMEM)

		return 0
	}

	at := p.realloc(ptr, uint32(total))

	if at == 0 && total != 0 {
		p.setErrno(syscall.ENOMEM)
	}

	return at
}

func (p *Process) libcFree(ptr uint32) {
	p.free(ptr)
}

func (p *Process) libcMlock(addr, n uint32) uint32 {
	return p.result(0, nil)
}

func (p *Process) libcMunlock(addr, n uint32) uint32 {
	return p.result(0, nil)
}

func (p *Process) libcMlockall(flags uint32) uint32 {
	return p.result(0, nil)
}

func (p *Process) libcMunlockall() uint32 {
	return p.result(0, nil)
}

// An asan object, one vm compiled with --asan, checks every access of its
// memory against the shadow map instead of the memory's bounds (aot.cpp
// lowererAsanAddress): a byte of the map for each byte of memory, shadowOK
// where the program may read and write and another value, which tells why
// not, where it may not. The map is the kernel's view of the memory. The
// stack and the data, from the first KiB to __heap_base, are accessible
// from the start; a block of the heap is accessible from when the allocator
// hands it out, for the bytes the program asked for, with the rest of the
// block a red zone, and poisoned from when it is freed until a block takes
// its place; the rest, the first KiB, the heap's free pages and the memory
// past the heap, was never allocated. The kernel checks its own accesses for
// the program's calls against the map too (span), so a read into a freed
// buffer is caught as the code's store would be. A program runs with the
// map only under `vm run --asan`, whose kernel compiles every program it
// runs so.

const (
	shadowFree    = 0x00 // never allocated: the first KiB, free pages, past the memory
	shadowRedzone = 0xfa // past what the program asked for, up to the block's size
	shadowFreed   = 0xfd // a freed block
	shadowOK      = 0xff
)

// shadow is the process's shadow map, nil but for an asan object.
func (p *Process) shadow() []byte {
	if p.inst == nil || p.inst.memory == nil {
		return nil
	}

	return p.inst.memory.shadow
}

// fill sets every byte of s to v.
func fill(s []byte, v byte) {
	if len(s) == 0 {
		return
	}

	s[0] = v

	for n := 1; n < len(s); n *= 2 {
		copy(s[n:], s[:n])
	}
}

// asanStart marks the memory before the image's init runs, which places
// the data segments: all of the initial memory but the first KiB, as the
// kernel does not know __heap_base yet.
func (p *Process) asanStart() {
	if sh := p.shadow(); sh != nil {
		fill(sh[boundSlack:p.inst.memory.pages*pageSize], shadowOK)
	}
}

// asanInit marks the memory as the program starts: the stack and the data
// up to __heap_base accessible, the heap's pages never allocated.
func (p *Process) asanInit() {
	sh := p.shadow()

	if sh == nil {
		return
	}

	size := p.inst.memory.pages * pageSize
	base, ok := p.inst.globalExport("__heap_base")

	if ok && uint64(base) < size {
		clear(sh[base:size])
	}
}

// shadowBlock marks a block the allocator handed out: n bytes accessible
// at at, the rest of its size bytes a red zone.
func (p *Process) shadowBlock(at, n, size uint32) {
	if sh := p.shadow(); sh != nil {
		fill(sh[uint64(at):uint64(at)+uint64(n)], shadowOK)
		fill(sh[uint64(at)+uint64(n):uint64(at)+uint64(size)], shadowRedzone)
	}
}

// shadowFree marks a block of size bytes at at freed.
func (p *Process) shadowFree(at, size uint32) {
	if sh := p.shadow(); sh != nil {
		fill(sh[uint64(at):uint64(at)+uint64(size)], shadowFreed)
	}
}

// blockAsked is how many bytes the program asked for of a block of size
// bytes at at: the accessible ones from its start, or all of it without
// the shadow map.
func (p *Process) blockAsked(at, size uint32) uint32 {
	sh := p.shadow()

	if sh == nil {
		return size
	}

	return uint32(okBytes(sh[uint64(at) : uint64(at)+uint64(size)]))
}

// okBytes counts the accessible bytes at the start of a range of the map.
func okBytes(s []byte) int {
	n := len(s)

	for len(s) >= 8 && binary.LittleEndian.Uint64(s) == math.MaxUint64 {
		s = s[8:]
	}

	for len(s) > 0 && s[0] == shadowOK {
		s = s[1:]
	}

	return n - len(s)
}

// asanCheck ends the process as a trap would when the n bytes at at are
// not all accessible: the kernel is about to read or write them for a
// call of the program. The range is within the memory already.
func (p *Process) asanCheck(at, n uint32) {
	sh := p.shadow()

	if sh == nil || n == 0 {
		return
	}

	ok := okBytes(sh[uint64(at) : uint64(at)+uint64(n)])

	if ok != int(n) {
		p.fatal(p.asanReport(&Trap{code: trapAsan, addr: uint64(at) + uint64(ok), kind: asanRange}))
	}
}

// asanReport describes a trap of an asan check: the access, and what the
// shadow map says of its first inaccessible byte.
func (p *Process) asanReport(t *Trap) string {
	bad := t.addr

	if sh := p.shadow(); sh != nil && t.width > 1 && bad < uint64(len(sh)) {
		bad += uint64(okBytes(sh[bad:min(bad+uint64(t.width), uint64(len(sh)))]))
	}

	where := ": "

	if bad != t.addr {
		where = fmt.Sprintf(", whose byte %#x is ", bad)
	}

	return "asan: " + t.access() + where + p.asanDescribe(bad)
}

// asanDescribe tells what the shadow map says of an address: the first KiB,
// past the memory, a freed block, the red zone past what a block's program
// asked for, or memory never allocated, of the heap or elsewhere.
func (p *Process) asanDescribe(addr uint64) string {
	sh := p.shadow()
	size := p.inst.memory.pages * pageSize

	switch {
	case sh == nil:
		return "no shadow map"
	case addr >= size:
		return fmt.Sprintf("past the memory's end %#x", size)
	case addr < boundSlack:
		return "in the first KiB, where nothing is"
	}

	start, asked, blockSize, ok := p.blockAround(uint32(addr))

	switch sh[addr] {
	case shadowOK:
		return "accessible"
	case shadowRedzone:
		// Only shadowBlock creates red zones for live allocations; freeing
		// changes the whole block to shadowFreed, including its red zone.
		return fmt.Sprintf("past the end of the %d-byte block at %#x", asked, start)
	case shadowFreed:
		if ok {
			return fmt.Sprintf("in the freed %d-byte block at %#x", blockSize, start)
		}

		return "in freed memory"
	}

	if h := p.heap; h != nil && addr >= uint64(h.base) {
		return "in free memory of the heap"
	}

	return "in memory never allocated"
}

// blockAround finds the block of the heap addr is in, used or freed: its
// start, the bytes the program asked for, as the map has them, and its
// size.
func (p *Process) blockAround(addr uint32) (start, asked, size uint32, ok bool) {
	h := p.heap

	if h == nil || addr < h.base || uint64(addr-h.base)>>heapPageShift >= uint64(len(h.pages)) {
		return 0, 0, 0, false
	}

	page := int32((addr - h.base) >> heapPageShift)

	for h.pages[page].kind == pageTail && page > 0 {
		page--
	}

	pg := &h.pages[page]

	switch {
	case pg.kind < smallClasses:
		size = classSizes[pg.kind]
		start = h.address(page) + (addr-h.address(page))/size*size
	case pg.kind == pageRun:
		size = pg.run << heapPageShift
		start = h.address(page)
	default:
		return 0, 0, 0, false
	}

	return start, p.blockAsked(start, size), size, true
}

// fd is the object of a descriptor.
func (p *Process) fd(fd uint32) (Object, error) {
	d, ok := p.files.at(int(fd))

	if !ok {
		return nil, syscall.EBADF
	}

	return d.obj, nil
}

// fileAt is the File of a descriptor: what any descriptor's object is.
func (p *Process) fileAt(fd uint32) (File, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if f := obj.asFile(); f != nil {
		return f, nil
	}

	return nil, syscall.EBADF
}

// transferAt is the Transfer of a descriptor: read on a directory is
// EISDIR, as Linux has it, write and the rest EBADF.
func (p *Process) transferAt(fd uint32, read bool) (Transfer, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if t := obj.asTransfer(); t != nil {
		return t, nil
	}

	if obj.asDir() != nil && read {
		return nil, syscall.EISDIR
	}

	return nil, syscall.EBADF
}

func (p *Process) seekerAt(fd uint32) (Seeker, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if s := obj.asSeeker(); s != nil {
		return s, nil
	}

	return nil, syscall.ESPIPE
}

func (p *Process) dirAt(fd uint32) (Dir, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if d := obj.asDir(); d != nil {
		return d, nil
	}

	return nil, syscall.ENOTDIR
}

func (p *Process) attrsAt(fd uint32) (Attrs, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if a := obj.asAttrs(); a != nil {
		return a, nil
	}

	return nil, syscall.EINVAL
}

func (p *Process) socketAt(fd uint32) (Socket, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if s := obj.asSocket(); s != nil {
		return s, nil
	}

	return nil, syscall.ENOTSOCK
}

func (p *Process) lockerAt(fd uint32) (Locker, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if l := obj.asLocker(); l != nil {
		return l, nil
	}

	return nil, syscall.EBADF
}

// install puts an object at the lowest free descriptor from min; the
// descriptor takes the reference.
func (p *Process) install(obj Object, min int, cloexec bool) (int, error) {
	if fd := p.files.lowest(min); fd < p.fdLimit() {
		p.files.put(fd, obj, cloexec)

		return fd, nil
	}

	obj.unref()

	return -1, syscall.EMFILE
}

func (p *Process) fdLimit() int {
	return int(min64(p.limits[rlimitNofile].cur, 1<<20))
}

// descriptorFree is EMFILE when the process has no descriptor free: what
// takes one checks first, as Linux takes one before the rest of the call,
// so open makes no file and accept takes no connection then.
func (p *Process) descriptorFree() error {
	if p.files.lowest(0) >= p.fdLimit() {
		return syscall.EMFILE
	}

	return nil
}

func min64(a, b uint64) uint64 {
	return min(a, b)
}

func (p *Process) closeFd(fd int) bool {
	obj := p.files.take(fd)

	if obj == nil {
		return false
	}

	obj.unref()

	return true
}

// dupTo makes new refer to old's object, closing what new referred to.
func (p *Process) dupTo(old, new uint32, cloexec bool) (int, error) {
	obj, err := p.fd(old)

	if err != nil {
		return 0, err
	}

	if uint64(new) >= p.limits[rlimitNofile].cur {
		return 0, syscall.EBADF
	}

	p.placeAt(obj, new, cloexec)

	return int(new), nil
}

// placeAt makes a descriptor refer to an object, closing what it referred
// to; the descriptor is below the process's limit.
func (p *Process) placeAt(obj Object, fd uint32, cloexec bool) {
	obj.ref()
	p.closeFd(int(fd))
	p.files.put(int(fd), obj, cloexec)
}

// idle waits for timeout milliseconds, forever when negative, or until a
// signal interrupts: then the error is EINTR, after the handlers ran when
// all of them restart calls and restart is set.
func (p *Process) idle(timeout int, restart bool) error {
	_, err := p.waitObjects(nil, timeout, restart)

	return err
}

// pollEntry is an object and the events asked of it, revents those ready.
type pollEntry struct {
	obj     Object
	events  int16
	revents int16
	sub     *subscription
}

// waitObjects waits until an object is ready for its events: it
// subscribes the process to them all and asks them all, then, as long as
// none is ready, asks one on its event, until the deadline of timeout
// milliseconds. A signal the call took interrupts it, in this wait or in
// one before it: then the error is EINTR, unless its handlers all asked
// to restart calls and restart is set. It returns how many are ready.
func (p *Process) waitObjects(entries []pollEntry, timeout int, restart bool) (int, error) {
	for i := range entries {
		entries[i].sub = p.subscription(uint64(i))
		entries[i].obj.subscribe(entries[i].sub, entries[i].events)
	}

	defer func() {
		for i := range entries {
			entries[i].obj.unsubscribe(entries[i].sub)
			p.release(entries[i].sub)
		}
	}()

	ready := 0
	_, err := p.wait(deadline(timeout), func(e *event) (bool, error) {
		switch {
		case e == nil || e.kind == eventSignal:
			// The first look, or after the handlers: all of it.
		case e.kind != eventChanged || e.id >= uint64(len(entries)) || e != &entries[e.id].sub.event:
			return false, nil
		case entries[e.id].obj.poll(entries[e.id].events) == 0:
			return false, nil
		}

		if ready = pollEntries(entries); ready > 0 {
			return true, nil
		}

		return p.interrupts(restart), syscall.EINTR
	})

	return ready, err
}

// pollEntries asks every entry's object for its events and counts the
// ready ones.
func pollEntries(entries []pollEntry) int {
	ready := 0

	for i := range entries {
		entries[i].revents = entries[i].obj.poll(entries[i].events)

		if entries[i].revents != 0 {
			ready++
		}
	}

	return ready
}

// waitObject waits until one object is ready for events.
func (p *Process) waitObject(obj Object, events int16, timeout int, restart bool) (int, error) {
	return p.waitObjects([]pollEntry{{obj: obj, events: events}}, timeout, restart)
}

// The calls on a descriptor: each finds the role its object plays
// (object.go) and leaves the rest to it.

// ioSpans reads a guest iovec array as spans of memory, which stays where
// it is during the call.
func (p *Process) ioSpans(iovs uint32, count uint32) ([][]byte, error) {
	return p.ioSpansIn(nil, iovs, count)
}

// ioSpansIn is ioSpans into out, when they fit there: a caller with room
// of its own for a few allocates none.
func (p *Process) ioSpansIn(out [][]byte, iovs uint32, count uint32) ([][]byte, error) {
	if count > 1024 {
		return nil, syscall.EINVAL
	}

	table := p.span(iovs, 8*count)

	if int(count) > cap(out) {
		out = make([][]byte, 0, count)
	}

	for i := range count {
		base, n := binary.LittleEndian.Uint32(table[8*i:]), binary.LittleEndian.Uint32(table[8*i+4:])
		b := p.span(base, n)

		if n > 0 {
			out = append(out, b)
		}
	}

	return out, nil
}

// gather copies a guest iovec array's bytes into one buffer of their
// length, made once: the spans are checked first, then EMSGSIZE past max
// bytes, before any is copied.
func (p *Process) gather(iovs, count uint32, max int) ([]byte, error) {
	return p.gatherIn(nil, iovs, count, max)
}

// gatherIn is gather into small, when the bytes fit there.
func (p *Process) gatherIn(small []byte, iovs, count uint32, max int) ([]byte, error) {
	if count > 1024 {
		return nil, syscall.EINVAL
	}

	table := p.span(iovs, 8*count)
	total := 0

	for i := range count {
		total += len(p.span(binary.LittleEndian.Uint32(table[8*i:]), binary.LittleEndian.Uint32(table[8*i+4:])))
	}

	if total > max {
		return nil, syscall.EMSGSIZE
	}

	data := small

	if total <= len(small) {
		data = small[:total]
	} else {
		data = make([]byte, total)
	}

	at := 0

	for i := range count {
		at += copy(data[at:], p.span(binary.LittleEndian.Uint32(table[8*i:]), binary.LittleEndian.Uint32(table[8*i+4:])))
	}

	return data, nil
}

// iovecs reads a guest iovec array as host iovecs into memory.
func (p *Process) iovecs(iovs uint32, count uint32) ([]syscall.Iovec, error) {
	spans, err := p.ioSpans(iovs, count)

	if err != nil {
		return nil, err
	}

	out := make([]syscall.Iovec, 0, len(spans))

	for _, b := range spans {
		out = append(out, syscall.Iovec{Base: &b[0], Len: uint64(len(b))})
	}

	return out, nil
}

// vectored runs readv or writev, or with an offset preadv or pwritev: the
// host's on a host object, else one transfer of the gathered bytes, which
// a read scatters.
func (p *Process) vectored(fd, iovs, count uint32, events int16, call uintptr, off uint64) uint32 {
	t, err := p.transferAt(fd, events == pollIn)

	if err != nil {
		return p.result(0, err)
	}

	spans, err := p.ioSpans(iovs, count)

	if err != nil || len(spans) == 0 {
		return p.result(0, err)
	}

	total := 0

	for _, b := range spans {
		total += len(b)
	}

	buf := make([]byte, 0, total)
	positioned := call == syscall.SYS_PREADV || call == syscall.SYS_PWRITEV

	if events == pollOut {
		for _, b := range spans {
			buf = append(buf, b...)
		}

		if positioned {
			return p.result(t.pwrite(p, buf, int64(off)))
		}

		return p.result(t.write(p, buf, ioMode{}))
	}

	buf = buf[:total]
	var n int

	if positioned {
		n, err = t.pread(p, buf, int64(off))
	} else {
		n, err = t.read(p, buf, ioMode{})
	}

	at := 0

	for _, b := range spans {
		if at >= n {
			break
		}

		at += copy(b, buf[at:n])
	}

	return p.result(n, err)
}

// recordLock runs F_GETLK, F_SETLK or F_SETLKW with a guest struct flock.
func (p *Process) recordLock(l Locker, cmd, at uint32) error {
	lk := syscall.Flock_t{Type: int16(p.u16(at)), Whence: int16(p.u16(at + 2)), Start: int64(p.u64(at + 8)), Len: int64(p.u64(at + 16))}

	if cmd == fGetlk {
		if err := l.recordLock(p, fOfdGetlk, &lk); err != nil {
			return err
		}

		p.put16(at, uint16(lk.Type))
		p.put16(at+2, uint16(lk.Whence))
		p.put64(at+8, uint64(lk.Start))
		p.put64(at+16, uint64(lk.Len))
		p.put32(at+24, uint32(lk.Pid))

		return nil
	}

	if cmd == fSetlkw {
		return l.recordLock(p, fOfdSetlkw, &lk)
	}

	return l.recordLock(p, fOfdSetlk, &lk)
}

// fdSet is a guest fd_set: 1024 bits in words of 32.
type fdSet struct {
	at uint32
}

func (s fdSet) has(p *Process, fd int) bool {
	return s.at != 0 && p.u32(s.at+uint32(fd/32*4))&(1<<(fd%32)) != 0
}

func (s fdSet) set(p *Process, fd int) {
	p.put32(s.at+uint32(fd/32*4), p.u32(s.at+uint32(fd/32*4))|1<<(fd%32))
}

const (
	sysSyncfs        = 306
	sysCopyFileRange = 326
	fdSetsize        = 1024
	pollInSet        = pollIn | pollHup | pollErr
	pollOutSet       = pollOut | pollErr
)

// selectFds waits as select does, with a timeout in milliseconds, and
// returns how many bits of the sets it left set.
func (p *Process) selectFds(n uint32, sets [3]fdSet, timeout int) (int, error) {
	if n > fdSetsize {
		return 0, syscall.EINVAL
	}

	var entries []pollEntry
	var fds []int

	for fd := range int(n) {
		events := int16(0)

		for i, ev := range []int16{pollIn, pollOut, pollPri} {
			if sets[i].has(p, fd) {
				events |= ev
			}
		}

		if events == 0 {
			continue
		}

		obj, err := p.fd(uint32(fd))

		if err != nil {
			return 0, err
		}

		entries = append(entries, pollEntry{obj: obj, events: events})
		fds = append(fds, fd)
	}

	ready, err := p.waitObjects(entries, timeout, false)

	if err != nil {
		return 0, err
	}

	for _, s := range sets {
		if s.at != 0 {
			clear(p.span(s.at, (n+31)/32*4))
		}
	}

	count := 0

	for i, e := range entries {
		if ready == 0 {
			break
		}

		// Each descriptor was validated above and remains in this process's
		// table throughout the non-restarting wait. A signal returns EINTR
		// before a handler could close one and let the wait poll it again.
		for k, mask := range []int16{pollInSet, pollOutSet, pollPri} {
			if e.events&[]int16{pollIn, pollOut, pollPri}[k] != 0 && e.revents&mask != 0 {
				sets[k].set(p, fds[i])
				count++
			}
		}
	}

	return count, nil
}

// withMask runs a wait with the signal mask of pselect or ppoll, when
// mask is not 0: a signal the wait took runs its handler before the old
// mask returns.
func (p *Process) withMask(mask uint32, wait func() (int, error)) (int, error) {
	if mask == 0 {
		return wait()
	}

	old := p.setMask(p.u64(mask))
	n, err := wait()

	if err == syscall.EINTR {
		p.handleSignals()
	}

	p.setMask(old)

	return n, err
}

func (p *Process) libcRead(fd, buf, n uint32) uint32 {
	t, err := p.transferAt(fd, true)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(t.read(p, p.span(buf, n), ioMode{}))
}

func (p *Process) libcWrite(fd, buf, n uint32) uint32 {
	t, err := p.transferAt(fd, false)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(t.write(p, p.span(buf, n), ioMode{}))
}

func (p *Process) libcPread(fd, buf, n uint32, off uint64) uint32 {
	t, err := p.transferAt(fd, true)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(t.pread(p, p.span(buf, n), int64(off)))
}

func (p *Process) libcPwrite(fd, buf, n uint32, off uint64) uint32 {
	t, err := p.transferAt(fd, false)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(t.pwrite(p, p.span(buf, n), int64(off)))
}

// copy_file_range copies between two files of the host, which the host
// copies, between a memfd and another file (copyWindow), or between files
// of servers (copyFiles); an offset given is read from the guest's memory
// and written back.
func (p *Process) libcCopy_file_range(in, offIn, out, offOut, n, flags uint32) uint32 {
	src, err := p.fileAt(in)

	if err != nil {
		return p.result(0, err)
	}

	dst, err := p.fileAt(out)

	if err != nil {
		return p.result(0, err)
	}

	fromWindow := src.asMemfd() != nil
	toWindow := dst.asMemfd() != nil

	if flags != 0 {
		return p.result(0, syscall.EINVAL)
	}

	var from, to *int64

	if offIn != 0 {
		from = new(int64)
		*from = int64(p.u64(offIn))
	}

	if offOut != 0 {
		to = new(int64)
		*to = int64(p.u64(offOut))
	}

	var r int

	if fromWindow || toWindow {
		r, err = copyWindow(p, src, dst, from, to, int(n))
	} else {
		r, err = copyFiles(p, src, dst, from, to, int(n))
	}

	if err != nil {
		return p.result(0, err)
	}

	if from != nil {
		p.put64(offIn, uint64(*from))
	}

	if to != nil {
		p.put64(offOut, uint64(*to))
	}

	return p.result(r, nil)
}

// copyFiles copies up to n bytes between files of servers, through a
// buffer of the kernel's, as far as src has them: from its position, or
// *from, to dst's, or *to, which move by what is copied. Both are regular
// files, src open to read and dst to write, not to append: Linux's errors
// else.
func copyFiles(p *Process, src, dst File, from, to *int64, n int) (int, error) {
	in, out := src.asTransfer(), dst.asTransfer()
	inFlags, _ := src.flags()
	outFlags, _ := dst.flags()

	if in == nil || out == nil || inFlags&syscall.O_ACCMODE == syscall.O_WRONLY || outFlags&syscall.O_ACCMODE == syscall.O_RDONLY ||
		outFlags&syscall.O_APPEND != 0 {
		return 0, syscall.EBADF
	}

	for _, f := range []File{src, dst} {
		var st syscall.Stat_t

		if err := f.stat(p, &st); err != nil {
			return 0, err
		}

		if st.Mode&syscall.S_IFMT == syscall.S_IFDIR {
			return 0, syscall.EISDIR
		}

		if st.Mode&syscall.S_IFMT != syscall.S_IFREG {
			return 0, syscall.EINVAL
		}
	}

	buf := make([]byte, min(n, packetMax))
	done := 0

	for done < n {
		chunk := buf[:min(len(buf), n-done)]
		var got int
		var err error

		if from != nil {
			got, err = in.pread(p, chunk, *from)
		} else {
			got, err = in.read(p, chunk, ioMode{})
		}

		if err != nil || got == 0 {
			if done > 0 {
				err = nil
			}

			return done, err
		}

		var put int

		if to != nil {
			put, err = out.pwrite(p, chunk[:got], *to)
			*to += int64(put)
		} else {
			put, err = out.write(p, chunk[:got], ioMode{})
		}

		if from != nil {
			*from += int64(put)
		}

		done += put

		if err != nil || put < got {
			if done > 0 {
				err = nil
			}

			return done, err
		}
	}

	return done, nil
}

func (p *Process) libcFdatasync(fd uint32) uint32 {
	n, err := p.attrsAt(fd)

	if err == nil {
		err = n.sync(p, true)
	}

	return p.result(0, err)
}

func (p *Process) libcSync() {
	syscall.Sync()
}

// syncfs has nothing to sync: a server's file system syncs as it writes.
func (p *Process) libcSyncfs(fd uint32) uint32 {
	_, err := p.fd(fd)

	return p.result(0, err)
}

func (p *Process) libcFlock(fd, op uint32) uint32 {
	l, err := p.lockerAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(0, l.flock(p, int(op)))
}

// lockf locks from the file's offset, as fcntl's write locks.
func (p *Process) libcLockf(fd, cmd uint32, n uint64) uint32 {
	l, err := p.lockerAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	lk := syscall.Flock_t{Type: fWrlck, Whence: 1, Len: int64(n)}

	switch cmd {
	case 0:
		lk.Type = fUnlck
		err = l.recordLock(p, fOfdSetlk, &lk)
	case 1:
		err = l.recordLock(p, fOfdSetlkw, &lk)
	case 2:
		err = l.recordLock(p, fOfdSetlk, &lk)
	case 3:
		lk.Type = fRdlck

		if err = l.recordLock(p, fOfdGetlk, &lk); err == nil && lk.Type != fUnlck {
			err = syscall.EACCES
		}
	default:
		err = syscall.EINVAL
	}

	return p.result(0, err)
}

// posix_fadvise and posix_fallocate return an error number. A file of a
// server takes advice as a file system of Linux's without a page cache
// does, all of it ignored, and has the space fallocate asks for as its
// size reaches the end of it, as a file of tmpfs has.
func (p *Process) libcPosix_fadvise(fd uint32, off, n uint64, advice uint32) uint32 {
	obj, err := p.fileAt(fd)

	if err != nil {
		return uint32(errnoOf(err))
	}

	var st syscall.Stat_t

	if err := obj.stat(p, &st); err != nil {
		return uint32(errnoOf(err))
	}

	switch {
	case st.Mode&syscall.S_IFMT == syscall.S_IFIFO:
		return uint32(syscall.ESPIPE)
	case int64(n) < 0 || advice > posixFadvNoreuse:
		return uint32(syscall.EINVAL)
	}

	return 0
}

const posixFadvNoreuse = 5

func (p *Process) libcPosix_fallocate(fd uint32, off, n uint64) uint32 {
	obj, err := p.fileAt(fd)

	if err != nil {
		return uint32(errnoOf(err))
	}

	return uint32(errnoOf(p.fallocate(obj, int64(off), int64(n))))
}

// fallocate of a file of a server, with Linux's errors in Linux's order.
func (p *Process) fallocate(obj File, off, n int64) error {
	if off < 0 || n <= 0 {
		return syscall.EINVAL
	}

	if fl, err := obj.flags(); err != nil || fl&syscall.O_ACCMODE == syscall.O_RDONLY {
		return syscall.EBADF
	}

	var st syscall.Stat_t

	if err := obj.stat(p, &st); err != nil {
		return err
	}

	a := obj.asAttrs()

	switch {
	case st.Mode&syscall.S_IFMT == syscall.S_IFIFO:
		return syscall.ESPIPE
	case st.Mode&syscall.S_IFMT == syscall.S_IFDIR:
		return syscall.EISDIR
	case st.Mode&syscall.S_IFMT != syscall.S_IFREG || a == nil:
		return syscall.ENODEV
	case off > math.MaxInt64-n:
		return syscall.EFBIG
	case off+n <= st.Size:
		return nil
	}

	return a.truncate(p, off+n)
}

// select writes back the time left, as Linux's.
func (p *Process) libcSelect(n, r, w, e, tv uint32) uint32 {
	timeout := -1
	var d time.Duration

	if tv != 0 {
		var err error

		if d, err = p.timeval(tv); err != nil {
			return p.result(0, err)
		}

		timeout = waitMs(d)
	}

	start := time.Now()
	count, err := p.selectFds(n, [3]fdSet{{r}, {w}, {e}}, timeout)

	if tv != 0 && err != syscall.EINVAL {
		p.putTimeval(tv, max(d-time.Since(start), 0))
	}

	return p.result(count, err)
}

func (p *Process) libcPselect(n, r, w, e, ts, mask uint32) uint32 {
	timeout := -1

	if ts != 0 {
		d, err := p.timespec(ts)

		if err != nil {
			return p.result(0, err)
		}

		timeout = waitMs(d)
	}

	return p.result(p.withMask(mask, func() (int, error) {
		return p.selectFds(n, [3]fdSet{{r}, {w}, {e}}, timeout)
	}))
}

func (p *Process) libcLseek(fd uint32, off uint64, whence uint32) uint64 {
	s, err := p.seekerAt(fd)

	if err != nil {
		return p.result64(0, err)
	}

	return p.result64(s.seek(p, int64(off), int(whence)))
}

func (p *Process) libcClose(fd uint32) uint32 {
	if !p.closeFd(int(fd)) {
		return p.result(0, syscall.EBADF)
	}

	return p.result(0, nil)
}

func (p *Process) libcDup(fd uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	obj.ref()

	return p.result(p.install(obj, 0, false))
}

func (p *Process) libcDup2(old, new uint32) uint32 {
	if _, err := p.fd(old); err == nil && old == new {
		return p.result(int(new), nil)
	}

	return p.result(p.dupTo(old, new, false))
}

func (p *Process) libcDup3(old, new, flags uint32) uint32 {
	if old == new || flags&^syscall.O_CLOEXEC != 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(p.dupTo(old, new, flags&syscall.O_CLOEXEC != 0))
}

func (p *Process) libcCfgetospeed(at uint32) uint32 {
	return p.u32(at+8) & cbaud
}

func (p *Process) libcCfgetispeed(at uint32) uint32 {
	return p.u32(at+8) & cbaud
}

// cfsetispeed's 0 is the output speed, as POSIX has it.
func (p *Process) libcCfsetispeed(at, speed uint32) uint32 {
	if speed == 0 {
		return 0
	}

	return p.cfsetspeed(at, speed)
}

func (p *Process) libcFsync(fd uint32) uint32 {
	n, err := p.attrsAt(fd)

	if err == nil {
		err = n.sync(p, false)
	}

	return p.result(0, err)
}

func (p *Process) libcFtruncate(fd uint32, size uint64) uint32 {
	n, err := p.attrsAt(fd)

	if err == nil {
		err = n.truncate(p, int64(size))
	}

	return p.result(0, err)
}

// statFile reads a file's metadata for a guest operation on a
// descriptor; what is no File has none: EBADF.
func statFile(p *Process, obj Object, st *syscall.Stat_t) error {
	if p.chaosFail("file-stat") {
		return syscall.EIO
	}

	f := obj.asFile()

	if f == nil {
		return syscall.EBADF
	}

	return f.stat(p, st)
}

func (p *Process) libcFstat(fd, at uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	var st syscall.Stat_t

	if err := statFile(p, obj, &st); err != nil {
		return p.result(0, err)
	}

	p.putStat(at, &st)

	return p.result(0, nil)
}

func (p *Process) libcFchmod(fd, mode uint32) uint32 {
	n, err := p.attrsAt(fd)

	if err == nil {
		err = n.chmod(p, mode&0o7777)
	}

	return p.result(0, err)
}

func (p *Process) libcFchown(fd, uid, gid uint32) uint32 {
	n, err := p.attrsAt(fd)

	if err == nil {
		err = n.chown(p, int(int32(uid)), int(int32(gid)))
	}

	return p.result(0, err)
}

func (p *Process) libcFutimes(fd, times uint32) uint32 {
	n, err := p.attrsAt(fd)

	if err == nil {
		err = n.utimes(p, p.timevals(times))
	}

	return p.result(0, err)
}

func (p *Process) libcFutimens(fd, times uint32) uint32 {
	n, err := p.attrsAt(fd)

	if err == nil {
		err = n.utimes(p, p.timespecs(times))
	}

	return p.result(0, err)
}

// varArg is the i-th int of a variadic call's argument area.
func (p *Process) varArg(va uint32, i uint32) uint32 {
	return p.u32(va + 4*i)
}

const (
	fDupfd        = 0
	fGetfd        = 1
	fSetfd        = 2
	fGetfl        = 3
	fSetfl        = 4
	fDupfdCloexec = 1030
)

func (p *Process) libcFcntl(fd, cmd, va uint32) uint32 {
	obj, err := p.fileAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	switch cmd {
	case fDupfd, fDupfdCloexec:
		min := p.varArg(va, 0)

		if uint64(min) >= p.limits[rlimitNofile].cur {
			return p.result(0, syscall.EINVAL)
		}

		obj.ref()

		return p.result(p.install(obj, int(min), cmd == fDupfdCloexec))
	case fGetfd:
		if p.files.slots[fd].cloexec {
			return p.result(1, nil)
		}

		return p.result(0, nil)
	case fSetfd:
		p.files.slots[fd].cloexec = p.varArg(va, 0)&1 != 0

		return p.result(0, nil)
	case fGetfl:
		return p.result(obj.flags())
	case fGetlk, fSetlk, fSetlkw:
		l, err := p.lockerAt(fd)

		if err != nil {
			return p.result(0, err)
		}

		return p.result(0, p.recordLock(l, cmd, p.varArg(va, 0)))
	case fSetfl:
		return p.result(0, obj.setFlags(int(p.varArg(va, 0))))
	}

	return p.result(0, syscall.EINVAL)
}

// struct termios flags and control characters, the guest's termios.h.
const (
	ignbrk = 0o000001
	brkint = 0o000002
	parmrk = 0o000010
	istrip = 0o000040
	inlcr  = 0o000100
	igncr  = 0o000200
	icrnl  = 0o000400
	ixon   = 0o002000
	opost  = 0o000001
	csize  = 0o000060
	cs8    = 0o000060
	parenb = 0o000400
	echo   = 0o000010
	echonl = 0o000100
	icanon = 0o000002
	isig   = 0o000001
	iexten = 0o100000
	vtime  = 5
	vmin   = 6
)

// cbaud is the speed of termios.h's c_cflag.
const cbaud = 0o010017

// cfsetspeed sets the speed of a guest's struct termios, as musl's: both
// speeds are c_cflag's.
func (p *Process) cfsetspeed(at, speed uint32) uint32 {
	if speed&^cbaud != 0 {
		p.setErrno(syscall.EINVAL)

		return ^uint32(0)
	}

	p.put32(at+8, p.u32(at+8)&^cbaud|speed)

	return 0
}

// cfmakeraw puts the guest's struct termios in raw mode, the flags of
// termios.h: no line editing, no signals, eight bit words.
func (p *Process) libcCfmakeraw(at uint32) {
	b := p.span(at, 60)
	le := binary.LittleEndian

	off := func(o int, clear, set uint32) {
		le.PutUint32(b[o:], le.Uint32(b[o:])&^clear|set)
	}

	off(0, ignbrk|brkint|parmrk|istrip|inlcr|igncr|icrnl|ixon, 0)
	off(4, opost, 0)
	off(8, csize|parenb, cs8)
	off(12, echo|echonl|icanon|isig|iexten, 0)
	b[17+vtime] = 0
	b[17+vmin] = 1
}

func (p *Process) libcPoll(at, n, timeout uint32) uint32 {
	return p.result(p.poll(at, n, int(int32(timeout))))
}

func (p *Process) libcPpoll(at, n, ts, mask uint32) uint32 {
	timeout := -1

	if ts != 0 {
		d, err := p.timespec(ts)

		if err != nil {
			return p.result(0, err)
		}

		timeout = waitMs(d)
	}

	return p.result(p.withMask(mask, func() (int, error) {
		return p.poll(at, n, timeout)
	}))
}

// poll polls a guest's array of struct pollfd.
func (p *Process) poll(at, n uint32, timeout int) (int, error) {
	if n > 1<<16 {
		return 0, syscall.EINVAL
	}

	guest := p.span(at, 8*n)
	entries := make([]pollEntry, 0, n)
	slot := make([]int, n)
	invalid := 0

	for i := range n {
		e := guest[8*i:]
		fd := int32(binary.LittleEndian.Uint32(e))
		binary.LittleEndian.PutUint16(e[6:], 0)
		slot[i] = -1

		if fd < 0 {
			continue
		}

		obj, err := p.fd(uint32(fd))

		if err != nil {
			binary.LittleEndian.PutUint16(e[6:], pollNval)
			invalid++

			continue
		}

		slot[i] = len(entries)
		entries = append(entries, pollEntry{obj: obj, events: int16(binary.LittleEndian.Uint16(e[4:]))})
	}

	if invalid > 0 {
		timeout = 0
	}

	ready, err := p.waitObjects(entries, timeout, false)

	if err != nil {
		return 0, err
	}

	if ready > 0 {
		guest = p.span(at, 8*n)

		for i := range n {
			if slot[i] >= 0 {
				binary.LittleEndian.PutUint16(guest[8*i+6:], uint16(entries[slot[i]].revents))
			}
		}
	}

	return ready + invalid, nil
}

// putStat writes a guest struct stat.
func (p *Process) putStat(at uint32, st *syscall.Stat_t) {
	b := p.span(at, 112)
	clear(b)
	le := binary.LittleEndian
	le.PutUint64(b, st.Dev)
	le.PutUint64(b[8:], st.Ino)
	le.PutUint32(b[16:], st.Mode)
	le.PutUint32(b[20:], uint32(st.Nlink))
	le.PutUint32(b[24:], st.Uid)
	le.PutUint32(b[28:], st.Gid)
	le.PutUint64(b[32:], st.Rdev)
	le.PutUint64(b[40:], uint64(st.Size))
	le.PutUint32(b[48:], uint32(st.Blksize))
	le.PutUint64(b[56:], uint64(st.Blocks))

	for i, t := range []syscall.Timespec{st.Atim, st.Mtim, st.Ctim} {
		le.PutUint64(b[64+16*i:], uint64(t.Sec))
		le.PutUint32(b[72+16*i:], uint32(t.Nsec))
	}
}

// timespecs reads a guest struct timespec[2], or nil for a null pointer.
func (p *Process) timespecs(at uint32) []syscall.Timespec {
	if at == 0 {
		return nil
	}

	b := p.span(at, 32)
	le := binary.LittleEndian

	return []syscall.Timespec{
		{Sec: int64(le.Uint64(b)), Nsec: int64(int32(le.Uint32(b[8:])))},
		{Sec: int64(le.Uint64(b[16:])), Nsec: int64(int32(le.Uint32(b[24:])))},
	}
}

func (p *Process) libcReadv(fd, iovs, count uint32) uint32 {
	return p.vectored(fd, iovs, count, pollIn, syscall.SYS_READV, 0)
}

func (p *Process) libcWritev(fd, iovs, count uint32) uint32 {
	return p.vectored(fd, iovs, count, pollOut, syscall.SYS_WRITEV, 0)
}

func (p *Process) libcPreadv(fd, iovs, count uint32, off uint64) uint32 {
	return p.vectored(fd, iovs, count, pollIn, syscall.SYS_PREADV, off)
}

func (p *Process) libcPwritev(fd, iovs, count uint32, off uint64) uint32 {
	return p.vectored(fd, iovs, count, pollOut, syscall.SYS_PWRITEV, off)
}

func (p *Process) libcPipe(fds uint32) uint32 {
	return p.pipe(fds, 0)
}

func (p *Process) libcPipe2(fds, flags uint32) uint32 {
	return p.pipe(fds, flags)
}

func (p *Process) libcCfsetospeed(at, speed uint32) uint32 {
	return p.cfsetspeed(at, speed)
}

func (p *Process) libcCfsetspeed(at, speed uint32) uint32 {
	return p.cfsetspeed(at, speed)
}

// ioctl: the descriptor's own requests, FIONBIO, FIOCLEX and FIONCLEX,
// are the kernel's, as are the console's foreground group, TIOCGPGRP and
// TIOCSPGRP (kernel.go), and FIONREAD of a pipe. The system has no
// terminal: the rest is ENOTTY.
func (p *Process) libcIoctl(fd, req, va uint32) uint32 {
	obj, err := p.fileAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	if t := obj.asTerminal(); t != nil {
		if r, done := p.terminalIoctl(t, fd, req, va); done {
			return r
		}
	}

	switch req {
	case fionbio:
		fl, err := obj.flags()

		if err != nil {
			return p.result(0, err)
		}

		fl &^= syscall.O_NONBLOCK

		if p.u32(p.varArg(va, 0)) != 0 {
			fl |= syscall.O_NONBLOCK
		}

		return p.result(0, obj.setFlags(fl))
	case fioclex, fionclex:
		p.files.slots[fd].cloexec = req == fioclex

		return p.result(0, nil)
	case tiocgpgrp:
		r := p.libcTcgetpgrp(fd)

		if int32(r) >= 0 {
			p.put32(p.varArg(va, 0), r)
			r = 0
		}

		return r
	case tiocspgrp:
		return p.libcTcsetpgrp(fd, p.u32(p.varArg(va, 0)))
	case tiocgwinsz:
		if obj.asConsole() == nil {
			break
		}

		w, err := hostWindow()

		if err == nil {
			copy(p.span(p.varArg(va, 0), sizeofWinsize), w[:])
		}

		return p.result(0, err)
	case fionread:
		i := obj.asIoctl()

		if i == nil {
			break
		}

		var n int32

		if err := i.ioctl(fionread, unsafe.Pointer(&n)); err != nil {
			return p.result(0, err)
		}

		p.put32(p.varArg(va, 0), uint32(n))

		return p.result(0, nil)
	}

	return p.result(0, syscall.ENOTTY)
}

// Paths are the guest's. A path starts at the process's root when it is
// absolute, else at the working directory or at the directory of a
// directory descriptor: directories the process holds, not names. The
// kernel walks none: a call goes to the directory it starts at with the
// path whole (Dir), and what serves the directory walks it, follows its
// links and makes the last step as Linux's namei and the call make it.

// checked is a path a call takes: none is ENOENT, one of PATH_MAX or more
// ENAMETOOLONG.
func checked(path string) error {
	switch {
	case path == "":
		return syscall.ENOENT
	case len(path) >= 4096:
		return syscall.ENAMETOOLONG
	}

	return nil
}

// onPath runs call on the directory a path relative to dirfd starts at,
// which walks it: none, or one of PATH_MAX or more, is the path's error.
func (p *Process) onPath(dirfd int32, path string, call func(d Dir) error) error {
	if err := checked(path); err != nil {
		return err
	}

	d, err := p.start(dirfd, path)

	if err != nil {
		return err
	}

	defer d.unref()

	return call(d)
}

// onPaths runs call on the directories two paths start at.
func (p *Process) onPaths(olddir int32, old string, newdir int32, new string, call func(from, to Dir) error) error {
	return p.onPath(olddir, old, func(from Dir) error {
		return p.onPath(newdir, new, func(to Dir) error {
			return call(from, to)
		})
	})
}

// start is the directory a path starts at, which the caller holds a
// reference to: the root for an absolute one, else the working directory
// or the directory of dirfd.
func (p *Process) start(dirfd int32, path string) (Dir, error) {
	d := p.root

	if !strings.HasPrefix(path, "/") {
		d = p.cwd

		if dirfd != atFdcwd {
			var err error

			if d, err = p.dirAt(uint32(dirfd)); err != nil {
				return nil, err
			}
		}
	}

	d.ref()

	return d, nil
}

// onAt runs a call on what an *at call names: byPath on the path, a last
// link followed unless AT_SYMLINK_NOFOLLOW, or byFile on the descriptor's
// own file when the path is empty with AT_EMPTY_PATH.
func (p *Process) onAt(dirfd int32, path string, flags uint32, byFile func(f Object) error, byPath func(d Dir, follow bool) error) error {
	if path == "" && flags&atEmptyPath != 0 {
		if dirfd == atFdcwd {
			return byFile(p.cwd)
		}

		obj, err := p.fd(uint32(dirfd))

		if err != nil {
			return err
		}

		return byFile(obj)
	}

	return p.onPath(dirfd, path, func(d Dir) error {
		return byPath(d, flags&atSymlinkNofollow == 0)
	})
}

// attrsCall is a call on a descriptor's attributes; a file without any
// has no such call: EINVAL.
func attrsCall(call func(a Attrs) error) func(f Object) error {
	return func(f Object) error {
		if a := f.asAttrs(); a != nil {
			return call(a)
		}

		return syscall.EINVAL
	}
}

// openAt opens what a path relative to dirfd names, as open(2) does.
func (p *Process) openAt(dirfd int32, path string, flags int, mode uint32) (Object, error) {
	var f Object
	err := p.onPath(dirfd, path, func(d Dir) error {
		var err error
		f, err = d.openPath(p, path, flags, mode)

		return err
	})

	return f, err
}

// open opens a guest path relative to dirfd and installs the file at the
// lowest descriptor.
func (p *Process) open(dirfd int32, path string, flags, va uint32) (int, error) {
	mode := uint32(0)

	if flags&(syscall.O_CREAT|0o20000000) != 0 {
		mode = p.varArg(va, 0) & 0o7777 &^ p.umask
	}

	if err := p.descriptorFree(); err != nil {
		return 0, err
	}

	f, err := p.openAt(dirfd, path, int(flags&^syscall.O_CLOEXEC), mode)

	if err != nil {
		return 0, err
	}

	return p.install(f, 0, flags&syscall.O_CLOEXEC != 0)
}

// openPath opens a guest path.
func (p *Process) openPath(path string, flags int, mode uint32) (Object, error) {
	return p.openAt(atFdcwd, path, flags, mode)
}

// readPath reads a file of the root whole, up to max bytes: the kernel's
// own reads of the databases and tables under the root. The error is the
// open's; what cannot be read, a directory, is read as empty, as musl
// reads it through fopen.
func (p *Process) readPath(path string, max int) ([]byte, error) {
	f, err := p.openPath(path, syscall.O_RDONLY, 0)

	if err != nil {
		return nil, err
	}

	defer f.unref()
	t := f.asTransfer()

	if t == nil {
		return nil, nil
	}

	var data []byte
	buf := make([]byte, 65536)

	for len(data) < max {
		n, err := t.read(p, buf, ioMode{})

		if err != nil || n == 0 {
			break
		}

		data = append(data, buf[:n]...)
	}

	return data, nil
}

func (p *Process) mkdir(dirfd int32, path string, mode uint32) error {
	return p.onPath(dirfd, path, func(d Dir) error {
		return d.mkdirPath(p, path, mode&0o7777&^p.umask)
	})
}

// mknod makes a FIFO or a regular file; a device is none the system
// makes, EPERM.
func (p *Process) mknod(dirfd int32, path string, mode uint32, dev uint64) error {
	switch mode & syscall.S_IFMT {
	case 0, syscall.S_IFREG, syscall.S_IFIFO:
	default:
		return syscall.EPERM
	}

	return p.onPath(dirfd, path, func(d Dir) error {
		return d.mknodPath(p, path, mode&^(p.umask&0o777))
	})
}

func (p *Process) symlink(target string, dirfd int32, path string) error {
	if target == "" {
		return syscall.ENOENT
	}

	return p.onPath(dirfd, path, func(d Dir) error {
		return d.symlinkPath(p, target, path)
	})
}

// remove takes the last name of a path relative to dirfd away, a
// directory's with dir.
func (p *Process) remove(dirfd int32, path string, dir bool) error {
	return p.onPath(dirfd, path, func(d Dir) error {
		return d.unlinkPath(p, path, dir)
	})
}

// rename moves the last name of one path to the last name of another.
func (p *Process) rename(olddir int32, old string, newdir int32, new string) error {
	return p.onPaths(olddir, old, newdir, new, func(from, to Dir) error {
		return from.renamePath(p, old, to, new)
	})
}

// link makes the last name of a new path a name of what an old one
// names, a last link of it followed when follow says.
func (p *Process) link(olddir int32, old string, newdir int32, new string, follow bool) error {
	return p.onPaths(olddir, old, newdir, new, func(from, to Dir) error {
		return from.linkPath(p, old, to, new, follow)
	})
}

// statPath is the metadata of what a path names, or of a last symbolic
// link itself.
func (p *Process) statPath(path string, follow bool, st *syscall.Stat_t) error {
	return p.onPath(atFdcwd, path, func(d Dir) error {
		return d.statPath(p, path, follow, st)
	})
}

// accessAt checks what a path names for mode, as access does, or with
// AT_SYMLINK_NOFOLLOW a last link itself.
func (p *Process) accessAt(dirfd int32, path string, mode, flags uint32) error {
	return p.onPath(dirfd, path, func(d Dir) error {
		return p.access(d, path, mode, flags)
	})
}

// access is what a path from d lets the process do, by its stat: the ids
// are vm's, real and effective alike.
func (p *Process) access(d Dir, path string, mode, flags uint32) error {
	var st syscall.Stat_t

	if err := d.statPath(p, path, flags&atSymlinkNofollow == 0, &st); err != nil {
		return err
	}

	if !p.permits(&st, mode&7) {
		return syscall.EACCES
	}

	return nil
}

// putStatvfs writes a guest struct statvfs.
func (p *Process) putStatvfs(at uint32, st *syscall.Statfs_t) {
	b := p.span(at, 72)
	clear(b)
	le := binary.LittleEndian
	// Linux fs/statfs.c already substitutes f_bsize for a zero f_frsize.
	le.PutUint32(b, uint32(st.Bsize))
	le.PutUint32(b[4:], uint32(st.Frsize))
	le.PutUint64(b[8:], st.Blocks)
	le.PutUint64(b[16:], st.Bfree)
	le.PutUint64(b[24:], st.Bavail)
	le.PutUint64(b[32:], st.Files)
	le.PutUint64(b[40:], st.Ffree)
	le.PutUint64(b[48:], st.Ffree)
	le.PutUint32(b[56:], uint32(st.Fsid.X__val[0]))
	le.PutUint32(b[60:], uint32(st.Flags))
	le.PutUint32(b[64:], uint32(st.Namelen))
}

// putStatfsOf writes a guest struct statfs.
func (p *Process) putStatfsOf(at uint32, st *syscall.Statfs_t) {
	b := p.span(at, 88)
	clear(b)
	le := binary.LittleEndian
	le.PutUint32(b, uint32(st.Type))
	le.PutUint32(b[4:], uint32(st.Bsize))
	le.PutUint64(b[8:], st.Blocks)
	le.PutUint64(b[16:], st.Bfree)
	le.PutUint64(b[24:], st.Bavail)
	le.PutUint64(b[32:], st.Files)
	le.PutUint64(b[40:], st.Ffree)
	le.PutUint32(b[48:], uint32(st.Fsid.X__val[0]))
	le.PutUint32(b[52:], uint32(st.Fsid.X__val[1]))
	le.PutUint32(b[56:], uint32(st.Namelen))
	le.PutUint32(b[60:], uint32(st.Frsize))
	le.PutUint32(b[64:], uint32(st.Flags))
}

func (p *Process) statfsPath(path string, st *syscall.Statfs_t) error {
	return p.onPath(atFdcwd, path, func(d Dir) error {
		return d.statfsPath(p, path, st)
	})
}

func (p *Process) libcOpen(path, flags, va uint32) uint32 {
	return p.result(p.open(atFdcwd, p.str(path), flags, va))
}

func (p *Process) libcOpenat(dirfd, path, flags, va uint32) uint32 {
	return p.result(p.open(int32(dirfd), p.str(path), flags, va))
}

func (p *Process) libcFstatat(dirfd, path, at, flags uint32) uint32 {
	var st syscall.Stat_t
	err := p.statFlags(int32(dirfd), p.str(path), flags, &st)

	if err == nil {
		p.putStat(at, &st)
	}

	return p.result(0, err)
}

// statFlags is fstatat's stat of a path relative to dirfd, with its flags;
// statAt follows the last link with follow, as WASI's path_filestat_get
// does (wasi.go).
func (p *Process) statFlags(dirfd int32, path string, flags uint32, st *syscall.Stat_t) error {
	return p.onAt(dirfd, path, flags, func(f Object) error {
		return statFile(p, f, st)
	}, func(d Dir, follow bool) error {
		return d.statPath(p, path, follow, st)
	})
}

func (p *Process) statAt(dirfd int32, path string, follow bool, st *syscall.Stat_t) error {
	if follow {
		return p.statFlags(dirfd, path, 0, st)
	}

	return p.statFlags(dirfd, path, atSymlinkNofollow, st)
}

func (p *Process) libcAccess(path, mode uint32) uint32 {
	return p.result(0, p.accessAt(atFdcwd, p.str(path), mode, 0))
}

// faccessat of a descriptor's own file: a file of the kernel is the
// process's to use.
func (p *Process) libcFaccessat(dirfd, path, mode, flags uint32) uint32 {
	return p.result(0, p.onAt(int32(dirfd), p.str(path), flags, func(f Object) error {
		return nil
	}, func(d Dir, follow bool) error {
		return p.access(d, p.str(path), mode, flags)
	}))
}

func (p *Process) libcTruncate(path uint32, size uint64) uint32 {
	return p.result(0, p.onPath(atFdcwd, p.str(path), func(d Dir) error {
		return d.truncatePath(p, p.str(path), int64(size))
	}))
}

func (p *Process) libcStatvfs(path, at uint32) uint32 {
	var st syscall.Statfs_t
	err := p.statfsPath(p.str(path), &st)

	if err == nil {
		p.putStatvfs(at, &st)
	}

	return p.result(0, err)
}

func (p *Process) libcFstatvfs(fd, at uint32) uint32 {
	n, err := p.attrsAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	var st syscall.Statfs_t

	if err := n.statfs(p, &st); err != nil {
		return p.result(0, err)
	}

	p.putStatvfs(at, &st)

	return p.result(0, nil)
}

// setCwd makes d, whose reference it takes, the working directory.
func (p *Process) setCwd(d Dir) {
	old := p.cwd
	p.cwd = d

	if old != nil {
		old.unref()
	}
}

// directory is the directory a path names, one the process may search,
// held: for a working directory or a root.
func (p *Process) directory(path string) (Dir, error) {
	var nd Dir
	err := p.onPath(atFdcwd, path, func(d Dir) error {
		f, err := d.openPath(p, path, oPath|syscall.O_DIRECTORY, 0)

		if err != nil {
			return err
		}

		h := f.asDir()

		if h == nil {
			f.unref()

			return syscall.ENOTDIR
		}

		if err := p.access(h, "", 1, 0); err != nil {
			h.unref()

			return err
		}

		nd = h

		return nil
	})

	return nd, err
}

// chdir makes a directory the working directory: the directory itself,
// which a rename does not change and a removal leaves without a path.
func (p *Process) chdir(path string) error {
	d, err := p.directory(path)

	if err != nil {
		return err
	}

	p.setCwd(d)

	return nil
}

func (p *Process) libcChdir(path uint32) uint32 {
	return p.result(0, p.chdir(p.str(path)))
}

// fchdir makes a directory descriptor's directory the working directory:
// a Dir's, which chroot(".") may then take, or an end of a seqpacket pair,
// which is the root of the server behind it (directory, pair_end.go);
// anything else is ENOTDIR.
func (p *Process) libcFchdir(fd uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	d := obj.asDir()

	if e := obj.asPair(); d == nil && e != nil && e.shared.kind == sockSeqpacket {
		made := e.directory(p)
		defer made.unref()
		d = made
	}

	if d == nil {
		return p.result(0, syscall.ENOTDIR)
	}

	d.ref()

	if err := p.access(d, "", 1, 0); err != nil {
		d.unref()

		return p.result(0, err)
	}

	p.setCwd(d)

	return p.result(0, nil)
}

// chroot makes a directory the process's root: ".." stops there and
// absolute paths start there from then on. The working directory stays
// what it is, as Linux leaves it.
func (p *Process) libcChroot(path uint32) uint32 {
	d, err := p.directory(p.str(path))

	if err != nil {
		return p.result(0, err)
	}

	p.root.unref()
	p.root = d

	return p.result(0, nil)
}

// cwdPath is the working directory's path: none past PATH_MAX, as
// Linux's getcwd has none.
func (p *Process) cwdPath() (string, error) {
	cwd, err := p.cwd.path(p)

	if err == nil && len(cwd) >= 4096 {
		err = syscall.ENAMETOOLONG
	}

	return cwd, err
}

func (p *Process) libcGetcwd(buf, size uint32) uint32 {
	cwd, err := p.cwdPath()

	if err != nil {
		return p.pointer(0, err)
	}

	if buf == 0 {
		at := p.newString(cwd)

		if at == 0 {
			return p.pointer(0, syscall.ENOMEM)
		}

		return p.pointer(at, nil)
	}

	if uint32(len(cwd)) >= size {
		return p.pointer(0, syscall.ERANGE)
	}

	p.putString(buf, cwd)

	return p.pointer(buf, nil)
}

func (p *Process) libcMkdir(path, mode uint32) uint32 {
	return p.result(0, p.mkdir(atFdcwd, p.str(path), mode))
}

func (p *Process) libcMkdirat(dirfd, path, mode uint32) uint32 {
	return p.result(0, p.mkdir(int32(dirfd), p.str(path), mode))
}

func (p *Process) libcRmdir(path uint32) uint32 {
	return p.result(0, p.remove(atFdcwd, p.str(path), true))
}

func (p *Process) libcUnlink(path uint32) uint32 {
	return p.result(0, p.remove(atFdcwd, p.str(path), false))
}

func (p *Process) libcUnlinkat(dirfd, path, flags uint32) uint32 {
	if flags&^atRemovedir != 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(0, p.remove(int32(dirfd), p.str(path), flags&atRemovedir != 0))
}

// remove unlinks a file or empties a directory, whichever it is.
func (p *Process) libcRemove(path uint32) uint32 {
	err := p.remove(atFdcwd, p.str(path), false)

	if err == syscall.EISDIR || err == syscall.EPERM {
		err = p.remove(atFdcwd, p.str(path), true)
	}

	return p.result(0, err)
}

func (p *Process) libcRename(old, new uint32) uint32 {
	return p.result(0, p.rename(atFdcwd, p.str(old), atFdcwd, p.str(new)))
}

func (p *Process) libcRenameat(olddir, old, newdir, new uint32) uint32 {
	return p.result(0, p.rename(int32(olddir), p.str(old), int32(newdir), p.str(new)))
}

func (p *Process) libcLink(old, new uint32) uint32 {
	return p.result(0, p.link(atFdcwd, p.str(old), atFdcwd, p.str(new), false))
}

func (p *Process) libcLinkat(olddir, old, newdir, new, flags uint32) uint32 {
	if flags&^atSymlinkFollow != 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(0, p.link(int32(olddir), p.str(old), int32(newdir), p.str(new), flags&atSymlinkFollow != 0))
}

func (p *Process) libcSymlink(target, path uint32) uint32 {
	return p.result(0, p.symlink(p.str(target), atFdcwd, p.str(path)))
}

func (p *Process) libcSymlinkat(target, dirfd, path uint32) uint32 {
	return p.result(0, p.symlink(p.str(target), int32(dirfd), p.str(path)))
}

// readlinkAt writes a link's target, as much as the buffer holds.
func (p *Process) readlinkAt(dirfd int32, path string, buf, size uint32) (int, error) {
	if p.chaosFail("readlink") {
		return 0, syscall.EIO
	}

	var target string
	err := p.onPath(dirfd, path, func(d Dir) error {
		var err error
		target, err = d.readlinkPath(p, path)

		return err
	})

	if err != nil {
		return 0, err
	}

	return copy(p.span(buf, size), target), nil
}

func (p *Process) libcReadlink(path, buf, size uint32) uint32 {
	return p.result(p.readlinkAt(atFdcwd, p.str(path), buf, size))
}

// readlinkat with an empty path reads the link a descriptor is: what is
// no link is ENOENT then, as Linux has it, not the named path's EINVAL.
func (p *Process) libcReadlinkat(dirfd, path, buf, size uint32) uint32 {
	if p.u8(path) != 0 {
		return p.result(p.readlinkAt(int32(dirfd), p.str(path), buf, size))
	}

	obj, err := p.fd(dirfd)

	if dirfd == uint32(atFdcwd&0xffffffff) {
		obj, err = p.cwd, nil
	}

	if err != nil {
		return p.result(0, err)
	}

	l := obj.asLink()

	if l == nil {
		return p.result(0, syscall.ENOENT)
	}

	target, err := l.readlink(p)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(copy(p.span(buf, size), target), nil)
}

// realpath is the path from the root of what a path names, with no ".",
// ".." or link in it.
func (p *Process) realpath(path string) (string, error) {
	var out string
	err := p.onPath(atFdcwd, path, func(d Dir) error {
		var err error
		out, err = d.realpathOf(p, path)

		return err
	})

	if err == nil && len(out) >= 4096 {
		err = syscall.ENAMETOOLONG
	}

	return out, err
}

func (p *Process) libcCanonicalize_file_name(path uint32) uint32 {
	guest, err := p.realpath(p.str(path))

	if err != nil {
		return p.pointer(0, err)
	}

	at := p.newString(guest)

	if at == 0 {
		return p.pointer(0, syscall.ENOMEM)
	}

	return p.pointer(at, nil)
}

func (p *Process) libcGet_current_dir_name() uint32 {
	cwd, err := p.cwdPath()

	if err != nil {
		return p.pointer(0, err)
	}

	at := p.newString(cwd)

	if at == 0 {
		return p.pointer(0, syscall.ENOMEM)
	}

	return p.pointer(at, nil)
}

func (p *Process) libcCreat(path, mode uint32) uint32 {
	f, err := p.openPath(p.str(path), syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC, mode&0o7777&^p.umask)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(p.install(f, 0, false))
}

func (p *Process) libcEaccess(path, mode uint32) uint32 {
	return p.result(0, p.accessAt(atFdcwd, p.str(path), mode, atEaccess))
}

func (p *Process) libcEuidaccess(path, mode uint32) uint32 {
	return p.result(0, p.accessAt(atFdcwd, p.str(path), mode, atEaccess))
}

func (p *Process) libcStatfs(path, at uint32) uint32 {
	var st syscall.Statfs_t
	err := p.statfsPath(p.str(path), &st)

	if err == nil {
		p.putStatfsOf(at, &st)
	}

	return p.result(0, err)
}

func (p *Process) libcFstatfs(fd, at uint32) uint32 {
	n, err := p.attrsAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	var st syscall.Statfs_t

	if err := n.statfs(p, &st); err != nil {
		return p.result(0, err)
	}

	p.putStatfsOf(at, &st)

	return p.result(0, nil)
}

func (p *Process) libcRealpath(path, resolved uint32) uint32 {
	guest, err := p.realpath(p.str(path))

	if err != nil {
		return p.pointer(0, err)
	}

	if resolved == 0 {
		resolved = p.newString(guest)

		if resolved == 0 {
			return p.pointer(0, syscall.ENOMEM)
		}

		return p.pointer(resolved, nil)
	}

	p.putString(resolved, guest)

	return p.pointer(resolved, nil)
}

func (p *Process) libcChmod(path, mode uint32) uint32 {
	return p.result(0, p.onPath(atFdcwd, p.str(path), func(d Dir) error {
		return d.chmodPath(p, p.str(path), mode&0o7777, true)
	}))
}

func (p *Process) libcFchmodat(dirfd, path, mode, flags uint32) uint32 {
	return p.result(0, p.onAt(int32(dirfd), p.str(path), flags, attrsCall(func(a Attrs) error {
		return a.chmod(p, mode&0o7777)
	}), func(d Dir, follow bool) error {
		return d.chmodPath(p, p.str(path), mode&0o7777, follow)
	}))
}

func (p *Process) libcFchownat(dirfd, path, uid, gid, flags uint32) uint32 {
	return p.result(0, p.onAt(int32(dirfd), p.str(path), flags, attrsCall(func(a Attrs) error {
		return a.chown(p, int(int32(uid)), int(int32(gid)))
	}), func(d Dir, follow bool) error {
		return d.chownPath(p, p.str(path), int(int32(uid)), int(int32(gid)), follow)
	}))
}

func (p *Process) libcMknod(path, mode uint32, dev uint64) uint32 {
	return p.result(0, p.mknod(atFdcwd, p.str(path), mode, dev))
}

func (p *Process) libcMknodat(dirfd, path, mode uint32, dev uint64) uint32 {
	return p.result(0, p.mknod(int32(dirfd), p.str(path), mode, dev))
}

func (p *Process) libcMkfifo(path, mode uint32) uint32 {
	return p.result(0, p.mknod(atFdcwd, p.str(path), sIfifo|mode&0o7777, 0))
}

func (p *Process) libcMkfifoat(dirfd, path, mode uint32) uint32 {
	return p.result(0, p.mknod(int32(dirfd), p.str(path), sIfifo|mode&0o7777, 0))
}

func (p *Process) libcUtimes(path, times uint32) uint32 {
	return p.result(0, p.utimens(atFdcwd, p.str(path), p.timevals(times), true))
}

func (p *Process) libcLutimes(path, times uint32) uint32 {
	return p.result(0, p.utimens(atFdcwd, p.str(path), p.timevals(times), false))
}

func (p *Process) libcUtime(path, times uint32) uint32 {
	var ts []syscall.Timespec

	if times != 0 {
		ts = []syscall.Timespec{{Sec: int64(p.u64(times))}, {Sec: int64(p.u64(times + 8))}}
	}

	return p.result(0, p.utimens(atFdcwd, p.str(path), ts, true))
}

// utimensat of a null path sets the times of the descriptor's own file,
// as Linux's does.
func (p *Process) libcUtimensat(dirfd, path, times, flags uint32) uint32 {
	if path == 0 {
		n, err := p.attrsAt(dirfd)

		if err == nil {
			err = n.utimes(p, p.timespecs(times))
		}

		return p.result(0, err)
	}

	return p.result(0, p.utimens(int32(dirfd), p.str(path), p.timespecs(times), flags&atSymlinkNofollow == 0))
}

// readdir_r returns an error number.
func (p *Process) libcReaddir_r(dir, entry, result uint32) uint32 {
	e, err := p.readdir(dir)
	p.put32(result, 0)

	if err != nil {
		return uint32(errnoOf(err))
	}

	if e != 0 {
		n := 19 + uint32(len(p.cstr(e+19))) + 1
		copy(p.span(entry, n), p.span(e, n))
		p.put32(result, entry)
	}

	return 0
}

func (p *Process) libcSeekdir(dir, at uint32) {
	p.rewinddir(dir)

	for p.u32(dir+dirIndex) < at {
		if e, _ := p.readdir(dir); e == 0 {
			return
		}
	}
}

func (p *Process) libcMkdtemp(template uint32) uint32 {
	err := p.temporary(template, 0, func(path string) error {
		return p.mkdir(atFdcwd, path, 0o700)
	})

	return p.pointer(template, err)
}

func (p *Process) libcMktemp(template uint32) uint32 {
	err := p.temporary(template, 0, func(path string) error {
		var st syscall.Stat_t
		err := p.statPath(path, false, &st)

		if err == syscall.ENOENT {
			return nil
		}

		if err == nil {
			return syscall.EEXIST
		}

		return err
	})

	if err != nil {
		p.span(template, 1)[0] = 0
		p.setErrno(errnoOf(err))
	}

	return p.pointer(template, nil)
}

// timevals reads a guest struct timeval[2] as timespecs, or nil for a
// null pointer.
func (p *Process) timevals(at uint32) []syscall.Timespec {
	if at == 0 {
		return nil
	}

	b := p.span(at, 32)
	le := binary.LittleEndian
	var ts []syscall.Timespec

	for i := range 2 {
		ts = append(ts, syscall.Timespec{Sec: int64(le.Uint64(b[16*i:])), Nsec: 1000 * int64(le.Uint64(b[16*i+8:]))})
	}

	return ts
}

func (p *Process) stat(path, at uint32, follow bool) uint32 {
	var st syscall.Stat_t
	err := p.statPath(p.str(path), follow, &st)

	if err == nil {
		p.putStat(at, &st)
	}

	return p.result(0, err)
}

func (p *Process) chown(path, uid, gid uint32, follow bool) uint32 {
	return p.result(0, p.onPath(atFdcwd, p.str(path), func(d Dir) error {
		return d.chownPath(p, p.str(path), int(int32(uid)), int(int32(gid)), follow)
	}))
}

// utimens sets the times of what a path names, or of a last symbolic
// link itself.
func (p *Process) utimens(dirfd int32, path string, ts []syscall.Timespec, follow bool) error {
	return p.onPath(dirfd, path, func(d Dir) error {
		return d.utimesPath(p, path, ts, follow)
	})
}

const tempLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// temporary fills the XXXXXX that ends a template, or comes before a
// suffix of its last bytes, with random letters until create makes what
// the path names, EEXIST meaning another try.
func (p *Process) temporary(template, suffix uint32, create func(path string) error) error {
	t := p.cstr(template)
	end := len(t) - int(suffix)

	if int(suffix) > len(t) || end < 6 || string(t[end-6:end]) != "XXXXXX" {
		return syscall.EINVAL
	}

	for range 100 {
		for i := end - 6; i < end; i++ {
			t[i] = tempLetters[mathrand.IntN(len(tempLetters))]
		}

		if err := create(string(t)); err != syscall.EEXIST {
			return err
		}
	}

	return syscall.EEXIST
}

// mkstemp makes a file of a template, open with flags.
func (p *Process) mkstemp(template, suffix, flags uint32) uint32 {
	fd := -1
	err := p.temporary(template, suffix, func(path string) error {
		f, err := p.openPath(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL|int(flags&^syscall.O_CLOEXEC), 0o600)

		if err == nil {
			fd, err = p.install(f, 0, flags&syscall.O_CLOEXEC != 0)
		}

		return err
	})

	return p.result(fd, err)
}

// A DIR is a block of the heap: the descriptor, the position and end of
// the entries in the buffer, how many entries readdir returned since the
// start, then the buffer, which the directory fills with entries of the
// guest's struct dirent layout. telldir is that count, which seekdir reads
// again from the start: a host's offset may not fit a long.
const (
	dirFd     = 0
	dirPos    = 4
	dirEnd    = 8
	dirIndex  = 12
	dirBuf    = 16
	dirBufLen = 4096
)

func (p *Process) libcOpendir(path uint32) uint32 {
	f, err := p.openPath(p.str(path), syscall.O_RDONLY|syscall.O_DIRECTORY, 0)

	if err != nil {
		return p.pointer(0, err)
	}

	fd, err := p.install(f, 0, true)

	if err != nil {
		return p.pointer(0, err)
	}

	dir := p.newDir(fd)

	if dir == 0 {
		p.closeFd(fd)

		return p.pointer(0, syscall.ENOMEM)
	}

	return p.pointer(dir, nil)
}

// newDir makes the DIR of a descriptor, or returns 0.
func (p *Process) newDir(fd int) uint32 {
	dir := p.malloc(dirBuf + dirBufLen)

	if dir != 0 {
		p.put32(dir+dirFd, uint32(fd))
		p.put32(dir+dirPos, 0)
		p.put32(dir+dirEnd, 0)
		p.put32(dir+dirIndex, 0)
	}

	return dir
}

// fdopendir makes a DIR of a directory's descriptor, which becomes
// close-on-exec, as musl's.
func (p *Process) libcFdopendir(fd uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.pointer(0, err)
	}

	var st syscall.Stat_t

	if err := statFile(p, obj, &st); err != nil {
		return p.pointer(0, err)
	}

	if obj.asDir() == nil || st.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		return p.pointer(0, syscall.ENOTDIR)
	}

	dir := p.newDir(int(fd))

	if dir == 0 {
		return p.pointer(0, syscall.ENOMEM)
	}

	p.files.slots[fd].cloexec = true

	return p.pointer(dir, nil)
}

func (p *Process) rewinddir(dir uint32) {
	if s, err := p.seekerAt(p.u32(dir + dirFd)); err == nil {
		s.seek(p, 0, 0)
	}

	p.put32(dir+dirPos, 0)
	p.put32(dir+dirEnd, 0)
	p.put32(dir+dirIndex, 0)
}

// direntCompare compares the names of two struct dirent** of scandir.
func (p *Process) direntCompare(a, b uint32, compare func(x, y []byte) int) uint32 {
	x, y := p.cstr(p.u32(a)+19), p.cstr(p.u32(b)+19)

	return uint32(int32(compare(x, y)))
}

func (p *Process) libcReaddir(dir uint32) uint32 {
	entry, err := p.readdir(dir)

	return p.pointer(entry, err)
}

// readdir returns the next entry of a DIR, or 0 at its end.
func (p *Process) readdir(dir uint32) (uint32, error) {
	pos, end := p.u32(dir+dirPos), p.u32(dir+dirEnd)

	if pos >= end {
		d, err := p.dirAt(p.u32(dir + dirFd))

		if err != nil {
			return 0, err
		}

		n, err := d.readdir(p, p.span(dir+dirBuf, dirBufLen))

		if err != nil || n == 0 {
			return 0, err
		}

		pos, end = 0, uint32(n)
		p.put32(dir+dirEnd, end)
	}

	entry := dir + dirBuf + pos
	p.put32(dir+dirPos, pos+uint32(p.u16(entry+16)))
	p.put32(dir+dirIndex, p.u32(dir+dirIndex)+1)

	return entry, nil
}

func (p *Process) libcClosedir(dir uint32) uint32 {
	fd := p.u32(dir + dirFd)
	p.free(dir)

	if !p.closeFd(int(fd)) {
		return p.result(0, syscall.EBADF)
	}

	return p.result(0, nil)
}

func (p *Process) libcStat(path, at uint32) uint32 {
	return p.stat(path, at, true)
}

func (p *Process) libcLstat(path, at uint32) uint32 {
	return p.stat(path, at, false)
}

// Process accounting needs privilege.
func (p *Process) libcAcct(path uint32) uint32 {
	return p.result(0, syscall.EPERM)
}

func (p *Process) libcChown(path, uid, gid uint32) uint32 {
	return p.chown(path, uid, gid, true)
}

func (p *Process) libcLchown(path, uid, gid uint32) uint32 {
	return p.chown(path, uid, gid, false)
}

func (p *Process) libcDirfd(dir uint32) uint32 {
	return p.u32(dir + dirFd)
}

func (p *Process) libcTelldir(dir uint32) uint32 {
	return p.u32(dir + dirIndex)
}

func (p *Process) libcAlphasort(a, b uint32) uint32 {
	return p.direntCompare(a, b, bytes.Compare)
}

func (p *Process) libcVersionsort(a, b uint32) uint32 {
	return p.direntCompare(a, b, strverscmp)
}

func (p *Process) libcMkstemp(template uint32) uint32 {
	return p.mkstemp(template, 0, 0)
}

func (p *Process) libcMkostemp(template, flags uint32) uint32 {
	return p.mkstemp(template, 0, flags)
}

func (p *Process) libcMkstemps(template, suffix uint32) uint32 {
	return p.mkstemp(template, suffix, 0)
}

func (p *Process) libcRewinddir(dir uint32) {
	p.rewinddir(dir)
}

const umountNofollow = 8

// mount mounts on the directory target names the directory source names,
// or the root of the file system whose socket source names, which the
// kernel connects to (unix_socket.go): the VFS's call (remote_dir.go),
// both paths from the working directory or the root. fstype, flags and
// data are not used; a process of the host's mounts nothing.
func (p *Process) libcMount(source, target, fstype, flags, data uint32) uint32 {
	from, to := p.str(source), p.str(target)

	for _, path := range []string{from, to} {
		if err := checked(path); err != nil {
			return p.result(0, err)
		}
	}

	err := p.onPath(atFdcwd, ".", func(v Dir) error {
		var st syscall.Stat_t

		if v.statPath(p, from, true, &st) != nil || st.Mode&syscall.S_IFMT != syscall.S_IFSOCK {
			return v.mountPath(p, to, from, nil)
		}

		conn, err := p.connectUnix(v, from, sockSeqpacket, false, new(atomic.Int64))

		if err != nil {
			return err
		}

		defer conn.unref()

		return v.mountPath(p, to, from, conn)
	})

	return p.result(0, err)
}

// umount2 unmounts what is mounted on the directory target names; the
// flags but UMOUNT_NOFOLLOW are not used.
func (p *Process) libcUmount2(target, flags uint32) uint32 {
	err := p.onPath(atFdcwd, p.str(target), func(v Dir) error {
		return v.unmountPath(p, p.str(target), flags&umountNofollow)
	})

	return p.result(0, err)
}

func (p *Process) libcUmount(target uint32) uint32 {
	return p.libcUmount2(target, 0)
}

// openFifo opens the FIFO of a server whose stat st is, with the flags of
// open.
func (p *Process) openFifo(st *syscall.Stat_t, flags int) (Object, error) {
	access := flags & syscall.O_ACCMODE
	reads, writes := access != syscall.O_WRONLY, access != syscall.O_RDONLY
	nonblock := flags&syscall.O_NONBLOCK != 0
	key := fileKey{st.Dev, st.Ino}
	k := p.k
	k.mu.Lock()
	pp := k.fifos[key]

	if pp == nil {
		pp = newPipe()
		pp.name = &fifoName{k: k, key: key, mode: st.Mode}
		k.fifos[key] = pp
	}

	if writes && !reads && nonblock {
		pp.mu.Lock()
		alone, empty := pp.readers == 0, pp.readers == 0 && pp.writers == 0
		pp.mu.Unlock()

		if alone {
			if empty {
				delete(k.fifos, key)
			}

			k.mu.Unlock()

			return nil, syscall.ENXIO
		}
	}

	readerOpens, writerOpens := pp.join(reads, writes)
	k.mu.Unlock()
	e := &pipeEnd{pipe: pp, reads: reads, writes: writes}
	e.refs.Store(1)
	e.nonblock.Store(nonblock)

	if nonblock || reads && writes {
		return e, nil
	}

	err := pp.meet(p, reads, readerOpens, writerOpens)

	if err != nil {
		e.unref()

		return nil, err
	}

	return e, nil
}

// pipe makes a pipe and installs its ends at the lowest descriptors.
func (p *Process) pipe(at, flags uint32) uint32 {
	if flags&^(syscall.O_CLOEXEC|syscall.O_NONBLOCK) != 0 {
		return p.result(0, syscall.EINVAL)
	}

	pp := newPipe()
	pp.readers, pp.writers = 1, 1
	ends := [2]*pipeEnd{{pipe: pp, reads: true}, {pipe: pp, writes: true}}
	cloexec := flags&syscall.O_CLOEXEC != 0
	var fds [2]int

	for i, e := range ends {
		e.refs.Store(1)
		e.nonblock.Store(flags&syscall.O_NONBLOCK != 0)
		fd, err := p.install(e, 0, cloexec)

		if err != nil {
			if i == 1 {
				p.closeFd(fds[0])
			} else {
				ends[1].unref()
			}

			return p.result(0, err)
		}

		fds[i] = fd
	}

	p.put32(at, uint32(fds[0]))
	p.put32(at+4, uint32(fds[1]))

	return p.result(0, nil)
}

// memfd_create makes a file of the n bytes of the process's memory at at,
// open in the access mode of flags, close-on-exec with O_CLOEXEC.
func (p *Process) libcMemfd_create(at, n, flags uint32) uint32 {
	mode := int(flags & syscall.O_ACCMODE)

	if flags&^(syscall.O_ACCMODE|syscall.O_CLOEXEC) != 0 || mode == syscall.O_ACCMODE {
		return p.result(0, syscall.EINVAL)
	}

	i := p.inst

	if i.memory == nil || uint64(at)+uint64(n) > i.memory.pages*pageSize {
		return p.result(0, syscall.EFAULT)
	}

	f := newMemfd(i, i.memory.data[at:at+n:at+n], mode)
	fd, err := p.install(f, 0, flags&syscall.O_CLOEXEC != 0)

	if err != nil {
		f.unref()

		return p.result(0, err)
	}

	return p.result(fd, nil)
}

// copyWindow is copy_file_range between a memfd and another file: the
// memfd's bytes are the memory it is of, so the other file's go straight
// there or come straight from there, one copy, a Windowed file's; another
// memfd's bytes are moved. from and to are the offsets given, nil for the
// files' positions, which move.
func copyWindow(p *Process, src, dst Object, from, to *int64, n int) (int, error) {
	if w := dst.asMemfd(); w != nil {
		return w.fill(p, src, from, to, n)
	}

	return src.asMemfd().drain(p, dst, from, to, n)
}

// putAddress writes an address the way accept and getsockname do: as much
// as the buffer at at holds, and the whole length at lenAt.
func (p *Process) putAddress(addr []byte, at, lenAt uint32) {
	if at == 0 || lenAt == 0 {
		return
	}

	room := p.u32(lenAt)
	copy(p.span(at, min(room, uint32(len(addr)))), addr)
	p.put32(lenAt, uint32(len(addr)))
}

// socket makes a socket of the kernel's, of AF_UNIX (unix_socket.go): the
// system has no network, so no other domain.
func (p *Process) libcSocket(domain, kind, protocol uint32) uint32 {
	if domain != afUnix {
		return p.result(0, syscall.EAFNOSUPPORT)
	}

	if kind&^(sockTypeMask|sockNonblock|sockCloexec) != 0 {
		return p.result(0, syscall.EINVAL)
	}

	if protocol != 0 {
		return p.result(0, syscall.EPROTONOSUPPORT)
	}

	nonblock, cloexec := kind&sockNonblock != 0, kind&sockCloexec != 0

	switch kind & sockTypeMask {
	case sockStream, sockSeqpacket:
		return p.result(p.install(newUnixSocket(int(kind&sockTypeMask), nonblock), 0, cloexec))
	case sockDgram, sockRaw:
		return p.result(p.install(newUnixDgram(nonblock), 0, cloexec))
	}

	return p.result(0, syscall.ESOCKTNOSUPPORT)
}

// socketpair makes a pair of the kernel's (pair_end.go): of
// SOCK_STREAM, SOCK_DGRAM, which SOCK_RAW is to AF_UNIX as Linux has it,
// or SOCK_SEQPACKET.
func (p *Process) libcSocketpair(domain, kind, protocol, at uint32) uint32 {
	if domain != afUnix {
		return p.result(0, syscall.EOPNOTSUPP)
	}

	if kind&^(sockTypeMask|sockNonblock|sockCloexec) != 0 {
		return p.result(0, syscall.EINVAL)
	}

	if protocol != 0 {
		return p.result(0, syscall.EPROTONOSUPPORT)
	}

	base := int(kind & sockTypeMask)

	if base == sockCall {
		return p.result(0, p.callPair(kind, at))
	}

	switch base {
	case sockStream, sockDgram, sockSeqpacket:
	case sockRaw:
		base = sockDgram
	default:
		return p.result(0, syscall.ESOCKTNOSUPPORT)
	}

	a, b := newPairOf(base, kind&sockNonblock != 0)
	cloexec := kind&sockCloexec != 0
	x, err := p.install(a, 0, cloexec)

	if err != nil {
		b.unref()

		return p.result(0, err)
	}

	y, err := p.install(b, 0, cloexec)

	if err != nil {
		p.closeFd(x)

		return p.result(0, err)
	}

	p.put32(at, uint32(x))
	p.put32(at+4, uint32(y))

	return p.result(0, nil)
}

func (p *Process) libcBind(fd, addr, n uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.bind(p, addr, n)
	}

	return p.result(0, err)
}

func (p *Process) libcConnect(fd, addr, n uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.connect(p, addr, n)
	}

	return p.result(0, err)
}

func (p *Process) libcListen(fd, backlog uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.listen(int(int32(backlog)))
	}

	return p.result(0, err)
}

func (p *Process) libcAccept(fd, addr, lenAt uint32) uint32 {
	return p.socketAccept(fd, addr, lenAt, 0)
}

func (p *Process) libcAccept4(fd, addr, lenAt, flags uint32) uint32 {
	return p.socketAccept(fd, addr, lenAt, flags)
}

func (p *Process) libcGetsockname(fd, addr, lenAt uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	name, err := s.sockname(p)

	if err != nil {
		return p.result(0, err)
	}

	p.putAddress(name, addr, lenAt)

	return p.result(0, nil)
}

func (p *Process) libcGetpeername(fd, addr, lenAt uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	name, err := s.peername(p)

	if err != nil {
		return p.result(0, err)
	}

	p.putAddress(name, addr, lenAt)

	return p.result(0, nil)
}

func (p *Process) libcGetsockopt(fd, level, name, at, lenAt uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.getsockopt(p, level, name, at, lenAt)
	}

	return p.result(0, err)
}

// callPair is socketpair's SOCK_CALL: a call pair (call_end.go), its
// serving end first.
func (p *Process) callPair(kind, at uint32) error {
	serving, asking := newCall(kind&sockNonblock != 0)
	cloexec := kind&sockCloexec != 0
	x, err := p.install(serving, 0, cloexec)

	if err != nil {
		asking.unref()

		return err
	}

	y, err := p.install(asking, 0, cloexec)

	if err != nil {
		p.closeFd(x)

		return err
	}

	p.put32(at, uint32(x))
	p.put32(at+4, uint32(y))

	return nil
}

func (p *Process) libcSetsockopt(fd, level, name, at, n uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.setsockopt(p, level, name, at, n)
	}

	return p.result(0, err)
}

func (p *Process) libcSend(fd, buf, n, flags uint32) uint32 {
	return p.socketSend(fd, buf, n, flags, 0, 0)
}

func (p *Process) libcSendto(fd, buf, n, flags, addr, alen uint32) uint32 {
	return p.socketSend(fd, buf, n, flags, addr, alen)
}

func (p *Process) libcRecv(fd, buf, n, flags uint32) uint32 {
	return p.socketRecv(fd, buf, n, flags, 0, 0)
}

func (p *Process) libcRecvfrom(fd, buf, n, flags, addr, lenAt uint32) uint32 {
	return p.socketRecv(fd, buf, n, flags, addr, lenAt)
}

func (p *Process) libcSendmsg(fd, msg, flags uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(s.sendmsg(p, msg, flags))
}

func (p *Process) libcRecvmsg(fd, msg, flags uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(s.recvmsg(p, msg, flags))
}

func (p *Process) libcShutdown(fd, how uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.shutdown(int(how))
	}

	return p.result(0, err)
}

func (p *Process) libcSockatmark(fd uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	// A socket of the kernel's has no out-of-band data: a stream is never
	// at its mark, and the other kinds have none, as Linux's.
	if s.socketKind() == sockStream {
		return p.result(0, nil)
	}

	return p.result(0, syscall.EOPNOTSUPP)
}

func (p *Process) socketAccept(fd, addr, lenAt, flags uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	if flags&^(sockNonblock|sockCloexec) != 0 {
		return p.result(0, syscall.EINVAL)
	}

	if err := p.descriptorFree(); err != nil {
		return p.result(0, err)
	}

	conn, peer, err := s.accept(p, flags)

	if err != nil {
		return p.result(0, err)
	}

	guest, err := p.install(conn, 0, flags&sockCloexec != 0)

	if err != nil {
		return p.result(0, err)
	}

	p.putAddress(peer, addr, lenAt)

	return p.result(guest, nil)
}

func (p *Process) socketSend(fd, buf, n, flags, addr, alen uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(s.sendto(p, buf, n, flags, addr, alen))
}

func (p *Process) socketRecv(fd, buf, n, flags, addr, lenAt uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	got, from, err := s.recvfrom(p, buf, n, flags)

	if err == nil && addr != 0 {
		p.putAddress(from, addr, lenAt)
	}

	return p.result(got, err)
}

// ucred is the process's own: its pid and real ids.
func (p *Process) ucred() *ucred {
	return &ucred{uint32(p.pid), p.cred.uid(), p.cred.gid()}
}

// rights is the files of the SCM_RIGHTS of a guest's control buffer, the
// process's own, which send takes references to, and the credentials of
// its SCM_CREDENTIALS, the process's own, as Linux has a process without
// privileges give: EPERM else. No other control message is known.
func (p *Process) rights(at, n uint32) ([]Object, *ucred, error) {
	var files []Object
	var cred *ucred

	for off := uint32(0); off+guestCmsgHeader <= n; {
		length := p.u32(at + off)

		if length < guestCmsgHeader || off+length > n {
			return nil, nil, syscall.EINVAL
		}

		level, kind := p.u32(at+off+4), p.u32(at+off+8)
		data := p.span(at+off+guestCmsgHeader, length-guestCmsgHeader)

		switch {
		case level == solSocket && kind == scmRights:
			for i := 0; i+4 <= len(data); i += 4 {
				f, err := p.fd(binary.LittleEndian.Uint32(data[i:]))

				if err != nil {
					return nil, nil, err
				}

				// A call's serving end goes as the call in hand.
				if e := f.asCall(); e != nil {
					f = e.pass()
				}

				files = append(files, f)
			}
		case level == solSocket && kind == scmCredentials && len(data) == ucredSize:
			c := ucred{binary.LittleEndian.Uint32(data), binary.LittleEndian.Uint32(data[4:]), binary.LittleEndian.Uint32(data[8:])}

			if !p.cred.privileged() && (c.pid != uint32(p.pid) || !slices.Contains(p.cred.uids[:], c.uid) || !slices.Contains(p.cred.gids[:], c.gid)) {
				return nil, nil, syscall.EPERM
			}

			cred = &c
		default:
			return nil, nil, syscall.EINVAL
		}

		off += guestAlign(length)
	}

	return files, cred, nil
}

// putRights installs a packet's files as the process's descriptors and
// writes them as one SCM_RIGHTS to the guest's control buffer of room
// bytes; those that do not fit, or all of them without a buffer, are
// dropped with MSG_CTRUNC. It returns the control bytes written and the
// flag.
func (p *Process) putRights(files []Object, at, room uint32, cloexec bool) (uint32, uint32) {
	if len(files) == 0 {
		return 0, 0
	}

	var fds []byte
	flags := uint32(0)

	for _, f := range files {
		if guestCmsgHeader+uint32(len(fds))+4 > room {
			f.unref()
			flags |= msgCtrunc

			continue
		}

		fd, err := p.install(f, 0, cloexec)

		if err != nil {
			flags |= msgCtrunc

			continue
		}

		fds = binary.LittleEndian.AppendUint32(fds, uint32(fd))
	}

	if len(fds) == 0 {
		return 0, flags
	}

	size := guestCmsgHeader + uint32(len(fds))
	p.put32(at, size)
	p.put32(at+4, solSocket)
	p.put32(at+8, scmRights)
	copy(p.span(at+guestCmsgHeader, uint32(len(fds))), fds)

	return min(guestAlign(size), room), flags
}

// unixSocketPath is the path of a guest's sockaddr_un: an abstract name is
// none of the VFS's.
func (p *Process) unixSocketPath(at, n uint32) (string, error) {
	if n <= 2 || n > sockaddrMax || p.u16(at) != afUnix {
		return "", syscall.EINVAL
	}

	path := p.span(at+2, n-2)

	if path[0] == 0 {
		return "", syscall.EOPNOTSUPP
	}

	if i := bytes.IndexByte(path, 0); i >= 0 {
		path = path[:i]
	}

	if len(path) == 0 || len(path) >= sunPathMax {
		return "", syscall.EINVAL
	}

	return string(path), nil
}

// connectUnix makes a connection of a kind to the socket a path of the
// VFS's names: the end of the pair that is the caller's; the other is in
// the queue of the socket's listener. A full queue is EAGAIN for nonblock,
// else a wait for room, which the timeout of to ends, SO_SNDTIMEO's,
// EAGAIN too.
func (p *Process) connectUnix(v Dir, path string, kind int, nonblock bool, to *atomic.Int64) (*pairEnd, error) {
	f, err := v.connectPath(p, path)

	if err != nil {
		return nil, err
	}

	l := f.asListener()

	if l == nil || l.kind != kind {
		f.unref()

		if f.asPair() != nil || l != nil {
			return nil, syscall.EPROTOTYPE
		}

		return nil, syscall.ECONNREFUSED
	}

	defer l.unref()
	mine, theirs := newPairOf(kind, nonblock)

	for {
		queued, refused := l.push(theirs)

		if queued {
			return mine, nil
		}

		if !refused && !nonblock {
			end, how := until(to)
			var timedOut bool

			if timedOut, err = l.conns.writable(p, func() bool { return !l.listens() }, end, how); err == nil && !timedOut {
				continue
			} else if err == nil {
				err = syscall.EAGAIN
			}
		} else if refused {
			err = syscall.ECONNREFUSED
		} else {
			err = syscall.EAGAIN
		}

		mine.unref()
		theirs.unref()

		return nil, err
	}
}

// mouthOf is the mouth of the datagram socket a path names, held.
func (p *Process) mouthOf(path string) (*pairEnd, error) {
	var mouth *pairEnd
	err := p.onPath(atFdcwd, path, func(v Dir) error {
		f, err := v.connectPath(p, path)

		if err != nil {
			return err
		}

		if e := f.asPair(); e != nil && e.shared.kind == sockDgram {
			mouth = e

			return nil
		}

		f.unref()

		return syscall.EPROTOTYPE
	})

	return mouth, err
}

// destination is the path of sendto's or sendmsg's address, none without
// one.
func (p *Process) destination(addr, alen uint32) (string, error) {
	if addr == 0 {
		return "", nil
	}

	return p.unixSocketPath(addr, alen)
}

// epollAt is the epoll of a descriptor: another file is EINVAL.
func (p *Process) epollAt(fd uint32) (*epoll, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	if ep := obj.asEpoll(); ep != nil {
		return ep, nil
	}

	return nil, syscall.EINVAL
}

func (p *Process) libcEpoll_create(size uint32) uint32 {
	if int32(size) <= 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(p.install(newEpoll(), 0, false))
}

func (p *Process) libcEpoll_create1(flags uint32) uint32 {
	if flags&^syscall.O_CLOEXEC != 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(p.install(newEpoll(), 0, flags&syscall.O_CLOEXEC != 0))
}

func (p *Process) libcEpoll_ctl(epfd, op, fd, at uint32) uint32 {
	ep, err := p.epollAt(epfd)

	if err != nil {
		return p.result(0, err)
	}

	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	if fd == epfd {
		return p.result(0, syscall.EINVAL)
	}

	var events uint32
	var data uint64

	if op != epollCtlDel {
		if at == 0 {
			return p.result(0, syscall.EFAULT)
		}

		b := p.span(at, epollEventSize)
		events, data = binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint64(b[8:])
	}

	return p.result(0, ep.ctl(op, epollKey{fd, obj}, events, data))
}

// epollWait waits for up to max ready members, for timeout milliseconds,
// forever when negative; a signal interrupts it, restarted or not.
func (p *Process) epollWait(epfd, at, max uint32, timeout int) (int, error) {
	if int32(max) <= 0 || max > epollMaxEvents {
		return 0, syscall.EINVAL
	}

	ep, err := p.epollAt(epfd)

	if err != nil {
		return 0, err
	}

	return ep.wait(p, p.span(at, max*epollEventSize), int(max), deadline(timeout))
}

func (p *Process) libcEpoll_wait(epfd, at, max, timeout uint32) uint32 {
	return p.result(p.epollWait(epfd, at, max, int(int32(timeout))))
}

func (p *Process) libcEpoll_pwait(epfd, at, max, timeout, mask uint32) uint32 {
	return p.result(p.withMask(mask, func() (int, error) {
		return p.epollWait(epfd, at, max, int(int32(timeout)))
	}))
}

// opened is the open file an OPEN's OK brings, the first of its files:
// the end of a pair its server made, which the OK's body, an opened,
// says what it is, a reference to it the caller's. With no body the end
// is the opener's as it is, a plain socket (/dev/init's). The end is the
// open file's from then on: the kernel makes requests of the open file's
// calls on connections to the pair's other end, the server's endpoint of
// it. What a server says wrongly of it is EIO.
func (p *Process) opened(pk *packet) (Object, error) {
	f := pk.files[0]

	if len(pk.data) == 0 {
		return f, nil
	}

	end := f.asPair()

	if len(pk.data) != sizeofOpened || end == nil || end.shared.kind != sockSeqpacket {
		f.unref()

		return nil, syscall.EIO
	}

	b := pk.data
	obj, err := served(p, binary.LittleEndian.Uint32(b), int(binary.LittleEndian.Uint32(b[4:])), binary.LittleEndian.Uint64(b[8:]),
		binary.LittleEndian.Uint64(b[16:]), binary.LittleEndian.Uint64(b[24:]), end)

	if err != nil {
		f.unref()

		return nil, syscall.EIO
	}

	return obj, nil
}

// head writes a request's header, the process the kernel calls for its
// user and group.
func (p *Process) head(data []byte, op, arg uint32) {
	binary.LittleEndian.PutUint32(data, op)
	binary.LittleEndian.PutUint32(data[4:], arg)
	binary.LittleEndian.PutUint32(data[8:], p.cred.euid())
	binary.LittleEndian.PutUint32(data[12:], p.cred.egid())
}

// disconnect lets go of the connections the process keeps, as it ends.
func (p *Process) disconnect() {
	p.disconnected = true

	for i := range p.kept {
		if c := p.kept[i].c; c != nil {
			c.unref()
		}

		p.kept[i] = keptConnection{}
	}
}

// permits tells whether the process may read (4), write (2) or execute
// (1) what a stat describes, as a server decides it: root everything but
// executing what nobody may, the owner by the owner's bits, a member of
// the group, its effective gid or a supplementary group, by the group's,
// everyone else by the others'.
func (p *Process) permits(st *syscall.Stat_t, want uint32) bool {
	if p.cred.privileged() {
		return want&1 == 0 || st.Mode&syscall.S_IFMT == syscall.S_IFDIR || st.Mode&0o111 != 0
	}

	bits := st.Mode

	switch {
	case p.cred.euid() == st.Uid:
		bits >>= 6
	case p.cred.inGroup(st.Gid):
		bits >>= 3
	}

	return bits&want == want
}

// rootID is the id of the process's root, or of the handle a call starts
// at for a process whose root is no handle: rootfs's own children, before
// they chroot.
func (p *Process) rootID(id uint64) uint64 {
	if root := p.root.asVFS(); root != nil {
		return root.id
	}

	return id
}

// The terminal's requests of ioctl, Linux's.
const (
	tiocsctty  = 0x540e
	tiocoutq   = 0x5411
	tiocnotty  = 0x5422
	tiocgsid   = 0x5429
	tiocgptn   = 0x80045430
	tiocsptlck = 0x40045431
)

// sameTerminal tells whether two opens are of one terminal.
func sameTerminal(a, b Terminal) bool {
	return a.device() == b.device()
}

// isCtty tells whether a terminal is the caller's controlling terminal.
func (p *Process) isCtty(t Terminal) bool {
	p.k.mu.Lock()
	c := p.k.cttys[p.sid]
	p.k.mu.Unlock()

	return c != nil && sameTerminal(c, t)
}

// openControlling opens the caller's controlling terminal anew, /dev/tty's
// open: another open of the terminal, which no owner's permission keeps
// out, as Linux's does not; ENXIO for a session that has none, or whose
// terminal hung up, which controls it no more.
func (p *Process) openControlling(flags int) (Object, error) {
	p.k.mu.Lock()
	c := p.k.cttys[p.sid]

	if c != nil {
		c.ref()
	}

	p.k.mu.Unlock()

	if c == nil {
		return nil, syscall.ENXIO
	}

	defer c.unref()

	if _, _, hungUp := c.jobs(); hungUp {
		return nil, syscall.ENXIO
	}

	return c.reopen(p, flags)
}

// acquire makes a terminal opened without O_NOCTTY the controlling
// terminal of the caller's session when it leads one that has none, as
// Linux's open does; one that controls another stays as it is.
func (p *Process) acquire(t Terminal) {
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

// background is job control's check of a call on a terminal: a process
// whose controlling terminal it is, of a group not in its foreground,
// stops by sig, SIGTTIN for a read, SIGTTOU for a write with TOSTOP or a
// change of the terminal. A read that ignores or blocks SIGTTIN is EIO,
// and one of an orphaned group, which no one would continue; a write or
// a change that ignores or blocks SIGTTOU goes on, one of an orphaned
// group is EIO. Else the group hears sig, and the call goes on once the
// process does, when its handlers ask for that: EINTR else.
func (p *Process) background(t Terminal, sig syscall.Signal) error {
	for {
		foreground, _, _ := t.jobs()
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

// changeAt is the terminal of a descriptor for a call that changes it,
// past job control's check.
func (p *Process) changeAt(fd uint32) (Terminal, error) {
	t, err := p.terminalAt(fd)

	if err == nil {
		err = p.background(t, syscall.SIGTTOU)
	}

	return t, err
}

// leaveSession is the going of a session's leader, or its TIOCNOTTY: its
// controlling terminal controls no session any more, and its foreground
// group hears SIGHUP, and SIGCONT after for a TIOCNOTTY, as Linux's
// pseudoterminals do.
func (p *Process) leaveSession(going bool) {
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

	foreground, _, _ := t.jobs()
	t.control(p, 0, 0)

	if foreground > 0 {
		p.k.signalGroup(foreground, int(syscall.SIGHUP))

		if !going {
			p.k.signalGroup(foreground, int(syscall.SIGCONT))
		}
	}

	t.unref()
}

// terminalAt is the terminal of a descriptor: EBADF for none open, ENOTTY
// for a file that is no terminal.
func (p *Process) terminalAt(fd uint32) (Terminal, error) {
	f, err := p.fd(fd)

	if err != nil {
		return nil, err
	}

	t := f.asTerminal()

	if t == nil {
		return nil, syscall.ENOTTY
	}

	return t, nil
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

	settings, err := t.getattr(p)

	if err == nil {
		copy(p.span(at, sizeofTermios), settings)
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

	return t.setattr(p, action, append([]byte(nil), p.span(at, sizeofTermios)...))
}

func (p *Process) libcTcflow(fd, action uint32) uint32 {
	t, err := p.changeAt(fd)

	if err == nil {
		err = t.flow(p, action)
	}

	return p.result(0, err)
}

func (p *Process) libcTcflush(fd, queue uint32) uint32 {
	t, err := p.changeAt(fd)

	if err == nil {
		err = t.flush(p, queue)
	}

	return p.result(0, err)
}

func (p *Process) libcTcdrain(fd uint32) uint32 {
	t, err := p.changeAt(fd)

	if err == nil {
		err = t.drain(p)
	}

	return p.result(0, err)
}

// A pseudoterminal has no line to break.
func (p *Process) libcTcsendbreak(fd, duration uint32) uint32 {
	_, err := p.changeAt(fd)

	return p.result(0, err)
}

// tcgetpgrp and tcsetpgrp are the foreground group of the caller's
// controlling terminal, or of the console (console.go); a master
// tells its slave's. The group set is one of the caller's session.
func (p *Process) libcTcgetpgrp(fd uint32) uint32 {
	if t, err := p.terminalAt(fd); err != syscall.ENOTTY {
		if err == nil && !p.isCtty(t) && !isMaster(p, t) {
			err = syscall.ENOTTY
		}

		if err != nil {
			return p.result(0, err)
		}

		group, err := t.foreground(p)

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
		return p.result(0, t.setForeground(p, pgrp))
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

// isMaster tells whether a terminal is a master, which has a slave.
func isMaster(p *Process, t Terminal) bool {
	_, err := t.name(p, true)

	return err == nil
}

// controls makes a terminal the controlling terminal of the caller's
// session, TIOCSCTTY's: the caller leads a session that has none, or has
// this one, and its group is the foreground group. EPERM when the
// terminal controls another session.
func (p *Process) controls(t Terminal) error {
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

	if err := t.control(p, sid, pgid); err != nil {
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
func (p *Process) notty(t Terminal) error {
	if !p.isCtty(t) {
		return syscall.ENOTTY
	}

	p.leaveSession(false)

	return nil
}

// terminalIoctl is ioctl's request on a terminal, and whether it is one
// of a terminal's.
func (p *Process) terminalIoctl(t Terminal, fd, req, va uint32) (uint32, bool) {
	arg := p.varArg(va, 0)

	switch req {
	case tcgets:
		return p.result(0, p.tcgetattr(fd, arg)), true
	case tcsets, tcsets + 1, tcsets + 2:
		return p.result(0, p.tcsetattr(fd, req-tcsets, arg)), true
	case tcflsh:
		return p.libcTcflush(fd, arg), true
	case tcxonc:
		return p.libcTcflow(fd, arg), true
	case tcsbrk:
		if err := p.background(t, syscall.SIGTTOU); err != nil || arg == 0 {
			return p.result(0, err), true
		}

		return p.result(0, t.drain(p)), true
	case tiocgwinsz:
		size, err := t.window(p)

		if err == nil {
			copy(p.span(arg, sizeofWinsize), size)
		}

		return p.result(0, err), true
	case tiocswinsz:
		return p.result(0, t.setWindow(p, append([]byte(nil), p.span(arg, sizeofWinsize)...))), true
	case fionread, tiocoutq:
		count := t.pending

		if req == tiocoutq {
			count = t.unsent
		}

		n, err := count(p)

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
func (p *Process) slaveNumber(t Terminal) (int, error) {
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
		var t Terminal

		if t, err = p.terminalAt(uint32(slave)); err == nil {
			err = t.setWindow(p, append([]byte(nil), p.span(winp, sizeofWinsize)...))
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

		if t.control(p, c.pid, c.pid) == nil {
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

	if p.passwordAt == 0 {
		p.setErrno(syscall.ENOMEM)

		return 0
	}

	at := p.passwordAt
	saved, err := t.getattr(p)

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	quiet := append([]byte(nil), saved...)
	le := binary.LittleEndian
	le.PutUint32(quiet, le.Uint32(quiet)&^(inlcr|igncr)|icrnl)
	le.PutUint32(quiet[12:], le.Uint32(quiet[12:])&^(echo|isig)|icanon)
	t.setattr(p, 2, quiet)
	t.write(p, p.span(prompt, uint32(len(p.str(prompt)))), ioMode{})
	n, err := t.read(p, p.span(at, passwordSize), ioMode{})

	if err == nil {
		if n > 0 && p.span(at, passwordSize)[n-1] == '\n' || n == passwordSize {
			n--
		}

		p.span(at, passwordSize)[n] = 0
	}

	t.setattr(p, 2, saved)
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

// isConsole tells whether a descriptor is the console's.
func (p *Process) isConsole(fd uint32) (bool, error) {
	obj, err := p.fd(fd)

	if err != nil {
		return false, err
	}

	return obj.asConsole() != nil, nil
}

func (p *Process) putTimespec(at uint32, d time.Duration) {
	p.put64(at, uint64(d/time.Second))
	p.put32(at+8, uint32(d%time.Second))
}

// timespec reads a struct timespec as a duration, which it bounds.
func (p *Process) timespec(at uint32) (time.Duration, error) {
	sec, nsec := int64(p.u64(at)), int64(int32(p.u32(at+8)))

	if sec < 0 || nsec < 0 || nsec >= 1e9 {
		return 0, syscall.EINVAL
	}

	return time.Duration(min(sec, 1<<33))*time.Second + time.Duration(nsec), nil
}

// timeval reads a struct timeval as a duration, which it bounds.
func (p *Process) timeval(at uint32) (time.Duration, error) {
	sec, usec := int64(p.u64(at)), int64(p.u64(at+8))

	if sec < 0 || usec < 0 || usec >= 1e6 {
		return 0, syscall.EINVAL
	}

	return time.Duration(min(sec, 1<<33))*time.Second + time.Duration(usec)*time.Microsecond, nil
}

func (p *Process) putTimeval(at uint32, d time.Duration) {
	p.put64(at, uint64(d/time.Second))
	p.put64(at+8, uint64(d%time.Second/time.Microsecond))
}

// The interval timers of setitimer: each sends its signal when its value
// passes, then every interval. All of them count real time, vm having
// no CPU clocks of the processes. They live through exec; a child has none.
//
// A timer is the process's own, and only the process touches it: no lock.
// Its epoch counts every time it is set, stopped, or taken as it fires.
// Setting it asks Go to put an event in the process's queue when the time
// comes, one that bears the epoch the timer had then; the process, as it
// takes the event, in a wait or where its code is interrupted, finds the
// epoch the timer's still, and then the timer has fired: it is set for
// its interval, and the process sends itself the signal. An event of
// another epoch is of a timer stopped or set again since, and is passed
// over. So a timer that is stopped sends nothing after: its signal is
// sent by the process itself, as it takes the event, before whatever it
// does next.
const (
	itimerReal = 0
	itimerProf = 2
)

var itimerSignals = [3]int{int(syscall.SIGALRM), int(syscall.SIGVTALRM), int(syscall.SIGPROF)}

// itimer is armed while timer is not nil, to fire at end, then every
// interval: end and interval are what is left of it and what it repeats
// by, for those who ask.
type itimer struct {
	epoch    uint64
	timer    *time.Timer
	end      time.Time
	interval time.Duration
}

// left is what remains of an armed timer, at least a microsecond, or 0.
func (t *itimer) left() time.Duration {
	if t.timer == nil {
		return 0
	}

	return max(time.Until(t.end), time.Microsecond)
}

// setTimer arms a timer, or disarms it for a zero value, and returns what
// was left of it and its interval.
func (p *Process) setTimer(which int, value, interval time.Duration) (time.Duration, time.Duration) {
	t := &p.timers[which]
	left, every := t.left(), t.interval

	if t.timer != nil {
		t.timer.Stop()
	}

	t.epoch++
	t.timer, t.interval = nil, 0

	if value > 0 {
		t.interval = interval
		p.armTimer(which, time.Now().Add(value))
	}

	return left, every
}

// armTimer has a timer fire at end: Go puts an event of the timer's epoch
// now in the process's queue then, and has the process take it though it
// runs its own code (nudge).
func (p *Process) armTimer(which int, end time.Time) {
	t := &p.timers[which]
	t.end = end
	epoch := t.epoch
	e := &event{kind: eventTimer, run: func(p *Process) { p.timerDue(which, epoch) }}

	t.timer = time.AfterFunc(time.Until(end), func() {
		p.post(e)
		p.nudge()
	})
}

// timerDue is the process taking the event of a timer set at epoch: the
// timer's still, it has fired.
func (p *Process) timerDue(which int, epoch uint64) {
	t := &p.timers[which]

	if epoch != t.epoch {
		return
	}

	t.epoch++

	if t.interval > 0 {
		end := t.end.Add(t.interval)

		if now := time.Now(); end.Before(now) {
			end = now.Add(t.interval)
		}

		p.armTimer(which, end)
	} else {
		t.timer = nil
	}

	p.sendSelf(itimerSignals[which])
}

// stopTimers disarms the timers of an ending process.
func (p *Process) stopTimers() {
	for i := range p.timers {
		t := &p.timers[i]

		if t.timer != nil {
			t.timer.Stop()
		}

		t.epoch++
		t.timer = nil
	}
}

// Time of the C locale. The zone is the guest's TZ at each call, as musl
// reads it, /etc/localtime without one: a zoneinfo file of the root for
// TZ that begins with a colon or has a slash before any comma, a path
// for one that begins with a slash or a dot, else a name under the
// directories of zoneDirs; UTC when there is no such file. Any other TZ
// is a POSIX zone, whose standard offset is taken. A process keeps the
// zone it read last, as musl does, until TZ changes.

type zoneCache struct {
	tz  string
	loc *time.Location
}

var zoneDirs = []string{"/usr/share/zoneinfo/", "/share/zoneinfo/", "/etc/zoneinfo/"}

// zoneMax is the most bytes of a zoneinfo file read.
const zoneMax = 1 << 20

func (p *Process) location() *time.Location {
	tz, ok := p.getenv("TZ")

	if !ok {
		tz = "/etc/localtime"
	}

	if p.zone.loc == nil || p.zone.tz != tz {
		p.zone = zoneCache{tz, p.parseZone(tz)}
	}

	return p.zone.loc
}

func (p *Process) parseZone(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}

	if slash := strings.IndexByte(tz, '/'); tz[0] == ':' || slash >= 0 && !strings.Contains(tz[:slash], ",") {
		return p.zoneFile(strings.TrimPrefix(tz, ":"))
	}

	return posixZone(tz)
}

// zoneFile is the zone of a zoneinfo file a TZ names, or UTC.
func (p *Process) zoneFile(name string) *time.Location {
	var paths []string

	switch {
	case strings.HasPrefix(name, "/") || strings.HasPrefix(name, "."):
		paths = []string{name}
	case len(name) <= 255 && !strings.Contains(name, "."):
		for _, dir := range zoneDirs {
			paths = append(paths, dir+name)
		}
	}

	for _, path := range paths {
		if data, err := p.readPath(path, zoneMax); err == nil && len(data) >= 44 && string(data[:4]) == "TZif" {
			if loc, err := time.LoadLocationFromTZData(name, data); err == nil {
				return loc
			}
		}
	}

	return time.UTC
}

// tzset sets tzname, timezone and daylight for the zone: its standard
// time's name and offset, and its daylight saving time's name, as this
// year has them.
func (p *Process) tzset() {
	if p.tzAt[0] == 0 {
		return
	}

	loc := p.location()
	year := time.Now().In(loc).Year()
	std, stdOffset := time.Date(year, time.January, 1, 0, 0, 0, 0, loc).Zone()
	dst, dstOffset := time.Date(year, time.July, 1, 0, 0, 0, 0, loc).Zone()

	if time.Date(year, time.January, 1, 0, 0, 0, 0, loc).IsDST() {
		std, stdOffset, dst, dstOffset = dst, dstOffset, std, stdOffset
	}

	p.put32(p.tzAt[0], p.constant(std))
	p.put32(p.tzAt[0]+4, p.constant(dst))
	p.put32(p.tzAt[1], uint32(int32(-stdOffset)))
	p.put32(p.tzAt[2], bit(stdOffset != dstOffset))
}

func (p *Process) readTm(at uint32) tmFields {
	b := p.span(at, 44)
	f := func(i int) int {
		return int(int32(binary.LittleEndian.Uint32(b[4*i:])))
	}
	tm := tmFields{f(0), f(1), f(2), f(3), f(4), f(5), f(6), f(7), f(8), f(9), ""}

	if zone := binary.LittleEndian.Uint32(b[40:]); zone != 0 {
		tm.zone = p.str(zone)
	}

	return tm
}

// writeTm writes a representable struct tm; its zone name is a constant.
func (p *Process) writeTm(at uint32, t time.Time) bool {
	tm := tmOf(t)

	if tm.year != int(int32(tm.year)) {
		return false
	}

	b := p.span(at, 44)

	for i, v := range []int{tm.sec, tm.min, tm.hour, tm.mday, tm.mon, tm.year, tm.wday, tm.yday, tm.isdst, tm.gmtoff} {
		binary.LittleEndian.PutUint32(b[4*i:], uint32(int32(v)))
	}

	p.put32(at+40, p.constant(tm.zone))

	return true
}

func (p *Process) libcTime(at uint32) uint64 {
	now := time.Now().Unix()

	if at != 0 {
		p.put64(at, uint64(now))
	}

	return uint64(now)
}

func (p *Process) libcGettimeofday(tv, tz uint32) uint32 {
	now := time.Now()

	if tv != 0 {
		p.put64(tv, uint64(now.Unix()))
		p.put64(tv+8, uint64(now.Nanosecond()/1000))
	}

	return 0
}

func (p *Process) libcLocaltime(t uint32) uint32 {
	return p.localtime(t, p.staticAt(staticTm))
}

func (p *Process) libcLocaltime_r(t, at uint32) uint32 {
	return p.localtime(t, at)
}

func (p *Process) localtime(t, at uint32) uint32 {
	p.tzset()

	if !p.writeTm(at, time.Unix(int64(p.u64(t)), 0).In(p.location())) {
		p.setErrno(syscall.EOVERFLOW)

		return 0
	}

	return at
}

func (p *Process) libcAsctime(tm uint32) uint32 {
	s, ok := asctime(p.readTm(tm))

	if !ok {
		p.setErrno(syscall.EOVERFLOW)

		return 0
	}

	at := p.staticAt(staticCtime)
	p.putString(at, s)

	return at
}

func (p *Process) libcAsctime_r(tm, buf uint32) uint32 {
	s, ok := asctime(p.readTm(tm))

	if !ok {
		p.setErrno(syscall.EOVERFLOW)

		return 0
	}

	p.putString(buf, s)

	return buf
}

func (p *Process) libcCtime_r(t, buf uint32) uint32 {
	s, ok := asctime(tmOf(time.Unix(int64(p.u64(t)), 0).In(p.location())))

	if !ok {
		p.setErrno(syscall.EOVERFLOW)

		return 0
	}

	p.putString(buf, s)

	return buf
}

func (p *Process) libcDifftime(t1, t0 uint64) float64 {
	return float64(int64(t1)) - float64(int64(t0))
}

// timegm is mktime in UTC: it normalizes the struct tm.
func (p *Process) libcTimegm(at uint32) uint64 {
	tm := p.readTm(at)
	t := time.Date(tm.year+1900, time.Month(tm.mon+1), tm.mday, tm.hour, tm.min, tm.sec, 0, time.UTC)

	if !p.writeTm(at, t) {
		p.setErrno(syscall.EOVERFLOW)

		return math.MaxUint64
	}

	return uint64(t.Unix())
}

func (p *Process) libcUsleep(us uint32) uint32 {
	left := p.sleep(time.Duration(us) * time.Microsecond)

	if left > 0 {
		return p.result(0, syscall.EINTR)
	}

	return p.result(0, nil)
}

func (p *Process) libcUalarm(value, interval uint32) uint32 {
	left, _ := p.setTimer(itimerReal, time.Duration(value)*time.Microsecond, time.Duration(interval)*time.Microsecond)

	return uint32(min(left/time.Microsecond, math.MaxUint32))
}

func (p *Process) libcGmtime(t uint32) uint32 {
	return p.gmtime(t, p.staticAt(staticTm))
}

func (p *Process) libcClock() uint64 {
	return uint64(clockGettime(clockProcessCputime) / time.Microsecond)
}

func (p *Process) libcClock_getres(clock, res uint32) uint32 {
	host, ok := hostClock(clock)

	if !ok {
		return p.result(0, syscall.EINVAL)
	}

	var t syscall.Timespec
	syscall.Syscall(syscall.SYS_CLOCK_GETRES, host, uintptr(unsafe.Pointer(&t)), 0)

	if res != 0 {
		p.put64(res, uint64(t.Sec))
		p.put32(res+8, uint32(t.Nsec))
	}

	return p.result(0, nil)
}

// clock_nanosleep returns an error number; it sleeps on the real time
// clocks.
func (p *Process) libcClock_nanosleep(clock, flags, req, rem uint32) uint32 {
	switch clock {
	case clockRealtime, clockMonotonic, clockBoottime:
	case clockProcessCputime:
		return uint32(syscall.ENOTSUP)
	default:
		return uint32(syscall.EINVAL)
	}

	d, err := p.timespec(req)

	if err != nil {
		return uint32(syscall.EINVAL)
	}

	if flags&timerAbstime != 0 {
		d -= clockGettime(uintptr(clock))
	}

	left := p.sleep(d)

	if left > 0 {
		if rem != 0 && flags&timerAbstime == 0 {
			p.putTimespec(rem, left)
		}

		return uint32(syscall.EINTR)
	}

	return 0
}

func (p *Process) libcSetitimer(which, value, old uint32) uint32 {
	if which > itimerProf {
		return p.result(0, syscall.EINVAL)
	}

	interval, err := p.timeval(value)

	if err != nil {
		return p.result(0, err)
	}

	v, err := p.timeval(value + 16)

	if err != nil {
		return p.result(0, err)
	}

	left, every := p.setTimer(int(which), v, interval)

	if old != 0 {
		p.putTimeval(old, every)
		p.putTimeval(old+16, left)
	}

	return p.result(0, nil)
}

func (p *Process) libcGetitimer(which, at uint32) uint32 {
	if which > itimerProf {
		return p.result(0, syscall.EINVAL)
	}

	t := &p.timers[which]
	left, every := t.left(), t.interval
	p.putTimeval(at, every)
	p.putTimeval(at+16, left)

	return p.result(0, nil)
}

func (p *Process) libcAlarm(seconds uint32) uint32 {
	left, _ := p.setTimer(itimerReal, time.Duration(seconds)*time.Second, 0)

	return uint32((left + time.Second - 1) / time.Second)
}

func (p *Process) libcMktime(at uint32) uint64 {
	p.tzset()
	tm := p.readTm(at)
	t := time.Date(tm.year+1900, time.Month(tm.mon+1), tm.mday, tm.hour, tm.min, tm.sec, 0, p.location())

	if !p.writeTm(at, t) {
		p.setErrno(syscall.EOVERFLOW)

		return math.MaxUint64
	}

	return uint64(t.Unix())
}

func (p *Process) libcCtime(t uint32) uint32 {
	return p.libcCtime_r(t, p.staticAt(staticCtime))
}

func (p *Process) libcStrftime(s, max, format, tm uint32) uint32 {
	out := strftime(string(p.cstr(format)), p.readTm(tm))

	if uint32(len(out)) >= max {
		return 0
	}

	p.putString(s, out)

	return uint32(len(out))
}

func (p *Process) libcStrptime(s, format, tm uint32) uint32 {
	fields := p.readTm(tm)
	n, ok := strptime(p.cstr(s), string(p.cstr(format)), &fields)

	if !ok {
		return 0
	}

	b := p.span(tm, 36)

	for i, v := range []int{fields.sec, fields.min, fields.hour, fields.mday, fields.mon, fields.year, fields.wday, fields.yday, fields.isdst} {
		binary.LittleEndian.PutUint32(b[4*i:], uint32(int32(v)))
	}

	return s + uint32(n)
}

func (p *Process) libcTimes(at uint32) uint64 {
	var t syscall.Tms
	ticks, err := syscall.Times(&t)

	if err != nil {
		return p.result64(0, err)
	}

	if at != 0 {
		for i, v := range []int64{t.Utime, t.Stime, t.Cutime, t.Cstime} {
			p.put64(at+uint32(8*i), uint64(v))
		}
	}

	return p.result64(int64(ticks), nil)
}

func (p *Process) libcSleep(seconds uint32) uint32 {
	left := p.sleep(time.Duration(seconds) * time.Second)

	return uint32((left + time.Second - 1) / time.Second)
}

func (p *Process) libcClock_gettime(clock, ts uint32) uint32 {
	t, err := p.clockNow(clock)

	if err != nil {
		return p.result(0, err)
	}

	p.put64(ts, uint64(t/1e9))
	p.put32(ts+8, uint32(t%1e9))

	return p.result(0, nil)
}

// clockNow is a clock's time in nanoseconds, as clock_gettime tells it.
func (p *Process) clockNow(clock uint32) (int64, error) {
	host, ok := hostClock(clock)

	if !ok {
		return 0, syscall.EINVAL
	}

	var t syscall.Timespec
	syscall.Syscall(syscall.SYS_CLOCK_GETTIME, host, uintptr(unsafe.Pointer(&t)), 0)

	return t.Sec*1e9 + t.Nsec, nil
}

func (p *Process) libcNanosleep(req, rem uint32) uint32 {
	d, err := p.timespec(req)

	if err != nil {
		return p.result(0, err)
	}

	left := p.sleep(d)

	if left > 0 {
		if rem != 0 {
			p.put64(rem, uint64(left/time.Second))
			p.put32(rem+8, uint32(left%time.Second/time.Nanosecond))
		}

		return p.result(0, syscall.EINTR)
	}

	return p.result(0, nil)
}

// gmtime writes a time as UTC's struct tm, or fails with EOVERFLOW when
// its year is no int.
func (p *Process) gmtime(t, at uint32) uint32 {
	sec := int64(p.u64(t))
	u := time.Unix(sec, 0).UTC()

	if !p.writeTm(at, u) {
		p.setErrno(syscall.EOVERFLOW)

		return 0
	}

	return at
}

// sleep waits for d or an interrupting signal and returns what is left.
func (p *Process) sleep(d time.Duration) time.Duration {
	end := time.Now().Add(d)

	for {
		left := time.Until(end)

		if left <= 0 {
			return 0
		}

		ms := min(left.Milliseconds()+1, 1<<30)

		if p.idle(int(ms), false) != nil {
			return max(time.Until(end), 0)
		}
	}
}

func (p *Process) libcClock_settime(clock, ts uint32) uint32 {
	return p.result(0, syscall.EPERM)
}

func (p *Process) libcTzset() {
	p.tzset()
}

// The clock is the host's, which no process may set.
func (p *Process) libcSettimeofday(tv, tz uint32) uint32 {
	return p.result(0, syscall.EPERM)
}

func (p *Process) libcStime(t uint32) uint32 {
	return p.result(0, syscall.EPERM)
}

func (p *Process) libcAdjtime(delta, old uint32) uint32 {
	return p.result(0, syscall.EPERM)
}

func (p *Process) libcGmtime_r(t, at uint32) uint32 {
	return p.gmtime(t, at)
}

// What the system tells of itself: memory, load and resource usage, which
// are the host's and vm's, configuration values, and the names of the
// login and terminals.

// The system logger: there is none to send messages to, so syslog writes
// one only to standard error, when openlog asked with LOG_PERROR. openlog
// copies the identity, as musl does; the static area keeps it, the options
// and the levels setlogmask masked.
const (
	logPid       = 0x01
	logPerror    = 0x20
	logIdentSize = 32
)

func (p *Process) syslog(priority, message uint32) {
	if p.u32(p.staticAt(staticLogMask))&(1<<(priority&7)) != 0 {
		return
	}

	option := p.u32(p.staticAt(staticLogOption))

	if option&logPerror == 0 {
		return
	}

	line := p.str(p.staticAt(staticLogIdent))

	if option&logPid != 0 {
		line += fmt.Sprintf("[%d]", p.pid)
	}

	line += ": " + p.str(message)

	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}

	if t, err := p.transferAt(2, false); err == nil {
		t.write(p, []byte(line), ioMode{})
	}
}

// pathconfValues are musl's values of pathconf by _PC_ name; -1 means no
// limit.
var pathconfValues = []int32{8, 255, 255, 255, 4096, 4096, 1, 1, 0, 1, -1, -1, -1, 64, 4096, 4096, 4096, 4096, 4096, -1, 1}

func (p *Process) pathconf(name uint32) uint32 {
	if name >= uint32(len(pathconfValues)) {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(int(pathconfValues[name]), nil)
}

// sysinfo writes the guest's struct sysinfo: the host's, its sizes in a
// unit that fits them in 32 bits, and the kernel's count of processes.
func (p *Process) sysinfo(at uint32) error {
	var si syscall.Sysinfo_t

	if err := syscall.Sysinfo(&si); err != nil {
		return err
	}

	sizes := []uint64{si.Totalram, si.Freeram, si.Sharedram, si.Bufferram, si.Totalswap, si.Freeswap, si.Totalhigh, si.Freehigh}
	unit := uint64(max(si.Unit, 1))
	scale := unit

	for _, s := range sizes {
		for s*unit/scale > math.MaxUint32 {
			scale *= 2
		}
	}

	b := p.span(at, 312)
	clear(b)
	p.put32(at, uint32(min(uint64(si.Uptime), math.MaxUint32)))

	for i, l := range si.Loads {
		p.put32(at+4+4*uint32(i), uint32(l))
	}

	for i, s := range sizes[:6] {
		p.put32(at+16+4*uint32(i), uint32(s*unit/scale))
	}

	p.put16(at+40, uint16(min(len(p.k.processes(func(*Process) bool { return true })), math.MaxUint16)))
	p.put32(at+44, uint32(sizes[6]*unit/scale))
	p.put32(at+48, uint32(sizes[7]*unit/scale))
	p.put32(at+52, uint32(scale))

	return nil
}

// pages is the host's memory, or its free memory, in pages.
func pages(free bool) int {
	var si syscall.Sysinfo_t

	if syscall.Sysinfo(&si) != nil {
		return -1
	}

	n := si.Totalram

	if free {
		n = si.Freeram
	}

	return int(min(n*uint64(max(si.Unit, 1))/4096, math.MaxInt32))
}

// putRusage writes the guest's struct rusage: CPU times and the counts
// of the host's, which vm's processes share.
func (p *Process) putRusage(at uint32, ru *syscall.Rusage) {
	clear(p.span(at, 88))

	if ru == nil {
		return
	}

	p.putTimeval(at, time.Duration(ru.Utime.Nano()))
	p.putTimeval(at+16, time.Duration(ru.Stime.Nano()))

	for i, v := range []int64{ru.Maxrss, ru.Ixrss, ru.Idrss, ru.Isrss, ru.Minflt, ru.Majflt, ru.Nswap, ru.Inblock, ru.Oublock, ru.Msgsnd,
		ru.Msgrcv, ru.Nsignals, ru.Nvcsw, ru.Nivcsw} {
		p.put32(at+32+4*uint32(i), uint32(min(v, math.MaxInt32)))
	}
}

// limitOf reads or sets a resource limit of a process.
func (p *Process) limitOf(q *Process, res, set, old uint32) error {
	if res >= rlimitCount {
		return syscall.EINVAL
	}

	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	cur := q.limits[res]

	if set != 0 {
		l := rlimit{p.u64(set), p.u64(set + 8)}

		if l.cur > l.max {
			return syscall.EINVAL
		}

		if l.max > cur.max && !p.cred.privileged() {
			return syscall.EPERM
		}

		q.limits[res] = l
	}

	if old != 0 {
		p.put64(old, cur.cur)
		p.put64(old+8, cur.max)
	}

	return nil
}

func (p *Process) libcOpenlog(ident, option, facility uint32) {
	name := ""

	if ident != 0 {
		name = truncate(p.str(ident), logIdentSize-1)
	}

	p.putString(p.staticAt(staticLogIdent), name)
	p.put32(p.staticAt(staticLogOption), option)
}

func (p *Process) libcCloselog() {
}

// setlogmask returns the levels it let through; 0 changes nothing.
func (p *Process) libcSetlogmask(mask uint32) uint32 {
	at := p.staticAt(staticLogMask)
	old := ^p.u32(at) & 0xff

	if mask != 0 {
		p.put32(at, ^mask&0xff)
	}

	return old
}

func (p *Process) libcSysinfo(at uint32) uint32 {
	return p.result(0, p.sysinfo(at))
}

func (p *Process) libcGet_nprocs() uint32 {
	return uint32(runtime.NumCPU())
}

func (p *Process) libcGet_nprocs_conf() uint32 {
	return uint32(runtime.NumCPU())
}

func (p *Process) libcGet_phys_pages() uint32 {
	return uint32(pages(false))
}

func (p *Process) libcGet_avphys_pages() uint32 {
	return uint32(pages(true))
}

func (p *Process) libcGetloadavg(at, n uint32) uint32 {
	switch {
	case n == 0:
		return 0
	case int32(n) < 0:
		return math.MaxUint32
	}

	var si syscall.Sysinfo_t

	if syscall.Sysinfo(&si) != nil {
		return math.MaxUint32
	}

	n = min(n, 3)

	for i := range n {
		p.put64(at+8*i, math.Float64bits(float64(si.Loads[i])/65536))
	}

	return n
}

// getrusage of the process, or its thread, is vm's: its
// children's are in it.
func (p *Process) libcGetrusage(who, at uint32) uint32 {
	switch int32(who) {
	case 0, 1:
		var ru syscall.Rusage

		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
			return p.result(0, err)
		}

		p.putRusage(at, &ru)
	case -1:
		p.putRusage(at, nil)
	default:
		return p.result(0, syscall.EINVAL)
	}

	return p.result(0, nil)
}

func (p *Process) libcGetrlimit(res, at uint32) uint32 {
	return p.result(0, p.limitOf(p, res, 0, at))
}

func (p *Process) libcSetrlimit(res, at uint32) uint32 {
	return p.result(0, p.limitOf(p, res, at, 0))
}

func (p *Process) libcPrlimit(pid, res, set, old uint32) uint32 {
	q, err := p.process(int32(pid))

	if err == nil {
		err = p.limitOf(q, res, set, old)
	}

	return p.result(0, err)
}

func (p *Process) libcGetdtablesize() uint32 {
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	return uint32(min(p.limits[rlimitNofile].cur, math.MaxInt32))
}

// confstr knows _CS_PATH.
func (p *Process) libcConfstr(name, buf, n uint32) uint32 {
	if name != 0 {
		p.setErrno(syscall.EINVAL)

		return 0
	}

	s := "/bin:/usr/bin"

	if n > 0 {
		p.putString(buf, s[:min(int(n-1), len(s))])
	}

	return uint32(len(s) + 1)
}

func (p *Process) libcPathconf(path, name uint32) uint32 {
	var st syscall.Stat_t

	if err := p.statPath(p.str(path), true, &st); err != nil {
		return p.result(0, err)
	}

	return p.pathconf(name)
}

func (p *Process) libcFpathconf(fd, name uint32) uint32 {
	if _, err := p.fd(fd); err != nil {
		return p.result(0, err)
	}

	return p.pathconf(name)
}

// The login name is LOGNAME's, as musl's.
func (p *Process) libcGetlogin() uint32 {
	_, value, _ := p.lookupEnv([]byte("LOGNAME"))

	return value
}

func (p *Process) libcGetlogin_r(buf, n uint32) uint32 {
	name, ok := p.getenv("LOGNAME")

	switch {
	case !ok:
		return uint32(syscall.ENXIO)
	case uint32(len(name)) >= n:
		return uint32(syscall.ERANGE)
	}

	p.putString(buf, name)

	return 0
}

func (p *Process) libcCtermid(s uint32) uint32 {
	if s == 0 {
		return p.constant("/dev/tty")
	}

	p.putString(s, "/dev/tty")

	return s
}

func (p *Process) libcTtyname(fd uint32) uint32 {
	name, e := p.terminalName(fd)

	if e != 0 {
		p.setErrno(e)

		return 0
	}

	return p.constant(name)
}

// ttyname_r returns an error number.
func (p *Process) libcTtyname_r(fd, buf, n uint32) uint32 {
	name, e := p.terminalName(fd)

	switch {
	case e != 0:
		return uint32(e)
	case uint32(len(name)) >= n:
		return uint32(syscall.ERANGE)
	}

	p.putString(buf, name)

	return 0
}

func (p *Process) libc__kernel_syslog(priority, message uint32) {
	p.syslog(priority, message)
}

// The rest of libc's services: random's generator, suboptions, daemon,
// prctl, getpass and the list of shells. random is musl's: its state is an
// array of words, the first n, i and j, in the guest's memory; the static
// area keeps the default one and the address of the one in use.

var randomInit = [32]uint32{
	0x00000000, 0x5851f42d, 0xc0b18ccf, 0xcbb5f646, 0xc7033129, 0x30705b04, 0x20fd5db4, 0x9a8b7f78,
	0x502959d8, 0xab894868, 0x6c0356a7, 0x88cdb7ff, 0xb477d43f, 0x70a3a52b, 0xa8e4baf1, 0xfd8341fc,
	0x8ae16fd9, 0x742d2f7a, 0x0d1f0796, 0x76035e09, 0x40f7702c, 0x6fa72ca5, 0xaaa84157, 0x58a0df74,
	0xc74a0364, 0xae533cc4, 0x04185faf, 0x6de3b115, 0x0cab8628, 0xf043bfa4, 0x398150e9, 0x37521657,
}

// randomState is the state random uses: its first word, then x[0]...
func (p *Process) randomState() uint32 {
	slot := p.staticAt(staticRandom)

	if at := p.u32(slot); at != 0 {
		return at
	}

	at := p.staticAt(staticRandomInit)

	for i, v := range randomInit {
		p.put32(at+4*uint32(i), v)
	}

	p.put32(at, 31<<16|3<<8)
	p.put32(slot, at)

	return at
}

func (p *Process) srandom(state, seed uint32) {
	n := p.u32(state) >> 16
	x := state + 4

	if n == 0 {
		p.put32(x, seed)

		return
	}

	i := uint32(1)

	if n == 31 || n == 7 {
		i = 3
	}

	s := uint64(seed)

	for k := range n {
		s = 6364136223846793005*s + 1
		p.put32(x+4*k, uint32(s>>32))
	}

	p.put32(x, p.u32(x)|1)
	p.put32(state, n<<16|i<<8)
}

func (p *Process) random() uint32 {
	state := p.randomState()
	h := p.u32(state)
	n, i, j := h>>16, h>>8&0xff, h&0xff
	x := state + 4

	if n == 0 {
		v := (1103515245*p.u32(x) + 12345) & 0x7fffffff
		p.put32(x, v)

		return v
	}

	if i >= n || j >= n {
		i, j = 0, 0
	}

	p.put32(x+4*i, p.u32(x+4*i)+p.u32(x+4*j))
	k := p.u32(x+4*i) >> 1

	if i++; i == n {
		i = 0
	}

	if j++; j == n {
		j = 0
	}

	p.put32(state, n<<16|i<<8|j)

	return k
}

// temper is rand_r's, musl's.
func temper(x uint32) uint32 {
	x ^= x >> 11
	x ^= x << 7 & 0x9d2c5680
	x ^= x << 15 & 0xefc60000
	x ^= x >> 18

	return x
}

// getsubopt takes the suboption *opt starts with, as musl's: it returns
// the index of its key or -1, its value at *val.
func (p *Process) getsubopt(opt, keys, val uint32) uint32 {
	s := p.u32(opt)
	p.put32(val, 0)
	text := p.cstr(s)
	n := uint32(len(text))

	if i := bytes.IndexByte(text, ','); i >= 0 {
		p.span(s+uint32(i), 1)[0] = 0
		p.put32(opt, s+uint32(i)+1)
		n = uint32(i)
	} else {
		p.put32(opt, s+n)
	}

	sub := string(p.span(s, n))

	for i := uint32(0); ; i++ {
		key := p.u32(keys + 4*i)

		if key == 0 {
			return math.MaxUint32
		}

		k := p.str(key)

		if !strings.HasPrefix(sub, k) {
			continue
		}

		switch {
		case len(sub) > len(k) && sub[len(k)] == '=':
			p.put32(val, s+uint32(len(k))+1)
		case len(sub) > len(k):
			continue
		}

		return i
	}
}

// daemon detaches the process as fork, setsid and the parent's exit would:
// it goes on with a new pid in a session of its own, its children and its
// old pid, which ends with status 0, its parent's. The first process ends
// vm when it ends, so it stays as it is.
func (p *Process) daemon(nochdir, noclose uint32) uint32 {
	if nochdir == 0 {
		p.root.ref()
		p.setCwd(p.root)
	}

	if noclose == 0 {
		obj, err := p.openPath("/dev/null", syscall.O_RDWR, 0)

		if err != nil {
			return p.result(0, err)
		}

		// daemon uses descriptors 0..2, present from runKernel and inherit.
		for fd := range uint32(3) {
			p.placeAt(obj, fd, false)
		}

		obj.unref()
	}

	k := p.k

	if p == k.first {
		return p.result(0, nil)
	}

	k.mu.Lock()
	parent := k.procs[p.ppid]
	old := p.pid
	k.lastPid++
	delete(k.procs, old)
	p.pid, p.pgid, p.sid, p.ppid = k.lastPid, k.lastPid, k.lastPid, k.first.pid
	k.procs[p.pid] = p

	for _, q := range k.procs {
		if q.ppid == old {
			q.ppid = k.first.pid
		}
	}

	k.mu.Unlock()

	if parent != nil && !parent.discardsChildren() {
		k.mu.Lock()
		k.procs[old] = &Process{k: k, pid: old, ppid: parent.pid, pgid: parent.pgid, sid: parent.sid, zombie: true}
		k.mu.Unlock()
		parent.childChanged(int(syscall.SIGCHLD))
	}

	return p.result(0, nil)
}

const (
	prSetPdeathsig  = 1
	prGetPdeathsig  = 2
	prGetDumpable   = 3
	prSetDumpable   = 4
	prSetName       = 15
	prGetName       = 16
	prSetNoNewPrivs = 38
	prGetNoNewPrivs = 39
)

// name is the process's name of prctl: its program's file name, at most
// 15 bytes, until it sets another.
func (p *Process) name() string {
	if p.comm != nil {
		return *p.comm
	}

	if p.image != nil && len(p.image.argv) > 0 {
		return truncate(path.Base(p.image.argv[0]), 15)
	}

	return ""
}

func (p *Process) prctl(option, va uint32) uint32 {
	arg := p.varArg(va, 0)

	switch option {
	case prSetPdeathsig:
		if arg != 0 && !validSignal(arg) {
			return p.result(0, syscall.EINVAL)
		}

		p.k.mu.Lock()
		p.pdeathsig = int(arg)
		p.k.mu.Unlock()
	case prGetPdeathsig:
		p.k.mu.Lock()
		sig := p.pdeathsig
		p.k.mu.Unlock()
		p.put32(arg, uint32(sig))
	case prGetDumpable:
		return p.result(int(bit(!p.undumpable)), nil)
	case prSetDumpable:
		if arg > 1 {
			return p.result(0, syscall.EINVAL)
		}

		p.undumpable = arg == 0
	case prSetName:
		name := truncate(string(p.cstr(arg)), 15)
		p.comm = &name
	case prGetName:
		b := p.span(arg, 16)
		clear(b)
		copy(b, p.name())
	case prSetNoNewPrivs:
		if arg != 1 {
			return p.result(0, syscall.EINVAL)
		}

		p.noNewPrivs = true
	case prGetNoNewPrivs:
		return p.result(int(bit(p.noNewPrivs)), nil)
	default:
		return p.result(0, syscall.EINVAL)
	}

	return p.result(0, nil)
}

// shells are /etc/shells' paths, or musl's two without it.
func (p *Process) shells() []string {
	data, err := p.readPath("/etc/shells", databaseMax)

	if err != nil {
		return []string{"/bin/sh", "/bin/csh"}
	}

	var out []string

	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}

	return out
}

func (p *Process) libcSrandom(seed uint32) {
	p.srandom(p.randomState(), seed)
}

// initstate makes a state of size bytes the one in use and returns
// the one before.
func (p *Process) libcInitstate(seed, state, size uint32) uint32 {
	if size < 8 {
		return 0
	}

	old := p.randomState()
	n := uint32(63)

	switch {
	case size < 32:
		n = 0
	case size < 64:
		n = 7
	case size < 128:
		n = 15
	case size < 256:
		n = 31
	}

	p.span(state, 4*(n+1))
	p.put32(state, n<<16)
	p.srandom(state, seed)
	p.put32(p.staticAt(staticRandom), state)

	return old
}

func (p *Process) libcSetstate(state uint32) uint32 {
	old := p.randomState()
	p.span(state, 4*(p.u32(state)>>16+1))
	p.put32(p.staticAt(staticRandom), state)

	return old
}

func (p *Process) libcRand_r(seed uint32) uint32 {
	s := p.u32(seed)*1103515245 + 12345
	p.put32(seed, s)

	return temper(s) / 2
}

// No program is set-user-ID: secure_getenv is getenv.
func (p *Process) libcSecure_getenv(name uint32) uint32 {
	_, value, _ := p.lookupEnv(p.cstr(name))

	return value
}

func (p *Process) libcGetusershell() uint32 {
	shell := nextEntry(p, staticShellIndex, p.shells())

	if shell == nil {
		return 0
	}

	block := p.staticResult(staticShell, uint32(len(*shell)+1))

	if block != 0 {
		p.putString(block, *shell)
	}

	return block
}

func (p *Process) libcSetusershell() {
	p.put32(p.staticAt(staticShellIndex), 0)
}

func (p *Process) libcEndusershell() {
	p.put32(p.staticAt(staticShellIndex), 0)
}

// The host's names, which no process may set; the host id is
// musl's, 0.
// getentropy fills a buffer of at most 256 bytes with the host's random
// bytes: what a system without a /dev/urandom of the host's reads them
// through, its devfs included.
func (p *Process) libcGetentropy(buf, n uint32) uint32 {
	if n > 256 {
		return p.result(0, syscall.EIO)
	}

	rand.Read(p.span(buf, n))

	return p.result(0, nil)
}

func (p *Process) libcGethostid() uint32 {
	return 0
}

func (p *Process) libcPsignal(sig, s uint32) {
	p.complain(s, signalMessage(int(sig)))
}

func (p *Process) libcPsiginfo(info, s uint32) {
	p.complain(s, signalMessage(int(p.u32(info))))
}

func (p *Process) libc__assert_fail(expr, file, line, function uint32) {
	msg := fmt.Sprintf("Assertion failed: %s (%s: %s: %d)\n", p.str(expr), p.str(file), p.str(function), int32(line))

	if t, err := p.transferAt(2, false); err == nil {
		t.write(p, []byte(msg), ioMode{})
	}

	p.abort()
}

// complain writes a message to standard error after a prefix, perror's
// way.
func (p *Process) complain(prefix uint32, msg string) {
	if prefix != 0 && p.u8(prefix) != 0 {
		msg = p.str(prefix) + ": " + msg
	}

	if t, err := p.transferAt(2, false); err == nil {
		t.write(p, []byte(msg+"\n"), ioMode{})
	}
}

func (p *Process) libcRandom() uint32 {
	return p.random()
}

func (p *Process) libcGetsubopt(opt, keys, val uint32) uint32 {
	return p.getsubopt(opt, keys, val)
}

func (p *Process) libcDaemon(nochdir, noclose uint32) uint32 {
	return p.daemon(nochdir, noclose)
}

func (p *Process) libcPrctl(option, va uint32) uint32 {
	return p.prctl(option, va)
}

// Hanging up the terminal needs privilege.
func (p *Process) libcVhangup() uint32 {
	return p.result(0, syscall.EPERM)
}

func (p *Process) libcSethostid(id uint32) uint32 {
	return p.result(0, syscall.EPERM)
}

const (
	utsSysname = "pizdos"
	utsRelease = "1"
	utsVersion = "#1"
	utsMachine = "wasm32"
	utsField   = 65
)

func (p *Process) libcUname(at uint32) uint32 {
	n := &p.k.names
	n.mu.Lock()
	fields := []string{utsSysname, n.host, utsRelease, utsVersion, utsMachine, n.domain}
	n.mu.Unlock()
	b := p.span(at, utsField*uint32(len(fields)))
	clear(b)

	for i, f := range fields {
		copy(b[i*utsField:], f)
	}

	return p.result(0, nil)
}

func (p *Process) libcGethostname(name, n uint32) uint32 {
	p.k.names.mu.Lock()
	host := p.k.names.host
	p.k.names.mu.Unlock()

	if n > 0 {
		p.putString(name, truncate(host, int(n-1)))
	}

	return p.result(0, nil)
}

func (p *Process) libcGetdomainname(name, n uint32) uint32 {
	p.k.names.mu.Lock()
	domain := p.k.names.domain
	p.k.names.mu.Unlock()

	if uint32(len(domain)) >= n {
		return p.result(0, syscall.EINVAL)
	}

	p.putString(name, domain)

	return p.result(0, nil)
}

func (p *Process) libcSethostname(name, n uint32) uint32 {
	return p.result(0, p.setName(&p.k.names.host, name, n))
}

func (p *Process) libcSetdomainname(name, n uint32) uint32 {
	return p.result(0, p.setName(&p.k.names.domain, name, n))
}

// setName sets one of the names to n bytes at name: EPERM for a process
// without privilege, EINVAL past what uname holds.
func (p *Process) setName(to *string, name, n uint32) error {
	if !p.cred.privileged() {
		return syscall.EPERM
	}

	if n >= utsField {
		return syscall.EINVAL
	}

	text := string(p.span(name, n))
	p.k.names.mu.Lock()
	*to = text
	p.k.names.mu.Unlock()

	return nil
}

// The user and group databases are the root's /etc/passwd and /etc/group,
// read at every call: a missing file is an empty database. The results of
// the functions without _r are heap blocks the static area points at, each
// freed by the next; so are the positions of getpwent and getgrent.

type passwdEntry struct {
	name, passwd      string
	uid, gid          uint32
	gecos, dir, shell string
}

type groupEntry struct {
	name, passwd string
	gid          uint32
	members      []string
}

// databaseMax bounds the files the databases read.
const databaseMax = 16 << 20

// databaseLines are the lines of a database, split in fields, those of
// the field count only.
func (p *Process) databaseLines(path string, fields int) [][]string {
	data, err := p.readPath(path, databaseMax)

	if err != nil {
		return nil
	}

	var out [][]string

	for _, line := range bytes.Split(data, []byte("\n")) {
		if parts := strings.Split(string(line), ":"); len(parts) == fields {
			out = append(out, parts)
		}
	}

	return out
}

func parseId(s string) (uint32, bool) {
	n, err := strconv.ParseUint(s, 10, 32)

	return uint32(n), err == nil
}

func (p *Process) passwdEntries() []passwdEntry {
	var out []passwdEntry

	for _, f := range p.databaseLines("/etc/passwd", 7) {
		uid, ok1 := parseId(f[2])
		gid, ok2 := parseId(f[3])

		if ok1 && ok2 {
			out = append(out, passwdEntry{f[0], f[1], uid, gid, f[4], f[5], f[6]})
		}
	}

	return out
}

func (p *Process) groupEntries() []groupEntry {
	var out []groupEntry

	for _, f := range p.databaseLines("/etc/group", 4) {
		gid, ok := parseId(f[2])

		if !ok {
			continue
		}

		var members []string

		if f[3] != "" {
			members = strings.Split(f[3], ",")
		}

		out = append(out, groupEntry{f[0], f[1], gid, members})
	}

	return out
}

// passwdSize is the room an entry's strings take.
func (e *passwdEntry) size() uint32 {
	return uint32(len(e.name) + len(e.passwd) + len(e.gecos) + len(e.dir) + len(e.shell) + 5)
}

// putPasswd writes a struct passwd at at, its strings at buf.
func (p *Process) putPasswd(at, buf uint32, e *passwdEntry) {
	ptrs := make([]uint32, 0, 5)

	for _, s := range []string{e.name, e.passwd, e.gecos, e.dir, e.shell} {
		p.putString(buf, s)
		ptrs = append(ptrs, buf)
		buf += uint32(len(s) + 1)
	}

	p.put32(at, ptrs[0])
	p.put32(at+4, ptrs[1])
	p.put32(at+8, e.uid)
	p.put32(at+12, e.gid)
	p.put32(at+16, ptrs[2])
	p.put32(at+20, ptrs[3])
	p.put32(at+24, ptrs[4])
}

// size is the room an entry's member pointers, aligned at align, and its
// strings take.
func (e *groupEntry) size(align uint32) uint32 {
	n := (4-align%4)%4 + 4*uint32(len(e.members)+1) + uint32(len(e.name)+len(e.passwd)+2)

	for _, m := range e.members {
		n += uint32(len(m) + 1)
	}

	return n
}

// putGroup writes a struct group at at, its members and strings at buf.
func (p *Process) putGroup(at, buf uint32, e *groupEntry) {
	mem := buf + (4-buf%4)%4
	s := mem + 4*uint32(len(e.members)+1)
	str := func(v string) uint32 {
		at := s
		p.putString(at, v)
		s += uint32(len(v) + 1)

		return at
	}

	p.put32(at, str(e.name))
	p.put32(at+4, str(e.passwd))
	p.put32(at+8, e.gid)
	p.put32(at+12, mem)

	for i, m := range e.members {
		p.put32(mem+4*uint32(i), str(m))
	}

	p.put32(mem+4*uint32(len(e.members)), 0)
}

// passwdResult is the reentrant functions' work: it returns an error
// number, and puts the entry at pwd or nothing at result.
func (p *Process) passwdResult(e *passwdEntry, pwd, buf, size, result uint32) uint32 {
	p.put32(result, 0)

	if e == nil {
		return 0
	}

	if e.size() > size {
		return uint32(syscall.ERANGE)
	}

	p.putPasswd(pwd, buf, e)
	p.put32(result, pwd)

	return 0
}

func (p *Process) groupResult(e *groupEntry, grp, buf, size, result uint32) uint32 {
	p.put32(result, 0)

	if e == nil {
		return 0
	}

	if e.size(buf) > size {
		return uint32(syscall.ERANGE)
	}

	p.putGroup(grp, buf, e)
	p.put32(result, grp)

	return 0
}

// staticResult replaces the heap block the static area keeps at slot with
// one of size bytes.
func (p *Process) staticResult(slot, size uint32) uint32 {
	at := p.staticAt(slot)
	p.free(p.u32(at))
	p.put32(at, 0)
	block := p.malloc(size)

	if block == 0 {
		p.setErrno(syscall.ENOMEM)

		return 0
	}

	p.put32(at, block)

	return block
}

func (p *Process) staticPasswd(e *passwdEntry) uint32 {
	if e == nil {
		return 0
	}

	block := p.staticResult(staticPasswd, 28+e.size())

	if block != 0 {
		p.putPasswd(block, block+28, e)
	}

	return block
}

func (p *Process) staticGroup(e *groupEntry) uint32 {
	if e == nil {
		return 0
	}

	block := p.staticResult(staticGroup, 16+e.size(16))

	if block != 0 {
		p.putGroup(block, block+16, e)
	}

	return block
}

func (p *Process) passwdBy(match func(*passwdEntry) bool) *passwdEntry {
	entries := p.passwdEntries()

	if i := slices.IndexFunc(entries, func(e passwdEntry) bool { return match(&e) }); i >= 0 {
		return &entries[i]
	}

	return nil
}

func (p *Process) groupBy(match func(*groupEntry) bool) *groupEntry {
	entries := p.groupEntries()

	if i := slices.IndexFunc(entries, func(e groupEntry) bool { return match(&e) }); i >= 0 {
		return &entries[i]
	}

	return nil
}

// nextEntry is the entry at the position the static area keeps at slot,
// which moves past it.
func nextEntry[E any](p *Process, slot uint32, entries []E) *E {
	at := p.staticAt(slot)
	i := p.u32(at)

	if i >= uint32(len(entries)) {
		return nil
	}

	p.put32(at, i+1)

	return &entries[i]
}

func byName(p *Process, name uint32) func(*passwdEntry) bool {
	s := p.str(name)

	return func(e *passwdEntry) bool { return e.name == s }
}

func byUid(uid uint32) func(*passwdEntry) bool {
	return func(e *passwdEntry) bool { return e.uid == uid }
}

func groupName(p *Process, name uint32) func(*groupEntry) bool {
	s := p.str(name)

	return func(e *groupEntry) bool { return e.name == s }
}

func byGid(gid uint32) func(*groupEntry) bool {
	return func(e *groupEntry) bool { return e.gid == gid }
}

// rewind starts the entries of a static slot again.
func (p *Process) rewind(slot uint32) {
	p.put32(p.staticAt(slot), 0)
}

func (p *Process) libcGetpwnam(name uint32) uint32 {
	return p.staticPasswd(p.passwdBy(byName(p, name)))
}

func (p *Process) libcGetpwuid(uid uint32) uint32 {
	return p.staticPasswd(p.passwdBy(byUid(uid)))
}

func (p *Process) libcGetpwnam_r(name, pwd, buf, size, result uint32) uint32 {
	return p.passwdResult(p.passwdBy(byName(p, name)), pwd, buf, size, result)
}

func (p *Process) libcGetpwuid_r(uid, pwd, buf, size, result uint32) uint32 {
	return p.passwdResult(p.passwdBy(byUid(uid)), pwd, buf, size, result)
}

func (p *Process) libcGetpwent() uint32 {
	return p.staticPasswd(nextEntry(p, staticPwent, p.passwdEntries()))
}

func (p *Process) libcSetpwent() {
	p.rewind(staticPwent)
}

func (p *Process) libcEndpwent() {
	p.rewind(staticPwent)
}

func (p *Process) libcGetgrnam(name uint32) uint32 {
	return p.staticGroup(p.groupBy(groupName(p, name)))
}

func (p *Process) libcGetgrgid(gid uint32) uint32 {
	return p.staticGroup(p.groupBy(byGid(gid)))
}

func (p *Process) libcGetgrnam_r(name, grp, buf, size, result uint32) uint32 {
	return p.groupResult(p.groupBy(groupName(p, name)), grp, buf, size, result)
}

func (p *Process) libcGetgrgid_r(gid, grp, buf, size, result uint32) uint32 {
	return p.groupResult(p.groupBy(byGid(gid)), grp, buf, size, result)
}

func (p *Process) libcGetgrent() uint32 {
	return p.staticGroup(nextEntry(p, staticGrent, p.groupEntries()))
}

func (p *Process) libcSetgrent() {
	p.rewind(staticGrent)
}

func (p *Process) libcEndgrent() {
	p.rewind(staticGrent)
}

// getgrouplist lists group and the groups that name user a member;
// when they do not fit it returns -1 and how many there are.
func (p *Process) libcGetgrouplist(user, group, groups, count uint32) uint32 {
	name := p.str(user)
	list := []uint32{group}

	for _, e := range p.groupEntries() {
		if slices.Contains(e.members, name) && !slices.Contains(list, e.gid) {
			list = append(list, e.gid)
		}
	}

	room := min(int(int32(p.u32(count))), len(list))

	for i := range max(room, 0) {
		p.put32(groups+4*uint32(i), list[i])
	}

	p.put32(count, uint32(len(list)))

	if room < len(list) {
		return ^uint32(0)
	}

	return uint32(len(list))
}

// Names of hosts and services. A host is looked up in the root's /etc/hosts,
// then by Go's resolver, which is the host's: its /etc/hosts and DNS. A
// service is the root's /etc/services' or, without that file, one of a few
// well known; protocols are musl's table. The results of the functions
// without _r are heap blocks the static area points at, as the user
// databases'. inet_aton accepts musl's number forms;
// the classful helpers use network byte order for struct in_addr.

const (
	aiPassive     = 0x01
	aiCanonname   = 0x02
	aiNumerichost = 0x04
	aiV4mapped    = 0x08
	aiAll         = 0x10
	aiAddrconfig  = 0x20
	aiNumericserv = 0x400
	niNumerichost = 0x01
	niNumericserv = 0x02
	niNofqdn      = 0x04
	niNamereqd    = 0x08
	niDgram       = 0x10
	eaiBadflags   = -1
	eaiNoname     = -2
	eaiAgain      = -3
	eaiFail       = -4
	eaiFamily     = -6
	eaiSocktype   = -7
	eaiService    = -8
	eaiMemory     = -10
	eaiSystem     = -11
	eaiOverflow   = -12
	hostNotFound  = 1
	tryAgain      = 2
	noRecovery    = 3
	ipprotoTCP    = 6
	ipprotoUDP    = 17
	resolveWait   = 10 * time.Second
)

var gaiMessages = []string{"Invalid flags", "Name does not resolve", "Try again", "Non-recoverable error", "Name has no usable address",
	"Unrecognized address family or invalid length", "Unrecognized socket type", "Unrecognized service", "Unknown error",
	"Out of memory", "System error", "Overflow"}

var hostMessages = []string{"Host not found", "Try again", "Non-recoverable error", "Address not available"}

// protocols is musl's table.
var protocols = []struct {
	number int
	name   string
}{
	{0, "ip"}, {1, "icmp"}, {2, "igmp"}, {3, "ggp"}, {4, "ipencap"}, {5, "st"}, {6, "tcp"}, {8, "egp"}, {12, "pup"}, {17, "udp"},
	{20, "hmp"}, {22, "xns-idp"}, {27, "rdp"}, {29, "iso-tp4"}, {36, "xtp"}, {37, "ddp"}, {38, "idpr-cmtp"}, {41, "ipv6"},
	{43, "ipv6-route"}, {44, "ipv6-frag"}, {45, "idrp"}, {46, "rsvp"}, {47, "gre"}, {50, "esp"}, {51, "ah"}, {57, "skip"},
	{58, "ipv6-icmp"}, {59, "ipv6-nonxt"}, {60, "ipv6-opts"}, {73, "rspf"}, {81, "vmtp"}, {89, "ospf"}, {94, "ipip"},
	{98, "encap"}, {103, "pim"}, {255, "raw"},
}

// wellKnown are the services without the root's /etc/services.
var wellKnown = []serviceEntry{
	{"echo", 7, "tcp"}, {"echo", 7, "udp"}, {"discard", 9, "tcp"}, {"discard", 9, "udp"}, {"daytime", 13, "tcp"},
	{"ftp-data", 20, "tcp"}, {"ftp", 21, "tcp"}, {"ssh", 22, "tcp"}, {"telnet", 23, "tcp"}, {"smtp", 25, "tcp"},
	{"time", 37, "tcp"}, {"domain", 53, "tcp"}, {"domain", 53, "udp"}, {"tftp", 69, "udp"}, {"finger", 79, "tcp"},
	{"http", 80, "tcp"}, {"pop3", 110, "tcp"}, {"sunrpc", 111, "tcp"}, {"sunrpc", 111, "udp"}, {"nntp", 119, "tcp"},
	{"ntp", 123, "udp"}, {"imap", 143, "tcp"}, {"snmp", 161, "udp"}, {"ldap", 389, "tcp"}, {"https", 443, "tcp"},
	{"syslog", 514, "udp"}, {"submission", 587, "tcp"}, {"imaps", 993, "tcp"}, {"pop3s", 995, "tcp"},
}

type serviceEntry struct {
	name  string
	port  int
	proto string
}

type hostEntry struct {
	addr  netip.Addr
	names []string
}

// fileLines are the lines of a file of the root, comments cut.
func (p *Process) fileLines(path string) ([]string, bool) {
	data, err := p.readPath(path, databaseMax)

	if err != nil {
		return nil, false
	}

	var out []string

	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")

		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}

	return out, true
}

func (p *Process) hostEntries() []hostEntry {
	lines, _ := p.fileLines("/etc/hosts")
	var out []hostEntry

	for _, line := range lines {
		fields := strings.Fields(line)

		if addr, err := netip.ParseAddr(fields[0]); err == nil && len(fields) > 1 {
			out = append(out, hostEntry{addr.WithZone(""), fields[1:]})
		}
	}

	return out
}

func (p *Process) services() []serviceEntry {
	lines, ok := p.fileLines("/etc/services")

	if !ok {
		return wellKnown
	}

	var out []serviceEntry

	for _, line := range lines {
		fields := strings.Fields(line)

		if len(fields) < 2 {
			continue
		}

		port, proto, _ := strings.Cut(fields[1], "/")
		n, err := strconv.Atoi(port)

		if err != nil {
			continue
		}

		for _, name := range append([]string{fields[0]}, fields[2:]...) {
			out = append(out, serviceEntry{name, n, proto})
		}
	}

	return out
}

// resolve looks a name up: the root's /etc/hosts, then Go's resolver. The
// error is an EAI_ code.
func (p *Process) resolve(name string) ([]netip.Addr, string, int) {
	var out []netip.Addr
	canonical := ""

	for _, e := range p.hostEntries() {
		for _, n := range e.names {
			if strings.EqualFold(n, name) {
				out = append(out, e.addr)

				if canonical == "" {
					canonical = e.names[0]
				}

				break
			}
		}
	}

	if len(out) > 0 {
		return out, canonical, 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), resolveWait)
	defer cancel()

	found, err := net.DefaultResolver.LookupNetIP(ctx, "ip", name)

	if err != nil {
		var dns *net.DNSError

		if errors.As(err, &dns) && (dns.IsTemporary || dns.IsTimeout) {
			return nil, "", eaiAgain
		}

		if errors.As(err, &dns) && dns.IsNotFound {
			return nil, "", eaiNoname
		}

		return nil, "", eaiFail
	}

	canonical = name

	if cname, err := net.DefaultResolver.LookupCNAME(ctx, name); err == nil && cname != "" {
		canonical = strings.TrimSuffix(cname, ".")
	}

	for _, a := range found {
		out = append(out, a.Unmap())
	}

	return out, canonical, 0
}

// reverse is a name of an address: the root's /etc/hosts', then the
// resolver's.
func (p *Process) reverse(addr netip.Addr) (string, bool) {
	for _, e := range p.hostEntries() {
		if e.addr == addr {
			return e.names[0], true
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), resolveWait)
	defer cancel()

	if names, err := net.DefaultResolver.LookupAddr(ctx, addr.String()); err == nil && len(names) > 0 {
		return strings.TrimSuffix(names[0], "."), true
	}

	return "", false
}

// servicePort is a service's port for a protocol, "" for any.
func (p *Process) servicePort(name, proto string) (int, bool) {
	for _, s := range p.services() {
		if s.name == name && (proto == "" || s.proto == proto) {
			return s.port, true
		}
	}

	return 0, false
}

func (p *Process) serviceName(port int, proto string) (serviceEntry, bool) {
	for _, s := range p.services() {
		if s.port == port && (proto == "" || s.proto == proto) {
			return s, true
		}
	}

	return serviceEntry{}, false
}

type socketKind struct {
	socktype, protocol int
}

func (p *Process) getaddrinfo(node, service, hints, res uint32) int {
	flags, family, socktype, protocol := 0, 0, 0, 0

	if hints != 0 {
		flags, family = int(int32(p.u32(hints))), int(int32(p.u32(hints+4)))
		socktype, protocol = int(int32(p.u32(hints+8))), int(int32(p.u32(hints+12)))
	}

	switch {
	case node == 0 && service == 0:
		return eaiNoname
	case flags&^(aiPassive|aiCanonname|aiNumerichost|aiV4mapped|aiAll|aiAddrconfig|aiNumericserv) != 0:
		return eaiBadflags
	case family != 0 && family != afInet && family != afInet6:
		return eaiFamily
	case socktype != 0 && socktype != sockStream && socktype != sockDgram && socktype != sockRaw:
		return eaiSocktype
	}

	kinds := []socketKind{{sockStream, ipprotoTCP}, {sockDgram, ipprotoUDP}}

	if socktype != 0 || protocol != 0 {
		kinds = nil

		for _, k := range []socketKind{{sockStream, ipprotoTCP}, {sockDgram, ipprotoUDP}, {sockRaw, protocol}} {
			if (socktype == 0 || k.socktype == socktype) && (protocol == 0 || k.protocol == protocol) {
				kinds = append(kinds, k)
			}
		}

		if len(kinds) == 0 {
			return eaiSocktype
		}
	}

	port := 0

	if service != 0 {
		name := p.str(service)
		n, err := strconv.ParseUint(name, 10, 16)

		switch {
		case err == nil:
			port = int(n)
		case flags&aiNumericserv != 0:
			return eaiNoname
		default:
			var kept []socketKind

			for _, k := range kinds {
				proto := "tcp"

				if k.socktype == sockDgram {
					proto = "udp"
				}

				if found, ok := p.servicePort(name, proto); ok && k.socktype != sockRaw {
					port = found
					kept = append(kept, k)
				}
			}

			if len(kept) == 0 {
				return eaiService
			}

			kinds = kept
		}
	}

	type address struct {
		addr  netip.Addr
		scope uint32
	}

	var addrs []address
	canonical := ""

	if node == 0 {
		any4, any6 := netip.IPv4Unspecified(), netip.IPv6Unspecified()

		if flags&aiPassive == 0 {
			any4, any6 = netip.AddrFrom4([4]byte{127, 0, 0, 1}), netip.IPv6Loopback()
		}

		addrs = []address{{any4, 0}, {any6, 0}}
	} else {
		name := p.str(node)

		if a, scope, ok := numericHost(name); ok {
			addrs, canonical = []address{{a, scope}}, name
		} else if flags&aiNumerichost != 0 {
			return eaiNoname
		} else {
			found, canon, code := p.resolve(name)

			if code != 0 {
				return code
			}

			canonical = canon

			for _, a := range found {
				addrs = append(addrs, address{a, 0})
			}
		}
	}

	var kept []address
	have6 := false

	for _, a := range addrs {
		have6 = have6 || a.addr.Is6()
	}

	// AI_ADDRCONFIG takes the families the system has an address of: it
	// has no network, so none.
	usable := flags&aiAddrconfig == 0
	usable4, usable6 := usable, usable

	for _, a := range addrs {
		switch {
		case family == afInet && !a.addr.Is4():
		case family == afInet6 && a.addr.Is4():
			if flags&aiV4mapped != 0 && (!have6 || flags&aiAll != 0) {
				kept = append(kept, address{netip.AddrFrom16(a.addr.As16()), 0})
			}
		case a.addr.Is4() && !usable4, a.addr.Is6() && !usable6:
		default:
			kept = append(kept, a)
		}
	}

	if len(kept) == 0 {
		return eaiNoname
	}

	first, prev := uint32(0), uint32(0)

	for _, a := range kept {
		for _, k := range kinds {
			addr := sockaddr(a.addr, port, a.scope)
			name := ""

			if first == 0 && flags&aiCanonname != 0 {
				name = canonical
			}

			size := 32 + uint32(len(addr))

			if name != "" {
				size += uint32(len(name)) + 1
			}

			node := p.malloc(size)

			if node == 0 {
				p.freeaddrinfo(first)

				return eaiMemory
			}

			clear(p.span(node, size))
			family := afInet6

			if a.addr.Is4() {
				family = afInet
			}

			for i, v := range []uint32{uint32(flags), uint32(family), uint32(k.socktype), uint32(k.protocol), uint32(len(addr)), node + 32} {
				p.put32(node+4*uint32(i), v)
			}

			copy(p.span(node+32, uint32(len(addr))), addr)

			if name != "" {
				p.putString(node+32+uint32(len(addr)), name)
				p.put32(node+24, node+32+uint32(len(addr)))
			}

			if first == 0 {
				first = node
			} else {
				p.put32(prev+28, node)
			}

			prev = node
		}
	}

	p.put32(res, first)

	return 0
}

func (p *Process) freeaddrinfo(node uint32) {
	for node != 0 {
		next := p.u32(node + 28)
		p.free(node)
		node = next
	}
}

func (p *Process) getnameinfo(sa, salen, host, hostlen, serv, servlen, flags uint32) int {
	if salen < 2 {
		return eaiFamily
	}

	var addr netip.Addr
	scope := uint32(0)

	switch p.u16(sa) {
	case afInet:
		if salen < 16 {
			return eaiFamily
		}

		addr = netip.AddrFrom4([4]byte(p.span(sa+4, 4)))
	case afInet6:
		if salen < 28 {
			return eaiFamily
		}

		addr, scope = netip.AddrFrom16([16]byte(p.span(sa+8, 16))), p.u32(sa+24)
	default:
		return eaiFamily
	}

	if host != 0 && hostlen > 0 {
		name, found := "", false

		if flags&niNumerichost == 0 {
			name, found = p.reverse(addr)

			if found && flags&niNofqdn != 0 {
				name, _, _ = strings.Cut(name, ".")
			}
		}

		if !found {
			if flags&niNamereqd != 0 {
				return eaiNoname
			}

			name = addr.String()

			if scope != 0 {
				name += "%" + strconv.Itoa(int(scope))
			}
		}

		if uint32(len(name)) >= hostlen {
			return eaiOverflow
		}

		p.putString(host, name)
	}

	if serv != 0 && servlen > 0 {
		port := int(binary.BigEndian.Uint16(p.span(sa+2, 2)))
		name := strconv.Itoa(port)

		if flags&niNumericserv == 0 {
			proto := "tcp"

			if flags&niDgram != 0 {
				proto = "udp"
			}

			if s, ok := p.serviceName(port, proto); ok {
				name = s.name
			}
		}

		if uint32(len(name)) >= servlen {
			return eaiOverflow
		}

		p.putString(serv, name)
	}

	return 0
}

// setHerrno sets h_errno, which __kernel_start told the kernel of.
func (p *Process) setHerrno(code int) {
	if p.herrnoAt != 0 {
		p.put32(p.herrnoAt, uint32(code))
	}
}

// hostent writes the static struct hostent of a name and its addresses of
// one family.
func (p *Process) hostent(name string, family int, addrs []netip.Addr) uint32 {
	length := 4

	if family == afInet6 {
		length = 16
	}

	size := 20 + 4 + 4*uint32(len(addrs)+1) + uint32(length*len(addrs)) + uint32(len(name)) + 1
	block := p.staticResult(staticHostent, size)

	if block == 0 {
		p.setHerrno(noRecovery)

		return 0
	}

	aliases := block + 20
	list := aliases + 4
	data := list + 4*uint32(len(addrs)+1)
	str := data + uint32(length*len(addrs))
	p.put32(aliases, 0)

	for i, a := range addrs {
		raw := a.As16()
		at := data + uint32(length*i)

		if family == afInet {
			v4 := a.As4()
			copy(p.span(at, 4), v4[:])
		} else {
			copy(p.span(at, 16), raw[:])
		}

		p.put32(list+4*uint32(i), at)
	}

	p.put32(list+4*uint32(len(addrs)), 0)
	p.putString(str, name)

	for i, v := range []uint32{str, aliases, uint32(family), uint32(length), list} {
		p.put32(block+4*uint32(i), v)
	}

	return block
}

func (p *Process) gethostbyname(name string, family int) uint32 {
	var found []netip.Addr
	canonical := name

	if a, _, ok := numericHost(name); ok {
		found = []netip.Addr{a}
	} else {
		addrs, canon, code := p.resolve(name)

		switch code {
		case 0:
		case eaiAgain:
			p.setHerrno(tryAgain)

			return 0
		case eaiFail:
			p.setHerrno(noRecovery)

			return 0
		default:
			p.setHerrno(hostNotFound)

			return 0
		}

		found, canonical = addrs, canon
	}

	var kept []netip.Addr

	for _, a := range found {
		if a.Is4() == (family == afInet) {
			kept = append(kept, a)
		}
	}

	if len(kept) == 0 {
		p.setHerrno(hostNotFound)

		return 0
	}

	return p.hostent(canonical, family, kept)
}

// servent writes the static struct servent of a service.
func (p *Process) servent(s serviceEntry) uint32 {
	size := 16 + 4 + uint32(len(s.name)+len(s.proto)) + 2
	block := p.staticResult(staticServent, size)

	if block == 0 {
		return 0
	}

	name := block + 20
	proto := name + uint32(len(s.name)) + 1
	p.putString(name, s.name)
	p.putString(proto, s.proto)
	p.put32(block+16, 0)
	var port [2]byte
	binary.BigEndian.PutUint16(port[:], uint16(s.port))

	for i, v := range []uint32{name, block + 16, uint32(binary.LittleEndian.Uint16(port[:])), proto} {
		p.put32(block+4*uint32(i), v)
	}

	return block
}

func (p *Process) protoent(number int, name string) uint32 {
	block := p.staticResult(staticProtoent, 12+4+uint32(len(name))+1)

	if block == 0 {
		return 0
	}

	p.putString(block+16, name)
	p.put32(block+12, 0)

	for i, v := range []uint32{block + 16, block + 12, uint32(number)} {
		p.put32(block+4*uint32(i), v)
	}

	return block
}

func (p *Process) libcGetaddrinfo(node, service, hints, res uint32) uint32 {
	return uint32(int32(p.getaddrinfo(node, service, hints, res)))
}

func (p *Process) libcGai_strerror(code uint32) uint32 {
	msg := "Unknown error"

	if c := -int(int32(code)); c >= 1 && c <= len(gaiMessages) {
		msg = gaiMessages[c-1]
	}

	return p.constant(msg)
}

func (p *Process) libcGetnameinfo(sa, salen, host, hostlen, serv, servlen, flags uint32) uint32 {
	return uint32(int32(p.getnameinfo(sa, salen, host, hostlen, serv, servlen, flags)))
}

func (p *Process) libcGethostbyname(name uint32) uint32 {
	return p.gethostbyname(p.str(name), afInet)
}

func (p *Process) libcGethostbyname2(name, family uint32) uint32 {
	if family != afInet && family != afInet6 {
		p.setHerrno(noRecovery)

		return 0
	}

	return p.gethostbyname(p.str(name), int(family))
}

func (p *Process) libcGethostbyaddr(addr, n, family uint32) uint32 {
	var a netip.Addr

	switch {
	case family == afInet && n == 4:
		a = netip.AddrFrom4([4]byte(p.span(addr, 4)))
	case family == afInet6 && n == 16:
		a = netip.AddrFrom16([16]byte(p.span(addr, 16)))
	default:
		p.setHerrno(noRecovery)

		return 0
	}

	name, ok := p.reverse(a)

	if !ok {
		p.setHerrno(hostNotFound)

		return 0
	}

	return p.hostent(name, int(family), []netip.Addr{a})
}

func (p *Process) libcGethostent() uint32 {
	e := nextEntry(p, staticHostIndex, p.hostEntries())

	if e == nil {
		return 0
	}

	family := afInet6

	if e.addr.Is4() {
		family = afInet
	}

	return p.hostent(e.names[0], family, []netip.Addr{e.addr})
}

func (p *Process) libcSethostent(stay uint32) {
	p.put32(p.staticAt(staticHostIndex), 0)
}

func (p *Process) libcEndhostent() {
	p.put32(p.staticAt(staticHostIndex), 0)
}

func (p *Process) libcGetservbyname(name, proto uint32) uint32 {
	want := ""

	if proto != 0 {
		want = p.str(proto)
	}

	port, ok := p.servicePort(p.str(name), want)

	if !ok {
		return 0
	}

	s, _ := p.serviceName(port, want)

	return p.servent(s)
}

func (p *Process) libcGetservbyport(port, proto uint32) uint32 {
	want := ""

	if proto != 0 {
		want = p.str(proto)
	}

	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], uint16(port))
	s, ok := p.serviceName(int(binary.BigEndian.Uint16(b[:])), want)

	if !ok {
		return 0
	}

	return p.servent(s)
}

func (p *Process) libcGetprotobyname(name uint32) uint32 {
	s := p.str(name)

	for _, e := range protocols {
		if e.name == s {
			return p.protoent(e.number, e.name)
		}
	}

	return 0
}

func (p *Process) libcGetprotobynumber(number uint32) uint32 {
	for _, e := range protocols {
		if e.number == int(int32(number)) {
			return p.protoent(e.number, e.name)
		}
	}

	return 0
}

// There is no networks database, as musl has none.
func (p *Process) libcGetnetbyname(name uint32) uint32 {
	return 0
}

func (p *Process) libcGetnetbyaddr(net, family uint32) uint32 {
	return 0
}

func (p *Process) libcHstrerror(code uint32) uint32 {
	msg := "Unknown error"

	if c := int(int32(code)); c >= 1 && c <= len(hostMessages) {
		msg = hostMessages[c-1]
	}

	return p.constant(msg)
}

func (p *Process) libcHerror(s uint32) {
	msg := "Unknown error"

	if p.herrnoAt != 0 {
		if c := int(int32(p.u32(p.herrnoAt))); c >= 1 && c <= len(hostMessages) {
			msg = hostMessages[c-1]
		}
	}

	p.complain(s, msg)
}

func (p *Process) libcInet_aton(s, at uint32) uint32 {
	a, ok := inetAton(p.str(s))

	if ok {
		copy(p.span(at, 4), a[:])
	}

	return bit(ok)
}

func (p *Process) libcInet_addr(s uint32) uint32 {
	a, ok := inetAton(p.str(s))

	if !ok {
		return 0xffffffff
	}

	return binary.LittleEndian.Uint32(a[:])
}

func (p *Process) libcInet_network(s uint32) uint32 {
	a, ok := inetAton(p.str(s))

	if !ok {
		return 0xffffffff
	}

	return binary.BigEndian.Uint32(a[:])
}

func (p *Process) libcInet_ntoa(in uint32) uint32 {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], in)
	at := p.staticAt(staticNtoa)
	p.putString(at, fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3]))

	return at
}

func (p *Process) libcInet_pton(family, s, dst uint32) uint32 {
	text := p.str(s)

	switch family {
	case afInet:
		a, err := netip.ParseAddr(text)

		if err != nil || !a.Is4() {
			return 0
		}

		v := a.As4()
		copy(p.span(dst, 4), v[:])
	case afInet6:
		a, err := netip.ParseAddr(text)

		if err != nil || !a.Is6() || a.Zone() != "" {
			return 0
		}

		v := a.As16()
		copy(p.span(dst, 16), v[:])
	default:
		p.setErrno(syscall.EAFNOSUPPORT)

		return ^uint32(0)
	}

	return 1
}

func (p *Process) libcInet_ntop(family, src, dst, size uint32) uint32 {
	var text string

	switch family {
	case afInet:
		text = netip.AddrFrom4([4]byte(p.span(src, 4))).String()
	case afInet6:
		text = netip.AddrFrom16([16]byte(p.span(src, 16))).String()
	default:
		p.setErrno(syscall.EAFNOSUPPORT)

		return 0
	}

	if uint32(len(text)) >= size {
		p.setErrno(syscall.ENOSPC)

		return 0
	}

	p.putString(dst, text)

	return dst
}

func (p *Process) libcInet_makeaddr(n, h uint32) uint32 {
	switch {
	case n < 256:
		h = h&0xffffff | n<<24
	case n < 65536:
		h = h&0xffff | n<<16
	case n < 1<<24:
		h = h&0xff | n<<8
	default:
		h |= n
	}

	return bits.ReverseBytes32(h)
}

func (p *Process) libcInet_lnaof(in uint32) uint32 {
	in = bits.ReverseBytes32(in)

	switch {
	case in>>24 < 128:
		return in & 0xffffff
	case in>>24 < 192:
		return in & 0xffff
	}

	return in & 0xff
}

func (p *Process) libcInet_netof(in uint32) uint32 {
	in = bits.ReverseBytes32(in)

	switch {
	case in>>24 < 128:
		return in >> 24
	case in>>24 < 192:
		return in >> 16
	}

	return in >> 8
}

func (p *Process) libcIf_nametoindex(name uint32) uint32 {
	index := interfaceIndex(p.str(name))

	if index == 0 {
		p.setErrno(syscall.ENXIO)
	}

	return index
}

func (p *Process) libcIf_indextoname(index, buf uint32) uint32 {
	if index == 0 || int(index) > len(interfaces) {
		p.setErrno(syscall.ENXIO)

		return 0
	}

	p.putString(buf, interfaces[index-1])

	return buf
}

// if_nameindex's array and names are one block, which
// if_freenameindex frees.
func (p *Process) libcIf_nameindex() uint32 {
	size := 8 * uint32(len(interfaces)+1)

	for _, name := range interfaces {
		size += uint32(len(name)) + 1
	}

	block := p.malloc(size)

	if block == 0 {
		p.setErrno(syscall.ENOBUFS)

		return 0
	}

	names := block + 8*uint32(len(interfaces)+1)

	for i, name := range interfaces {
		p.put32(block+8*uint32(i), uint32(i+1))
		p.put32(block+8*uint32(i)+4, names)
		p.putString(names, name)
		names += uint32(len(name)) + 1
	}

	p.put32(block+8*uint32(len(interfaces)), 0)
	p.put32(block+8*uint32(len(interfaces))+4, 0)

	return block
}

func (p *Process) libcFreeaddrinfo(res uint32) {
	p.freeaddrinfo(res)
}

func (p *Process) libcIf_freenameindex(block uint32) {
	p.free(block)
}

// Locales and character classes. The multibyte functions of lib/c are
// UTF-8's in every locale, so a locale is a name per category and nothing
// more: its codeset is UTF-8 and its items are the C locale's. Names follow
// musl: "" takes the environment's, C and POSIX are the C locale, any
// other name of at most 23 bytes without a slash or a leading dot is kept,
// and an invalid one is C.UTF-8, which only LC_CTYPE keeps by that name.
// Unlike musl's, a name of another codeset is refused, as one the system
// does not have.

const (
	lcCtype    = 0
	lcNumeric  = 1
	lcTime     = 2
	lcMonetary = 4
	lcMessages = 5
	lcAll      = 6
	// localeNameMax is musl's LOCALE_NAME_MAX.
	localeNameMax = 23
)

var localeVars = [lcAll]string{"LC_CTYPE", "LC_NUMERIC", "LC_TIME", "LC_COLLATE", "LC_MONETARY", "LC_MESSAGES"}

// localeName is the locale a category takes for a name: "" for C.
func (p *Process) localeName(cat int, name string) string {
	if name == "" {
		for _, v := range []string{"LC_ALL", localeVars[cat], "LANG"} {
			if s, ok := p.getenv(v); ok && s != "" {
				name = s

				break
			}
		}
	}

	if name == "" || len(name) > localeNameMax || name[0] == '.' || strings.Contains(name, "/") {
		name = "C.UTF-8"
	}

	switch {
	case name == "C" || name == "POSIX":
		return ""
	case name == "C.UTF-8" && cat != lcCtype:
		return ""
	}

	return name
}

// setlocale sets a category's locale, or all of them from one name or
// from the "a;b;c;d;e;f" a query of LC_ALL returns when they differ, and
// returns the name.
func (p *Process) setlocale(cat uint32, name uint32) uint32 {
	if cat > lcAll {
		return 0
	}

	if name != 0 {
		s := p.str(name)
		parts := strings.Split(s, ";")
		var names [lcAll]string

		for i := range lcAll {
			part := s

			if cat == lcAll {
				part = parts[min(i, len(parts)-1)]
			} else if i != int(cat) {
				continue
			}

			names[i] = p.localeName(i, part)

			if !utf8Codeset(names[i]) {
				return 0
			}
		}

		for i := range lcAll {
			if cat == lcAll || i == int(cat) {
				p.locale[i] = names[i]
			}
		}
	}

	if cat < lcAll {
		return p.constant(localeShown(p.locale[cat]))
	}

	names := make([]string, lcAll)

	for i, n := range p.locale {
		names[i] = localeShown(n)
	}

	for _, n := range names {
		if n != names[0] {
			return p.constant(strings.Join(names, ";"))
		}
	}

	return p.constant(names[0])
}

// The items of nl_langinfo are a category and an index.
var langItems = map[int][]string{
	lcNumeric: {".", ""},
	lcTime: {
		"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat",
		"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday",
		"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
		"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December",
		"AM", "PM", "%a %b %e %T %Y", "%m/%d/%y", "%H:%M:%S", "%I:%M:%S %p",
		"", "", "%m/%d/%y", "0123456789", "%a %b %e %T %Y", "%H:%M:%S",
	},
	lcMessages: {"^[yY]", "^[nN]", "yes", "no"},
}

const (
	codeset = 14
	// localeNameItem is the index of musl's _NL_LOCALE_NAME.
	localeNameItem = 0xffff
)

func (p *Process) langinfo(item uint32) string {
	cat, idx := int(item>>16), int(item&0xffff)

	switch {
	case item == codeset:
		return "UTF-8"
	case idx == localeNameItem && cat < lcAll:
		return localeShown(p.locale[cat])
	case idx < len(langItems[cat]):
		return langItems[cat][idx]
	}

	return ""
}

// lconv offsets of the guest's locale.h: ten strings, then the chars.
const (
	lconvStrings = 10
	lconvChars   = 14
	charMax      = 127
)

func (p *Process) localeconv() uint32 {
	at := p.staticAt(staticLconv)
	p.put32(at, p.staticAt(staticDot))

	for i := uint32(1); i < lconvStrings; i++ {
		p.put32(at+4*i, p.staticAt(staticEmpty))
	}

	chars := p.span(at+4*lconvStrings, lconvChars)

	for i := range chars {
		chars[i] = charMax
	}

	return at
}

var wcClassNames = []string{"alnum", "alpha", "blank", "cntrl", "digit", "graph", "lower", "print", "punct", "space", "upper", "xdigit"}

func (p *Process) libcNl_langinfo(item uint32) uint32 {
	return p.constant(p.langinfo(item))
}

func (p *Process) libcTowlower(c uint32) uint32 {
	return towlower(c)
}

func (p *Process) libcTowupper(c uint32) uint32 {
	return towupper(c)
}

func (p *Process) libcWctype(name uint32) uint32 {
	s := p.str(name)

	for i, n := range wcClassNames {
		if n == s {
			return uint32(i + 1)
		}
	}

	return 0
}

func (p *Process) libcIswctype(c, class uint32) uint32 {
	return bit(iswClass(int(class), c))
}

func (p *Process) libcWctrans(name uint32) uint32 {
	switch p.str(name) {
	case "toupper":
		return 1
	case "tolower":
		return 2
	}

	return 0
}

func (p *Process) libcTowctrans(c, trans uint32) uint32 {
	switch trans {
	case 1:
		return towupper(c)
	case 2:
		return towlower(c)
	}

	return c
}

func (p *Process) libcIswalnum(c uint32) uint32 {
	return bit(iswClass(wcAlnum, c))
}

func (p *Process) libcIswalpha(c uint32) uint32 {
	return bit(iswClass(wcAlpha, c))
}

func (p *Process) libcIswblank(c uint32) uint32 {
	return bit(iswClass(wcBlank, c))
}

func (p *Process) libcIswcntrl(c uint32) uint32 {
	return bit(iswClass(wcCntrl, c))
}

func (p *Process) libcIswdigit(c uint32) uint32 {
	return bit(iswClass(wcDigit, c))
}

func (p *Process) libcIswgraph(c uint32) uint32 {
	return bit(iswClass(wcGraph, c))
}

func (p *Process) libcIswlower(c uint32) uint32 {
	return bit(iswClass(wcLower, c))
}

func (p *Process) libcIswprint(c uint32) uint32 {
	return bit(iswClass(wcPrint, c))
}

func (p *Process) libcIswpunct(c uint32) uint32 {
	return bit(iswClass(wcPunct, c))
}

func (p *Process) libcIswspace(c uint32) uint32 {
	return bit(iswClass(wcSpace, c))
}

func (p *Process) libcIswupper(c uint32) uint32 {
	return bit(iswClass(wcUpper, c))
}

func (p *Process) libcIswxdigit(c uint32) uint32 {
	return bit(iswClass(wcXdigit, c))
}

func (p *Process) libcSetlocale(cat, name uint32) uint32 {
	return p.setlocale(cat, name)
}

func (p *Process) libcLocaleconv() uint32 {
	return p.localeconv()
}

func (p *Process) libcIsalnum(c uint32) uint32 {
	return ctype("alnum", c)
}

func (p *Process) libcIsalpha(c uint32) uint32 {
	return ctype("alpha", c)
}

func (p *Process) libcIsascii(c uint32) uint32 {
	return bit(c < 0x80)
}

func (p *Process) libcIsblank(c uint32) uint32 {
	return ctype("blank", c)
}

func (p *Process) libcIscntrl(c uint32) uint32 {
	return ctype("cntrl", c)
}

func (p *Process) libcIsgraph(c uint32) uint32 {
	return ctype("graph", c)
}

func (p *Process) libcIslower(c uint32) uint32 {
	return ctype("lower", c)
}

func (p *Process) libcIsprint(c uint32) uint32 {
	return ctype("print", c)
}

func (p *Process) libcIspunct(c uint32) uint32 {
	return ctype("punct", c)
}

func (p *Process) libcIsupper(c uint32) uint32 {
	return ctype("upper", c)
}

func (p *Process) libcIsxdigit(c uint32) uint32 {
	return ctype("xdigit", c)
}

func (p *Process) libcToascii(c uint32) uint32 {
	return c & 0x7f
}

// bounded is up to n bytes of memory at at, all the memory has; past its
// end a search that found nothing traps.
func (p *Process) bounded(at, n uint32) []byte {
	m := p.mem()

	if uint64(at) > uint64(len(m)) {
		oob()
	}

	end := min(uint64(at)+uint64(n), uint64(len(m)))

	return m[at:end]
}

func (p *Process) search(at, n uint32, c byte) (uint32, bool) {
	b := p.bounded(at, n)
	i := bytes.IndexByte(b, c)

	if i < 0 && uint32(len(b)) < n {
		oob()
	}

	// What it read: up to the byte found, or all n.
	read := uint32(i + 1)

	if i < 0 {
		read = n
	}

	if p.inst.memory.shadow != nil {
		p.asanCheck(at, read)
	}

	if i < 0 {
		return 0, false
	}

	return at + uint32(i), true
}

func (p *Process) strnlen(at, n uint32) uint32 {
	end, ok := p.search(at, n, 0)

	if !ok {
		return n
	}

	return end - at
}

// limited is the string at at as a call that looks at no more than n
// bytes of it sees it: its bytes with its NUL, or its first n with none
// among them. Past the memory's end short of both it traps.
func (p *Process) limited(at, n uint32) []byte {
	b := p.bounded(at, n)

	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i+1]
	} else if uint32(len(b)) < n {
		oob()
	}

	if p.inst.memory.shadow != nil {
		p.asanCheck(at, uint32(len(b)))
	}

	return b
}

// strncmp compares two strings by their first n bytes at most: where
// one ends first its NUL is the lesser byte, so the bytes both have, the
// NUL with them, decide. The result is -1, 0 or 1; without regard to
// case, fold, the difference of the first two letters that differ.
func (p *Process) strncmp(a, b, n uint32, fold bool) uint32 {
	x, y := p.limited(a, n), p.limited(b, n)
	m := min(len(x), len(y))

	if !fold {
		return uint32(int32(bytes.Compare(x[:m], y[:m])))
	}

	for i := range m {
		if c, d := lower(x[i]), lower(y[i]); c != d {
			return cmp(int(c), int(d))
		}
	}

	return 0
}

// set is the bytes of the string at at.
func (p *Process) set(at uint32) [256]bool {
	var s [256]bool

	for _, c := range p.cstr(at) {
		s[c] = true
	}

	return s
}

// span counts the leading bytes of the string at at that are in set, or
// with reject that are not.
func (p *Process) spanSet(at, set uint32, reject bool) uint32 {
	in := p.set(set)
	s := p.cstr(at)

	for i, c := range s {
		if in[c] == reject {
			return uint32(i)
		}
	}

	return uint32(len(s))
}

func (p *Process) strstr(h, n uint32, fold bool) uint32 {
	hay, needle := p.cstr(h), p.cstr(n)

	if fold {
		hay, needle = lowerBytes(hay), lowerBytes(needle)
	}

	i := bytes.Index(hay, needle)

	if i < 0 {
		return 0
	}

	return h + uint32(i)
}

func (p *Process) strcpy(d, s uint32) uint32 {
	src := p.cstr(s)
	n := uint32(len(src))
	copy(p.span(d, n+1), p.span(s, n+1))

	return d + n
}

func (p *Process) stpncpy(d, s, n uint32) uint32 {
	l := p.strnlen(s, n)
	dst := p.span(d, n)
	copy(dst, p.span(s, l))
	clear(dst[l:])

	return d + l
}

func (p *Process) strndup(s, n uint32) uint32 {
	l := p.strnlen(s, n)
	at := p.malloc(l + 1)

	if at == 0 {
		p.setErrno(syscall.ENOMEM)

		return 0
	}

	copy(p.span(at, l), p.span(s, l))
	p.span(at+l, 1)[0] = 0

	return at
}

// strtok is strtok_r with the save pointer's value.
func (p *Process) strtok(s, delim uint32, save uint32) (token, next uint32) {
	if s == 0 {
		s = save
	}

	if s == 0 {
		return 0, 0
	}

	s += p.spanSet(s, delim, false)

	if p.u8(s) == 0 {
		return 0, 0
	}

	end := s + p.spanSet(s, delim, true)

	if p.u8(end) == 0 {
		return s, 0
	}

	p.span(end, 1)[0] = 0

	return s, end + 1
}

func (p *Process) libcStrcasecmp(a, b uint32) uint32 {
	return p.strncmp(a, b, ^uint32(0), true)
}

func (p *Process) libcStrchr(s, c uint32) uint32 {
	str := p.cstr(s)

	if byte(c) == 0 {
		return s + uint32(len(str))
	}

	if i := bytes.IndexByte(str, byte(c)); i >= 0 {
		return s + uint32(i)
	}

	return 0
}

func (p *Process) libcStrchrnul(s, c uint32) uint32 {
	str := p.cstr(s)

	if i := bytes.IndexByte(str, byte(c)); i >= 0 && byte(c) != 0 {
		return s + uint32(i)
	}

	return s + uint32(len(str))
}

func (p *Process) libcStrrchr(s, c uint32) uint32 {
	str := p.cstr(s)

	if byte(c) == 0 {
		return s + uint32(len(str))
	}

	if i := bytes.LastIndexByte(str, byte(c)); i >= 0 {
		return s + uint32(i)
	}

	return 0
}

func (p *Process) libcStrncpy(d, s, n uint32) uint32 {
	p.stpncpy(d, s, n)

	return d
}

func (p *Process) libcStrdup(s uint32) uint32 {
	return p.strndup(s, ^uint32(0))
}

func (p *Process) libcStrtok_r(s, delim, save uint32) uint32 {
	token, next := p.strtok(s, delim, p.u32(save))
	p.put32(save, next)

	return token
}

func (p *Process) libcStrsep(at, delim uint32) uint32 {
	s := p.u32(at)

	if s == 0 {
		return 0
	}

	end := s + p.spanSet(s, delim, true)

	if p.u8(end) == 0 {
		p.put32(at, 0)
	} else {
		p.span(end, 1)[0] = 0
		p.put32(at, end+1)
	}

	return s
}

func (p *Process) libcStrverscmp(a, b uint32) uint32 {
	return uint32(int32(strverscmp(p.cstr(a), p.cstr(b))))
}

func (p *Process) libcAbs(n uint32) uint32 {
	if int32(n) < 0 {
		return uint32(-int32(n))
	}

	return n
}

func (p *Process) libcStrerror(e uint32) uint32 {
	at := p.staticAt(staticStrerror)
	p.putString(at, truncate(errorMessage(syscall.Errno(e)), staticStrsignal-staticStrerror-1))

	return at
}

func (p *Process) libcStrsignal(sig uint32) uint32 {
	at := p.staticAt(staticStrsignal)
	p.putString(at, truncate(signalMessage(int(sig)), staticLconv-staticStrsignal-1))

	return at
}

func (p *Process) libcDirname(s uint32) uint32 {
	if s == 0 || p.u8(s) == 0 {
		return p.staticAt(staticDot)
	}

	b := p.cstr(s)
	i := len(b) - 1

	for ; b[i] == '/'; i-- {
		if i == 0 {
			return p.staticAt(staticSlash)
		}
	}

	for ; b[i] != '/'; i-- {
		if i == 0 {
			return p.staticAt(staticDot)
		}
	}

	for ; b[i] == '/'; i-- {
		if i == 0 {
			return p.staticAt(staticSlash)
		}
	}

	p.span(s+uint32(i)+1, 1)[0] = 0

	return s
}

func (p *Process) libcFnmatch(pattern, s, flags uint32) uint32 {
	if fnmatch(p.cstr(pattern), p.cstr(s), int(flags), true) {
		return 0
	}

	return 1
}

func (p *Process) libcMemrchr(s, c, n uint32) uint32 {
	if i := bytes.LastIndexByte(p.span(s, n), byte(c)); i >= 0 {
		return s + uint32(i)
	}

	return 0
}

func (p *Process) libcRawmemchr(s, c uint32) uint32 {
	at, _ := p.search(s, math.MaxUint32, byte(c))

	return at
}

func (p *Process) libcMemccpy(d, s, c, n uint32) uint32 {
	end, found := p.search(s, n, byte(c))
	l := n

	if found {
		l = end - s + 1
	}

	copy(p.span(d, l), p.span(s, l))

	if found {
		return d + l
	}

	return 0
}

func (p *Process) libcSwab(s, d, n uint32) {
	if int32(n) < 2 {
		return
	}

	n &^= 1
	src, dst := p.span(s, n), p.span(d, n)

	for i := uint32(0); i < n; i += 2 {
		dst[i], dst[i+1] = src[i+1], src[i]
	}
}

func (p *Process) libcStrtok(s, delim uint32) uint32 {
	save := p.staticAt(staticStrtok)
	token, next := p.strtok(s, delim, p.u32(save))
	p.put32(save, next)

	return token
}

// strcoll and strxfrm are the C locale's: the order of the bytes.
func (p *Process) libcStrcoll(a, b uint32) uint32 {
	return uint32(int32(bytes.Compare(p.cstr(a), p.cstr(b))))
}

func (p *Process) libcStrxfrm(d, s, n uint32) uint32 {
	l := uint32(len(p.cstr(s)))

	if n > l {
		p.strcpy(d, s)
	}

	return l
}

func (p *Process) libcStrerror_r(e, buf, n uint32) uint32 {
	msg := errorMessage(syscall.Errno(e))

	if uint32(len(msg)) >= n {
		if n > 0 {
			p.putString(buf, msg[:n-1])
		}

		return uint32(syscall.ERANGE)
	}

	p.putString(buf, msg)

	return 0
}

func (p *Process) libcBasename(s uint32) uint32 {
	if s == 0 || p.u8(s) == 0 {
		return p.staticAt(staticDot)
	}

	b := p.cstr(s)
	i := len(b) - 1

	for ; i > 0 && b[i] == '/'; i-- {
		b[i] = 0
	}

	for ; i > 0 && b[i-1] != '/'; i-- {
	}

	return s + uint32(i)
}

func (p *Process) libcFfs(x uint32) uint32 {
	return ffs(uint64(x))
}

func (p *Process) libcFfsl(x uint32) uint32 {
	return ffs(uint64(x))
}

func (p *Process) libcFfsll(x uint64) uint32 {
	return ffs(x)
}

func (p *Process) libcLabs(n uint32) uint32 {
	if int32(n) < 0 {
		return uint32(-int32(n))
	}

	return n
}

func (p *Process) libcLlabs(n uint64) uint64 {
	if int64(n) < 0 {
		return uint64(-int64(n))
	}

	return n
}

func (p *Process) libcDiv(ret, a, b uint32) {
	q, r := divide(int64(int32(a)), int64(int32(b)), math.MinInt32)
	p.put32(ret, uint32(q))
	p.put32(ret+4, uint32(r))
}

func (p *Process) libcLldiv(ret uint32, a, b uint64) {
	q, r := divide(int64(a), int64(b), math.MinInt64)
	p.put64(ret, uint64(q))
	p.put64(ret+8, uint64(r))
}

func (p *Process) libcStrncmp(a, b, n uint32) uint32 {
	return p.strncmp(a, b, n, false)
}

func (p *Process) libcStrncasecmp(a, b, n uint32) uint32 {
	return p.strncmp(a, b, n, true)
}

func (p *Process) libcStrcasestr(h, n uint32) uint32 {
	return p.strstr(h, n, true)
}

func (p *Process) libcStpcpy(d, s uint32) uint32 {
	return p.strcpy(d, s)
}

func (p *Process) libcStpncpy(d, s, n uint32) uint32 {
	return p.stpncpy(d, s, n)
}

func (p *Process) libcStrndup(s, n uint32) uint32 {
	return p.strndup(s, n)
}

func (p *Process) libcIsdigit(c uint32) uint32 {
	if c >= '0' && c <= '9' {
		return 1
	}

	return 0
}

func (p *Process) libcIsspace(c uint32) uint32 {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return 1
	}

	return 0
}

func (p *Process) libcTolower(c uint32) uint32 {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}

	return c
}

func (p *Process) libcToupper(c uint32) uint32 {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}

	return c
}

func (p *Process) libcMemmem(h, hn, n, nn uint32) uint32 {
	if i := bytes.Index(p.span(h, hn), p.span(n, nn)); i >= 0 {
		return h + uint32(i)
	}

	return 0
}

func (p *Process) libcMempcpy(d, s, n uint32) uint32 {
	copy(p.span(d, n), p.span(s, n))

	return d + n
}

func (p *Process) libcStrnlen(s, n uint32) uint32 {
	return p.strnlen(s, n)
}

func (p *Process) libcStrncat(d, s, n uint32) uint32 {
	end := d + uint32(len(p.cstr(d)))
	l := p.strnlen(s, n)
	copy(p.span(end, l), p.span(s, l))
	p.span(end+l, 1)[0] = 0

	return d
}

func (p *Process) libcStrlcpy(d, s, n uint32) uint32 {
	src := p.cstr(s)

	if n > 0 {
		k := min(uint32(len(src)), n-1)
		copy(p.span(d, k), src[:k])
		p.span(d+k, 1)[0] = 0
	}

	return uint32(len(src))
}

func (p *Process) libcStrlcat(d, s, n uint32) uint32 {
	l := p.strnlen(d, n)
	src := p.cstr(s)

	if l == n {
		return l + uint32(len(src))
	}

	k := min(uint32(len(src)), n-l-1)
	copy(p.span(d+l, k), src[:k])
	p.span(d+l+k, 1)[0] = 0

	return l + uint32(len(src))
}

func (p *Process) libcStrstr(h, n uint32) uint32 {
	return p.strstr(h, n, false)
}

func (p *Process) libcStrpbrk(s, accept uint32) uint32 {
	n := p.spanSet(s, accept, true)

	if p.u8(s+n) == 0 {
		return 0
	}

	return s + n
}

func (p *Process) libcExplicit_bzero(d, n uint32) {
	clear(p.span(d, n))
}

func (p *Process) libcBcopy(s, d, n uint32) {
	copy(p.span(d, n), p.span(s, n))
}

func (p *Process) libcBzero(d, n uint32) {
	clear(p.span(d, n))
}

// The memory and string functions lib/str has. The code a module
// compiles to calls lib/str's at a direct call of one, its ranges and
// strings checked there (bin/aot); these are the kernel's own, which a
// call through a pointer to the import reaches, and what that code must
// do the same as: a range past the memory's end traps, a comparison's
// result is -1, 0 or 1, memchr looks no further than the memory has.
func (p *Process) libcMemcpy(d, s, n uint32) uint32 {
	copy(p.span(d, n), p.span(s, n))

	return d
}

func (p *Process) libcMemmove(d, s, n uint32) uint32 {
	copy(p.span(d, n), p.span(s, n))

	return d
}

func (p *Process) libcMemset(d, c, n uint32) uint32 {
	b := p.span(d, n)

	for i := range b {
		b[i] = byte(c)
	}

	return d
}

func (p *Process) libcMemcmp(a, b, n uint32) uint32 {
	return uint32(int32(bytes.Compare(p.span(a, n), p.span(b, n))))
}

func (p *Process) libcBcmp(a, b, n uint32) uint32 {
	return p.libcMemcmp(a, b, n)
}

func (p *Process) libcMemchr(s, c, n uint32) uint32 {
	at, found := p.search(s, n, byte(c))

	if !found {
		return 0
	}

	return at
}

func (p *Process) libcStrlen(s uint32) uint32 {
	return uint32(len(p.cstr(s)))
}

func (p *Process) libcStrcpy(d, s uint32) uint32 {
	p.strcpy(d, s)

	return d
}

func (p *Process) libcStrcat(d, s uint32) uint32 {
	p.strcpy(d+uint32(len(p.cstr(d))), s)

	return d
}

func (p *Process) libcStrcmp(a, b uint32) uint32 {
	return uint32(int32(bytes.Compare(p.cstr(a), p.cstr(b))))
}

func (p *Process) libcStrspn(s, set uint32) uint32 {
	return p.spanSet(s, set, false)
}

func (p *Process) libcStrcspn(s, set uint32) uint32 {
	return p.spanSet(s, set, true)
}

// Wide strings. A wchar_t is a 32-bit int, compared signed as musl does.
// Like the byte strings they are not system calls; a string that runs past
// the end of memory traps.

// wide is the memory of n wide characters at at.
func (p *Process) wide(at, n uint32) []byte {
	m := p.mem()
	end := uint64(at) + 4*uint64(n)

	if end > uint64(len(m)) {
		oob()
	}

	return m[at:end]
}

// wcharAt is the i-th wide character from at.
func (p *Process) wcharAt(at, i uint32) uint32 {
	m := p.mem()
	o := uint64(at) + 4*uint64(i)

	if o+4 > uint64(len(m)) {
		oob()
	}

	return binary.LittleEndian.Uint32(m[o:])
}

func (p *Process) wcsnlen(at, n uint32) uint32 {
	for i := uint32(0); i < n; i++ {
		if p.wcharAt(at, i) == 0 {
			return i
		}
	}

	return n
}

func (p *Process) wcslen(at uint32) uint32 {
	return p.wcsnlen(at, math.MaxUint32)
}

// wstring is the wide string at at, without its terminator.
func (p *Process) wstring(at uint32) []uint32 {
	n := p.wcslen(at)
	b := p.wide(at, n)
	out := make([]uint32, n)

	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[4*i:])
	}

	return out
}

// wcmp is a C comparison of two wide characters.
func wcmp(a, b uint32) uint32 {
	switch {
	case int32(a) < int32(b):
		return math.MaxUint32
	case int32(a) > int32(b):
		return 1
	}

	return 0
}

// wcsspan counts the leading characters of the string at at that are in
// the string set, or with reject that are not.
func (p *Process) wcsspan(at, set uint32, reject bool) uint32 {
	chars := p.wstring(set)

	for i := uint32(0); ; i++ {
		c := p.wcharAt(at, i)

		if c == 0 || slices.Contains(chars, c) == reject {
			return i
		}
	}
}

func (p *Process) wcscpy(d, s uint32) uint32 {
	n := p.wcslen(s) + 1
	copy(p.wide(d, n), p.wide(s, n))

	return d
}

// wcsftime is strftime of a wide format, its result wide.
func (p *Process) wcsftime(s, max, format, tm uint32) uint32 {
	var f strings.Builder

	for _, c := range p.wstring(format) {
		r := rune(c)

		if c > unicode.MaxRune || !utf8.ValidRune(r) {
			r = utf8.RuneError
		}

		f.WriteRune(r)
	}

	out := []rune(strftime(f.String(), p.readTm(tm)))

	if uint32(len(out)) >= max {
		return 0
	}

	b := p.wide(s, uint32(len(out))+1)

	for i, r := range out {
		binary.LittleEndian.PutUint32(b[4*i:], uint32(r))
	}

	binary.LittleEndian.PutUint32(b[4*len(out):], 0)

	return uint32(len(out))
}

func (p *Process) libcWcsncpy(d, s, n uint32) uint32 {
	l := p.wcsnlen(s, n)
	dst := p.wide(d, n)
	copy(dst, p.wide(s, l))
	clear(dst[4*l:])

	return d
}

func (p *Process) libcWcscat(d, s uint32) uint32 {
	p.wcscpy(d+4*p.wcslen(d), s)

	return d
}

func (p *Process) libcWcscmp(a, b uint32) uint32 {
	for i := uint32(0); ; i++ {
		x, y := p.wcharAt(a, i), p.wcharAt(b, i)

		if x != y || x == 0 {
			return wcmp(x, y)
		}
	}
}

func (p *Process) libcWcsncmp(a, b, n uint32) uint32 {
	for i := uint32(0); i < n; i++ {
		x, y := p.wcharAt(a, i), p.wcharAt(b, i)

		if x != y || x == 0 {
			return wcmp(x, y)
		}
	}

	return 0
}

// wcscoll and wcsxfrm are the C locale's: the order of the values.
func (p *Process) libcWcscoll(a, b uint32) uint32 {
	for i := uint32(0); ; i++ {
		x, y := p.wcharAt(a, i), p.wcharAt(b, i)

		if x != y || x == 0 {
			return wcmp(x, y)
		}
	}
}

func (p *Process) libcWcsxfrm(d, s, n uint32) uint32 {
	l := p.wcslen(s)

	if l < n {
		copy(p.wide(d, l+1), p.wide(s, l+1))
	} else if n > 0 {
		copy(p.wide(d, n-1), p.wide(s, n-1))
		binary.LittleEndian.PutUint32(p.wide(d+4*(n-1), 1), 0)
	}

	return l
}

func (p *Process) libcWcschr(s, c uint32) uint32 {
	for i := uint32(0); ; i++ {
		x := p.wcharAt(s, i)

		if x == c {
			return s + 4*i
		}

		if x == 0 {
			return 0
		}
	}
}

func (p *Process) libcWcsrchr(s, c uint32) uint32 {
	for i := p.wcslen(s); ; i-- {
		if p.wcharAt(s, i) == c {
			return s + 4*i
		}

		if i == 0 {
			return 0
		}
	}
}

func (p *Process) libcWcsstr(h, n uint32) uint32 {
	needle := p.wide(n, p.wcslen(n))
	hay := p.wide(h, p.wcslen(h))

	for from := 0; ; {
		i := bytes.Index(hay[from:], needle)

		if i < 0 {
			return 0
		}

		if (from+i)%4 == 0 {
			return h + uint32(from+i)
		}

		from += i + 1
	}
}

func (p *Process) libcWcspbrk(s, set uint32) uint32 {
	at := s + 4*p.wcsspan(s, set, true)

	if p.wcharAt(at, 0) == 0 {
		return 0
	}

	return at
}

func (p *Process) libcWcstok(s, sep, save uint32) uint32 {
	if s == 0 {
		if s = p.u32(save); s == 0 {
			return 0
		}
	}

	s += 4 * p.wcsspan(s, sep, false)

	if p.wcharAt(s, 0) == 0 {
		p.put32(save, 0)

		return 0
	}

	end := s + 4*p.wcsspan(s, sep, true)

	if p.wcharAt(end, 0) == 0 {
		p.put32(save, 0)
	} else {
		p.put32(end, 0)
		p.put32(save, end+4)
	}

	return s
}

func (p *Process) libcWcsdup(s uint32) uint32 {
	n := p.wcslen(s) + 1

	if uint64(n)*4 > heapMax {
		p.setErrno(syscall.ENOMEM)

		return 0
	}

	at := p.malloc(4 * n)

	if at == 0 {
		p.setErrno(syscall.ENOMEM)

		return 0
	}

	copy(p.wide(at, n), p.wide(s, n))

	return at
}

func (p *Process) libcWmemchr(s, c, n uint32) uint32 {
	for i := uint32(0); i < n; i++ {
		if p.wcharAt(s, i) == c {
			return s + 4*i
		}
	}

	return 0
}

func (p *Process) libcWmemcmp(a, b, n uint32) uint32 {
	for i := uint32(0); i < n; i++ {
		if x, y := p.wcharAt(a, i), p.wcharAt(b, i); x != y {
			return wcmp(x, y)
		}
	}

	return 0
}

func (p *Process) libcWmemcpy(d, s, n uint32) uint32 {
	copy(p.wide(d, n), p.wide(s, n))

	return d
}

func (p *Process) libcWmemmove(d, s, n uint32) uint32 {
	copy(p.wide(d, n), p.wide(s, n))

	return d
}

func (p *Process) libcWmemset(d, c, n uint32) uint32 {
	b := p.wide(d, n)

	for i := 0; i < len(b); i += 4 {
		binary.LittleEndian.PutUint32(b[i:], c)
	}

	return d
}

func (p *Process) libcWcwidth(c uint32) uint32 {
	return uint32(wcwidth(c))
}

func (p *Process) libcWcswidth(s, n uint32) uint32 {
	total := int32(0)

	for i := uint32(0); i < n; i++ {
		c := p.wcharAt(s, i)

		if c == 0 {
			break
		}

		w := wcwidth(c)

		if w < 0 {
			return uint32(w)
		}

		total += w
	}

	return uint32(total)
}

func (p *Process) libcWcslen(s uint32) uint32 {
	return p.wcslen(s)
}

func (p *Process) libcWcsnlen(s, n uint32) uint32 {
	return p.wcsnlen(s, n)
}

func (p *Process) libcWcscpy(d, s uint32) uint32 {
	return p.wcscpy(d, s)
}

func (p *Process) libcWcsspn(s, set uint32) uint32 {
	return p.wcsspan(s, set, false)
}

func (p *Process) libcWcscspn(s, set uint32) uint32 {
	return p.wcsspan(s, set, true)
}

func (p *Process) libcWcsftime(s, max, format, tm uint32) uint32 {
	return p.wcsftime(s, max, format, tm)
}

var regMessages = map[uint32]string{
	0: "No error", regNomatch: "No match", regBadpat: "Invalid regexp", 3: "Unknown collating element",
	4: "Unknown character class name", regEescape: "Trailing backslash", regEsubreg: "Invalid back reference",
	regEbrack: "Missing ']'", regEparen: "Missing ')'", regEbrace: "Missing '}'", regBadbr: "Invalid contents of {}",
	11: "Invalid character range", 12: "Out of memory", regBadrpt: "Repetition not preceded by valid expression",
}

func (p *Process) libcRegcomp(preg, pattern, flags uint32) uint32 {
	pat := bytes.Clone(p.cstr(pattern))
	src, nsub, code := translateRegex(pat, flags, false, false)

	if code != 0 {
		return code
	}

	re, err := regexp.Compile(src)

	if err != nil {
		if parsed, ok := err.(*syntax.Error); ok && parsed.Code == syntax.ErrInvalidCharRange {
			return regErange
		}

		return regBadpat
	}

	re.Longest()
	p.nextRegex++
	r := &posixRegex{pattern: pat, flags: flags, nsub: nsub}
	r.variants[0] = re
	p.regexes[p.nextRegex] = r
	p.put32(preg, uint32(nsub))
	p.put32(preg+4, p.nextRegex)

	return 0
}

func (p *Process) libcRegexec(preg, s, nmatch, pmatch, eflags uint32) uint32 {
	r, ok := p.regexes[p.u32(preg+4)]

	if !ok {
		return regBadpat
	}

	var subject []byte
	base := uint32(0)

	if eflags&regStartend != 0 {
		so, eo := p.u32(pmatch), p.u32(pmatch+4)

		if eo < so {
			return regBadpat
		}

		subject, base = p.span(s+so, eo-so), so
	} else {
		subject = p.cstr(s)
	}

	boundary := r.flags&regNewline != 0 && eflags&(regNotbol|regNoteol) != 0
	prefix := 0

	if boundary {
		suffix := 0

		if eflags&regNotbol != 0 {
			prefix = 1
		}

		if eflags&regNoteol != 0 {
			suffix = 1
		}

		padded := make([]byte, prefix+len(subject)+suffix)
		copy(padded[prefix:], subject)
		subject = padded
	}

	m := r.variant(eflags).FindSubmatchIndex(subject)

	if m == nil {
		return regNomatch
	}

	if boundary {
		m = m[2:]

		for i, at := range m {
			if at >= 0 {
				m[i] = at - prefix
			}
		}
	}

	if r.flags&regNosub == 0 {
		out := p.span(pmatch, 8*nmatch)

		for i := range int(nmatch) {
			so, eo := int32(-1), int32(-1)

			if 2*i < len(m) && m[2*i] >= 0 {
				so, eo = int32(m[2*i])+int32(base), int32(m[2*i+1])+int32(base)
			}

			binary.LittleEndian.PutUint32(out[8*i:], uint32(so))
			binary.LittleEndian.PutUint32(out[8*i+4:], uint32(eo))
		}
	}

	return 0
}

func (p *Process) libcRegfree(preg uint32) {
	delete(p.regexes, p.u32(preg+4))
}

func (p *Process) libcRegerror(code, preg, buf, size uint32) uint32 {
	msg, ok := regMessages[code]

	if !ok {
		msg = "Unknown error"
	}

	if size > 0 {
		p.putString(buf, truncate(msg, int(size)-1))
	}

	return uint32(len(msg) + 1)
}

func (p *Process) glob(pattern, flags, errfunc, out uint32) uint32 {
	g := &globber{p: p, flags: flags, errfunc: errfunc}
	offs := uint32(0)

	if flags&globDooffs != 0 {
		offs = p.u32(out + 8)
	}

	if flags&globAppend == 0 {
		p.put32(out, 0)
		p.put32(out+4, 0)
		p.put32(out+8, offs)
	}

	pat, ok := g.tilde(p.str(pattern))

	if !ok || pat == "" {
		return globNomatch
	}

	if strings.HasPrefix(pat, "/") {
		g.walk("/", strings.Split(pat[1:], "/"))
	} else {
		g.walk("", strings.Split(pat, "/"))
	}

	if g.aborted {
		return globAborted
	}

	if len(g.found) == 0 {
		if flags&globNocheck == 0 {
			return globNomatch
		}

		g.found = []string{p.str(pattern)}
	}

	if flags&globNosort == 0 {
		slices.Sort(g.found)
	}

	if flags&globMark != 0 {
		for i, name := range g.found {
			var st syscall.Stat_t
			isDir := p.statPath(name, true, &st) == nil && st.Mode&syscall.S_IFMT == syscall.S_IFDIR

			if isDir && !strings.HasSuffix(name, "/") {
				g.found[i] = name + "/"
			}
		}
	}

	count, old := p.u32(out), p.u32(out+4)

	total := uint64(offs) + uint64(count) + uint64(len(g.found)) + 1

	if total*4 > heapMax {
		return globNospace
	}

	pathv := p.malloc(uint32(total * 4))

	if pathv == 0 {
		return globNospace
	}

	clear(p.span(pathv, offs*4))

	if old != 0 {
		copy(p.span(pathv+offs*4, count*4), p.span(old+offs*4, count*4))
	}

	for i, name := range g.found {
		s := p.newString(name)

		if s == 0 {
			p.put32(pathv+(offs+count+uint32(i))*4, 0)
			p.globFree(pathv, offs, count+uint32(i))

			return globNospace
		}

		p.put32(pathv+(offs+count+uint32(i))*4, s)
	}

	p.put32(pathv+(offs+count+uint32(len(g.found)))*4, 0)
	p.free(old)
	p.put32(out, count+uint32(len(g.found)))
	p.put32(out+4, pathv)

	return 0
}

// globFree frees count strings of a pathv after offs, and the array.
func (p *Process) globFree(pathv, offs, count uint32) {
	for i := range count {
		p.free(p.u32(pathv + (offs+i)*4))
	}

	p.free(pathv)
}

func (p *Process) libcGlobfree(out uint32) {
	if pathv := p.u32(out + 4); pathv != 0 {
		p.globFree(pathv, p.u32(out+8), p.u32(out))
	}

	p.put32(out, 0)
	p.put32(out+4, 0)
}

func (p *Process) libcGlob(pattern, flags, errfunc, out uint32) uint32 {
	return p.glob(pattern, flags, errfunc, out)
}

func (p *Process) libcCrypt(key, setting uint32) uint32 {
	pw, s := p.str(key), p.str(setting)
	var out string
	ok := false

	switch {
	case strings.HasPrefix(s, "$1$"):
		out, ok = md5crypt(pw, s)
	case strings.HasPrefix(s, "$5$"):
		out, ok = shacrypt(pw, s, 32)
	case strings.HasPrefix(s, "$6$"):
		out, ok = shacrypt(pw, s, 64)
	case !strings.HasPrefix(s, "$") && !strings.HasPrefix(s, "_"):
		out, ok = descrypt(pw, s)
	}

	if !ok {
		p.setErrno(syscall.EINVAL)

		return 0
	}

	block := p.staticResult(staticCrypt, uint32(len(out))+1)

	if block != 0 {
		p.putString(block, out)
	}

	return block
}

// sized checks the sizes of the structures a call passes, each followed
// by the kernel's. A program built for a layout this kernel does not know
// ends by SIGSYS, before the kernel touches its memory.
func (p *Process) sized(call string, sizes ...uint32) {
	for k := 0; k+1 < len(sizes); k += 2 {
		if sizes[k] != sizes[k+1] {
			p.crash(fmt.Sprintf("%s: a structure of %d bytes, which this kernel knows of %d", call, sizes[k], sizes[k+1]), callers(1))
			p.exitWith(int(syscall.SIGSYS))
		}
	}
}

// wasiPreopen opens the process's root as WASI's preopened "/", at the
// lowest descriptor free after 2: none for a process without a root.
func (p *Process) wasiPreopen() {
	p.wasiRoot = -1
	f, err := p.openAt(atFdcwd, "/", syscall.O_RDONLY|syscall.O_DIRECTORY, 0)

	if err != nil {
		return
	}

	if fd, err := p.install(f, 3, true); err == nil {
		p.wasiRoot = fd
	}
}
