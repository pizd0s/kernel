//go:build linux && amd64

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/bits"
	mathrand "math/rand/v2"
	"net/netip"
	"os"
	"path"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	itimerSignals  = [3]int{SIGALRM, SIGVTALRM, SIGPROF}
	pathconfValues = []int32{8, 255, 255, 255, 4096, 4096, 1, 1, 0, 1, -1, -1, -1, 64, 4096, 4096, 4096, 4096, 4096, -1, 1}
	devctlValues   = map[uint32]bool{TCFLSH: true, TCXONC: true, TCSBRK: true}
)

var randomInit = [32]uint32{
	0x00000000, 0x5851f42d, 0xc0b18ccf, 0xcbb5f646, 0xc7033129, 0x30705b04, 0x20fd5db4, 0x9a8b7f78,
	0x502959d8, 0xab894868, 0x6c0356a7, 0x88cdb7ff, 0xb477d43f, 0x70a3a52b, 0xa8e4baf1, 0xfd8341fc,
	0x8ae16fd9, 0x742d2f7a, 0x0d1f0796, 0x76035e09, 0x40f7702c, 0x6fa72ca5, 0xaaa84157, 0x58a0df74,
	0xc74a0364, 0xae533cc4, 0x04185faf, 0x6de3b115, 0x0cab8628, 0xf043bfa4, 0x398150e9, 0x37521657,
}

var hostRlimits = [RLIM_NLIMITS]int{
	RLIMIT_CPU:        syscall.RLIMIT_CPU,
	RLIMIT_FSIZE:      syscall.RLIMIT_FSIZE,
	RLIMIT_DATA:       syscall.RLIMIT_DATA,
	RLIMIT_STACK:      syscall.RLIMIT_STACK,
	RLIMIT_CORE:       syscall.RLIMIT_CORE,
	RLIMIT_RSS:        hostRlimitRss,
	RLIMIT_NPROC:      hostRlimitNproc,
	RLIMIT_NOFILE:     syscall.RLIMIT_NOFILE,
	RLIMIT_MEMLOCK:    hostRlimitMemlock,
	RLIMIT_AS:         syscall.RLIMIT_AS,
	RLIMIT_LOCKS:      hostRlimitLocks,
	RLIMIT_SIGPENDING: hostRlimitSigpending,
	RLIMIT_MSGQUEUE:   hostRlimitMsgqueue,
	RLIMIT_NICE:       hostRlimitNice,
	RLIMIT_RTPRIO:     hostRlimitRtprio,
	RLIMIT_RTTIME:     hostRlimitRttime,
}

var devctlData = map[uint32]uint32{
	FIONBIO: 4, TIOCGPGRP: 4, TIOCSPGRP: 4, FIONREAD: 4, TIOCOUTQ: 4, TIOCGPTN: 4, TIOCGSID: 4,
	TIOCGWINSZ: sizeofWinsize, TIOCSWINSZ: sizeofWinsize,
	TCGETS: sizeofTermios, TCSETS: sizeofTermios, TCSETS + 1: sizeofTermios, TCSETS + 2: sizeofTermios,
}

const (
	staticDot        = 0
	staticSlash      = 2
	staticTm         = 8
	staticCtime      = 56
	staticStrerror   = 88
	staticStrsignal  = 184
	staticStrtok     = 256
	staticRandom     = 260
	staticRandomInit = 264
	staticNtoa       = 392
	staticCrypt      = 408
	staticSize       = 412
	niceMin          = -20
	niceMax          = 19
	ngroupsMax       = 65536
	stopSignals      = 1<<(SIGSTOP-1) | 1<<(SIGTSTP-1) | 1<<(SIGTTIN-1) | 1<<(SIGTTOU-1)
	unblockable      = 1<<(SIGKILL-1) | 1<<(SIGSTOP-1)
	redZone          = 128
	shadowFree       = 0x00
	shadowRedzone    = 0xfa
	shadowFreed      = 0xfd
	shadowOK         = 0xff
	sysSyncfs        = 306
	sysCopyFileRange = 326
	pollInSet        = POLLIN | POLLHUP | POLLERR
	pollOutSet       = POLLOUT | POLLERR
	tempLetters      = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	dirFd            = 0
	dirPos           = 4
	dirEnd           = 8
	dirIndex         = 12
	dirBuf           = 16
	dirBufLen        = 4096
	utsSysname       = "pizdos"
	utsRelease       = "1"
	utsVersion       = "#1"
	utsMachine       = "wasm32"
	windowsKept      = 8
	packetsKept      = 8
	packetRoomKept   = 4096
	traceStringMax   = 64
)

const (
	actTerminate = iota
	actIgnore
	actStop
	actContinue
)

const (
	signalNone = iota
	signalTaken
	signalFinal
)

type Process struct {
	packets []*Packet

	stats  Slots[FileStat]
	sends  Slots[SockSend]
	recvs  Slots[SockRecv]
	reads  Slots[FileRead]
	writes Slots[FileWrite]
	epolls Slots[EpollWait]
	k      *Kernel
	pid    int32
	ppid   int32
	pgid   atomic.Int32
	sid    atomic.Int32

	nice      int32
	policy    int32
	pdeathsig int

	comm       *string
	undumpable bool
	noNewPrivs bool
	inst       *Instance

	image *ExecImage
	files FdTable

	root  Object
	cwd   Object
	umask uint32

	errnoAt   uint32
	environAt uint32
	tzAt      [3]uint32
	nameAt    uint32

	heap   *Heap
	stack  *Stack
	static uint32
	env    uint32
	seed   uint64
	limits [RLIM_NLIMITS]Rlimit
	cred   Cred

	chaos     *ChaosRulesOf
	modes     uint32
	classes   uint32
	sink      Object
	failure   Errno
	iconvs    map[uint32]*Iconv
	nextIconv uint32

	spawnFiles     map[uint32][]SpawnAction
	nextSpawnFiles uint32
	semUndo        map[*SemSet]bool
	aios           map[uint32]*AioOp

	posixTimers  map[int32]*PosixTimer
	syncs        map[int32]Object
	nextSync     int32
	keys         [PTHREAD_KEYS_MAX]Key
	thread       Thread
	semOpens     map[string]*SemOpen
	defaultStack uint32
	defaultGuard uint32

	constants map[string]uint32
	siginfo   uint32

	zone       *time.Location
	envStrings map[uint32]bool
	mappings   map[uint32]Mapping

	timers [3]Itimer

	jumps   map[uint32]*JmpBuf
	stackAt uint32
	sig     SignalState

	vfork *VforkFrame

	zombie bool
	status int

	halted bool
	report int

	childEvents atomic.Uint64

	token   atomic.Uintptr
	chasing atomic.Bool

	events   EventQueue
	own      OwnEvents
	spare    *Subscription
	receives SpareWaiters[Received]
	turns    SpareWaiters[Turn]

	unwound bool
	starved bool

	asking    *Answer
	cancelled bool

	interruption uint64
}

type ExecImage struct {
	code *Code
	argv []string
	envp []string

	path string
}

type ProcExit struct {
	status int
}

func proc() *Process {
	p := current().proc

	p.interruption = signalNone

	return p
}

func (p *Process) libcExecmode(modes, classes, sink uint32) uint32 {
	if modes&^(EXEC_ASAN|EXEC_TRACE) != 0 || classes&^(TRACE_SYSTEM|TRACE_LIBRARY|TRACE_PROCESS|TRACE_SIGNALS|TRACE_FAULTS) != 0 {
		return p.result(0, EINVAL)
	}

	var to Object

	if int32(sink) != -1 {
		t, err := p.fd(sink)

		if err != nil {
			return p.result(0, err)
		}

		to = t
	}

	if modes&EXEC_TRACE != 0 && (to == nil || classes == 0) || modes&EXEC_TRACE == 0 && (to != nil || classes != 0) {
		return p.result(0, EINVAL)
	}

	if to != nil {
		to.ref()
	}

	p.setSink(to)
	p.modes, p.classes = modes, classes

	return p.result(0, nil)
}

func (p *Process) setSink(to Object) {
	if p.sink != nil {
		p.sink.unref()
	}

	p.sink = to
}

func (p *Process) tracedCalls() uint32 {
	if p.modes&EXEC_TRACE == 0 {
		return 0
	}

	return p.classes & (TRACE_SYSTEM | TRACE_LIBRARY)
}

func (p *Process) traces(class uint32) bool {
	return p.sink != nil && p.modes&EXEC_TRACE != 0 && p.classes&class != 0
}

func (p *Process) sinkWrite(b []byte) {
	for p.sink != nil {
		_, err := p.fileWrite(p.sink, bytesFile(b), len(b), IoMode{quiet: true})

		if err == nil || err == EAGAIN {
			return
		}

		if err != EINTR {
			p.setSink(nil)
			p.modes, p.classes = p.modes&^EXEC_TRACE, 0
		}
	}
}

func (p *Process) trace(class uint32, text string) {
	if p.traces(class) {
		p.sinkWrite(fmt.Appendf(nil, "[%d] %s\n", p.pid, text))
	}
}

func (p *Process) traceString(at uint32) string {
	if at == 0 {
		return "0"
	}

	if p.inst == nil {
		return fmt.Sprintf("%#x", at)
	}

	m := p.mem()

	if uint64(at) >= uint64(len(m)) {
		return fmt.Sprintf("%#x", at)
	}

	s := m[at:min(uint64(len(m)), uint64(at)+traceStringMax)]

	if n := bytes.IndexByte(s, 0); n >= 0 {
		return strconv.Quote(string(s[:n]))
	}

	return strconv.Quote(string(s)) + "..."
}

func (p *Process) traceValue(kind byte, v reflect.Value) string {
	if v.Kind() == reflect.Float32 || v.Kind() == reflect.Float64 {
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	}

	u := v.Uint()
	n := int64(u)

	if v.Kind() == reflect.Uint32 {
		n = int64(int32(u))
	}

	switch {
	case kind == 's' && v.Kind() == reflect.Uint32:
		return p.traceString(uint32(u))
	case kind == 'o':
		return fmt.Sprintf("%#o", u)
	case kind != 'p' && kind != 's' && n >= -4096 && n < 65536, u == 0:
		return strconv.FormatInt(n, 10)
	}

	return fmt.Sprintf("%#x", u)
}

func (p *Process) traceCall(class uint32, name string, args, out []reflect.Value, failure Errno) {
	if !p.traces(class) {
		return
	}

	var b strings.Builder

	format := callFormats[name]

	fmt.Fprintf(&b, "[%d] %s(", p.pid, name)

	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}

		kind := byte('d')

		if i+1 < len(format) {
			kind = format[i+1]
		}

		b.WriteString(p.traceValue(kind, a))
	}

	b.WriteString(")")

	for _, r := range out {
		kind := byte('d')

		if len(format) > 0 {
			kind = format[0]
		}

		b.WriteString(" = ")

		if failed := r.Kind() == reflect.Uint32 && int32(r.Uint()) == -1 || r.Kind() == reflect.Uint64 && int64(r.Uint()) == -1; failed && failure != 0 {
			fmt.Fprintf(&b, "-1 %s (%s)", errnoNames[failure], failure.error())
		} else {
			b.WriteString(p.traceValue(kind, r))
		}
	}

	b.WriteString("\n")
	p.sinkWrite([]byte(b.String()))
}

func (p *Process) chaosStart(env []string) {
	if p.chaos == nil {
		p.chaos = chaosRulesIn(env)
	}
}

func (p *Process) chaosFail(point string) bool {
	return p != nil && p.chaos.fail(point)
}

func (p *Process) chaosMoves() bool {
	return p != nil && p.chaos.holds("memory-move")
}

func (p *Process) chaosCompacts() bool {
	return p != nil && p.chaos.holds("memory-compact")
}

func (p *Process) firstCwd() (Object, string) {
	p.root.ref()

	return p.root, "/"
}

func (p *Process) run(img *ExecImage) int {
	return p.runFrom(img, nil, nil)
}

func (p *Process) runFrom(img *ExecImage, i *Instance, enter func()) int {
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
		p.trace(TRACE_FAULTS, err.error())
		status = 1 << 8
	}

	p.terminate(status)

	return status
}

func (p *Process) runImage(img *ExecImage, i *Instance, enter func()) (next *ExecImage, status int) {
	if i == nil {
		var err error

		if i, err = newInstance(p.k.pool, p, img.code, runStack); err != nil {
			p.starved = true

			throw(err)
		}

		i.attach(p)
		p.heap, p.stack, p.static, p.env, p.errnoAt, p.environAt, p.siginfo = nil, nil, 0, 0, 0, 0, 0
		p.tzAt, p.nameAt, p.zone = [3]uint32{}, 0, nil
		p.constants = map[string]uint32{}
		p.iconvs, p.spawnFiles = map[uint32]*Iconv{}, map[uint32][]SpawnAction{}
		p.syncs, p.nextSync, p.semOpens, p.keys, p.thread = map[int32]Object{}, 0, map[string]*SemOpen{}, [PTHREAD_KEYS_MAX]Key{}, Thread{}
		p.defaultStack, p.defaultGuard = 0, 0

		for _, t := range p.posixTimers {
			p.disarmPosix(t)
		}

		p.posixTimers = nil
		p.aioDrop()
		p.envStrings, p.mappings = map[uint32]bool{}, map[uint32]Mapping{}
		p.jumps, p.stackAt = map[uint32]*JmpBuf{}, 0
		enter = func() {
			p.chaosStart(img.envp)
			p.asanStart()
			i.start()
			p.stackStart()
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

		if t, ok := e.(*Trap); ok {
			msg := t.Error()

			if t.code == trapAsan {
				msg = who.asanReport(t)
				who.stderrReport(msg, t.stack)
			}

			who.traceStack(TRACE_FAULTS, msg, t.stack)
		}

		p.endBorrowers(i, e)

		switch e := e.(type) {
		case nil:
		case *ProcExit:
			status = e.status
		case *ExecImage:
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

	p.raiseInterrupt()

	if p.interruptDue() {
		p.chase()
	}

	enter()

	return nil, 0
}

func trapSignal(code uint32) int {
	switch code {
	case trapMemoryOOB, trapTableOOB, trapNullRef, trapUndefinedElem, trapUninitElem, trapAsan:
		return SIGSEGV
	case trapIntOverflow, trapDivByZero, trapConversion:
		return SIGFPE
	}

	return SIGILL
}

func (p *Process) terminate(status int) {
	p.unwound = true

	if sig := status & 0x7f; sig != 0 {
		p.trace(TRACE_PROCESS, "killed "+signalName(sig))
	} else {
		p.trace(TRACE_PROCESS, fmt.Sprintf("exit %d", status>>8&0xff))
	}

	p.undoSems()
	p.aioDrop()
	p.stopTimers()
	p.leaveSession(true)

	for fd := range p.files.size() {
		p.closeFd(fd)
	}

	p.root.unref()
	p.cwd.unref()
	p.setSink(nil)
	p.sig.mu.Lock()
	p.sig.dead = true
	p.sig.mu.Unlock()

	if p.pid == 0 {
		return
	}

	k := p.k

	k.mu.Lock()

	parent := k.procs[p.ppid]
	adopted := false

	var bereaved []*Process
	var joined []int32

	if parent != nil && parent.pgid.Load() != p.pgid.Load() && parent.sid.Load() == p.sid.Load() {
		joined = append(joined, p.pgid.Load())
	}

	for _, q := range k.procs {
		if q.ppid == p.pid {
			q.ppid = k.first.pid
			adopted = adopted || q.zombie

			if q.pdeathsig != 0 && !q.zombie {
				bereaved = append(bereaved, q)
			}

			if !q.zombie && q.pgid.Load() != p.pgid.Load() && q.sid.Load() == p.sid.Load() {
				joined = append(joined, q.pgid.Load())
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
		k.first.childChanged(SIGCHLD)
	}

	for _, q := range bereaved {
		q.send(q.pdeathsig)
	}

	if notify {
		parent.childChanged(SIGCHLD)
	}

	if p == k.first {
		k.exit <- hostCode(status)
	}
}

func (p *Process) discardsChildren() bool {
	p.sig.mu.Lock()

	defer p.sig.mu.Unlock()

	act := p.sig.actions[SIGCHLD]

	return act.handler == SIG_IGN || act.flags&SA_NOCLDWAIT != 0
}

func (p *Process) childChanged(sig int) {
	p.childEvents.Add(1)

	if sig != 0 {
		p.send(sig)
	}

	p.post(&p.own.child)
}

func hostCode(status int) int {
	if status&0x7f != 0 {
		return 128 + status&0x7f
	}

	return status >> 8 & 0xff
}

func (p *Process) exitWith(status int) {
	if v := p.vfork; v != nil {
		p.vfork = nil
		p.terminate(status)
		v.resume(p.pid)
	}

	panic(&ProcExit{status})
}

func (p *Process) libc__errno_location() uint32 {
	return p.errnoAt
}

func (p *Process) libc__kernel_begin(errnoAt, environAt, argcAt, argvAt, tznameAt, timezoneAt, daylightAt uint32) {
	p.errnoAt, p.environAt = errnoAt, environAt
	p.tzAt = [3]uint32{tznameAt, timezoneAt, daylightAt}

	argv := p.stringArray(p.image.argv)

	p.env = p.stringArray(p.image.envp)

	if argv == 0 || p.env == 0 {
		p.exitWith(SIGABRT)
	}

	if len(p.image.argv) > 0 {
		p.nameAt = p.u32(argv) + uint32(strings.LastIndexByte(p.image.argv[0], '/')+1)
	}

	p.put32(argcAt, uint32(len(p.image.argv)))
	p.put32(argvAt, argv)
	p.put32(environAt, p.env)
}

func (p *Process) libc__kernel_name() uint32 {
	return p.nameAt
}

func (p *Process) libc__kernel_program(buf, size uint32) uint32 {
	path := p.image.path

	if path == "" {
		return p.pointer(0, ENOENT)
	}

	if uint32(len(path)) >= size {
		return p.pointer(0, ERANGE)
	}

	p.putString(buf, path)

	return p.pointer(buf, nil)
}

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

func (p *Process) newString(s string) uint32 {
	at := p.malloc(uint32(len(s) + 1))

	if at != 0 {
		b := p.span(at, uint32(len(s)+1))

		copy(b, s)
		b[len(s)] = 0
	}

	return at
}

func (p *Process) mem() []byte {
	return p.inst.mem
}

func oob() {
	panic(newTrap(trapMemoryOOB, 1))
}

func (p *Process) memory(at, n uint32, mode int) *MemfdFile {
	p.span(at, n)

	return p.inst.window(int64(at), int64(n), narrowed(O_RDWR, mode))
}

func (p *Process) memoryDone(w *MemfdFile) {
	p.inst.windowDone(w)
}

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

func (p *Process) putString(at uint32, s string) {
	b := p.span(at, uint32(len(s)+1))

	copy(b, s)
	b[len(s)] = 0
}

func (p *Process) constant(s string) uint32 {
	if at, ok := p.constants[s]; ok {
		return at
	}

	at := p.newString(s)

	if at == 0 {
		p.exitWith(SIGABRT)
	}

	p.constants[s] = at

	return at
}

func errnoOf(err error) Errno {
	var e Errno
	errors.As(err, &e)

	return e
}

func (p *Process) setErrno(e Errno) {
	p.failure = e
	p.put32(p.errnoAt, uint32(e))
}

func (p *Process) errno() Errno {
	return Errno(p.u32(p.errnoAt))
}

func (p *Process) result(r int, err error) uint32 {
	if err != nil {
		p.setErrno(errnoOf(err))
		r = -1
	}

	return uint32(r)
}

func (p *Process) result64(r int64, err error) uint64 {
	if err != nil {
		p.setErrno(errnoOf(err))
		r = -1
	}

	return uint64(r)
}

func (p *Process) pointer(at uint32, err error) uint32 {
	if err != nil {
		p.setErrno(errnoOf(err))
		at = 0
	}

	return at
}

func (p *Process) staticAt(off uint32) uint32 {
	if p.static == 0 {
		p.static = p.malloc(staticSize)

		if p.static == 0 {
			p.exitWith(SIGABRT)
		}

		clear(p.span(p.static, staticSize))
		p.putString(p.static+staticDot, ".")
		p.putString(p.static+staticSlash, "/")
	}

	return p.static + off
}

type Rlimit struct {
	cur, max uint64
}

func hostLimits() [RLIM_NLIMITS]Rlimit {
	var out [RLIM_NLIMITS]Rlimit

	for i := range out {
		var l syscall.Rlimit

		if syscall.Getrlimit(hostRlimits[i], &l) == nil {
			out[i] = Rlimit{l.Cur, l.Max}
		} else {
			out[i] = Rlimit{^uint64(0), ^uint64(0)}
		}
	}

	return out
}

func (p *Process) exec(path string, argv, envp []string) error {
	return p.execFrom(path, nil, argv, envp)
}

func (p *Process) execFrom(path string, f Object, argv, envp []string) error {
	img, err := p.loadFrom(path, f, argv)

	if err != nil {
		return err
	}

	img.envp = envp
	p.trace(TRACE_PROCESS, "exec "+strconv.Quote(path))
	p.execImage(img)

	return ELOOP
}

func (p *Process) loadFrom(path string, first Object, argv []string) (*ExecImage, error) {
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

		var st Stat

		t := f

		if err := statFile(p, f, &st); err != nil {
			f.unref()

			return nil, err
		}

		if fl, _ := p.fileFlags(f); fl&O_PATH != 0 || st.Mode&S_IFMT != S_IFREG {
			f.unref()

			return nil, EACCES
		}

		if !p.permits(&st, 1) {
			f.unref()

			return nil, EACCES
		}

		head := make([]byte, 256)
		n, _ := p.filePread(t, bytesFile(head), len(head), 0)

		head = head[:max(n, 0)]

		if bytes.HasPrefix(head, []byte("#!")) {
			f.unref()

			line, _, _ := bytes.Cut(head[2:], []byte("\n"))
			fields := strings.Fields(string(line))

			if len(fields) == 0 {
				return nil, ENOEXEC
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

		return &ExecImage{code: code, argv: argv, path: program}, nil
	}

	return nil, ELOOP
}

func (p *Process) openProgram(path string) (Object, string, error) {
	var f Object
	var program string

	err := p.onPath(AT_FDCWD, path, func(d Object) error {
		var err error

		if f, err = p.dirOpen(d, path, O_RDONLY, 0); err != nil {
			return err
		}

		if real, err := p.dirRealpath(d, path); err == nil {
			program = real
		}

		return nil
	})

	return f, program, err
}

func (p *Process) execReset() {
	for fd, d := range p.files.slots {
		if d.obj != nil && d.cloexec {
			p.closeFd(fd)
		}
	}

	p.sig.mu.Lock()

	for i, a := range p.sig.actions {
		if a.handler != SIG_DFL && a.handler != SIG_IGN {
			p.sig.actions[i] = SigAction{}
		}
	}

	p.sig.altstack.sp, p.sig.altstack.size = 0, 0
	p.sig.mu.Unlock()
	p.comm = nil
}

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

func (p *Process) variableName(at uint32) (string, bool) {
	name := p.str(at)

	if name == "" || strings.IndexByte(name, '=') >= 0 {
		p.setErrno(EINVAL)

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

	return uint32(p.pgid.Load())
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
		return p.result(0, EINVAL)
	}

	for i, g := range groups {
		p.put32(list+uint32(4*i), g)
	}

	return p.result(len(groups), nil)
}

func (p *Process) libcSysconf(name uint32) uint32 {
	switch name {
	case 85:
		return uint32(pages(false))
	case 86:
		return uint32(pages(true))
	}

	values := map[uint32]int64{
		0: 131072, 1: int64(min64(p.limits[RLIMIT_NPROC].cur, 1<<30)), 2: 100, 3: 65536,
		4: int64(min64(p.limits[RLIMIT_NOFILE].cur, 1<<30)), 5: int64(min64(p.limits[RLIMIT_NOFILE].cur, 1<<30)), 6: -1,
		7: 1, 8: 1, 29: 200809, 30: 4096, 43: 2048, 60: 1024,
		69: -1, 70: -1, 71: 256, 72: 32, 83: int64(runtime.NumCPU()), 84: int64(runtime.NumCPU()), 149: 200809, 173: 40, 180: 64,
	}

	if v, ok := values[name]; ok {
		return uint32(v)
	}

	return p.result(0, EINVAL)
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

func (p *Process) libcPutenv(s uint32) uint32 {
	name, _, set := strings.Cut(p.str(s), "=")

	if name == "" {
		p.setErrno(EINVAL)

		return math.MaxUint32
	}

	if !set {
		s = 0
	}

	if !p.setVariable(name, s) {
		p.setErrno(ENOMEM)

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
		p.setErrno(ENOMEM)

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
		p.setErrno(ENOMEM)

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

func (p *Process) libcExecv(path, argv uint32) uint32 {
	return p.result(0, p.exec(p.str(path), p.strings(argv), p.environment()))
}

func (p *Process) libcExecle(path, arg0, va uint32) uint32 {
	argv := p.argList(arg0, va)
	envp := p.varArg(va, uint32(max(len(argv), 1)))

	if arg0 == 0 {
		envp = p.varArg(va, 0)
	}

	return p.result(0, p.exec(p.str(path), argv, p.strings(envp)))
}

func (p *Process) libcFexecve(fd, argv, envp uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	path := fmt.Sprintf("/dev/fd/%d", fd)

	return p.result(0, p.execFrom(path, obj, p.strings(argv), p.strings(envp)))
}

func (p *Process) libcPosix_spawn(pid, path, actions, attr, argv, envp uint32) uint32 {
	child, err := p.spawn(p.str(path), actions, attr, p.strings(argv), p.strings(envp))

	return p.spawned(pid, child, err)
}

func (p *Process) spawned(pid uint32, child int32, err error) uint32 {
	if err != nil {
		return uint32(errnoOf(err))
	}

	if pid != 0 {
		p.put32(pid, uint32(child))
	}

	return 0
}

func (p *Process) spawn(file string, actions, attr uint32, argv, envp []string) (int32, error) {
	var list []SpawnAction

	if actions != 0 {
		l, ok := p.spawnFiles[p.u32(actions)]

		if !ok {
			return 0, EINVAL
		}

		list = l
	}

	if p.chaosFail("fork") {
		return 0, EAGAIN
	}

	c, err := p.k.newProcess(p)

	if err != nil {
		return 0, err
	}

	c.inherit(p)

	img, path, err := c.spawnSetup(p, file, list, attr, argv)

	if err != nil {
		c.unmade()

		return 0, err
	}

	img.envp = envp
	c.execReset()

	parent := p.pid

	go func() {
		c.trace(TRACE_PROCESS, fmt.Sprintf("child of %d", parent))
		c.trace(TRACE_PROCESS, "exec "+strconv.Quote(path))
		c.run(img)
	}()

	return c.pid, nil
}

func (c *Process) spawnSetup(p *Process, file string, list []SpawnAction, attr uint32, argv []string) (*ExecImage, string, error) {
	if attr != 0 {
		if err := c.spawnAttributes(p, attr); err != nil {
			return nil, "", err
		}
	}

	for _, a := range list {
		if err := c.spawnAction(a); err != nil {
			return nil, "", err
		}
	}

	img, err := c.loadFrom(file, nil, argv)

	return img, file, err
}

func (c *Process) spawnAttributes(p *Process, attr uint32) error {
	flags := p.u32(attr)

	switch {
	case flags&POSIX_SPAWN_SETSID != 0:
		c.k.mu.Lock()
		c.sid.Store(c.pid)
		c.pgid.Store(c.pid)
		c.k.mu.Unlock()
	case flags&POSIX_SPAWN_SETPGROUP != 0:
		group := int32(p.u32(attr + 4))

		if group == 0 {
			group = c.pid
		}

		if group < 0 || group != c.pid && !c.groupExists(group) {
			return EPERM
		}

		c.k.mu.Lock()
		c.pgid.Store(group)
		c.k.mu.Unlock()
	}

	if flags&POSIX_SPAWN_RESETIDS != 0 {
		c.k.mu.Lock()
		c.cred.uids[1], c.cred.gids[1] = c.cred.uids[0], c.cred.gids[0]
		c.k.mu.Unlock()
	}

	c.sig.mu.Lock()

	if flags&POSIX_SPAWN_SETSIGMASK != 0 {
		c.sig.blocked = p.u64(attr+16) &^ unblockable
	}

	if flags&POSIX_SPAWN_SETSIGDEF != 0 {
		set := p.u64(attr + 8)

		for sig := 1; sig < NSIG; sig++ {
			if set&sigBit(sig) != 0 {
				c.sig.actions[sig] = SigAction{}
			}
		}
	}

	c.sig.mu.Unlock()

	switch {
	case flags&POSIX_SPAWN_SETSCHEDULER != 0:
		return p.scheduler(c, p.u32(attr+24), attr+28)
	case flags&POSIX_SPAWN_SETSCHEDPARAM != 0 && p.u32(attr+28) != 0:
		return EINVAL
	}

	return nil
}

func (c *Process) spawnAction(a SpawnAction) error {
	switch a.kind {
	case spawnOpen:
		if int(a.fd) >= c.fdLimit() {
			return EBADF
		}

		mode := uint32(0)

		if a.flags&(O_CREAT|0o20000000) != 0 {
			mode = a.mode & 0o7777 &^ c.umask
		}

		c.closeFd(int(a.fd))

		f, err := c.openAt(AT_FDCWD, a.path, int(a.flags&^O_CLOEXEC), mode)

		if err != nil {
			return err
		}

		c.files.put(int(a.fd), f, a.flags&O_CLOEXEC != 0)
	case spawnClose:
		if !c.closeFd(int(a.fd)) && int(a.fd) >= c.fdLimit() {
			return EBADF
		}
	case spawnDup2:
		if a.fd != a.to {
			_, err := c.dupTo(a.fd, a.to, false)

			return err
		}

		if _, ok := c.files.at(int(a.fd)); !ok {
			return EBADF
		}

		c.files.slots[a.fd].cloexec = false
	case spawnChdir:
		return c.chdir(a.path)
	case spawnFchdir:
		return c.fchdir(a.fd)
	}

	return nil
}

func (p *Process) unmade() {
	for fd := range p.files.size() {
		p.closeFd(fd)
	}

	p.root.unref()
	p.cwd.unref()
	p.setSink(nil)
	p.sig.mu.Lock()
	p.sig.dead = true
	p.sig.mu.Unlock()
	p.k.mu.Lock()
	delete(p.k.procs, p.pid)
	p.k.mu.Unlock()
}

func (p *Process) libcPosix_spawn_file_actions_init(actions uint32) uint32 {
	p.nextSpawnFiles++
	p.spawnFiles[p.nextSpawnFiles] = nil
	p.put32(actions, p.nextSpawnFiles)

	return 0
}

func (p *Process) libcPosix_spawn_file_actions_destroy(actions uint32) uint32 {
	id := p.u32(actions)

	if _, ok := p.spawnFiles[id]; !ok {
		return uint32(EINVAL)
	}

	delete(p.spawnFiles, id)
	p.put32(actions, 0)

	return 0
}

func (p *Process) addSpawnAction(actions uint32, a SpawnAction) uint32 {
	id := p.u32(actions)
	list, ok := p.spawnFiles[id]

	if !ok {
		return uint32(EINVAL)
	}

	p.spawnFiles[id] = append(list, a)

	return 0
}

func (p *Process) spawnDescriptor(fd uint32) bool {
	return int32(fd) >= 0 && int(fd) < p.fdLimit()
}

func (p *Process) libcPosix_spawn_file_actions_addopen(actions, fd, path, flags, mode uint32) uint32 {
	if !p.spawnDescriptor(fd) {
		return uint32(EBADF)
	}

	return p.addSpawnAction(actions, SpawnAction{kind: spawnOpen, fd: fd, path: p.str(path), flags: flags, mode: mode})
}

func (p *Process) libcPosix_spawn_file_actions_addclose(actions, fd uint32) uint32 {
	if int32(fd) < 0 {
		return uint32(EBADF)
	}

	return p.addSpawnAction(actions, SpawnAction{kind: spawnClose, fd: fd})
}

func (p *Process) libcPosix_spawn_file_actions_adddup2(actions, fd, to uint32) uint32 {
	if !p.spawnDescriptor(fd) || !p.spawnDescriptor(to) {
		return uint32(EBADF)
	}

	return p.addSpawnAction(actions, SpawnAction{kind: spawnDup2, fd: fd, to: to})
}

func (p *Process) libcPosix_spawn_file_actions_addchdir(actions, path uint32) uint32 {
	return p.addSpawnAction(actions, SpawnAction{kind: spawnChdir, path: p.str(path)})
}

func (p *Process) libcPosix_spawn_file_actions_addfchdir(actions, fd uint32) uint32 {
	if int32(fd) < 0 {
		return uint32(EBADF)
	}

	return p.addSpawnAction(actions, SpawnAction{kind: spawnFchdir, fd: fd})
}

func (p *Process) libcPosix_spawn_file_actions_addchdir_np(actions, path uint32) uint32 {
	return p.libcPosix_spawn_file_actions_addchdir(actions, path)
}

func (p *Process) libcPosix_spawn_file_actions_addfchdir_np(actions, fd uint32) uint32 {
	return p.libcPosix_spawn_file_actions_addfchdir(actions, fd)
}

func (p *Process) libcPosix_spawnattr_init(attr uint32) uint32 {
	p.put32(attr, 0)
	p.put32(attr+4, 0)
	p.put64(attr+8, 0)
	p.put64(attr+16, 0)
	p.put32(attr+24, SCHED_OTHER)
	p.put32(attr+28, 0)

	return 0
}

func (p *Process) libcPosix_spawnattr_destroy(attr uint32) uint32 {
	return 0
}

func (p *Process) libcPosix_spawnattr_getflags(attr, out uint32) uint32 {
	p.put16(out, uint16(p.u32(attr)))

	return 0
}

func (p *Process) libcPosix_spawnattr_setflags(attr, flags uint32) uint32 {
	all := uint32(POSIX_SPAWN_RESETIDS | POSIX_SPAWN_SETPGROUP | POSIX_SPAWN_SETSIGDEF | POSIX_SPAWN_SETSIGMASK | POSIX_SPAWN_SETSCHEDPARAM |
		POSIX_SPAWN_SETSCHEDULER | POSIX_SPAWN_SETSID)

	if flags&^all != 0 {
		return uint32(EINVAL)
	}

	p.put32(attr, flags)

	return 0
}

func (p *Process) libcPosix_spawnattr_getpgroup(attr, out uint32) uint32 {
	p.put32(out, p.u32(attr+4))

	return 0
}

func (p *Process) libcPosix_spawnattr_setpgroup(attr, pgroup uint32) uint32 {
	p.put32(attr+4, pgroup)

	return 0
}

func (p *Process) libcPosix_spawnattr_getsigdefault(attr, out uint32) uint32 {
	p.put64(out, p.u64(attr+8))

	return 0
}

func (p *Process) libcPosix_spawnattr_setsigdefault(attr, set uint32) uint32 {
	p.put64(attr+8, p.u64(set))

	return 0
}

func (p *Process) libcPosix_spawnattr_getsigmask(attr, out uint32) uint32 {
	p.put64(out, p.u64(attr+16))

	return 0
}

func (p *Process) libcPosix_spawnattr_setsigmask(attr, set uint32) uint32 {
	p.put64(attr+16, p.u64(set))

	return 0
}

func (p *Process) libcPosix_spawnattr_getschedpolicy(attr, out uint32) uint32 {
	p.put32(out, p.u32(attr+24))

	return 0
}

func (p *Process) libcPosix_spawnattr_setschedpolicy(attr, policy uint32) uint32 {
	p.put32(attr+24, policy)

	return 0
}

func (p *Process) libcPosix_spawnattr_getschedparam(attr, out uint32) uint32 {
	p.put32(out, p.u32(attr+28))

	return 0
}

func (p *Process) libcPosix_spawnattr_setschedparam(attr, param uint32) uint32 {
	p.put32(attr+28, p.u32(param))

	return 0
}

func waitClock(clock uint32) bool {
	return clock == CLOCK_REALTIME || clock == CLOCK_MONOTONIC
}

func (p *Process) libcPthread_mutex_clocklock(mutex, clock, at uint32) uint32 {
	if !waitClock(clock) {
		return uint32(EINVAL)
	}

	return codeOf(p.timedLock(mutex, defaultMutex, false, at, clock))
}

func (p *Process) libcPthread_cond_clockwait(cond, mutex, clock, at uint32) uint32 {
	if !waitClock(clock) {
		return uint32(EINVAL)
	}

	return codeOf(p.condTimedwait(cond, mutex, at, &clock))
}

func (p *Process) libcPthread_rwlock_clockrdlock(lock, clock, at uint32) uint32 {
	if !waitClock(clock) {
		return uint32(EINVAL)
	}

	return codeOf(p.timedLock(lock, defaultRWLock, true, at, clock))
}

func (p *Process) libcPthread_rwlock_clockwrlock(lock, clock, at uint32) uint32 {
	if !waitClock(clock) {
		return uint32(EINVAL)
	}

	return codeOf(p.timedLock(lock, defaultRWLock, false, at, clock))
}

func (p *Process) libcSem_clockwait(sem, clock, at uint32) uint32 {
	if !waitClock(clock) {
		return p.result(0, EINVAL)
	}

	err := p.syncCall(sem, nil, &SyncLock{thread: p.pid, try: true})

	if err != EAGAIN {
		return p.result(0, err)
	}

	end, err := p.deadlineOf(at, clock)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(0, p.syncCall(sem, nil, &SyncLock{thread: p.pid, end: end}))
}

func (p *Process) libcSig2str(sig, out uint32) uint32 {
	name, ok := signalAbbrev(int(int32(sig)))

	if !ok {
		return ^uint32(0)
	}

	b := p.span(out, uint32(len(name))+1)

	copy(b, name)
	b[len(name)] = 0

	return 0
}

func (p *Process) libcStr2sig(name, out uint32) uint32 {
	sig, ok := signalNumbered(p.str(name))

	if !ok {
		return ^uint32(0)
	}

	p.put32(out, uint32(sig))

	return 0
}

func (p *Process) libcTcgetwinsize(fd, at uint32) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	size, err := p.ttyWindow(t)

	if err == nil {
		copy(p.span(at, sizeofWinsize), size)
	}

	return p.result(0, err)
}

func (p *Process) libcTcsetwinsize(fd, at uint32) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(0, p.ttySetWindow(t, append([]byte(nil), p.span(at, sizeofWinsize)...)))
}

func (p *Process) libcPosix_getdents(fd, buf, n, flags uint32) uint32 {
	if flags != 0 {
		return p.result(0, EINVAL)
	}

	o, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	if fl, err := p.fileFlags(o); err == nil && (fl&O_PATH != 0 || fl&O_ACCMODE == O_WRONLY) {
		return p.result(0, EBADF)
	}

	w := p.memory(buf, n, O_WRONLY)
	count, err := p.fileReaddir(o, w, int(n))

	p.memoryDone(w)

	if err != nil || count != 0 || n >= direntRoom(NAME_MAX) {
		return p.result(count, err)
	}

	at, err := p.fileSeek(o, 0, SEEK_CUR)

	if err != nil {
		return p.result(0, err)
	}

	if next, err := p.fileReaddir(o, bytesFile(make([]byte, direntRoom(NAME_MAX))), int(direntRoom(NAME_MAX))); err != nil || next == 0 {
		return p.result(0, err)
	}

	if _, err := p.fileSeek(o, at, SEEK_SET); err != nil {
		return p.result(0, err)
	}

	return p.result(0, EINVAL)
}

func direntRoom(length uint32) uint32 {
	return (19 + length + 1 + 7) &^ 7
}

func (p *Process) libcPosix_devctl(fd, cmd, data, n, info uint32) uint32 {
	arg := data

	if need, ok := devctlData[cmd]; ok && (data == 0 || n != 0 && n < need) {
		return uint32(EINVAL)
	}

	if devctlValues[cmd] {
		arg = 0

		if data != 0 {
			if n != 0 && n < 4 {
				return uint32(EINVAL)
			}

			arg = p.u32(data)
		}
	}

	saved := p.errno()
	r := p.ioctl(fd, cmd, arg)

	if int32(r) == -1 {
		failed := p.failure

		p.setErrno(saved)

		return uint32(failed)
	}

	if info != 0 {
		p.put32(info, r)
	}

	return 0
}

func (p *Process) ended() bool {
	p.sig.mu.Lock()

	defer p.sig.mu.Unlock()

	return p.sig.dead
}

func (p *Process) undoSet(s *SemSet) {
	if p.semUndo == nil {
		p.semUndo = map[*SemSet]bool{}
	}

	p.semUndo[s] = true
}

func (p *Process) undoSems() {
	for s := range p.semUndo {
		s.undoExit(p)
	}

	p.semUndo = nil
}

func (p *Process) libcMq_open(name, flags, va uint32) uint32 {
	key, err := semaphoreName(p.str(name))

	if err != nil {
		return p.result(0, err)
	}

	access := int(flags & O_ACCMODE)

	if access == O_ACCMODE {
		return p.result(0, EINVAL)
	}

	p.k.mu.Lock()

	q := p.k.queues[key]
	made := false

	switch {
	case q != nil && flags&(O_CREAT|O_EXCL) == O_CREAT|O_EXCL:
		p.k.mu.Unlock()

		return p.result(0, EEXIST)
	case q == nil && flags&O_CREAT == 0:
		p.k.mu.Unlock()

		return p.result(0, ENOENT)
	case q == nil:
		maxmsg, msgsize := mqDefaultMaxmsg, mqDefaultMsgsize

		if attr := p.varArg(va, 1); attr != 0 {
			maxmsg, msgsize = int(int32(p.u32(attr+4))), int(int32(p.u32(attr+8)))
		}

		if maxmsg <= 0 || msgsize <= 0 || maxmsg > mqMaxmsg || msgsize > mqMsgsize || maxmsg*msgsize > mqBytes {
			p.k.mu.Unlock()

			return p.result(0, EINVAL)
		}

		q = newMessageQueue(key, p.varArg(va, 0)&0o777&^p.umask, p.cred.euid(), p.cred.egid(), maxmsg, msgsize)
		p.k.queues[key] = q
		made = true
	}

	p.k.mu.Unlock()

	want := map[int]uint32{O_RDONLY: 4, O_WRONLY: 2, O_RDWR: 6}[access]

	if !made && !p.permits(&Stat{Mode: q.mode, Uid: q.uid, Gid: q.gid}, want) {
		return p.result(0, EACCES)
	}

	return p.result(p.install(newMqEnd(q, access, flags&O_NONBLOCK != 0), 0, true))
}

func (p *Process) mqRequest(fd uint32, r Request) error {
	o, err := p.fd(fd)

	if err != nil {
		return err
	}

	return p.request(o, r)
}

func (p *Process) libcMq_close(fd uint32) uint32 {
	if err := p.mqRequest(fd, &MqAttr{}); err != nil {
		return p.result(0, err)
	}

	p.closeFd(int(fd))

	return p.result(0, nil)
}

func (p *Process) libcMq_unlink(name uint32) uint32 {
	key, err := semaphoreName(p.str(name))

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	q := p.k.queues[key]

	switch {
	case q == nil:
		return p.result(0, ENOENT)
	case !p.cred.privileged() && p.cred.euid() != q.uid:
		return p.result(0, EACCES)
	}

	delete(p.k.queues, key)

	return p.result(0, nil)
}

func (p *Process) mqSend(fd, msg, n, prio, at uint32, timed bool) uint32 {
	if prio >= MQ_PRIO_MAX {
		return p.result(0, EINVAL)
	}

	return p.result(0, p.mqRequest(fd, &MqSend{from: msg, n: n, prio: prio, at: at, timed: timed}))
}

func (p *Process) libcMq_send(fd, msg, n, prio uint32) uint32 {
	return p.mqSend(fd, msg, n, prio, 0, false)
}

func (p *Process) libcMq_timedsend(fd, msg, n, prio, at uint32) uint32 {
	return p.mqSend(fd, msg, n, prio, at, true)
}

func (p *Process) mqReceive(fd, buf, n, prio, at uint32, timed bool) uint32 {
	r := MqReceive{room: int(n), at: at, timed: timed}

	if err := p.mqRequest(fd, &r); err != nil {
		return p.result(0, err)
	}

	copy(p.span(buf, uint32(len(r.message.data))), r.message.data)

	if prio != 0 {
		p.put32(prio, r.message.prio)
	}

	return p.result(len(r.message.data), nil)
}

func (p *Process) libcMq_receive(fd, buf, n, prio uint32) uint32 {
	return p.mqReceive(fd, buf, n, prio, 0, false)
}

func (p *Process) libcMq_timedreceive(fd, buf, n, prio, at uint32) uint32 {
	return p.mqReceive(fd, buf, n, prio, at, true)
}

func (p *Process) putMqAttr(at uint32, r *MqAttr) {
	flags := uint32(0)

	if r.nonblock {
		flags = O_NONBLOCK
	}

	for i, v := range []uint32{flags, uint32(r.maxmsg), uint32(r.msgsize), uint32(r.curmsgs)} {
		p.put32(at+4*uint32(i), v)
	}
}

func (p *Process) libcMq_getattr(fd, attr uint32) uint32 {
	r := MqAttr{}

	if err := p.mqRequest(fd, &r); err != nil {
		return p.result(0, err)
	}

	p.putMqAttr(attr, &r)

	return p.result(0, nil)
}

func (p *Process) libcMq_setattr(fd, attr, old uint32) uint32 {
	r := MqAttr{set: true, want: p.u32(attr)&O_NONBLOCK != 0}

	if err := p.mqRequest(fd, &r); err != nil {
		return p.result(0, err)
	}

	if old != 0 {
		p.putMqAttr(old, &r)
	}

	return p.result(0, nil)
}

func (p *Process) libcMq_notify(fd, event uint32) uint32 {
	r := MqNotify{set: event != 0}

	if r.set {
		e, err := p.sigEvent(event)

		if err != nil {
			return p.result(0, err)
		}

		r.event = e
	}

	return p.result(0, p.mqRequest(fd, &r))
}

func (p *Process) libcFtok(path, id uint32) uint32 {
	var st Stat

	if err := p.statPath(p.str(path), true, &st); err != nil {
		return p.result(0, err)
	}

	return id&0xff<<24 | uint32(st.Dev&0xff)<<16 | uint32(st.Ino&0xffff)
}

func ipcGet[T any](p *Process, t *IpcTable[T], key int32, flags uint32, perm func(T) IpcPerm, fits func(T) error, made func(IpcPerm) (T, error)) (int32, error) {
	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	if id, ok := t.keys[key]; ok && key != IPC_PRIVATE {
		o := t.objects[id]
		m := perm(o)

		if flags&(IPC_CREAT|IPC_EXCL) == IPC_CREAT|IPC_EXCL {
			return 0, EEXIST
		}

		if !m.grants(&p.cred, (flags>>6|flags>>3|flags)&7) {
			return 0, EACCES
		}

		return id, fits(o)
	}

	if key != IPC_PRIVATE && flags&IPC_CREAT == 0 {
		return 0, ENOENT
	}

	if len(t.objects) >= ipcMax {
		return 0, ENOSPC
	}

	o, err := made(IpcPerm{key: key, uid: p.cred.euid(), gid: p.cred.egid(), cuid: p.cred.euid(), cgid: p.cred.egid(), mode: flags & 0o777})

	if err != nil {
		return 0, err
	}

	return t.add(key, o), nil
}

func (p *Process) putIpcPerm(at uint32, m IpcPerm) {
	for i, v := range []uint32{uint32(m.key), m.uid, m.gid, m.cuid, m.cgid, m.mode, uint32(m.seq)} {
		p.put32(at+4*uint32(i), v)
	}
}

func (p *Process) libcMsgget(key, flags uint32) uint32 {
	id, err := ipcGet(p, &p.k.msgs, int32(key), flags, (*MsgQueue).permission, func(*MsgQueue) error { return nil },
		func(m IpcPerm) (*MsgQueue, error) { return newMsgQueue(m), nil })

	return p.result(int(id), err)
}

func (p *Process) msgQueue(id uint32) (*MsgQueue, error) {
	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	q, ok := p.k.msgs.objects[int32(id)]

	if !ok {
		return nil, EINVAL
	}

	return q, nil
}

func (p *Process) libcMsgsnd(id, msg, n, flags uint32) uint32 {
	q, err := p.msgQueue(id)

	switch {
	case err != nil:
		return p.result(0, err)
	case n > MSGMAX || int32(p.u32(msg)) < 1:
		return p.result(0, EINVAL)
	}

	m := SysvMessage{mtype: int32(p.u32(msg)), text: bytes.Clone(p.span(msg+4, n))}

	return p.result(0, q.send(p, m, flags&IPC_NOWAIT != 0))
}

func (p *Process) libcMsgrcv(id, msg, n, mtype, flags uint32) uint32 {
	q, err := p.msgQueue(id)

	if err != nil {
		return p.result(0, err)
	}

	if int32(n) < 0 {
		return p.result(0, EINVAL)
	}

	m, err := q.receive(p, int(n), int32(mtype), flags)

	if err != nil {
		return p.result(0, err)
	}

	p.put32(msg, uint32(m.mtype))
	copy(p.span(msg+4, uint32(len(m.text))), m.text)

	return p.result(len(m.text), nil)
}

func (p *Process) libcMsgctl(id, cmd, buf uint32) uint32 {
	q, err := p.msgQueue(id)

	if err != nil {
		return p.result(0, err)
	}

	switch cmd {
	case IPC_STAT:
		q.mu.Lock()

		if !q.perm.grants(&p.cred, 4) {
			q.mu.Unlock()

			return p.result(0, EACCES)
		}

		m, stime, rtime, ctime, qnum, qbytes, lspid, lrpid, bytes := q.perm, q.stime, q.rtime, q.ctime, len(q.messages), q.qbytes, q.lspid, q.lrpid, q.bytes

		q.mu.Unlock()
		p.putIpcPerm(buf, m)

		for i, v := range []int64{stime, rtime, ctime} {
			p.put64(buf+32+8*uint32(i), uint64(v))
		}

		for i, v := range []uint32{uint32(qnum), uint32(qbytes), uint32(lspid), uint32(lrpid), uint32(bytes)} {
			p.put32(buf+56+4*uint32(i), v)
		}

		return p.result(0, nil)
	case IPC_SET:
		uid, gid, mode, qbytes := p.u32(buf+4), p.u32(buf+8), p.u32(buf+20), int(p.u32(buf+60))

		q.mu.Lock()

		defer q.mu.Unlock()

		switch {
		case !q.perm.owned(&p.cred):
			return p.result(0, EPERM)
		case qbytes > q.qbytes && qbytes > MSGMNB && !p.cred.privileged():
			return p.result(0, EPERM)
		}

		q.perm.uid, q.perm.gid, q.perm.mode, q.qbytes, q.ctime = uid, gid, mode&0o777, qbytes, time.Now().Unix()

		return p.result(0, nil)
	case IPC_RMID:
		m := q.permission()

		if !m.owned(&p.cred) {
			return p.result(0, EPERM)
		}

		p.k.mu.Lock()
		p.k.msgs.remove(int32(id), m.key)
		p.k.mu.Unlock()
		q.remove()

		return p.result(0, nil)
	}

	return p.result(0, EINVAL)
}

func (p *Process) libcSemget(key, n, flags uint32) uint32 {
	count := int(int32(n))

	if count < 0 || count > SEMMSL {
		return p.result(0, EINVAL)
	}

	id, err := ipcGet(p, &p.k.sems, int32(key), flags, (*SemSet).permission, func(s *SemSet) error {
		if count > len(s.sems) {
			return EINVAL
		}

		return nil
	}, func(m IpcPerm) (*SemSet, error) {
		if count == 0 {
			return nil, EINVAL
		}

		return newSemSet(m, count), nil
	})

	return p.result(int(id), err)
}

func (p *Process) semSet(id uint32) (*SemSet, error) {
	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	s, ok := p.k.sems.objects[int32(id)]

	if !ok {
		return nil, EINVAL
	}

	return s, nil
}

func (p *Process) semop(id, at, n uint32, end time.Time) uint32 {
	s, err := p.semSet(id)

	switch {
	case err != nil:
		return p.result(0, err)
	case n == 0:
		return p.result(0, EINVAL)
	case n > SEMOPM:
		return p.result(0, E2BIG)
	}

	ops := make([]SemOp, n)

	for i := range ops {
		b := at + 6*uint32(i)

		ops[i] = SemOp{num: p.u16(b), op: int16(p.u16(b + 2)), flags: int16(p.u16(b + 4))}

		if int(ops[i].num) >= len(s.sems) {
			return p.result(0, EFBIG)
		}
	}

	return p.result(0, s.operate(p, ops, end))
}

func (p *Process) libcSemop(id, ops, n uint32) uint32 {
	return p.semop(id, ops, n, time.Time{})
}

func (p *Process) libcSemtimedop(id, ops, n, at uint32) uint32 {
	end := time.Time{}

	if at != 0 {
		d, err := p.timespec(at)

		if err != nil {
			return p.result(0, err)
		}

		end = time.Now().Add(d)
	}

	return p.semop(id, ops, n, end)
}

func (p *Process) libcSemctl(id, num, cmd, va uint32) uint32 {
	s, err := p.semSet(id)

	if err != nil {
		return p.result(0, err)
	}

	n, count := int(int32(num)), len(s.sems)

	switch cmd {
	case GETVAL, GETPID, GETNCNT, GETZCNT, SETVAL:
		if n < 0 || n >= count {
			return p.result(0, EINVAL)
		}
	case GETALL, SETALL, IPC_STAT, IPC_SET, IPC_RMID:
	default:
		return p.result(0, EINVAL)
	}

	arg := uint32(0)

	switch cmd {
	case SETVAL, GETALL, SETALL, IPC_STAT, IPC_SET:
		arg = p.u32(p.varArg(va, 0))
	}

	switch cmd {
	case SETVAL:
		return p.result(0, p.semSetValues(s, n, []int32{int32(arg)}))
	case SETALL:
		values := make([]int32, count)

		for i := range values {
			values[i] = int32(p.u16(arg + 2*uint32(i)))
		}

		return p.result(0, p.semSetValues(s, 0, values))
	case IPC_SET:
		uid, gid, mode := p.u32(arg+4), p.u32(arg+8), p.u32(arg+20)

		s.mu.Lock()

		defer s.mu.Unlock()

		if !s.perm.owned(&p.cred) {
			return p.result(0, EPERM)
		}

		s.perm.uid, s.perm.gid, s.perm.mode, s.ctime = uid, gid, mode&0o777, time.Now().Unix()

		return p.result(0, nil)
	case IPC_RMID:
		m := s.permission()

		if !m.owned(&p.cred) {
			return p.result(0, EPERM)
		}

		p.k.mu.Lock()
		p.k.sems.remove(int32(id), m.key)
		p.k.mu.Unlock()
		s.remove()

		return p.result(0, nil)
	}

	s.mu.Lock()

	if !s.perm.grants(&p.cred, 4) {
		s.mu.Unlock()

		return p.result(0, EACCES)
	}

	m, otime, ctime := s.perm, s.otime, s.ctime
	values := slices.Clone(s.sems)

	s.mu.Unlock()

	switch cmd {
	case GETVAL:
		return uint32(values[n].value)
	case GETPID:
		return uint32(values[n].pid)
	case GETNCNT:
		return uint32(values[n].ncnt)
	case GETZCNT:
		return uint32(values[n].zcnt)
	case GETALL:
		for i, v := range values {
			p.put16(arg+2*uint32(i), uint16(v.value))
		}

		return 0
	}

	p.putIpcPerm(arg, m)
	p.put64(arg+32, uint64(otime))
	p.put64(arg+40, uint64(ctime))
	p.put16(arg+48, uint16(count))

	return 0
}

func (p *Process) semSetValues(s *SemSet, from int, values []int32) error {
	for _, v := range values {
		if v < 0 || v > SEMVMX {
			return ERANGE
		}
	}

	s.mu.Lock()

	defer s.mu.Unlock()

	switch {
	case s.removed:
		return EIDRM
	case !s.perm.grants(&p.cred, 2):
		return EACCES
	}

	for i, v := range values {
		s.set(p, from+i, v)
	}

	return nil
}

func (p *Process) sigEvent(at uint32) (SigEvent, error) {
	e := SigEvent{value: p.u32(at), signal: int(p.u32(at + 4)), kind: p.u32(at + 8)}

	switch e.kind {
	case SIGEV_NONE:
	case SIGEV_SIGNAL:
		if !validSignal(uint32(e.signal)) {
			return e, EINVAL
		}
	case SIGEV_THREAD:
		return e, EAGAIN
	default:
		return e, EINVAL
	}

	return e, nil
}

func (p *Process) aioSubmit(cb uint32, kind int, list *AioList) error {
	fd, buf, n, off := p.u32(cb), p.u32(cb+12), p.u32(cb+16), int64(p.u64(cb+40))
	event, err := p.sigEvent(cb + 20)

	if err != nil {
		return err
	}

	o, err := p.fd(fd)

	if err != nil {
		return err
	}

	fl, err := p.fileFlags(o)

	switch {
	case err != nil:
		fl = O_RDWR
	case fl&O_PATH != 0:
		return EBADF
	}

	switch {
	case kind == aioRead && fl&O_ACCMODE == O_WRONLY, kind == aioWrite && fl&O_ACCMODE == O_RDONLY:
		return EBADF
	case (kind == aioRead || kind == aioWrite) && (off < 0 || int32(n) < 0):
		return EINVAL
	case len(p.aios) >= AIO_MAX:
		return EAGAIN
	}

	op := &AioOp{cb: cb, fd: fd, kind: kind, file: o, stream: kind == aioWrite && fl&O_APPEND != 0, offset: off, n: int(n), buf: buf, event: event,
		list: list, stop: make(chan struct{}), over: make(chan struct{})}

	switch kind {
	case aioRead:
		p.span(buf, n)
	case aioWrite:
		op.data = bytes.Clone(p.span(buf, n))
	default:
		for _, b := range p.aios {
			if b.file == o && !b.finished.Load() {
				op.before = append(op.before, b)
			}
		}
	}

	op.done = Event{kind: eventAio, run: func(q *Process) { q.aioTake(op) }}
	op.halt = Event{kind: eventAio, run: (*Process).aioHalt}

	if p.aios == nil {
		p.aios = map[uint32]*AioOp{}
	}

	if list != nil {
		list.left++
	}

	o.ref()
	p.aios[cb] = op
	op.agent = p.k.process(p.pid, p)

	go op.run(p)

	return nil
}

func (p *Process) aioHalt() {
	p.interruption = signalFinal

	if r := p.asking; r != nil && !p.cancelled {
		p.cancelled = true
		r.cancel(p)
	}
}

func (p *Process) aioFailed(cb uint32, err error) {
	op := &AioOp{cb: cb, result: -1, err: errnoOf(err), taken: true}

	op.finished.Store(true)

	if p.aios == nil {
		p.aios = map[uint32]*AioOp{}
	}

	p.aios[cb] = op
}

func (p *Process) aioNotify(e SigEvent) {
	if e.kind == SIGEV_SIGNAL {
		p.sendSelfInfo(e.signal, SigInfo{code: SI_ASYNCIO, pid: p.pid, uid: p.cred.uid(), value: e.value})
	}
}

func (p *Process) aioTake(op *AioOp) {
	if op.taken || !op.finished.Load() || p.aios[op.cb] != op {
		return
	}

	op.taken = true

	if op.kind == aioRead && len(op.data) > 0 {
		copy(p.span(op.buf, uint32(len(op.data))), op.data)
	}

	op.data = nil
	p.aioNotify(op.event)

	if l := op.list; l != nil {
		if l.left--; l.left == 0 {
			p.aioNotify(l.event)
		}
	}
}

func (p *Process) aioDrop() {
	for _, op := range p.aios {
		if !op.finished.Load() {
			op.abandon()
		}
	}

	p.aios = nil
}

func (p *Process) libcAio_read(cb uint32) uint32 {
	return p.result(0, p.aioSubmit(cb, aioRead, nil))
}

func (p *Process) libcAio_write(cb uint32) uint32 {
	return p.result(0, p.aioSubmit(cb, aioWrite, nil))
}

func (p *Process) libcAio_fsync(op, cb uint32) uint32 {
	switch op {
	case O_SYNC:
		return p.result(0, p.aioSubmit(cb, aioSync, nil))
	case O_DSYNC:
		return p.result(0, p.aioSubmit(cb, aioDataSync, nil))
	}

	return p.result(0, EINVAL)
}

func (p *Process) libcAio_error(cb uint32) uint32 {
	op := p.aios[cb]

	if op == nil {
		return p.result(0, EINVAL)
	}

	p.aioTake(op)

	if !op.taken {
		return uint32(EINPROGRESS)
	}

	return uint32(op.err)
}

func (p *Process) libcAio_return(cb uint32) uint32 {
	op := p.aios[cb]

	if op == nil {
		return p.result(0, EINVAL)
	}

	p.aioTake(op)

	if !op.taken {
		return p.result(0, EINVAL)
	}

	delete(p.aios, cb)

	return uint32(op.result)
}

func (p *Process) aioListed(list, n uint32) []*AioOp {
	var ops []*AioOp

	for i := uint32(0); int32(n) > 0 && i < n; i++ {
		if op := p.aios[p.u32(list+4*i)]; op != nil {
			ops = append(ops, op)
		}
	}

	return ops
}

func (p *Process) libcAio_suspend(list, n, timeout uint32) uint32 {
	ops := p.aioListed(list, n)
	end := time.Time{}

	if timeout != 0 {
		d, err := p.timespec(timeout)

		if err != nil {
			return p.result(0, err)
		}

		end = time.Now().Add(d)
	}

	timedOut, err := p.wait(end, func(*Event) (bool, error) {
		for _, op := range ops {
			if p.aioTake(op); op.taken {
				return true, nil
			}
		}

		return p.interrupts(false), EINTR
	})

	switch {
	case err != nil:
		return p.result(0, err)
	case timedOut:
		return p.result(0, EAGAIN)
	}

	return 0
}

func (p *Process) libcLio_listio(mode, list, n, sig uint32) uint32 {
	if mode != LIO_WAIT && mode != LIO_NOWAIT || int32(n) < 0 || n > AIO_LISTIO_MAX {
		return p.result(0, EINVAL)
	}

	l := &AioList{event: SigEvent{kind: SIGEV_NONE}}

	if mode == LIO_NOWAIT && sig != 0 {
		e, err := p.sigEvent(sig)

		if err != nil {
			return p.result(0, err)
		}

		l.event = e
	}

	var ops []*AioOp

	failed := false

	for i := range n {
		cb := p.u32(list + 4*i)

		if cb == 0 {
			continue
		}

		kind := aioRead

		switch p.u32(cb + 4) {
		case LIO_NOP:
			continue
		case LIO_READ:
		case LIO_WRITE:
			kind = aioWrite
		default:
			p.aioFailed(cb, EINVAL)
			failed = true

			continue
		}

		if err := p.aioSubmit(cb, kind, l); err != nil {
			p.aioFailed(cb, err)
			failed = true

			continue
		}

		ops = append(ops, p.aios[cb])
	}

	if mode == LIO_NOWAIT {
		if l.left == 0 {
			p.aioNotify(l.event)
		}

		if failed {
			return p.result(0, EIO)
		}

		return 0
	}

	_, err := p.wait(time.Time{}, func(*Event) (bool, error) {
		all := true

		for _, op := range ops {
			p.aioTake(op)
			all = all && op.taken
		}

		if all {
			return true, nil
		}

		return p.interrupts(false), EINTR
	})

	if err != nil {
		return p.result(0, err)
	}

	for _, op := range ops {
		failed = failed || op.err != 0
	}

	if failed {
		return p.result(0, EIO)
	}

	return 0
}

func (p *Process) libcAio_cancel(fd, cb uint32) uint32 {
	if _, err := p.fd(fd); err != nil {
		return p.result(0, err)
	}

	var pending []*AioOp

	for key, op := range p.aios {
		if cb != 0 && key != cb || op.fd != fd {
			continue
		}

		if p.aioTake(op); !op.taken {
			pending = append(pending, op)
		}
	}

	if len(pending) == 0 {
		return AIO_ALLDONE
	}

	for _, op := range pending {
		op.abandon()
	}

	p.wait(time.Time{}, func(*Event) (bool, error) {
		for _, op := range pending {
			if p.aioTake(op); !op.taken {
				return false, nil
			}
		}

		return true, nil
	})

	for _, op := range pending {
		if op.err != ECANCELED {
			return AIO_NOTCANCELED
		}
	}

	return AIO_CANCELED
}

func (p *Process) libcGetrandom(buf, n, flags uint32) uint32 {
	if flags&^(GRND_NONBLOCK|GRND_RANDOM|GRND_INSECURE) != 0 || flags&(GRND_RANDOM|GRND_INSECURE) == GRND_RANDOM|GRND_INSECURE {
		return p.result(0, EINVAL)
	}

	rand.Read(p.span(buf, n))

	return n
}

func (p *Process) libcPosix_madvise(addr, n, advice uint32) uint32 {
	switch {
	case advice > POSIX_MADV_DONTNEED:
		return uint32(EINVAL)
	case uint64(addr)+uint64(n) > p.inst.memory.pages*pageSize:
		return uint32(ENOMEM)
	}

	return 0
}

func (p *Process) libcExecl(path, arg0, va uint32) uint32 {
	return p.result(0, p.exec(p.str(path), p.argList(arg0, va), p.environment()))
}

func (p *Process) libc_exit(code uint32) {
	p.exitWith(int(code&0xff) << 8)
}

func (p *Process) libc_Exit(code uint32) {
	p.exitWith(int(code&0xff) << 8)
}

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
		if q.pgid.Load() == pgid && q.sid.Load() == p.sid.Load() {
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
			return p.result(0, ESRCH)
		}

		target = q
	}

	group := int32(pgid)

	if group == 0 {
		group = target.pid
	}

	if group < 0 || group != target.pid && !p.groupExists(group) {
		return p.result(0, EPERM)
	}

	p.k.mu.Lock()
	target.pgid.Store(group)
	p.k.mu.Unlock()

	return p.result(0, nil)
}

func (p *Process) setIds(ids *[3]uint32, r, e, s uint32) error {
	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	if !p.cred.privileged() && !(may(r, ids[0], ids[1], ids[2]) && may(e, ids[0], ids[1], ids[2]) && may(s, ids[0], ids[1], ids[2])) {
		return EPERM
	}

	for i, id := range [3]uint32{r, e, s} {
		if id != keep {
			ids[i] = id
		}
	}

	return nil
}

func (p *Process) setId(ids *[3]uint32, id uint32) error {
	if p.cred.privileged() {
		return p.setIds(ids, id, id, id)
	}

	if id != ids[0] && id != ids[2] {
		return EPERM
	}

	return p.setIds(ids, keep, id, keep)
}

func (p *Process) setReIds(ids *[3]uint32, r, e uint32) error {
	if !p.cred.privileged() && (r != keep && r != ids[0] && r != ids[1] || !may(e, ids[0], ids[1], ids[2])) {
		return EPERM
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

func (p *Process) process(pid int32) (*Process, error) {
	if pid == 0 || pid == p.pid {
		return p, nil
	}

	p.k.mu.Lock()

	q, ok := p.k.procs[pid]

	p.k.mu.Unlock()

	if !ok || q.zombie {
		return nil, ESRCH
	}

	return q, nil
}

func (p *Process) prioTargets(which, who uint32) ([]*Process, error) {
	switch which {
	case PRIO_PROCESS:
		q, err := p.process(int32(who))

		if err != nil {
			return nil, err
		}

		return []*Process{q}, nil
	case PRIO_PGRP:
		pgid := int32(who)

		if pgid == 0 {
			pgid = p.pgid.Load()
		}

		return p.k.processes(func(q *Process) bool { return q.pgid.Load() == pgid && !q.zombie }), nil
	case PRIO_USER:
		if who == 0 {
			who = p.cred.uid()
		}

		return p.k.processes(func(q *Process) bool { return !q.zombie && q.cred.uid() == who }), nil
	}

	return nil, EINVAL
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
		if q.pgid.Load() == p.pid {
			return p.result(0, EPERM)
		}
	}

	p.sid.Store(p.pid)
	p.pgid.Store(p.pid)

	return p.result(int(p.pid), nil)
}

func (p *Process) libcGetsid(pid uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	return p.result(int(q.sid.Load()), nil)
}

func (p *Process) libcGetpgid(pid uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	return p.result(int(q.pgid.Load()), nil)
}

func (p *Process) libcSetpgrp() uint32 {
	return p.libcSetpgid(0, 0)
}

func (p *Process) libcNice(inc uint32) uint32 {
	p.k.mu.Lock()

	n := min(max(p.nice+int32(inc), niceMin), niceMax)

	if n < p.nice && !p.cred.privileged() {
		p.k.mu.Unlock()

		return p.result(0, EPERM)
	}

	p.nice = n
	p.k.mu.Unlock()

	return p.result(int(n), nil)
}

func (p *Process) libcGetpriority(which, who uint32) uint32 {
	targets, err := p.prioTargets(which, who)

	if err == nil && len(targets) == 0 {
		err = ESRCH
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
		err = ESRCH
	}

	if err != nil {
		return p.result(0, err)
	}

	n := min(max(int32(prio), niceMin), niceMax)

	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	for _, q := range targets {
		if n < q.nice && !p.cred.privileged() {
			return p.result(0, EACCES)
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
	case SCHED_FIFO, SCHED_RR:
		return p.result(99, nil)
	case SCHED_OTHER, SCHED_BATCH, SCHED_IDLE:
		return p.result(0, nil)
	}

	return p.result(0, EINVAL)
}

func (p *Process) libcSched_get_priority_min(policy uint32) uint32 {
	switch policy {
	case SCHED_FIFO, SCHED_RR:
		return p.result(1, nil)
	case SCHED_OTHER, SCHED_BATCH, SCHED_IDLE:
		return p.result(0, nil)
	}

	return p.result(0, EINVAL)
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

func (p *Process) libcSched_setparam(pid, param uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return p.result(0, err)
	}

	if p.u32(param) != 0 {
		return p.result(0, EINVAL)
	}

	return p.result(0, nil)
}

func (p *Process) libcSched_setscheduler(pid, policy, param uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	return p.result(0, p.scheduler(q, policy, param))
}

func (p *Process) scheduler(q *Process, policy, param uint32) error {
	switch policy {
	case SCHED_OTHER, SCHED_BATCH, SCHED_IDLE:
		if p.u32(param) != 0 {
			return EINVAL
		}

		p.k.mu.Lock()
		q.policy = int32(policy)
		p.k.mu.Unlock()

		return nil
	case SCHED_FIFO, SCHED_RR:
		return EPERM
	}

	return EINVAL
}

func (p *Process) libcSched_rr_get_interval(pid, ts uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return p.result(0, err)
	}

	p.putTimespec(ts, 100*time.Millisecond)

	return p.result(0, nil)
}

func (p *Process) libcSetgroups(n, list uint32) uint32 {
	if n > ngroupsMax {
		return p.result(0, EINVAL)
	}

	groups := make([]uint32, n)

	for i := range groups {
		groups[i] = p.u32(list + uint32(4*i))
	}

	return p.result(0, p.setGroups(groups))
}

func (p *Process) setGroups(groups []uint32) error {
	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	if !p.cred.privileged() {
		return EPERM
	}

	p.cred.groups = groups

	return nil
}

type VforkFrame struct {
	parent *Process
	jump   Jump
	stack  uint32
}

func (v *VforkFrame) resume(pid int32) {
	p := v.parent

	p.inst.attach(p)
	p.raiseInterrupt()
	binary.LittleEndian.PutUint32(p.inst.block[p.stackPointer():], v.stack)
	p.inst.longjmp(&v.jump, uint32(pid))
	p.crash("vfork: the function that called vfork has returned", SIGABRT)
}

func (p *Process) startVfork() uint32 {
	v := &VforkFrame{parent: p}

	if !p.inst.setjmp(&v.jump) {
		p.crash("vfork: called through a pointer", SIGABRT)
	}

	v.stack = binary.LittleEndian.Uint32(p.inst.block[p.stackPointer():])

	c, err := p.k.newProcess(p)

	if err != nil {
		return p.result(0, err)
	}

	c.setInstance(p.inst)
	c.image, c.vfork = p.image, v
	c.heap, c.stack, c.static, c.env, c.errnoAt, c.environAt, c.siginfo = p.heap, p.stack, p.static, p.env, p.errnoAt, p.environAt, p.siginfo
	c.constants, c.jumps, c.stackAt = p.constants, p.jumps, p.stackAt
	c.iconvs, c.nextIconv = p.iconvs, p.nextIconv
	c.spawnFiles, c.nextSpawnFiles = p.spawnFiles, p.nextSpawnFiles
	c.syncs, c.nextSync, c.semOpens, c.keys, c.thread = p.syncs, p.nextSync, p.semOpens, p.keys, p.thread
	c.defaultStack, c.defaultGuard = p.defaultStack, p.defaultGuard
	c.envStrings, c.mappings, c.tzAt, c.zone = p.envStrings, p.mappings, p.tzAt, p.zone
	c.nameAt = p.nameAt
	c.token.Store(p.token.Load())
	c.inherit(p)
	p.inst.attach(c)
	c.trace(TRACE_PROCESS, fmt.Sprintf("child of %d", p.pid))

	return c.result(0, nil)
}

func (p *Process) fork() (int32, error) {
	if p.chaosFail("fork") {
		return 0, EAGAIN
	}

	j := new(Jump)

	if !p.inst.setjmp(j) {
		p.crash("fork: called through a pointer", SIGABRT)
	}

	frames, ok := p.inst.frames(j, "_start")

	if !ok {
		return 0, ENOSYS
	}

	from := p.stack.live(binary.LittleEndian.Uint32(p.inst.block[p.stackPointer():]))
	to := p.inst.memory.pages * pageSize

	if p.heap != nil {
		to = uint64(p.heap.top)
	}

	inst, err := p.inst.clone(p, runStack, from, to)

	if err != nil {
		return 0, err
	}

	c, err := p.k.newProcess(p)

	if err != nil {
		inst.release()

		return 0, err
	}

	inst.attach(c)
	c.image = p.image
	c.heap, c.stack, c.static, c.env, c.errnoAt, c.environAt, c.siginfo = p.heap.clone(), p.stack.clone(), p.static, p.env, p.errnoAt, p.environAt, p.siginfo
	c.stackAt, c.tzAt, c.zone = p.stackAt, p.tzAt, p.zone
	c.nameAt = p.nameAt
	c.constants, c.jumps = map[string]uint32{}, map[uint32]*JmpBuf{}
	c.iconvs, c.nextIconv = map[uint32]*Iconv{}, p.nextIconv
	c.spawnFiles, c.nextSpawnFiles = map[uint32][]SpawnAction{}, p.nextSpawnFiles

	for id, cd := range p.iconvs {
		c.iconvs[id] = cd.copy()
	}

	for id, list := range p.spawnFiles {
		c.spawnFiles[id] = slices.Clone(list)
	}

	for s, at := range p.constants {
		c.constants[s] = at
	}

	c.syncs, c.nextSync, c.semOpens, c.keys, c.thread = map[int32]Object{}, p.nextSync, map[string]*SemOpen{}, p.keys, p.thread
	c.defaultStack, c.defaultGuard = p.defaultStack, p.defaultGuard

	for h, o := range p.syncs {
		r := SyncCopy{}

		if p.request(o, &r) == nil && r.copy != nil {
			c.syncs[h] = r.copy
		}
	}

	for name, o := range p.semOpens {
		copied := *o

		c.semOpens[name] = &copied
	}

	c.envStrings, c.mappings = maps.Clone(p.envStrings), map[uint32]Mapping{}

	for at, m := range p.mappings {
		if m.file != nil {
			m.file.ref()
			m.read = bytes.Clone(m.read)
		}

		c.mappings[at] = m
	}

	for buf, o := range p.jumps {
		if o.sp() <= j.sp() {
			copied := *o

			c.jumps[buf] = &copied
		}
	}

	c.inherit(p)

	parent := p.pid

	go c.runFrom(p.image, inst, func() {
		c.trace(TRACE_PROCESS, fmt.Sprintf("child of %d", parent))
		inst.resume(frames)
	})

	return c.pid, nil
}

func (c *Process) inherit(p *Process) {
	c.files = FdTable{slots: slices.Clone(p.files.slots), used: p.files.used.clone()}

	for _, d := range c.files.slots {
		if d.obj != nil {
			d.obj.ref()
		}
	}

	p.sig.mu.Lock()
	c.sig.actions, c.sig.blocked, c.sig.altstack = p.sig.actions, p.sig.blocked, p.sig.altstack
	p.sig.mu.Unlock()
}

func (p *Process) execImage(img *ExecImage) {
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

func (p *Process) endBorrowers(i *Instance, e any) {
	for q := i.host.(*Process); q != p; {
		v := q.vfork

		q.vfork = nil

		status := SIGKILL

		if t, ok := e.(*Trap); ok {
			status = trapSignal(t.code)
		}

		q.terminate(status)
		q = v.parent
	}

	i.attach(p)
}

func (p *Process) pidSelector(pid int32) func(q *Process) bool {
	switch {
	case pid > 0:
		return func(q *Process) bool { return q.pid == pid }
	case pid == 0:
		pgid := p.pgid.Load()

		return func(q *Process) bool { return q.pgid.Load() == pgid }
	case pid == -1:
		return func(*Process) bool { return true }
	}

	return func(q *Process) bool { return q.pgid.Load() == -pid }
}

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

		if q.zombie && options&WEXITED != 0 {
			if options&WNOWAIT == 0 {
				delete(k.procs, q.pid)
			}

			return q.pid, q.status, true
		}

		if r := q.report; r&0xff == stoppedBy && options&WUNTRACED != 0 || r == continued && options&WCONTINUED != 0 {
			if options&WNOWAIT == 0 {
				q.report = 0
			}

			return q.pid, r, true
		}
	}

	return 0, 0, found
}

func (p *Process) waitChild(selects func(*Process) bool, options uint32) (int32, int, error) {
	for {
		seen := p.childEvents.Load()
		q, status, found := p.reap(selects, options)

		if q != 0 {
			return q, status, nil
		}

		if !found {
			return 0, 0, ECHILD
		}

		if options&WNOHANG != 0 {
			return 0, 0, nil
		}

		if err := p.waitEvent(func() bool { return p.childEvents.Load() != seen }); err != nil {
			return 0, 0, err
		}
	}
}

func (p *Process) waitpid(pid int32, at, options uint32) (int, error) {
	const (
		clone     = uint32(1 << 31)
		all       = uint32(1 << 30)
		notThread = uint32(1 << 29)
		basic     = WNOHANG | WUNTRACED | WCONTINUED
	)

	if options&^(basic|clone|all|notThread) != 0 {
		return 0, EINVAL
	}

	if options&clone != 0 && options&all == 0 {
		return 0, ECHILD
	}

	q, status, err := p.waitChild(p.pidSelector(pid), options&basic|WEXITED)

	if err == nil && q != 0 && at != 0 {
		p.put32(at, uint32(status))
	}

	return int(q), err
}

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
	case idtype == P_ALL:
		selects = func(*Process) bool { return true }
	case idtype == P_PID && int32(id) > 0:
		selects = p.pidSelector(int32(id))
	case idtype == P_PGID && int32(id) >= 0:
		pgid := int32(id)

		if pgid == 0 {
			pgid = p.pgid.Load()
		}

		selects = func(q *Process) bool { return q.pgid.Load() == pgid }
	default:
		return p.result(0, EINVAL)
	}

	if options&(WEXITED|WUNTRACED|WCONTINUED) == 0 || options&^(WNOHANG|WEXITED|WUNTRACED|WCONTINUED|WNOWAIT) != 0 {
		return p.result(0, EINVAL)
	}

	q, status, err := p.waitChild(selects, options)

	if err != nil {
		return p.result(0, err)
	}

	if info != 0 {
		clear(p.span(info, sizeofSiginfo))

		if q != 0 {
			code, value := CLD_EXITED, status>>8&0xff

			switch {
			case status&0xff == stoppedBy:
				code = CLD_STOPPED
			case status == continued:
				code, value = CLD_CONTINUED, SIGCONT
			case status&0x7f != 0:
				code, value = CLD_KILLED, status&0x7f
			}

			p.put32(info, uint32(SIGCHLD))
			p.put32(info+8, uint32(code))
			p.put32(info+12, uint32(q))

			p.put32(info+16, p.cred.uid())
			p.put32(info+20, uint32(value))
		}
	}

	return p.result(0, nil)
}

func (p *Process) stopped(sig int) {
	p.k.mu.Lock()
	p.halted, p.report = true, sig<<8|stoppedBy
	p.k.mu.Unlock()
	p.tellParent(SA_NOCLDSTOP)

	p.wait(time.Time{}, func(*Event) (bool, error) {
		p.k.mu.Lock()

		halted := p.halted

		p.k.mu.Unlock()
		p.sig.mu.Lock()

		killed := p.sig.pending&sigBit(SIGKILL) != 0

		p.sig.mu.Unlock()

		return !halted || killed, nil
	})
}

func (p *Process) cont() {
	p.k.mu.Lock()

	was := p.halted

	if was {
		p.halted, p.report = false, continued
	}

	p.k.mu.Unlock()

	if was {
		p.post(&p.own.continued)
		p.tellParent(SA_NOCLDSTOP)
	}
}

func (p *Process) tellParent(flag uint32) {
	p.k.mu.Lock()

	parent := p.k.procs[p.ppid]

	p.k.mu.Unlock()

	if parent == nil {
		return
	}

	parent.sig.mu.Lock()

	quiet := parent.sig.actions[SIGCHLD].flags&flag != 0

	parent.sig.mu.Unlock()

	if quiet {
		parent.childChanged(0)
	} else {
		parent.childChanged(SIGCHLD)
	}
}

func (p *Process) waitEvent(ready func() bool) error {
	_, err := p.wait(time.Time{}, func(*Event) (bool, error) {
		if ready() {
			return true, nil
		}

		return p.interrupts(true), EINTR
	})

	return err
}

func (p *Process) libc_Fork() uint32 {
	pid, err := p.fork()

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

func defaultAction(sig int) int {
	switch sig {
	case SIGCHLD, SIGURG, SIGWINCH:
		return actIgnore
	case SIGCONT:
		return actContinue
	case SIGSTOP, SIGTSTP, SIGTTIN, SIGTTOU:
		return actStop
	}

	return actTerminate
}

func sigBit(sig int) uint64 {
	return 1 << (sig - 1)
}

func (s *SignalState) sent(sig int) {
	switch {
	case sig == SIGCONT:
		s.drop(stopSignals)
	case sigBit(sig)&stopSignals != 0:
		s.drop(sigBit(SIGCONT))
	}
}

func (s *SignalState) post(sig int, info SigInfo) bool {
	switch {
	case sig >= SIGRTMIN && len(s.infos[sig]) >= sigQueueMax:
		return false
	case sig >= SIGRTMIN || s.pending&sigBit(sig) == 0:
		s.infos[sig] = append(s.infos[sig], info)
	}

	s.pending |= sigBit(sig)

	return true
}

func (s *SignalState) taken(sig int) SigInfo {
	var info SigInfo

	if q := s.infos[sig]; len(q) > 0 {
		info, s.infos[sig] = q[0], q[1:]
	}

	if len(s.infos[sig]) == 0 {
		s.pending &^= sigBit(sig)
		s.infos[sig] = nil
	}

	return info
}

func (s *SignalState) drop(mask uint64) {
	for sig := 1; sig < NSIG; sig++ {
		if mask&sigBit(sig) != 0 {
			s.infos[sig] = nil
		}
	}

	s.pending &^= mask
}

type SigAction struct {
	handler uint32
	flags   uint32
	mask    uint64
}

type SignalState struct {
	mu      sync.Mutex
	actions [NSIG]SigAction
	blocked uint64
	pending uint64
	infos   [NSIG][]SigInfo

	dead bool

	altstack struct{ sp, size uint32 }
}

func (s *SignalState) deliverable() uint64 {
	s.mu.Lock()

	defer s.mu.Unlock()

	return s.pending &^ s.blocked
}

func (a SigAction) ignored(sig int) bool {
	return a.handler == SIG_IGN || a.handler == SIG_DFL && defaultAction(sig) == actIgnore
}

func (p *Process) send(sig int) {
	p.sendInfo(sig, SigInfo{})
}

func (p *Process) sendInfo(sig int, info SigInfo) bool {
	if sig == SIGCONT {
		p.cont()
	}

	p.sig.mu.Lock()
	p.sig.sent(sig)

	act := p.sig.actions[sig]
	blocked := p.sig.blocked&sigBit(sig) != 0

	if act.ignored(sig) && !blocked {
		p.sig.mu.Unlock()

		return true
	}

	if p == p.k.first && !blocked && act.handler == SIG_DFL && defaultAction(sig) == actTerminate {
		restoreHostTerminal()
		os.Exit(128 + sig)
	}

	if !p.sig.post(sig, info) {
		p.sig.mu.Unlock()

		return false
	}

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

	return true
}

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

			if token == 0 || halted || !p.interruptDue() || !p.chasing.CompareAndSwap(false, true) {
				return
			}
		}
	}()
}

func (p *Process) interruptDue() bool {
	return p.events.count.Load() != 0 || p.sig.deliverable() != 0
}

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

func (p *Process) setInstance(i *Instance) {
	p.sig.mu.Lock()
	p.inst = i
	p.sig.mu.Unlock()
}

func (p *Process) setInterrupt(v uint32) {
	i := p.inst

	if i == nil || p.sig.dead {
		return
	}

	if m := i.memory; m != nil {
		m.enter()

		defer m.leave()
	}

	atomic.StoreUint32((*uint32)(unsafe.Pointer(&i.block[offInterrupt])), v)
}

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

func (p *Process) sendSelf(sig int) {
	p.sendSelfInfo(sig, SigInfo{pid: int32(p.pid), uid: uint32(p.cred.uid())})
}

func (p *Process) sendSelfInfo(sig int, info SigInfo) bool {
	p.sig.mu.Lock()
	p.sig.sent(sig)

	act := p.sig.actions[sig]
	posted := true

	if !act.ignored(sig) || p.sig.blocked&sigBit(sig) != 0 {
		posted = p.sig.post(sig, info)
	}

	p.sig.mu.Unlock()
	p.signalled()

	return posted
}

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

func (p *Process) interrupts(restart bool) bool {
	if p.interruption == signalFinal || p.interruption == signalTaken && !restart {
		p.interruption = signalNone

		return true
	}

	return false
}

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

func (p *Process) ask(r *Answer) (*Answer, bool) {
	outer, was := p.asking, p.cancelled

	p.asking, p.cancelled = r, false

	return outer, was
}

func (p *Process) unask(outer *Answer, was bool) {
	p.asking, p.cancelled = outer, was
}

func (p *Process) take() (int, SigAction, SigInfo) {
	p.sig.mu.Lock()

	defer p.sig.mu.Unlock()

	ready := p.sig.pending &^ p.sig.blocked

	if ready == 0 {
		return 0, SigAction{}, SigInfo{}
	}

	sig := bits.TrailingZeros64(ready) + 1
	info := p.delivered(p.sig.taken(sig))
	act := p.sig.actions[sig]

	if act.handler != SIG_DFL && act.handler != SIG_IGN && act.flags&SA_RESETHAND != 0 {
		p.sig.actions[sig] = SigAction{}
	}

	return sig, act, info
}

func (p *Process) delivered(info SigInfo) SigInfo {
	if info.code != SI_TIMER {
		return info
	}

	if t := p.posixTimers[info.timer]; t != nil {
		info.overrun, t.last, t.overrun = t.overrun, t.overrun, 0
	}

	return info
}

func (p *Process) deliver() {
	p.handleSignals()
}

func (p *Process) interrupted() bool {
	_, restart := p.handleSignals()

	return restart
}

func (p *Process) handleSignals() (taken, restart bool) {
	restart = true

	for {
		sig, act, info := p.take()

		if sig == 0 {
			return taken, restart
		}

		taken = true
		p.trace(TRACE_SIGNALS, "signal "+signalName(sig))

		switch act.handler {
		case SIG_IGN:
		case SIG_DFL:
			switch defaultAction(sig) {
			case actTerminate:
				p.exitWith(sig)
			case actStop:
				p.stop(sig)
			}
		default:
			if act.flags&SA_RESTART == 0 {
				restart = false
			}

			p.runHandler(sig, act, info)
		}
	}
}

func (p *Process) stop(sig int) {
	if p == p.k.first {
		if p.k.console != nil {
			p.k.console.settle()
		}

		runtime.LockOSThread()

		defer runtime.UnlockOSThread()

		throw(syscall.Tgkill(os.Getpid(), syscall.Gettid(), syscall.SIGSTOP))

		return
	}

	p.stopped(sig)
}

func (p *Process) runHandler(sig int, act SigAction, info SigInfo) {
	if p.chaosMoves() && p.inst.memory != nil {
		p.inst.move()
	}

	p.sig.mu.Lock()

	saved := p.sig.blocked

	p.sig.blocked |= act.mask &^ unblockable

	if act.flags&SA_NODEFER == 0 {
		p.sig.blocked |= sigBit(sig)
	}

	alt := p.sig.altstack

	p.sig.mu.Unlock()

	errno := p.errno()
	sp := binary.LittleEndian.Uint32(p.inst.block[p.stackPointer():])
	moved := act.flags&SA_ONSTACK != 0 && alt.size != 0 && sp-alt.sp > alt.size

	if moved {
		binary.LittleEndian.PutUint32(p.inst.block[p.stackPointer():], (alt.sp+alt.size)&^15)
	}

	if act.flags&SA_SIGINFO != 0 {
		if p.siginfo == 0 {
			p.siginfo = p.malloc(sizeofSiginfo)
		}

		p.putSiginfo(p.siginfo, sig, info)

		f, _ := p.inst.export("__call_signal_action")

		f.Interface().(func(uintptr, uint32, uint32, uint32, uint32))(p.inst.handle(), act.handler, uint32(sig), p.siginfo, 0)
	} else {
		f, _ := p.inst.export("__call_signal_handler")

		f.Interface().(func(uintptr, uint32, uint32))(p.inst.handle(), act.handler, uint32(sig))
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
		case SIG_BLOCK:
			mask |= s
		case SIG_UNBLOCK:
			mask &^= s
		case SIG_SETMASK:
			mask = s
		default:
			return EINVAL
		}

		p.setMask(mask)
	}

	return nil
}

func (p *Process) sigwait(set uint64, timeout time.Duration) (int, SigInfo, error) {
	set &^= unblockable
	var end time.Time

	if timeout >= 0 {
		end = time.Now().Add(timeout)
	}

	old := p.setMask(p.mask() | set)

	defer p.setMask(old)

	sig := 0

	var info SigInfo

	timedOut, err := p.wait(end, func(*Event) (bool, error) {
		p.sig.mu.Lock()

		if ready := p.sig.pending & set; ready != 0 {
			sig = bits.TrailingZeros64(ready) + 1
			info = p.delivered(p.sig.taken(sig))
		}

		p.sig.mu.Unlock()

		if sig != 0 {
			return true, nil
		}

		return p.interrupts(false), EINTR
	})

	if timedOut {
		return 0, info, EAGAIN
	}

	return sig, info, err
}

func (p *Process) putSiginfo(at uint32, sig int, info SigInfo) {
	if at == 0 {
		return
	}

	clear(p.span(at, sizeofSiginfo))
	p.put32(at, uint32(sig))
	p.put32(at+8, uint32(info.code))
	p.put32(at+12, uint32(info.pid))
	p.put32(at+16, info.uid)
	p.put32(at+20, uint32(info.status))
	p.put32(at+32, info.value)
	p.put32(at+36, uint32(info.timer))
	p.put32(at+40, uint32(info.overrun))
}

func (p *Process) setMember(sig uint32) bool {
	if !validSignal(sig) || sig == 32 || sig == 33 {
		p.setErrno(EINVAL)

		return false
	}

	return true
}

func (p *Process) setAction(sig int, act SigAction) SigAction {
	p.sig.mu.Lock()

	defer p.sig.mu.Unlock()

	old := p.sig.actions[sig]

	p.sig.actions[sig] = act

	if act.ignored(sig) {
		p.sig.drop(sigBit(sig))
	}

	return old
}

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
	return sig >= 1 && sig < NSIG
}

func (p *Process) kill(pid int32, sig uint32) error {
	return p.killInfo(pid, sig, SigInfo{code: SI_USER})
}

func (p *Process) killInfo(pid int32, sig uint32, info SigInfo) error {
	if sig != 0 && !validSignal(sig) {
		return EINVAL
	}

	targets := p.k.signalTargets(p, pid)

	if len(targets) == 0 {
		return ESRCH
	}

	info.pid, info.uid = int32(p.pid), uint32(p.cred.uid())

	if sig != 0 {
		for _, q := range targets {
			posted := q == p && q.sendSelfInfo(int(sig), info) || q != p && q.sendInfo(int(sig), info)

			if !posted {
				return EAGAIN
			}
		}
	}

	return nil
}

func (p *Process) libcSignal(sig, handler uint32) uint32 {
	if !validSignal(sig) || sig == uint32(SIGKILL) || sig == uint32(SIGSTOP) {
		p.setErrno(EINVAL)

		return ^uint32(0)
	}

	return p.setAction(int(sig), SigAction{handler: handler, flags: SA_RESTART}).handler
}

func (p *Process) libcSigaction(sig, act, old uint32) uint32 {
	if !validSignal(sig) || act != 0 && (sig == uint32(SIGKILL) || sig == uint32(SIGSTOP)) {
		return p.result(0, EINVAL)
	}

	p.sig.mu.Lock()

	prev := p.sig.actions[sig]

	p.sig.mu.Unlock()

	if act != 0 {
		p.setAction(int(sig), SigAction{handler: p.u32(act), mask: p.u64(act + 8), flags: p.u32(act + 16)})
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

func (p *Process) libcSigwait(set, at uint32) uint32 {
	for {
		sig, _, err := p.sigwait(p.u64(set), -1)

		if err == nil {
			p.put32(at, uint32(sig))

			return 0
		}
	}
}

func (p *Process) libcSigwaitinfo(set, info uint32) uint32 {
	sig, si, err := p.sigwait(p.u64(set), -1)

	if err == nil {
		p.putSiginfo(info, sig, si)
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

	sig, si, err := p.sigwait(p.u64(set), d)

	if err == nil {
		p.putSiginfo(info, sig, si)
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
			flags = SS_DISABLE
		case on:
			flags = SS_ONSTACK
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
		return p.result(0, EPERM)
	case flags&^(SS_ONSTACK|SS_DISABLE|1<<31) != 0:
		return p.result(0, EINVAL)
	case flags&SS_DISABLE != 0:
		alt.sp, alt.size = 0, 0
	case p.u32(ss+8) < MINSIGSTKSZ:
		return p.result(0, ENOMEM)
	default:
		alt.sp, alt.size = p.u32(ss), p.u32(ss+8)
		p.span(alt.sp, alt.size)
	}

	p.sig.mu.Lock()
	p.sig.altstack = alt
	p.sig.mu.Unlock()

	return p.result(0, nil)
}

func (p *Process) libcPause() uint32 {
	for {
		if p.idle(-1, false) != nil {
			break
		}
	}

	return p.result(0, EINTR)
}

func (p *Process) suspendWith(mask uint64) uint32 {
	old := p.setMask(mask)

	for {
		if p.idle(-1, false) != nil {
			break
		}
	}

	p.handleSignals()
	p.setMask(old)

	return p.result(0, EINTR)
}

func (p *Process) libcSigsuspend(set uint32) uint32 {
	return p.suspendWith(p.u64(set))
}

func (p *Process) libcRaise(sig uint32) uint32 {
	if !validSignal(sig) {
		return p.result(0, EINVAL)
	}

	p.sendSelf(int(sig))

	return p.result(0, nil)
}

func (p *Process) libcKill(pid, sig uint32) uint32 {
	return p.result(0, p.kill(int32(pid), sig))
}

func (p *Process) libcSigqueue(pid, sig, value uint32) uint32 {
	if int32(pid) <= 0 {
		return p.result(0, EINVAL)
	}

	return p.result(0, p.killInfo(int32(pid), sig, SigInfo{code: SI_QUEUE, value: p.u32(value)}))
}

func (p *Process) libcKillpg(pgrp, sig uint32) uint32 {
	if int32(pgrp) <= 1 {
		return p.result(0, EINVAL)
	}

	return p.result(0, p.kill(-int32(pgrp), sig))
}

func (p *Process) abort() {
	abrt := SIGABRT

	p.setMask(p.mask() &^ sigBit(abrt))
	p.sendSelf(abrt)
	p.deliver()
	p.setAction(abrt, SigAction{})
	p.exitWith(abrt)
}

func (p *Process) libcAbort() {
	p.abort()
}

type JmpBuf struct {
	Jump
	stack uint32
	mask  uint64
	saved bool
}

func (p *Process) stackPointer() uint32 {
	if p.stackAt == 0 {
		at, ok := p.inst.globalAt("__stack_pointer")

		if !ok {
			p.crash("setjmp: no __stack_pointer", SIGABRT)
		}

		p.stackAt = at
	}

	return p.stackAt
}

func (p *Process) setjmp(buf uint32, saveMask bool) uint32 {
	j := p.jumps[buf]

	if j == nil {
		j = new(JmpBuf)
	}

	if !p.inst.setjmp(&j.Jump) {
		p.crash("setjmp: called through a pointer", SIGABRT)
	}

	j.stack = binary.LittleEndian.Uint32(p.inst.block[p.stackPointer():])
	j.mask, j.saved = p.mask(), saveMask

	p.jumps[buf] = j

	return 0
}

func (p *Process) longjmp(buf, val uint32) {
	j := p.jumps[buf]

	if j == nil {
		p.crash("longjmp: the jmp_buf was not set", SIGABRT)
	}

	if val == 0 {
		val = 1
	}

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
	p.crash("longjmp: the function that set the jmp_buf has returned", SIGABRT)
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

func (p *Process) post(e *Event) {
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

func (p *Process) ring() {
	p.events.seat.Ring()
}

func (p *Process) wake(l *Hook) {
	p.post(&l.sub.event)
}

func (p *Process) subscription(id uint64) *Subscription {
	s := p.spare

	if s == nil {
		s = &Subscription{}
	} else {
		p.spare, s.spare = s.spare, nil
	}

	s.init(p, id)

	return s
}

func (p *Process) release(s *Subscription) {
	p.events.drop(&s.event)
	s.spare, p.spare = p.spare, s
}

func (p *Process) watch(mu *sync.Mutex, l *Subscribers, out bool, end time.Time, how Signalled) (timedOut bool, err error) {
	s := p.subscription(0)
	h := &s.in

	if out {
		h = &s.out
	}

	l.add(h)
	mu.Unlock()

	back := false

	defer func() {
		if !back {
			mu.Lock()
			l.remove(h)
			mu.Unlock()
		}
	}()

	timedOut, err = p.wait(end, func(e *Event) (bool, error) {
		if e == &s.event {
			return true, nil
		}

		if how != waitsOn && p.interrupts(how == restarts) {
			return true, EINTR
		}

		return e != nil && e.kind == eventSignal, nil
	})

	mu.Lock()
	l.remove(h)
	p.release(s)

	back = true

	return timedOut, err
}

func (p *Process) wait(end time.Time, done func(e *Event) (bool, error)) (timedOut bool, err error) {
	if over, err := done(nil); over {
		return false, err
	}

	var deadline *Event

	if !end.IsZero() {
		deadline = &Event{kind: eventDeadline}

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

			p.events.seat.Sit()
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

func (p *Process) await(done *atomic.Bool, end time.Time, how Signalled) (timedOut bool, err error) {
	return p.wait(end, func(*Event) (bool, error) {
		if done.Load() {
			return true, nil
		}

		if how == waitsOn {
			return false, nil
		}

		return p.interrupts(how == restarts), EINTR
	})
}

func (p *Process) ensure(end uint64) bool {
	m := p.inst.memory
	size := m.pages * pageSize

	if end <= size {
		return true
	}

	need := (end - size + pageSize - 1) / pageSize
	more := max(need, min(m.pages/8, 256))

	if m.grow(uint32(more), p) != math.MaxUint32 {
		return true
	}

	return m.grow(uint32(need), p) != math.MaxUint32
}

func (p *Process) heapInit() *Heap {
	base, ok := p.inst.globalExport("__heap_base")

	if !ok {
		p.crash("no __heap_base", SIGABRT)
	}

	p.heap = newHeap(base)

	return p.heap
}

func (p *Process) heapGrow(h *Heap) bool {
	if uint64(h.base)+uint64(len(h.segments)+1)*segmentSize > math.MaxUint32 {
		return false
	}

	h.segments = append(h.segments, HeapSegment{mask: math.MaxUint32, prev: noPage, next: noPage})

	for range segmentPages {
		h.pages = append(h.pages, HeapPage{kind: pageFree})
	}

	h.file(int32(len(h.segments) - 1))

	return true
}

func (p *Process) heapReach(h *Heap, end uint32) bool {
	if end <= h.top {
		return true
	}

	if !p.ensure(uint64(end)) {
		return false
	}

	h.top = end

	return true
}

func (p *Process) takePages(h *Heap, count uint32) (int32, bool) {
	if count > segmentPages {
		return p.takeSegments(h, count)
	}

	need := uint32(math.MaxUint32)

	if count < segmentPages {
		need = 1<<count - 1
	}

	for {
		for k := count; k <= segmentPages; k++ {
			s := h.runs[k]

			if s == noPage {
				continue
			}

			seg := &h.segments[s]
			runs := seg.mask

			for i := uint32(1); i < count; i++ {
				runs &= runs >> 1
			}

			start := bits.TrailingZeros32(runs)
			first := s*segmentPages + int32(start)

			if !p.heapReach(h, h.address(first+int32(count))) {
				return 0, false
			}

			seg.mask &^= need << start
			h.file(s)

			return first, true
		}

		if !p.heapGrow(h) {
			return 0, false
		}
	}
}

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

	first := int32(at * segmentPages)

	if !p.heapReach(h, h.address(first+int32(count))) {
		return 0, false
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

		h.file(int32(s))

		left -= n
	}

	return first, true
}

func (p *Process) refill(h *Heap, cls int) bool {
	c := &h.classes[cls]

	if page := c.partial; page != noPage {
		h.unlist(cls, page)
		c.current = page

		return true
	}

	page, ok := p.takePages(h, uint32(classPages[cls]))

	if !ok {
		return false
	}

	for q := page + 1; q < page+int32(classPages[cls]); q++ {
		h.pages[q].kind, h.pages[q].run = pageSlab, uint32(page)
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

func (p *Process) mallocRun(h *Heap, n uint64) uint32 {
	count := pagesFor(n)
	first, ok := p.takePages(h, count)

	if !ok {
		return 0
	}

	h.markRun(first, count)

	return h.address(first)
}

func (p *Process) malloc(n uint32) uint32 {
	h := p.heap

	if h == nil {
		h = p.heapInit()
	}

	if n > smallLimit {
		if n > heapMax {
			return 0
		}

		at := p.mallocRun(h, uint64(n))

		if at != 0 && p.shadow() != nil {
			p.shadowBlock(at, n, pagesFor(uint64(n))<<heapPageShift)
		}

		return at
	}

	cls := classOf(n)
	c := &h.classes[cls]

	for {
		if page := c.current; page != noPage {
			pg := &h.pages[page]
			slot := uint16(math.MaxUint16)

			if k := len(pg.freed); k > 0 {
				slot, pg.freed = pg.freed[k-1], pg.freed[:k-1]
			} else if pg.bump < classSlots[cls] {
				slot = pg.bump
				pg.bump++
			}

			if slot != math.MaxUint16 {
				pg.used[slot/64] |= 1 << (slot % 64)
				pg.live++

				at := h.address(page) + uint32(slot)*classSizes[cls]

				if p.shadow() != nil {
					p.shadowBlock(at, n, classSizes[cls])
				}

				return at
			}
		}

		if !p.refill(h, cls) {
			return 0
		}
	}
}

func (p *Process) usedBlock(ptr uint32) (page int32, slot, size uint32, ok bool) {
	h := p.heap

	if h == nil {
		return 0, 0, 0, false
	}

	if page = h.pageOf(ptr); page == noPage {
		return 0, 0, 0, false
	}

	pg := &h.pages[page]

	if pg.kind == pageRun {
		if ptr != h.address(page) {
			return 0, 0, 0, false
		}

		return page, 0, pg.run << heapPageShift, true
	}

	if slot, ok = h.slotOf(page, ptr); !ok {
		return 0, 0, 0, false
	}

	return page, slot, classSizes[pg.kind], true
}

func (p *Process) block(ptr uint32, what string) (int32, uint32, uint32) {
	page, slot, size, ok := p.usedBlock(ptr)

	if !ok {
		p.invalidPointer(what, ptr)
	}

	return page, slot, size
}

func (p *Process) invalidPointer(what string, ptr uint32) {
	p.crash(fmt.Sprintf("%s(): invalid pointer %#x", what, ptr), SIGABRT)
}

func (p *Process) free(ptr uint32) {
	if ptr == 0 {
		return
	}

	h := p.heap
	page := int32(noPage)

	if h != nil {
		page = h.pageOf(ptr)
	}

	if page == noPage {
		p.invalidPointer("free", ptr)

		return
	}

	pg := &h.pages[page]

	if pg.kind == pageRun {
		if ptr != h.address(page) {
			p.invalidPointer("free", ptr)

			return
		}

		if p.shadow() != nil {
			p.shadowFree(ptr, pg.run<<heapPageShift)
		}

		h.releasePages(page, pg.run)

		return
	}

	slot, ok := h.slotOf(page, ptr)

	if !ok {
		p.invalidPointer("free", ptr)

		return
	}

	cls := int(pg.kind)

	if p.shadow() != nil {
		p.shadowFree(ptr, classSizes[cls])
	}

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

		h.releasePages(page, uint32(classPages[cls]))
	} else if !pg.listed {
		h.list(cls, page)
	}
}

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

	if !p.heapReach(h, h.address(end)) {
		return false
	}

	for q := page + int32(run); q < end; q++ {
		h.segments[q/segmentPages].mask &^= 1 << (q % segmentPages)
	}

	for s := (page + int32(run)) / segmentPages; s <= (end-1)/segmentPages; s++ {
		h.file(s)
	}

	h.markRun(page, count)

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

func (p *Process) crash(msg string, status int) {
	stack := callers(1)

	p.stderrReport(msg, stack)
	p.traceStack(TRACE_FAULTS, msg, stack)
	p.exitWith(status)
}

func (p *Process) stderrReport(msg string, stack []uintptr) {
	name := "?"

	if p.image != nil && len(p.image.argv) > 0 {
		name = p.image.argv[0]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s[%d]: %s\n", name, p.pid, msg)

	for _, line := range traceLines(stack) {
		fmt.Fprintf(&b, "%s[%d]:     %s\n", name, p.pid, line)
	}

	if t, err := p.fd(2); err == nil {
		p.fileWrite(t, bytesFile([]byte(b.String())), b.Len(), IoMode{quiet: true})
	}
}

func (p *Process) traceStack(class uint32, msg string, stack []uintptr) {
	if !p.traces(class) {
		return
	}

	p.trace(class, msg)

	for _, line := range traceLines(stack) {
		p.trace(class, "    "+line)
	}
}

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

	if size := max(n, align); size <= smallLimit && align <= heapPageSize {
		cls := classOf(1 << bits.Len32(size-1))
		at := p.malloc(classSizes[cls])

		if at != 0 {
			p.shadowBlock(at, n, classSizes[cls])
		}

		return at
	}

	count := pagesFor(uint64(max(n, 1)))

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

func (p *Process) stackStart() {
	if at, ok := p.inst.globalAt("__stack_pointer"); ok {
		p.stack = newStack(binary.LittleEndian.Uint32(p.inst.block[at:]))
	}
}

func (p *Process) stackGrow(sp uint32, n uint64) uint32 {
	s := p.stack

	if s == nil {
		panic(newTrap(trapStack, 2))
	}

	block := (sp - 1) &^ (stackBlock - 1)
	need := n + boundSlack

	if at, ok := s.next[block]; ok {
		if uint64(s.sizes[at]) >= need {
			return at + s.sizes[at]
		}

		delete(s.next, block)
		p.stackRelease(at)
	}

	size := (need + stackBlock - 1) &^ (stackBlock - 1)

	if s.total+size > stackLimit {
		panic(newTrap(trapStack, 2))
	}

	at := p.memalign(stackBlock, uint32(size))

	if at == 0 {
		panic(newTrap(trapStack, 2))
	}

	s.next[block], s.sizes[at], s.total = at, uint32(size), s.total+size

	return at + uint32(size)
}

func (p *Process) stackRelease(at uint32) {
	s := p.stack
	size := s.sizes[at]

	for block := at; block < at+size; block += stackBlock {
		if next, ok := s.next[block]; ok {
			delete(s.next, block)
			p.stackRelease(next)
		}
	}

	delete(s.sizes, at)
	s.total -= uint64(size)
	p.free(at)
}

type Mapping struct {
	length uint32
	file   Object
	offset int64
	read   []byte
}

func (p *Process) writeBack(at uint32, m Mapping, from, n uint32) error {
	if m.read == nil {
		return nil
	}

	var st Stat

	if err := statFile(p, m.file, &st); err != nil {
		return err
	}

	n = uint32(max(min(int64(n), st.Size-m.offset-int64(from)), 0))

	was := m.read[from : from+n]

	for i := 0; i < len(was); {
		now := p.span(at+from, n)

		if now[i] == was[i] {
			i++

			continue
		}

		j := i

		for j < len(now) && now[j] != was[j] {
			j++
		}

		w := p.memory(at+from+uint32(i), uint32(j-i), O_RDONLY)
		_, err := p.filePwrite(m.file, w, j-i, m.offset+int64(from)+int64(i))

		p.memoryDone(w)

		if err != nil {
			return err
		}

		copy(was[i:j], p.span(at+from+uint32(i), uint32(j-i)))
		i = j
	}

	return nil
}

func (p *Process) unmap(at uint32) {
	m := p.mappings[at]

	p.writeBack(at, m, 0, m.length)
	delete(p.mappings, at)

	if m.file != nil {
		m.file.unref()
	}

	p.free(at)
}

func (p *Process) unmapAll() {
	for at, m := range p.mappings {
		p.writeBack(at, m, 0, m.length)

		if m.file != nil {
			m.file.unref()
		}
	}

	clear(p.mappings)
}

func (p *Process) mappingOf(addr uint32) (uint32, Mapping, bool) {
	for at, m := range p.mappings {
		if addr-at < m.length {
			return at, m, true
		}
	}

	return 0, Mapping{}, false
}

func (p *Process) libcMalloc(n uint32) uint32 {
	at := p.malloc(n)

	if at == 0 {
		p.setErrno(ENOMEM)
	}

	return at
}

func (p *Process) libcCalloc(n, size uint32) uint32 {
	total := uint64(n) * uint64(size)

	if total > math.MaxUint32 {
		p.setErrno(ENOMEM)

		return 0
	}

	at := p.malloc(uint32(total))

	if at == 0 {
		p.setErrno(ENOMEM)

		return 0
	}

	clear(p.span(at, uint32(total)))

	return at
}

func (p *Process) libcRealloc(ptr, n uint32) uint32 {
	at := p.realloc(ptr, n)

	if at == 0 && n != 0 {
		p.setErrno(ENOMEM)
	}

	return at
}

func (p *Process) libcMallopt(param, value uint32) uint32 {
	return 1
}

func (p *Process) libcGetpagesize() uint32 {
	return 4096
}

func (p *Process) libcMmap(addr, n, prot, flags, fd uint32, off uint64) uint32 {
	const failed = math.MaxUint32

	if n == 0 || off&4095 != 0 || flags&15 < 1 || flags&15 > 3 {
		p.setErrno(EINVAL)

		return failed
	}

	rounded := pageRound(n)

	if rounded > math.MaxUint32 {
		p.setErrno(ENOMEM)

		return failed
	}

	n = uint32(rounded)

	at := p.memalign(4096, n)

	if at == 0 {
		p.setErrno(ENOMEM)

		return failed
	}

	clear(p.span(at, n))

	m := Mapping{length: n}

	if flags&MAP_ANONYMOUS == 0 {
		obj, err := p.fd(fd)

		var f Object

		if err == nil {
			if _, failed := p.fileSeek(obj, 0, 1); failed != nil {
				err = ENODEV
			} else {
				f = obj

				w := p.memory(at, n, O_WRONLY)

				_, err = p.filePread(f, w, int(n), int64(off))

				p.memoryDone(w)

				if err == ESPIPE {
					err = ENODEV
				}
			}
		}

		if err != nil {
			p.free(at)
			p.setErrno(errnoOf(err))

			return failed
		}

		f.ref()
		m.file, m.offset = f, int64(off)

		if flags&MAP_SHARED != 0 && prot&PROT_WRITE != 0 {
			m.read = bytes.Clone(p.span(at, n))
		}
	}

	p.mappings[at] = m

	return at
}

func (p *Process) libcMunmap(addr, n uint32) uint32 {
	if n == 0 {
		return p.result(0, EINVAL)
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
		return p.result(0, EINVAL)
	}

	at, m, ok := p.mappingOf(addr)

	if !ok {
		return p.result(0, ENOMEM)
	}

	n = min(n, m.length-(addr-at))

	return p.result(0, p.writeBack(at, m, addr-at, n))
}

func (p *Process) libcMadvise(addr, n, advice uint32) uint32 {
	b := p.span(addr, n)

	if advice != MADV_DONTNEED {
		return p.result(0, nil)
	}

	at, m, ok := p.mappingOf(addr)

	switch {
	case !ok || m.file == nil:
		clear(b)
	case m.read == nil:
		in := b[:min(n, m.length-(addr-at))]

		clear(in)

		w := p.memory(addr, uint32(len(in)), O_WRONLY)

		p.filePread(m.file, w, len(in), m.offset+int64(addr-at))
		p.memoryDone(w)
	}

	return p.result(0, nil)
}

func (p *Process) libcMprotect(addr, n, prot uint32) uint32 {
	p.span(addr, n)

	return p.result(0, nil)
}

func (p *Process) libcMemalign(align, n uint32) uint32 {
	if !alignment(align, false) {
		p.setErrno(EINVAL)

		return 0
	}

	at := p.memalign(align, n)

	if at == 0 {
		p.setErrno(ENOMEM)
	}

	return at
}

func (p *Process) libcAligned_alloc(align, n uint32) uint32 {
	if !alignment(align, false) {
		p.setErrno(EINVAL)

		return 0
	}

	at := p.memalign(align, n)

	if at == 0 {
		p.setErrno(ENOMEM)
	}

	return at
}

func (p *Process) libcPosix_memalign(out, align, n uint32) uint32 {
	if !alignment(align, true) {
		return uint32(EINVAL)
	}

	at := p.memalign(align, n)

	if at == 0 {
		return uint32(ENOMEM)
	}

	p.put32(out, at)

	return 0
}

func (p *Process) libcMalloc_usable_size(ptr uint32) uint32 {
	if ptr == 0 {
		return 0
	}

	_, _, size := p.block(ptr, "malloc_usable_size")

	return p.blockAsked(ptr, size)
}

func (p *Process) libcReallocarray(ptr, n, size uint32) uint32 {
	total := uint64(n) * uint64(size)

	if total > math.MaxUint32 {
		p.setErrno(ENOMEM)

		return 0
	}

	at := p.realloc(ptr, uint32(total))

	if at == 0 && total != 0 {
		p.setErrno(ENOMEM)
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

func (p *Process) shadow() []byte {
	if p.inst == nil || p.inst.memory == nil {
		return nil
	}

	return p.inst.memory.shadow
}

func fill(s []byte, v byte) {
	if len(s) == 0 {
		return
	}

	s[0] = v

	for n := 1; n < len(s); n *= 2 {
		copy(s[n:], s[:n])
	}
}

func (p *Process) asanStart() {
	if sh := p.shadow(); sh != nil {
		fill(sh[boundSlack:p.inst.memory.pages*pageSize], shadowOK)
	}
}

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

func (p *Process) shadowBlock(at, n, size uint32) {
	if sh := p.shadow(); sh != nil {
		fill(sh[uint64(at):uint64(at)+uint64(n)], shadowOK)
		fill(sh[uint64(at)+uint64(n):uint64(at)+uint64(size)], shadowRedzone)
	}
}

func (p *Process) shadowFree(at, size uint32) {
	if sh := p.shadow(); sh != nil {
		fill(sh[uint64(at):uint64(at)+uint64(size)], shadowFreed)
	}
}

func (p *Process) blockAsked(at, size uint32) uint32 {
	sh := p.shadow()

	if sh == nil {
		return size
	}

	return uint32(okBytes(sh[uint64(at) : uint64(at)+uint64(size)]))
}

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

func (p *Process) asanCheck(at, n uint32) {
	sh := p.shadow()

	if sh == nil || n == 0 {
		return
	}

	ok := okBytes(sh[uint64(at) : uint64(at)+uint64(n)])

	if ok != int(n) {
		p.crash(p.asanReport(&Trap{code: trapAsan, addr: uint64(at) + uint64(ok), kind: asanRange}), SIGABRT)
	}
}

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

func (p *Process) blockAround(addr uint32) (start, asked, size uint32, ok bool) {
	h := p.heap

	if h == nil || addr < h.base || uint64(addr-h.base)>>heapPageShift >= uint64(len(h.pages)) {
		return 0, 0, 0, false
	}

	page := int32((addr - h.base) >> heapPageShift)

	if h.pages[page].kind == pageSlab {
		page = int32(h.pages[page].run)
	}

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

func (p *Process) fd(fd uint32) (Object, error) {
	d, ok := p.files.at(int(fd))

	if !ok {
		return nil, EBADF
	}

	return d.obj, nil
}

func (p *Process) install(obj Object, min int, cloexec bool) (int, error) {
	if fd := p.files.lowest(min); fd < p.fdLimit() {
		p.files.put(fd, obj, cloexec)

		return fd, nil
	}

	obj.unref()

	return -1, EMFILE
}

func (p *Process) fdLimit() int {
	return int(min64(p.limits[RLIMIT_NOFILE].cur, 1<<20))
}

func (p *Process) descriptorFree() error {
	if p.files.lowest(0) >= p.fdLimit() {
		return EMFILE
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

func (p *Process) dupTo(old, new uint32, cloexec bool) (int, error) {
	obj, err := p.fd(old)

	if err != nil {
		return 0, err
	}

	if uint64(new) >= p.limits[RLIMIT_NOFILE].cur {
		return 0, EBADF
	}

	p.placeAt(obj, new, cloexec)

	return int(new), nil
}

func (p *Process) placeAt(obj Object, fd uint32, cloexec bool) {
	obj.ref()
	p.closeFd(int(fd))
	p.files.put(int(fd), obj, cloexec)
}

func (p *Process) idle(timeout int, restart bool) error {
	_, err := p.waitObjects(nil, timeout, restart)

	return err
}

type PollEntry struct {
	obj     Object
	events  int16
	revents int16
	sub     *Subscription
}

func (p *Process) waitObjects(entries []PollEntry, timeout int, restart bool) (int, error) {
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

	_, err := p.wait(deadline(timeout), func(e *Event) (bool, error) {
		switch {
		case e == nil || e.kind == eventSignal:

		case e.kind != eventChanged || e.id >= uint64(len(entries)) || e != &entries[e.id].sub.event:
			return false, nil
		case entries[e.id].obj.poll(entries[e.id].events, nil) == 0:
			return false, nil
		}

		if ready = pollEntries(entries); ready > 0 {
			return true, nil
		}

		return p.interrupts(restart), EINTR
	})

	return ready, err
}

func pollEntries(entries []PollEntry) int {
	ready := 0

	for i := range entries {
		entries[i].revents = entries[i].obj.poll(entries[i].events, nil)

		if entries[i].revents != 0 {
			ready++
		}
	}

	return ready
}

func (p *Process) waitObject(obj Object, events int16, timeout int, restart bool) (int, error) {
	return p.waitObjects([]PollEntry{{obj: obj, events: events}}, timeout, restart)
}

func (p *Process) ioSpans(iovs uint32, count uint32) ([][]byte, error) {
	return p.ioSpansIn(nil, iovs, count)
}

func (p *Process) ioSpansIn(out [][]byte, iovs uint32, count uint32) ([][]byte, error) {
	if count > 1024 {
		return nil, EINVAL
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

func (p *Process) gather(iovs, count uint32, max int) ([]byte, error) {
	return p.gatherIn(nil, iovs, count, max)
}

func (p *Process) gatherIn(small []byte, iovs, count uint32, max int) ([]byte, error) {
	if count > 1024 {
		return nil, EINVAL
	}

	var few [4][]byte

	table := p.span(iovs, 8*count)
	total := 0

	for i := range count {
		b := p.span(binary.LittleEndian.Uint32(table[8*i:]), binary.LittleEndian.Uint32(table[8*i+4:]))

		if i < uint32(len(few)) {
			few[i] = b
		}

		total += len(b)
	}

	if total > max {
		return nil, EMSGSIZE
	}

	data := small

	if total <= len(small) {
		data = small[:total]
	} else {
		data = make([]byte, total)
	}

	at := 0

	for i := range count {
		b := few[min(i, uint32(len(few)-1))]

		if i >= uint32(len(few)) {
			b = p.span(binary.LittleEndian.Uint32(table[8*i:]), binary.LittleEndian.Uint32(table[8*i+4:]))
		}

		at += copy(data[at:], b)
	}

	return data, nil
}

func (p *Process) vectored(fd, iovs, count uint32, events int16, positioned bool, off uint64) uint32 {
	t, err := p.fd(fd)

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

	if events == POLLOUT {
		for _, b := range spans {
			buf = append(buf, b...)
		}

		if positioned {
			return p.result(p.filePwrite(t, bytesFile(buf), len(buf), int64(off)))
		}

		return p.result(p.fileWrite(t, bytesFile(buf), len(buf), IoMode{}))
	}

	buf = buf[:total]
	var n int

	if positioned {
		n, err = p.filePread(t, bytesFile(buf), total, int64(off))
	} else {
		n, err = p.fileRead(t, bytesFile(buf), total, IoMode{})
	}

	spans, _ = p.ioSpans(iovs, count)

	at := 0

	for _, b := range spans {
		if at >= n {
			break
		}

		at += copy(b, buf[at:n])
	}

	return p.result(n, err)
}

func (p *Process) recordLock(l Object, cmd, at uint32) error {
	lk := Flock{Type: int16(p.u16(at)), Whence: int16(p.u16(at + 2)), Start: int64(p.u64(at + 8)), Len: int64(p.u64(at + 16))}

	if cmd == F_GETLK {
		if err := p.fileLock(l, fOfdGetlk, &lk); err != nil {
			return err
		}

		p.put16(at, uint16(lk.Type))
		p.put16(at+2, uint16(lk.Whence))
		p.put64(at+8, uint64(lk.Start))
		p.put64(at+16, uint64(lk.Len))
		p.put32(at+24, uint32(lk.Pid))

		return nil
	}

	if cmd == F_SETLKW {
		return p.fileLock(l, fOfdSetlkw, &lk)
	}

	return p.fileLock(l, fOfdSetlk, &lk)
}

type FdSet struct {
	at uint32
}

func (s FdSet) has(p *Process, fd int) bool {
	return s.at != 0 && p.u32(s.at+uint32(fd/32*4))&(1<<(fd%32)) != 0
}

func (s FdSet) set(p *Process, fd int) {
	p.put32(s.at+uint32(fd/32*4), p.u32(s.at+uint32(fd/32*4))|1<<(fd%32))
}

func (p *Process) selectFds(n uint32, sets [3]FdSet, timeout int) (int, error) {
	if n > FD_SETSIZE {
		return 0, EINVAL
	}

	var entries []PollEntry
	var fds []int

	for fd := range int(n) {
		events := int16(0)

		for i, ev := range []int16{POLLIN, POLLOUT, POLLPRI} {
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

		entries = append(entries, PollEntry{obj: obj, events: events})
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

		for k, mask := range []int16{pollInSet, pollOutSet, POLLPRI} {
			if e.events&[]int16{POLLIN, POLLOUT, POLLPRI}[k] != 0 && e.revents&mask != 0 {
				sets[k].set(p, fds[i])
				count++
			}
		}
	}

	return count, nil
}

func (p *Process) withMask(mask uint32, wait func() (int, error)) (int, error) {
	if mask == 0 {
		return wait()
	}

	old := p.setMask(p.u64(mask))
	n, err := wait()

	if err == EINTR {
		p.handleSignals()
	}

	p.setMask(old)

	return n, err
}

func (p *Process) libcRead(fd, buf, n uint32) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	w := p.memory(buf, n, O_WRONLY)
	count, err := p.fileRead(t, w, int(n), IoMode{})

	p.memoryDone(w)

	return p.result(count, err)
}

func (p *Process) libcWrite(fd, buf, n uint32) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	w := p.memory(buf, n, O_RDONLY)
	count, err := p.fileWrite(t, w, int(n), IoMode{})

	p.memoryDone(w)

	return p.result(count, err)
}

func (p *Process) libcPread(fd, buf, n uint32, off uint64) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	w := p.memory(buf, n, O_WRONLY)
	count, err := p.filePread(t, w, int(n), int64(off))

	p.memoryDone(w)

	return p.result(count, err)
}

func (p *Process) libcPwrite(fd, buf, n uint32, off uint64) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	w := p.memory(buf, n, O_RDONLY)
	count, err := p.filePwrite(t, w, int(n), int64(off))

	p.memoryDone(w)

	return p.result(count, err)
}

func (p *Process) libcCopy_file_range(in, offIn, out, offOut, n, flags uint32) uint32 {
	src, err := p.fd(in)

	if err != nil {
		return p.result(0, err)
	}

	dst, err := p.fd(out)

	if err != nil {
		return p.result(0, err)
	}

	if flags != 0 {
		return p.result(0, EINVAL)
	}

	inFlags, _ := p.fileFlags(src)
	outFlags, _ := p.fileFlags(dst)
	r, w := src, dst

	if inFlags&O_ACCMODE == O_WRONLY || outFlags&O_ACCMODE == O_RDONLY || outFlags&O_APPEND != 0 {
		return p.result(0, EBADF)
	}

	from, err := p.copyOffset(src, offIn)

	if err != nil {
		return p.result(0, err)
	}

	to, err := p.copyOffset(dst, offOut)

	if err != nil {
		return p.result(0, err)
	}

	count, err := p.fileCopyTo(r, w, from, to, int(n))

	if count == 0 && err != nil {
		return p.result(0, err)
	}

	p.copied(src, offIn, from+int64(count))
	p.copied(dst, offOut, to+int64(count))

	return p.result(count, nil)
}

func (p *Process) copyOffset(f Object, at uint32) (int64, error) {
	off := int64(0)

	if at != 0 {
		off = int64(p.u64(at))
	} else if pos, err := p.fileSeek(f, 0, 1); err != nil {
		return 0, EINVAL
	} else {
		off = pos
	}

	if off < 0 {
		return 0, EINVAL
	}

	return off, nil
}

func (p *Process) copied(f Object, at uint32, off int64) {
	if at != 0 {
		p.put64(at, uint64(off))

		return
	}

	p.fileSeek(f, off, 0)
}

func copyThrough(p *Process, in, out Object, from, to int64, n int) (int, error) {
	for _, t := range []Object{in, out} {
		var st Stat

		if err := p.fileStat(t, &st); err == EBADF {
			return 0, EINVAL
		} else if err != nil {
			return 0, err
		}

		if st.Mode&S_IFMT == S_IFDIR {
			return 0, EISDIR
		}

		if st.Mode&S_IFMT != S_IFREG {
			return 0, EINVAL
		}
	}

	buf := make([]byte, min(n, packetMax))
	file := bytesFile(buf)
	done := 0

	for done < n {
		chunk := buf[:min(len(buf), n-done)]
		got, err := p.filePread(in, file, len(chunk), from+int64(done))

		if err != nil || got == 0 {
			if done > 0 {
				err = nil
			}

			return done, err
		}

		put, err := p.filePwrite(out, file, got, to+int64(done))

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
	n, err := p.fd(fd)

	if err == nil {
		err = p.request(n, &FileSync{data: true})
	}

	return p.result(0, err)
}

func (p *Process) libcSync() {
	syscall.Sync()
}

func (p *Process) libcSyncfs(fd uint32) uint32 {
	_, err := p.fd(fd)

	return p.result(0, err)
}

func (p *Process) libcFlock(fd, op uint32) uint32 {
	l, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(0, p.fileFlock(l, int(op)))
}

func (p *Process) libcLockf(fd, cmd uint32, n uint64) uint32 {
	l, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	lk := Flock{Type: F_WRLCK, Whence: 1, Len: int64(n)}

	switch cmd {
	case 0:
		lk.Type = F_UNLCK
		err = p.fileLock(l, fOfdSetlk, &lk)
	case 1:
		err = p.fileLock(l, fOfdSetlkw, &lk)
	case 2:
		err = p.fileLock(l, fOfdSetlk, &lk)
	case 3:
		lk.Type = F_RDLCK

		if err = p.fileLock(l, fOfdGetlk, &lk); err == nil && lk.Type != F_UNLCK {
			err = EACCES
		}
	default:
		err = EINVAL
	}

	return p.result(0, err)
}

func (p *Process) libcPosix_fadvise(fd uint32, off, n uint64, advice uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return uint32(errnoOf(err))
	}

	var st Stat

	if err := p.fileStat(obj, &st); err != nil {
		return uint32(errnoOf(err))
	}

	switch {
	case st.Mode&S_IFMT == S_IFIFO:
		return uint32(ESPIPE)
	case int64(n) < 0 || advice > POSIX_FADV_NOREUSE:
		return uint32(EINVAL)
	}

	return 0
}

func (p *Process) libcPosix_fallocate(fd uint32, off, n uint64) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return uint32(errnoOf(err))
	}

	return uint32(errnoOf(p.fallocate(obj, int64(off), int64(n))))
}

func (p *Process) fallocate(obj Object, off, n int64) error {
	if off < 0 || n <= 0 {
		return EINVAL
	}

	if fl, err := p.fileFlags(obj); err != nil || fl&O_ACCMODE == O_RDONLY {
		return EBADF
	}

	var st Stat

	if err := p.fileStat(obj, &st); err != nil {
		return err
	}

	switch {
	case st.Mode&S_IFMT == S_IFIFO:
		return ESPIPE
	case st.Mode&S_IFMT == S_IFDIR:
		return EISDIR
	case st.Mode&S_IFMT != S_IFREG:
		return ENODEV
	case off > math.MaxInt64-n:
		return EFBIG
	case off+n <= st.Size:
		return nil
	}

	if err := p.request(obj, &FileTruncate{size: off + n}); err != EINVAL {
		return err
	}

	return ENODEV
}

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
	count, err := p.selectFds(n, [3]FdSet{{r}, {w}, {e}}, timeout)

	if tv != 0 && err != EINVAL {
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
		return p.selectFds(n, [3]FdSet{{r}, {w}, {e}}, timeout)
	}))
}

func (p *Process) libcLseek(fd uint32, off uint64, whence uint32) uint64 {
	s, err := p.fd(fd)

	if err != nil {
		return p.result64(0, err)
	}

	return p.result64(p.fileSeek(s, int64(off), int(whence)))
}

func (p *Process) libcClose(fd uint32) uint32 {
	if !p.closeFd(int(fd)) {
		return p.result(0, EBADF)
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
	if old == new || flags&^O_CLOEXEC != 0 {
		return p.result(0, EINVAL)
	}

	return p.result(p.dupTo(old, new, flags&O_CLOEXEC != 0))
}

func (p *Process) libcCfgetospeed(at uint32) uint32 {
	return p.u32(at+8) & CBAUD
}

func (p *Process) libcCfgetispeed(at uint32) uint32 {
	return p.u32(at+8) & CBAUD
}

func (p *Process) libcCfsetispeed(at, speed uint32) uint32 {
	if speed == 0 {
		return 0
	}

	return p.cfsetspeed(at, speed)
}

func (p *Process) libcFsync(fd uint32) uint32 {
	n, err := p.fd(fd)

	if err == nil {
		err = p.request(n, &FileSync{data: false})
	}

	return p.result(0, err)
}

func (p *Process) libcFtruncate(fd uint32, size uint64) uint32 {
	n, err := p.fd(fd)

	if err == nil {
		err = p.request(n, &FileTruncate{size: int64(size)})
	}

	return p.result(0, err)
}

func statFile(p *Process, obj Object, st *Stat) error {
	if p.chaosFail("file-stat") {
		return EIO
	}

	return p.fileStat(obj, st)
}

func (p *Process) libcFstat(fd, at uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	var st Stat

	if err := statFile(p, obj, &st); err != nil {
		return p.result(0, err)
	}

	p.putStat(at, &st)

	return p.result(0, nil)
}

func (p *Process) libcFchmod(fd, mode uint32) uint32 {
	n, err := p.fd(fd)

	if err == nil {
		err = p.request(n, &FileChmod{mode: mode & 0o7777})
	}

	return p.result(0, err)
}

func (p *Process) libcFchown(fd, uid, gid uint32) uint32 {
	n, err := p.fd(fd)

	if err == nil {
		err = p.request(n, &FileChown{uid: int(int32(uid)), gid: int(int32(gid))})
	}

	return p.result(0, err)
}

func (p *Process) libcFutimes(fd, times uint32) uint32 {
	n, err := p.fd(fd)

	if err == nil {
		err = p.request(n, &FileUtimes{ts: p.timevals(times)})
	}

	return p.result(0, err)
}

func (p *Process) libcFutimens(fd, times uint32) uint32 {
	n, err := p.fd(fd)

	if err == nil {
		err = p.request(n, &FileUtimes{ts: p.timespecs(times)})
	}

	return p.result(0, err)
}

func (p *Process) varArg(va uint32, i uint32) uint32 {
	return p.u32(va + 4*i)
}

func (p *Process) libcFcntl(fd, cmd, va uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	switch cmd {
	case F_DUPFD, F_DUPFD_CLOEXEC:
		min := p.varArg(va, 0)

		if uint64(min) >= p.limits[RLIMIT_NOFILE].cur {
			return p.result(0, EINVAL)
		}

		obj.ref()

		return p.result(p.install(obj, int(min), cmd == F_DUPFD_CLOEXEC))
	case F_GETFD:
		if p.files.slots[fd].cloexec {
			return p.result(1, nil)
		}

		return p.result(0, nil)
	case F_SETFD:
		p.files.slots[fd].cloexec = p.varArg(va, 0)&1 != 0

		return p.result(0, nil)
	case F_GETFL:
		return p.result(p.fileFlags(obj))
	case F_GETLK, F_SETLK, F_SETLKW:
		l, err := p.fd(fd)

		if err != nil {
			return p.result(0, err)
		}

		return p.result(0, p.recordLock(l, cmd, p.varArg(va, 0)))
	case F_SETFL:
		return p.result(0, p.fileSetFlags(obj, int(p.varArg(va, 0))))
	}

	return p.result(0, EINVAL)
}

func (p *Process) cfsetspeed(at, speed uint32) uint32 {
	if speed&^CBAUD != 0 {
		p.setErrno(EINVAL)

		return ^uint32(0)
	}

	p.put32(at+8, p.u32(at+8)&^CBAUD|speed)

	return 0
}

func (p *Process) libcCfmakeraw(at uint32) {
	b := p.span(at, 60)
	le := binary.LittleEndian

	off := func(o int, clear, set uint32) {
		le.PutUint32(b[o:], le.Uint32(b[o:])&^clear|set)
	}

	off(0, IGNBRK|BRKINT|PARMRK|ISTRIP|INLCR|IGNCR|ICRNL|IXON, 0)
	off(4, OPOST, 0)
	off(8, CSIZE|PARENB, CS8)
	off(12, ECHO|ECHONL|ICANON|ISIG|IEXTEN, 0)
	b[17+VTIME] = 0
	b[17+VMIN] = 1
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

func (p *Process) poll(at, n uint32, timeout int) (int, error) {
	if n > 1<<16 {
		return 0, EINVAL
	}

	guest := p.span(at, 8*n)
	entries := make([]PollEntry, 0, n)
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
			binary.LittleEndian.PutUint16(e[6:], POLLNVAL)
			invalid++

			continue
		}

		slot[i] = len(entries)
		entries = append(entries, PollEntry{obj: obj, events: int16(binary.LittleEndian.Uint16(e[4:]))})
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

func (p *Process) putStat(at uint32, st *Stat) {
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

	for i, t := range []Timespec{st.Atim, st.Mtim, st.Ctim} {
		le.PutUint64(b[64+16*i:], uint64(t.Sec))
		le.PutUint32(b[72+16*i:], uint32(t.Nsec))
	}
}

func (p *Process) timespecs(at uint32) []Timespec {
	if at == 0 {
		return nil
	}

	b := p.span(at, 32)
	le := binary.LittleEndian

	return []Timespec{
		{Sec: int64(le.Uint64(b)), Nsec: int64(int32(le.Uint32(b[8:])))},
		{Sec: int64(le.Uint64(b[16:])), Nsec: int64(int32(le.Uint32(b[24:])))},
	}
}

func (p *Process) libcReadv(fd, iovs, count uint32) uint32 {
	return p.vectored(fd, iovs, count, POLLIN, false, 0)
}

func (p *Process) libcWritev(fd, iovs, count uint32) uint32 {
	return p.vectored(fd, iovs, count, POLLOUT, false, 0)
}

func (p *Process) libcPreadv(fd, iovs, count uint32, off uint64) uint32 {
	return p.vectored(fd, iovs, count, POLLIN, true, off)
}

func (p *Process) libcPwritev(fd, iovs, count uint32, off uint64) uint32 {
	return p.vectored(fd, iovs, count, POLLOUT, true, off)
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

func (p *Process) libcIoctl(fd, req, va uint32) uint32 {
	return p.ioctl(fd, req, p.varArg(va, 0))
}

func (p *Process) ioctl(fd, req, arg uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	if p.device(obj) != (FileKey{}) {
		if r, done := p.terminalIoctl(obj, fd, req, arg); done {
			return r
		}
	}

	switch req {
	case FIONBIO:
		fl, err := p.fileFlags(obj)

		if err != nil {
			return p.result(0, err)
		}

		fl &^= O_NONBLOCK

		if p.u32(arg) != 0 {
			fl |= O_NONBLOCK
		}

		return p.result(0, p.fileSetFlags(obj, fl))
	case FIOCLEX, FIONCLEX:
		p.files.slots[fd].cloexec = req == FIOCLEX

		return p.result(0, nil)
	case TIOCGPGRP:
		r := p.libcTcgetpgrp(fd)

		if int32(r) >= 0 {
			p.put32(arg, r)
			r = 0
		}

		return r
	case TIOCSPGRP:
		return p.libcTcsetpgrp(fd, p.u32(arg))
	case TIOCGWINSZ:
		w, err := p.ttyWindow(obj)

		if err == ENOTTY {
			break
		}

		if err == nil {
			copy(p.span(arg, sizeofWinsize), w)
		}

		return p.result(0, err)
	case FIONREAD:
		var n int32

		if err := p.fileIoctl(obj, FIONREAD, unsafe.Pointer(&n)); err == ENOTTY {
			break
		} else if err != nil {
			return p.result(0, err)
		}

		p.put32(arg, uint32(n))

		return p.result(0, nil)
	}

	return p.result(0, ENOTTY)
}

func checked(path string) error {
	switch {
	case path == "":
		return ENOENT
	case len(path) >= 4096:
		return ENAMETOOLONG
	}

	return nil
}

func (p *Process) onPath(dirfd int32, path string, call func(d Object) error) error {
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

func (p *Process) onPaths(olddir int32, old string, newdir int32, new string, call func(from, to Object) error) error {
	return p.onPath(olddir, old, func(from Object) error {
		return p.onPath(newdir, new, func(to Object) error {
			return call(from, to)
		})
	})
}

func (p *Process) start(dirfd int32, path string) (Object, error) {
	d := p.root

	if !strings.HasPrefix(path, "/") {
		d = p.cwd

		if dirfd != AT_FDCWD {
			var err error

			if d, err = p.fd(uint32(dirfd)); err != nil {
				return nil, err
			}
		}
	}

	d.ref()

	return d, nil
}

func (p *Process) onAt(dirfd int32, path string, flags uint32, byFile func(f Object) error, byPath func(d Object, follow bool) error) error {
	if path == "" && flags&AT_EMPTY_PATH != 0 {
		if dirfd == AT_FDCWD {
			return byFile(p.cwd)
		}

		obj, err := p.fd(uint32(dirfd))

		if err != nil {
			return err
		}

		return byFile(obj)
	}

	return p.onPath(dirfd, path, func(d Object) error {
		return byPath(d, flags&AT_SYMLINK_NOFOLLOW == 0)
	})
}

func (p *Process) openAt(dirfd int32, path string, flags int, mode uint32) (Object, error) {
	var f Object

	err := p.onPath(dirfd, path, func(d Object) error {
		var err error
		f, err = p.dirOpen(d, path, flags, mode)

		return err
	})

	return f, err
}

func (p *Process) open(dirfd int32, path string, flags, va uint32) (int, error) {
	mode := uint32(0)

	if flags&(O_CREAT|0o20000000) != 0 {
		mode = p.varArg(va, 0) & 0o7777 &^ p.umask
	}

	if err := p.descriptorFree(); err != nil {
		return 0, err
	}

	f, err := p.openAt(dirfd, path, int(flags&^O_CLOEXEC), mode)

	if err != nil {
		return 0, err
	}

	return p.install(f, 0, flags&O_CLOEXEC != 0)
}

func (p *Process) openPath(path string, flags int, mode uint32) (Object, error) {
	return p.openAt(AT_FDCWD, path, flags, mode)
}

func (p *Process) dirNames(path string) ([]string, error) {
	d, err := p.openPath(path, O_RDONLY|O_DIRECTORY, 0)

	if err != nil {
		return nil, err
	}

	defer d.unref()

	var names []string

	buf := make([]byte, 4096)
	file := bytesFile(buf)

	for {
		n, err := p.fileReaddir(d, file, len(buf))

		if err != nil || n == 0 {
			return names, err
		}

		for at := 0; at+19 <= n; {
			reclen := int(binary.LittleEndian.Uint16(buf[at+16:]))
			name := buf[at+19 : at+reclen]

			if end := bytes.IndexByte(name, 0); end >= 0 {
				name = name[:end]
			}

			if string(name) != "." && string(name) != ".." {
				names = append(names, string(name))
			}

			at += reclen
		}
	}
}

func (p *Process) mkdir(dirfd int32, path string, mode uint32) error {
	return p.onPath(dirfd, path, func(d Object) error {
		return p.dirMkdir(d, path, mode&0o7777&^p.umask)
	})
}

func (p *Process) mknod(dirfd int32, path string, mode uint32, dev uint64) error {
	switch mode & S_IFMT {
	case 0, S_IFREG, S_IFIFO, S_IFSOCK:
	default:
		return EPERM
	}

	return p.onPath(dirfd, path, func(d Object) error {
		return p.dirMknod(d, path, mode&^(p.umask&0o777))
	})
}

func (p *Process) symlink(target string, dirfd int32, path string) error {
	if target == "" {
		return ENOENT
	}

	return p.onPath(dirfd, path, func(d Object) error {
		return p.dirSymlink(d, target, path)
	})
}

func (p *Process) remove(dirfd int32, path string, dir bool) error {
	return p.onPath(dirfd, path, func(d Object) error {
		return p.dirUnlink(d, path, dir)
	})
}

func (p *Process) rename(olddir int32, old string, newdir int32, new string) error {
	return p.onPaths(olddir, old, newdir, new, func(from, to Object) error {
		return p.dirRename(from, old, to, new)
	})
}

func (p *Process) link(olddir int32, old string, newdir int32, new string, follow bool) error {
	return p.onPaths(olddir, old, newdir, new, func(from, to Object) error {
		return p.dirLink(from, old, to, new, follow)
	})
}

func (p *Process) statPath(path string, follow bool, st *Stat) error {
	return p.onPath(AT_FDCWD, path, func(d Object) error {
		return p.dirStat(d, path, follow, st)
	})
}

func (p *Process) accessAt(dirfd int32, path string, mode, flags uint32) error {
	return p.onPath(dirfd, path, func(d Object) error {
		return p.access(d, path, mode, flags)
	})
}

func (p *Process) access(d Object, path string, mode, flags uint32) error {
	var st Stat

	if err := p.dirStat(d, path, flags&AT_SYMLINK_NOFOLLOW == 0, &st); err != nil {
		return err
	}

	if !p.permits(&st, mode&7) {
		return EACCES
	}

	return nil
}

func (p *Process) putStatvfs(at uint32, st *Statfs) {
	b := p.span(at, 72)

	clear(b)

	le := binary.LittleEndian

	le.PutUint32(b, uint32(st.Bsize))
	le.PutUint32(b[4:], uint32(st.Frsize))
	le.PutUint64(b[8:], st.Blocks)
	le.PutUint64(b[16:], st.Bfree)
	le.PutUint64(b[24:], st.Bavail)
	le.PutUint64(b[32:], st.Files)
	le.PutUint64(b[40:], st.Ffree)
	le.PutUint64(b[48:], st.Ffree)
	le.PutUint32(b[56:], uint32(st.Fsid[0]))
	le.PutUint32(b[60:], uint32(st.Flags))
	le.PutUint32(b[64:], uint32(st.Namelen))
}

func (p *Process) putStatfsOf(at uint32, st *Statfs) {
	b := p.span(at, sizeofStatfs)

	clear(b)

	le := binary.LittleEndian

	le.PutUint32(b, uint32(st.Type))
	le.PutUint32(b[4:], uint32(st.Bsize))
	le.PutUint64(b[8:], st.Blocks)
	le.PutUint64(b[16:], st.Bfree)
	le.PutUint64(b[24:], st.Bavail)
	le.PutUint64(b[32:], st.Files)
	le.PutUint64(b[40:], st.Ffree)
	le.PutUint32(b[48:], uint32(st.Fsid[0]))
	le.PutUint32(b[52:], uint32(st.Fsid[1]))
	le.PutUint32(b[56:], uint32(st.Namelen))
	le.PutUint32(b[60:], uint32(st.Frsize))
	le.PutUint32(b[64:], uint32(st.Flags))
}

func (p *Process) statfsPath(path string, st *Statfs) error {
	return p.onPath(AT_FDCWD, path, func(d Object) error {
		return p.dirStatfs(d, path, st)
	})
}

func (p *Process) libcOpen(path, flags, va uint32) uint32 {
	return p.result(p.open(AT_FDCWD, p.str(path), flags, va))
}

func (p *Process) libcOpenat(dirfd, path, flags, va uint32) uint32 {
	return p.result(p.open(int32(dirfd), p.str(path), flags, va))
}

func (p *Process) libcFstatat(dirfd, path, at, flags uint32) uint32 {
	var st Stat

	err := p.statFlags(int32(dirfd), p.str(path), flags, &st)

	if err == nil {
		p.putStat(at, &st)
	}

	return p.result(0, err)
}

func (p *Process) statFlags(dirfd int32, path string, flags uint32, st *Stat) error {
	return p.onAt(dirfd, path, flags, func(f Object) error {
		return statFile(p, f, st)
	}, func(d Object, follow bool) error {
		return p.dirStat(d, path, follow, st)
	})
}

func (p *Process) statAt(dirfd int32, path string, follow bool, st *Stat) error {
	if follow {
		return p.statFlags(dirfd, path, 0, st)
	}

	return p.statFlags(dirfd, path, AT_SYMLINK_NOFOLLOW, st)
}

func (p *Process) libcAccess(path, mode uint32) uint32 {
	return p.result(0, p.accessAt(AT_FDCWD, p.str(path), mode, 0))
}

func (p *Process) libcFaccessat(dirfd, path, mode, flags uint32) uint32 {
	return p.result(0, p.onAt(int32(dirfd), p.str(path), flags, func(f Object) error {
		return nil
	}, func(d Object, follow bool) error {
		return p.access(d, p.str(path), mode, flags)
	}))
}

func (p *Process) libcTruncate(path uint32, size uint64) uint32 {
	return p.result(0, p.onPath(AT_FDCWD, p.str(path), func(d Object) error {
		return p.dirTruncate(d, p.str(path), int64(size))
	}))
}

func (p *Process) libcStatvfs(path, at uint32) uint32 {
	var st Statfs

	err := p.statfsPath(p.str(path), &st)

	if err == nil {
		p.putStatvfs(at, &st)
	}

	return p.result(0, err)
}

func (p *Process) libcFstatvfs(fd, at uint32) uint32 {
	n, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	var st Statfs

	if err := p.request(n, &FileStatfs{st: &st}); err != nil {
		return p.result(0, err)
	}

	p.putStatvfs(at, &st)

	return p.result(0, nil)
}

func (p *Process) setCwd(d Object) {
	old := p.cwd

	p.cwd = d

	if old != nil {
		old.unref()
	}
}

func (p *Process) directory(path string) (Object, error) {
	var nd Object

	err := p.onPath(AT_FDCWD, path, func(d Object) error {
		f, err := p.dirOpen(d, path, O_PATH|O_DIRECTORY, 0)

		if err != nil {
			return err
		}

		h := f

		if err := p.access(h, "", 1, 0); err != nil {
			h.unref()

			return err
		}

		nd = h

		return nil
	})

	return nd, err
}

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

func (p *Process) libcAttach(fd uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	obj.ref()

	d, err := served(p, openedDir, O_RDONLY, 0, 0, 0, POLLIN|POLLOUT, obj)

	if err != nil {
		obj.unref()

		return p.result(0, err)
	}

	return p.result(p.install(d, 0, false))
}

func (p *Process) libcWatchdir(fd, flags uint32) uint32 {
	if flags&^(O_CLOEXEC|O_NONBLOCK) != 0 {
		return p.result(0, EINVAL)
	}

	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	r := DirWatch{flags: O_RDONLY | flags&O_NONBLOCK}

	if err := p.request(obj, &r); err != nil {
		return p.result(0, err)
	}

	return p.result(p.install(r.file, 0, flags&O_CLOEXEC != 0))
}

func (p *Process) libcFchdir(fd uint32) uint32 {
	return p.result(0, p.fchdir(fd))
}

func (p *Process) fchdir(fd uint32) error {
	obj, err := p.fd(fd)

	if err != nil {
		return err
	}

	d := obj

	d.ref()

	var st Stat

	if err := p.dirStat(d, "", true, &st); err != nil || st.Mode&S_IFMT != S_IFDIR || !p.permits(&st, 1) {
		d.unref()

		switch {
		case err != nil:
		case st.Mode&S_IFMT != S_IFDIR:
			err = ENOTDIR
		default:
			err = EACCES
		}

		return err
	}

	p.setCwd(d)

	return nil
}

func (p *Process) libcChroot(path uint32) uint32 {
	d, err := p.directory(p.str(path))

	if err != nil {
		return p.result(0, err)
	}

	p.root.unref()
	p.root = d

	return p.result(0, nil)
}

func (p *Process) cwdPath() (string, error) {
	cwd, err := p.dirPath(p.cwd)

	if err == nil && len(cwd) >= 4096 {
		err = ENAMETOOLONG
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
			return p.pointer(0, ENOMEM)
		}

		return p.pointer(at, nil)
	}

	if uint32(len(cwd)) >= size {
		return p.pointer(0, ERANGE)
	}

	p.putString(buf, cwd)

	return p.pointer(buf, nil)
}

func (p *Process) libcMkdir(path, mode uint32) uint32 {
	return p.result(0, p.mkdir(AT_FDCWD, p.str(path), mode))
}

func (p *Process) libcMkdirat(dirfd, path, mode uint32) uint32 {
	return p.result(0, p.mkdir(int32(dirfd), p.str(path), mode))
}

func (p *Process) libcRmdir(path uint32) uint32 {
	return p.result(0, p.remove(AT_FDCWD, p.str(path), true))
}

func (p *Process) libcUnlink(path uint32) uint32 {
	return p.result(0, p.remove(AT_FDCWD, p.str(path), false))
}

func (p *Process) libcUnlinkat(dirfd, path, flags uint32) uint32 {
	if flags&^AT_REMOVEDIR != 0 {
		return p.result(0, EINVAL)
	}

	return p.result(0, p.remove(int32(dirfd), p.str(path), flags&AT_REMOVEDIR != 0))
}

func (p *Process) libcRemove(path uint32) uint32 {
	err := p.remove(AT_FDCWD, p.str(path), false)

	if err == EISDIR || err == EPERM {
		err = p.remove(AT_FDCWD, p.str(path), true)
	}

	return p.result(0, err)
}

func (p *Process) libcRename(old, new uint32) uint32 {
	return p.result(0, p.rename(AT_FDCWD, p.str(old), AT_FDCWD, p.str(new)))
}

func (p *Process) libcRenameat(olddir, old, newdir, new uint32) uint32 {
	return p.result(0, p.rename(int32(olddir), p.str(old), int32(newdir), p.str(new)))
}

func (p *Process) libcLink(old, new uint32) uint32 {
	return p.result(0, p.link(AT_FDCWD, p.str(old), AT_FDCWD, p.str(new), false))
}

func (p *Process) libcLinkat(olddir, old, newdir, new, flags uint32) uint32 {
	if flags&^AT_SYMLINK_FOLLOW != 0 {
		return p.result(0, EINVAL)
	}

	return p.result(0, p.link(int32(olddir), p.str(old), int32(newdir), p.str(new), flags&AT_SYMLINK_FOLLOW != 0))
}

func (p *Process) libcSymlink(target, path uint32) uint32 {
	return p.result(0, p.symlink(p.str(target), AT_FDCWD, p.str(path)))
}

func (p *Process) libcSymlinkat(target, dirfd, path uint32) uint32 {
	return p.result(0, p.symlink(p.str(target), int32(dirfd), p.str(path)))
}

func (p *Process) readlinkAt(dirfd int32, path string, buf, size uint32) (int, error) {
	if p.chaosFail("readlink") {
		return 0, EIO
	}

	var target string

	err := p.onPath(dirfd, path, func(d Object) error {
		var err error
		target, err = p.dirReadlink(d, path)

		return err
	})

	if err != nil {
		return 0, err
	}

	return copy(p.span(buf, size), target), nil
}

func (p *Process) libcReadlink(path, buf, size uint32) uint32 {
	return p.result(p.readlinkAt(AT_FDCWD, p.str(path), buf, size))
}

func (p *Process) libcReadlinkat(dirfd, path, buf, size uint32) uint32 {
	if p.u8(path) != 0 {
		return p.result(p.readlinkAt(int32(dirfd), p.str(path), buf, size))
	}

	obj, err := p.fd(dirfd)

	if dirfd == uint32(AT_FDCWD&0xffffffff) {
		obj, err = p.cwd, nil
	}

	if err != nil {
		return p.result(0, err)
	}

	if fl, _ := p.fileFlags(obj); fl&O_PATH == 0 {
		return p.result(0, ENOENT)
	}

	target, err := p.dirReadlink(obj, "")

	if err != nil {
		return p.result(0, err)
	}

	return p.result(copy(p.span(buf, size), target), nil)
}

func (p *Process) realpath(path string) (string, error) {
	var out string

	err := p.onPath(AT_FDCWD, path, func(d Object) error {
		var err error
		out, err = p.dirRealpath(d, path)

		return err
	})

	if err == nil && len(out) >= 4096 {
		err = ENAMETOOLONG
	}

	return out, err
}

func (p *Process) libcCreat(path, mode uint32) uint32 {
	f, err := p.openPath(p.str(path), O_WRONLY|O_CREAT|O_TRUNC, mode&0o7777&^p.umask)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(p.install(f, 0, false))
}

func (p *Process) libcEaccess(path, mode uint32) uint32 {
	return p.result(0, p.accessAt(AT_FDCWD, p.str(path), mode, AT_EACCESS))
}

func (p *Process) libcStatfs(path, at uint32) uint32 {
	var st Statfs

	err := p.statfsPath(p.str(path), &st)

	if err == nil {
		p.putStatfsOf(at, &st)
	}

	return p.result(0, err)
}

func (p *Process) libcFstatfs(fd, at uint32) uint32 {
	n, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	var st Statfs

	if err := p.request(n, &FileStatfs{st: &st}); err != nil {
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
			return p.pointer(0, ENOMEM)
		}

		return p.pointer(resolved, nil)
	}

	p.putString(resolved, guest)

	return p.pointer(resolved, nil)
}

func (p *Process) libcChmod(path, mode uint32) uint32 {
	return p.result(0, p.onPath(AT_FDCWD, p.str(path), func(d Object) error {
		return p.dirChmod(d, p.str(path), mode&0o7777, true)
	}))
}

func (p *Process) libcFchmodat(dirfd, path, mode, flags uint32) uint32 {
	return p.fchmodat(int32(dirfd), path, mode, flags)
}

func (p *Process) fchmodat(dirfd int32, path, mode, flags uint32) uint32 {
	return p.result(0, p.onAt(dirfd, p.str(path), flags, func(a Object) error {
		return p.request(a, &FileChmod{mode: mode & 0o7777})
	}, func(d Object, follow bool) error {
		return p.dirChmod(d, p.str(path), mode&0o7777, follow)
	}))
}

func (p *Process) libcFchownat(dirfd, path, uid, gid, flags uint32) uint32 {
	return p.result(0, p.onAt(int32(dirfd), p.str(path), flags, func(a Object) error {
		return p.request(a, &FileChown{uid: int(int32(uid)), gid: int(int32(gid))})
	}, func(d Object, follow bool) error {
		return p.dirChown(d, p.str(path), int(int32(uid)), int(int32(gid)), follow)
	}))
}

func (p *Process) libcMknod(path, mode uint32, dev uint64) uint32 {
	return p.result(0, p.mknod(AT_FDCWD, p.str(path), mode, dev))
}

func (p *Process) libcMknodat(dirfd, path, mode uint32, dev uint64) uint32 {
	return p.result(0, p.mknod(int32(dirfd), p.str(path), mode, dev))
}

func (p *Process) libcMkfifo(path, mode uint32) uint32 {
	return p.result(0, p.mknod(AT_FDCWD, p.str(path), S_IFIFO|mode&0o7777, 0))
}

func (p *Process) libcMkfifoat(dirfd, path, mode uint32) uint32 {
	return p.result(0, p.mknod(int32(dirfd), p.str(path), S_IFIFO|mode&0o7777, 0))
}

func (p *Process) libcUtimes(path, times uint32) uint32 {
	return p.result(0, p.utimens(AT_FDCWD, p.str(path), p.timevals(times), true))
}

func (p *Process) libcLutimes(path, times uint32) uint32 {
	return p.result(0, p.utimens(AT_FDCWD, p.str(path), p.timevals(times), false))
}

func (p *Process) libcUtime(path, times uint32) uint32 {
	var ts []Timespec

	if times != 0 {
		ts = []Timespec{{Sec: int64(p.u64(times))}, {Sec: int64(p.u64(times + 8))}}
	}

	return p.result(0, p.utimens(AT_FDCWD, p.str(path), ts, true))
}

func (p *Process) libcUtimensat(dirfd, path, times, flags uint32) uint32 {
	if path == 0 {
		n, err := p.fd(dirfd)

		if err == nil {
			err = p.request(n, &FileUtimes{ts: p.timespecs(times)})
		}

		return p.result(0, err)
	}

	return p.result(0, p.utimens(int32(dirfd), p.str(path), p.timespecs(times), flags&AT_SYMLINK_NOFOLLOW == 0))
}

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
		return p.mkdir(AT_FDCWD, path, 0o700)
	})

	return p.pointer(template, err)
}

func (p *Process) libcMktemp(template uint32) uint32 {
	err := p.temporary(template, 0, func(path string) error {
		var st Stat

		err := p.statPath(path, false, &st)

		if err == ENOENT {
			return nil
		}

		if err == nil {
			return EEXIST
		}

		return err
	})

	if err != nil {
		p.span(template, 1)[0] = 0
		p.setErrno(errnoOf(err))
	}

	return p.pointer(template, nil)
}

func (p *Process) timevals(at uint32) []Timespec {
	if at == 0 {
		return nil
	}

	b := p.span(at, 32)
	le := binary.LittleEndian

	var ts []Timespec

	for i := range 2 {
		ts = append(ts, Timespec{Sec: int64(le.Uint64(b[16*i:])), Nsec: 1000 * int64(le.Uint64(b[16*i+8:]))})
	}

	return ts
}

func (p *Process) stat(path, at uint32, follow bool) uint32 {
	var st Stat

	err := p.statPath(p.str(path), follow, &st)

	if err == nil {
		p.putStat(at, &st)
	}

	return p.result(0, err)
}

func (p *Process) chown(path, uid, gid uint32, follow bool) uint32 {
	return p.result(0, p.onPath(AT_FDCWD, p.str(path), func(d Object) error {
		return p.dirChown(d, p.str(path), int(int32(uid)), int(int32(gid)), follow)
	}))
}

func (p *Process) utimens(dirfd int32, path string, ts []Timespec, follow bool) error {
	return p.onPath(dirfd, path, func(d Object) error {
		return p.dirUtimes(d, path, ts, follow)
	})
}

func (p *Process) temporary(template, suffix uint32, create func(path string) error) error {
	t := p.cstr(template)
	end := len(t) - int(suffix)

	if int(suffix) > len(t) || end < 6 || string(t[end-6:end]) != "XXXXXX" {
		return EINVAL
	}

	for range 100 {
		for i := end - 6; i < end; i++ {
			t[i] = tempLetters[mathrand.IntN(len(tempLetters))]
		}

		if err := create(string(t)); err != EEXIST {
			return err
		}
	}

	return EEXIST
}

func (p *Process) mkstemp(template, suffix, flags uint32) uint32 {
	fd := -1

	err := p.temporary(template, suffix, func(path string) error {
		f, err := p.openPath(path, O_RDWR|O_CREAT|O_EXCL|int(flags&^O_CLOEXEC), 0o600)

		if err == nil {
			fd, err = p.install(f, 0, flags&O_CLOEXEC != 0)
		}

		return err
	})

	return p.result(fd, err)
}

func (p *Process) libcOpendir(path uint32) uint32 {
	f, err := p.openPath(p.str(path), O_RDONLY|O_DIRECTORY, 0)

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

		return p.pointer(0, ENOMEM)
	}

	return p.pointer(dir, nil)
}

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

func (p *Process) libcFdopendir(fd uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.pointer(0, err)
	}

	var st Stat

	if err := statFile(p, obj, &st); err != nil {
		return p.pointer(0, err)
	}

	if st.Mode&S_IFMT != S_IFDIR {
		return p.pointer(0, ENOTDIR)
	}

	dir := p.newDir(int(fd))

	if dir == 0 {
		return p.pointer(0, ENOMEM)
	}

	p.files.slots[fd].cloexec = true

	return p.pointer(dir, nil)
}

func (p *Process) rewinddir(dir uint32) {
	if s, err := p.fd(p.u32(dir + dirFd)); err == nil {
		p.fileSeek(s, 0, 0)
	}

	p.put32(dir+dirPos, 0)
	p.put32(dir+dirEnd, 0)
	p.put32(dir+dirIndex, 0)
}

func (p *Process) direntCompare(a, b uint32, compare func(x, y []byte) int) uint32 {
	x, y := p.cstr(p.u32(a)+19), p.cstr(p.u32(b)+19)

	return uint32(int32(compare(x, y)))
}

func (p *Process) libcReaddir(dir uint32) uint32 {
	entry, err := p.readdir(dir)

	return p.pointer(entry, err)
}

func (p *Process) readdir(dir uint32) (uint32, error) {
	pos, end := p.u32(dir+dirPos), p.u32(dir+dirEnd)

	if pos >= end {
		d, err := p.fd(p.u32(dir + dirFd))

		if err != nil {
			return 0, err
		}

		w := p.memory(dir+dirBuf, dirBufLen, O_WRONLY)
		n, err := p.fileReaddir(d, w, dirBufLen)

		p.memoryDone(w)

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
		return p.result(0, EBADF)
	}

	return p.result(0, nil)
}

func (p *Process) libcStat(path, at uint32) uint32 {
	return p.stat(path, at, true)
}

func (p *Process) libcLstat(path, at uint32) uint32 {
	return p.stat(path, at, false)
}

func (p *Process) libcAcct(path uint32) uint32 {
	return p.result(0, EPERM)
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

func (p *Process) libcMount(source, target, fstype, flags, data uint32) uint32 {
	from, to := p.str(source), p.str(target)

	for _, path := range []string{from, to} {
		if err := checked(path); err != nil {
			return p.result(0, err)
		}
	}

	err := p.onPath(AT_FDCWD, ".", func(v Object) error {
		var st Stat

		if p.dirStat(v, from, true, &st) != nil || st.Mode&S_IFMT != S_IFSOCK {
			return p.dirMount(v, to, from, nil)
		}

		conn, err := p.connectUnix(v, from, SOCK_SEQPACKET, false, new(atomic.Int64))

		if err != nil {
			return err
		}

		defer conn.unref()

		return p.dirMount(v, to, from, conn)
	})

	return p.result(0, err)
}

func (p *Process) libcUmount2(target, flags uint32) uint32 {
	err := p.onPath(AT_FDCWD, p.str(target), func(v Object) error {
		return p.dirUnmount(v, p.str(target), flags&UMOUNT_NOFOLLOW)
	})

	return p.result(0, err)
}

func (p *Process) libcUmount(target uint32) uint32 {
	return p.libcUmount2(target, 0)
}

func (p *Process) openFifo(st *Stat, flags int) (Object, error) {
	access := flags & O_ACCMODE
	reads, writes := access != O_WRONLY, access != O_RDONLY
	nonblock := flags&O_NONBLOCK != 0
	key := FileKey{st.Dev, st.Ino}
	k := p.k

	k.mu.Lock()

	pp := k.fifos[key]

	if pp == nil {
		pp = newPipe()
		pp.name = &FifoName{k: k, key: key, mode: st.Mode}

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

			return nil, ENXIO
		}
	}

	readerOpens, writerOpens := pp.join(reads, writes)

	k.mu.Unlock()

	e := &PipeEnd{pipe: pp, reads: reads, writes: writes}

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

func (p *Process) pipe(at, flags uint32) uint32 {
	if flags&^(O_CLOEXEC|O_NONBLOCK) != 0 {
		return p.result(0, EINVAL)
	}

	pp := newPipe()

	pp.readers, pp.writers = 1, 1

	ends := [2]*PipeEnd{{pipe: pp, reads: true}, {pipe: pp, writes: true}}
	cloexec := flags&O_CLOEXEC != 0

	var fds [2]int

	for i, e := range ends {
		e.refs.Store(1)
		e.nonblock.Store(flags&O_NONBLOCK != 0)

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

func (p *Process) libcMemfile(at, n, flags uint32) uint32 {
	mode := int(flags & O_ACCMODE)

	if flags&^(O_ACCMODE|O_CLOEXEC) != 0 || mode == O_ACCMODE {
		return p.result(0, EINVAL)
	}

	i := p.inst

	if i.memory == nil || uint64(at)+uint64(n) > i.memory.pages*pageSize {
		return p.result(0, EFAULT)
	}

	f := newMemfd(i.memory, int64(at), int64(n), mode)
	fd, err := p.install(f, 0, flags&O_CLOEXEC != 0)

	if err != nil {
		f.unref()

		return p.result(0, err)
	}

	return p.result(fd, nil)
}

func (p *Process) putAddress(addr []byte, at, lenAt uint32) {
	if at == 0 || lenAt == 0 {
		return
	}

	room := p.u32(lenAt)

	copy(p.span(at, min(room, uint32(len(addr)))), addr)
	p.put32(lenAt, uint32(len(addr)))
}

func (p *Process) libcSocket(domain, kind, protocol uint32) uint32 {
	if domain == AF_INET || domain == AF_INET6 {
		return p.result(p.inetSocket(domain, kind, protocol))
	}

	if domain != AF_UNIX {
		return p.result(0, EAFNOSUPPORT)
	}

	if kind&^(sockTypeMask|SOCK_NONBLOCK|SOCK_CLOEXEC) != 0 {
		return p.result(0, EINVAL)
	}

	if protocol != 0 {
		return p.result(0, EPROTONOSUPPORT)
	}

	nonblock, cloexec := kind&SOCK_NONBLOCK != 0, kind&SOCK_CLOEXEC != 0

	switch kind & sockTypeMask {
	case SOCK_STREAM, SOCK_SEQPACKET:
		return p.result(p.install(newUnixSocket(int(kind&sockTypeMask), nonblock), 0, cloexec))
	case SOCK_DGRAM, SOCK_RAW:
		return p.result(p.install(newUnixDgram(nonblock), 0, cloexec))
	}

	return p.result(0, ESOCKTNOSUPPORT)
}

func (p *Process) inetSocket(domain, kind, protocol uint32) (int, error) {
	if kind&^(sockTypeMask|SOCK_NONBLOCK|SOCK_CLOEXEC) != 0 {
		return 0, EINVAL
	}

	obj, err := p.dirOpen(p.root, sockmgrSocket, O_RDWR|int(bit(kind&SOCK_NONBLOCK != 0))*O_NONBLOCK, 0)

	if err == ENOENT {
		return 0, EAFNOSUPPORT
	}

	if err != nil {
		return 0, err
	}

	if err := p.request(obj, &SockCreate{domain: domain, kind: kind &^ SOCK_CLOEXEC, protocol: protocol}); err != nil {
		obj.unref()

		return 0, err
	}

	return p.install(newIndirect(obj), 0, kind&SOCK_CLOEXEC != 0)
}

func (p *Process) libcSocketpair(domain, kind, protocol, at uint32) uint32 {
	if domain != AF_UNIX {
		return p.result(0, EOPNOTSUPP)
	}

	if kind&^(sockTypeMask|SOCK_NONBLOCK|SOCK_CLOEXEC) != 0 {
		return p.result(0, EINVAL)
	}

	if protocol != 0 {
		return p.result(0, EPROTONOSUPPORT)
	}

	base := int(kind & sockTypeMask)

	switch base {
	case SOCK_STREAM, SOCK_DGRAM, SOCK_SEQPACKET:
	case SOCK_RAW:
		base = SOCK_DGRAM
	default:
		return p.result(0, ESOCKTNOSUPPORT)
	}

	a, b := newPairOf(base, kind&SOCK_NONBLOCK != 0)
	cloexec := kind&SOCK_CLOEXEC != 0
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
	s, err := p.fd(fd)

	if err == nil {
		err = p.request(s, &SockBind{addr: p.socketAddress(addr, n)})
	}

	return p.result(0, err)
}

func (p *Process) libcConnect(fd, addr, n uint32) uint32 {
	s, err := p.fd(fd)

	if err == nil {
		err = p.request(s, &SockConnect{addr: p.socketAddress(addr, n)})
	}

	return p.result(0, err)
}

func (p *Process) libcListen(fd, backlog uint32) uint32 {
	s, err := p.fd(fd)

	if err == nil {
		err = p.request(s, &SockListen{backlog: int(int32(backlog))})
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
	s, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	name, err := p.sockName(s, false)

	if err != nil {
		return p.result(0, err)
	}

	p.putAddress(name, addr, lenAt)

	return p.result(0, nil)
}

func (p *Process) libcGetpeername(fd, addr, lenAt uint32) uint32 {
	s, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	name, err := p.sockName(s, true)

	if err != nil {
		return p.result(0, err)
	}

	p.putAddress(name, addr, lenAt)

	return p.result(0, nil)
}

func (p *Process) libcGetsockopt(fd, level, name, at, lenAt uint32) uint32 {
	s, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	value, err := p.sockGetopt(s, level, name)

	if err == nil {
		length := min(p.u32(lenAt), uint32(len(value)))

		copy(p.span(at, length), value)
		p.put32(lenAt, length)
	}

	return p.result(0, err)
}

func (p *Process) libcSetsockopt(fd, level, name, at, n uint32) uint32 {
	s, err := p.fd(fd)

	if err == nil {
		err = p.request(s, &SockSetopt{level: level, name: name, value: slices.Clone(p.span(at, min(n, 16)))})
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
	return p.result(p.sendmsg(fd, msg, flags))
}

func (p *Process) sendmsg(fd, msg, flags uint32) (int, error) {
	s, err := p.fd(fd)

	if err != nil {
		return 0, err
	}

	h := p.span(msg, sizeofMsghdr)
	to := p.destination(binary.LittleEndian.Uint32(h), binary.LittleEndian.Uint32(h[4:]))
	pk := p.packet()
	files, cred, err := p.rights(pk.files[:0], binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:]))

	if err != nil {
		return 0, err
	}

	iov, count := binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:])
	data, err := p.gatherIn(pk.data[:cap(pk.data)], iov, count, packetMax)

	if err == EMSGSIZE && p.sockKind(s) == SOCK_STREAM {
		data, err = p.gather(iov, count, math.MaxInt)
	}

	if err != nil {
		return 0, err
	}

	pk.data, pk.files, pk.cred = data, files, cred

	return p.sockSend(s, pk, to, sendMode(flags))
}

func (p *Process) libcSendmmsg(fd, vec, vlen, flags uint32) uint32 {
	if _, err := p.fd(fd); err != nil {
		return p.result(0, err)
	}

	vlen = min(vlen, UIO_MAXIOV)

	for i := range vlen {
		at := vec + i*sizeofMmsghdr
		n, err := p.sendmsg(fd, at, flags)

		if err != nil {
			if i == 0 {
				return p.result(0, err)
			}

			return i
		}

		p.put32(at+sizeofMsghdr, uint32(n))
	}

	return vlen
}

func (p *Process) packet() *Packet {
	k := len(p.packets)

	if k == 0 {
		return &Packet{}
	}

	pk := p.packets[k-1]

	p.packets = p.packets[:k-1]

	return pk
}

func (p *Process) packetDone(pk *Packet) {
	if pk == nil || len(p.packets) >= packetsKept {
		return
	}

	data, files := pk.data[:0], pk.files[:0]

	clear(pk.files)

	if cap(data) > packetRoomKept {
		data = nil
	}

	*pk = Packet{data: data, files: files}
	p.packets = append(p.packets, pk)
}

func sendMode(flags uint32) IoMode {
	return IoMode{nonblock: flags&MSG_DONTWAIT != 0, quiet: flags&MSG_NOSIGNAL != 0}
}

func (p *Process) libcRecvmsg(fd, msg, flags uint32) uint32 {
	return p.result(p.recvmsg(fd, msg, flags))
}

func (p *Process) recvmsg(fd, msg, flags uint32) (int, error) {
	var few [4][]byte

	s, err := p.fd(fd)

	if err != nil {
		return 0, err
	}

	h := p.span(msg, sizeofMsghdr)
	name, nameRoom := binary.LittleEndian.Uint32(h), binary.LittleEndian.Uint32(h[4:])
	iov, count := binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:])
	control, room := binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:])
	spans, err := p.ioSpansIn(few[:0], iov, count)

	if err != nil {
		return 0, err
	}

	r := p.recvs.get()

	defer p.recvs.put(r)

	r.into, r.want, r.flags = Into{p: p, at: iov, n: count, vector: true}, totalLength(spans), flags

	if err := p.request(s, r); err != nil {
		return 0, err
	}

	written, cut := p.putControl(r.files, r.cred, control, room, flags&MSG_CMSG_CLOEXEC != 0)

	if name != 0 {
		copy(p.span(name, min(nameRoom, uint32(len(r.addr)))), r.addr)
	}

	h = p.span(msg, sizeofMsghdr)

	if name != 0 {
		binary.LittleEndian.PutUint32(h[4:], uint32(len(r.addr)))
	}

	binary.LittleEndian.PutUint32(h[20:], written)
	binary.LittleEndian.PutUint32(h[24:], r.out|cut|flags&MSG_CMSG_CLOEXEC)
	p.packetDone(r.pk)

	return r.n, nil
}

func (p *Process) libcRecvmmsg(fd, vec, vlen, flags, timeout uint32) uint32 {
	s, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	var end time.Time

	if timeout != 0 {
		d, err := p.timespec(timeout)

		if err != nil {
			return p.result(0, err)
		}

		end = time.Now().Add(d)
	}

	vlen = min(vlen, UIO_MAXIOV)

	for i := range vlen {
		at := vec + i*sizeofMmsghdr
		mode := flags &^ MSG_WAITFORONE

		if i > 0 && flags&MSG_WAITFORONE != 0 {
			mode |= MSG_DONTWAIT
		}

		n, received, err := p.recvmsgBy(s, fd, at, mode, end)

		if !received {
			return i
		}

		if err != nil {
			if i == 0 {
				return p.result(0, err)
			}

			return i
		}

		p.put32(at+sizeofMsghdr, uint32(n))
	}

	return vlen
}

func (p *Process) recvmsgBy(s Object, fd, msg, flags uint32, end time.Time) (int, bool, error) {
	if end.IsZero() || flags&MSG_DONTWAIT != 0 {
		n, err := p.recvmsg(fd, msg, flags)

		return n, true, err
	}

	for {
		wait := 0

		if left := time.Until(end); left > 0 {
			wait = waitMs(left)
		}

		ready, err := p.waitObjects([]PollEntry{{obj: s, events: POLLIN}}, wait, false)

		if err != nil || ready == 0 {
			return 0, err != nil, err
		}

		if n, err := p.recvmsg(fd, msg, flags|MSG_DONTWAIT); err != EAGAIN {
			return n, true, err
		}
	}
}

func (p *Process) libcShutdown(fd, how uint32) uint32 {
	s, err := p.fd(fd)

	if err == nil {
		err = p.request(s, &SockShutdown{how: int(how)})
	}

	return p.result(0, err)
}

func (p *Process) libcSockatmark(fd uint32) uint32 {
	s, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	switch p.sockKind(s) {
	case 0:
		return p.result(0, ENOTSOCK)
	case SOCK_STREAM:
		return p.result(0, nil)
	}

	return p.result(0, EOPNOTSUPP)
}

func (p *Process) socketAccept(fd, addr, lenAt, flags uint32) uint32 {
	s, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	if flags&^(SOCK_NONBLOCK|SOCK_CLOEXEC) != 0 {
		return p.result(0, EINVAL)
	}

	if err := p.descriptorFree(); err != nil {
		return p.result(0, err)
	}

	conn, peer, err := p.sockAccept(s, flags)

	if err != nil {
		return p.result(0, err)
	}

	guest, err := p.install(conn, 0, flags&SOCK_CLOEXEC != 0)

	if err != nil {
		return p.result(0, err)
	}

	p.putAddress(peer, addr, lenAt)

	return p.result(guest, nil)
}

func (p *Process) socketSend(fd, buf, n, flags, addr, alen uint32) uint32 {
	s, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	to := p.destination(addr, alen)

	return p.result(p.sockSend(s, &Packet{data: slices.Clone(p.span(buf, n))}, to, sendMode(flags)))
}

func (p *Process) socketRecv(fd, buf, n, flags, addr, lenAt uint32) uint32 {
	s, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	r := p.recvs.get()

	defer p.recvs.put(r)

	r.into, r.want, r.flags = Into{p: p, at: buf, n: n}, int(n), flags
	err = p.request(s, r)

	dropFiles(r.files)

	if err == nil && addr != 0 {
		p.putAddress(r.addr, addr, lenAt)
	}

	p.packetDone(r.pk)

	return p.result(r.n, err)
}

func (p *Process) putControl(files []Object, cred *Ucred, control, room uint32, cloexec bool) (uint32, uint32) {
	written, cut := p.putRights(files, control, room, cloexec)

	if cred == nil {
		return written, cut
	}

	size := sizeofCmsghdr + uint32(sizeofUcred)

	if room-written < size {
		return written, cut | MSG_CTRUNC
	}

	out := p.span(control+written, size)

	binary.LittleEndian.PutUint32(out, size)
	binary.LittleEndian.PutUint32(out[4:], SOL_SOCKET)
	binary.LittleEndian.PutUint32(out[8:], SCM_CREDENTIALS)
	binary.LittleEndian.PutUint32(out[12:], cred.pid)
	binary.LittleEndian.PutUint32(out[16:], cred.uid)
	binary.LittleEndian.PutUint32(out[20:], cred.gid)

	return written + min(guestAlign(size), room-written), cut
}

func (p *Process) ucred() *Ucred {
	return &Ucred{uint32(p.pid), p.cred.uid(), p.cred.gid()}
}

func (p *Process) rights(files []Object, at, n uint32) ([]Object, *Ucred, error) {
	var cred *Ucred

	if n < sizeofCmsghdr {
		return files, nil, nil
	}

	control := p.span(at, n)

	for off := uint32(0); off+sizeofCmsghdr <= n; {
		length := binary.LittleEndian.Uint32(control[off:])

		if length < sizeofCmsghdr || off+length > n {
			return nil, nil, EINVAL
		}

		level, kind := binary.LittleEndian.Uint32(control[off+4:]), binary.LittleEndian.Uint32(control[off+8:])
		data := control[off+sizeofCmsghdr : off+length]

		switch {
		case level == SOL_SOCKET && kind == SCM_RIGHTS:
			for i := 0; i+4 <= len(data); i += 4 {
				f, err := p.fd(binary.LittleEndian.Uint32(data[i:]))

				if err != nil {
					return nil, nil, err
				}

				files = append(files, f)
			}
		case level == SOL_SOCKET && kind == SCM_CREDENTIALS && len(data) == sizeofUcred:
			c := Ucred{binary.LittleEndian.Uint32(data), binary.LittleEndian.Uint32(data[4:]), binary.LittleEndian.Uint32(data[8:])}

			if !p.cred.privileged() && (c.pid != uint32(p.pid) || !slices.Contains(p.cred.uids[:], c.uid) || !slices.Contains(p.cred.gids[:], c.gid)) {
				return nil, nil, EPERM
			}

			cred = &c
		default:
			return nil, nil, EINVAL
		}

		off += guestAlign(length)
	}

	return files, cred, nil
}

func (p *Process) putRights(files []Object, at, room uint32, cloexec bool) (uint32, uint32) {
	if len(files) == 0 {
		return 0, 0
	}

	fits := 0

	if room >= sizeofCmsghdr {
		fits = min(len(files), int((room-sizeofCmsghdr)/4))
	}

	var out []byte

	if fits > 0 {
		out = p.span(at, sizeofCmsghdr+uint32(4*fits))
	}

	flags, put := uint32(0), uint32(0)

	for i, f := range files {
		if i >= fits {
			f.unref()
			flags |= MSG_CTRUNC

			continue
		}

		fd, err := p.install(f, 0, cloexec)

		if err != nil {
			flags |= MSG_CTRUNC

			continue
		}

		binary.LittleEndian.PutUint32(out[sizeofCmsghdr+4*put:], uint32(fd))
		put++
	}

	if put == 0 {
		return 0, flags
	}

	size := sizeofCmsghdr + 4*put

	binary.LittleEndian.PutUint32(out, size)
	binary.LittleEndian.PutUint32(out[4:], SOL_SOCKET)
	binary.LittleEndian.PutUint32(out[8:], SCM_RIGHTS)

	return min(guestAlign(size), room), flags
}

func (p *Process) unixSocketPath(at, n uint32) (string, error) {
	if n <= 2 || n > sizeofSockaddrStorage || p.u16(at) != AF_UNIX {
		return "", EINVAL
	}

	path := p.span(at+2, n-2)

	if path[0] == 0 {
		return "", EOPNOTSUPP
	}

	if i := bytes.IndexByte(path, 0); i >= 0 {
		path = path[:i]
	}

	if len(path) == 0 || len(path) >= sunPathMax {
		return "", EINVAL
	}

	return string(path), nil
}

func (p *Process) bindName(path string, held Object) (*SocketName, error) {
	var name *SocketName

	err := p.onPath(AT_FDCWD, path, func(v Object) error {
		var st Stat

		err := p.dirStat(v, path, false, &st)

		if err == ENOENT {
			if err = p.dirMknod(v, path, S_IFSOCK|0o777&^p.umask); err != nil {
				return err
			}

			err = p.dirStat(v, path, false, &st)
		}

		switch {
		case err != nil:
			return err
		case st.Mode&S_IFMT != S_IFSOCK:
			return EADDRINUSE
		}

		key := FileKey{st.Dev, st.Ino}

		p.k.mu.Lock()

		defer p.k.mu.Unlock()

		if p.k.sockets[key] != nil {
			return EADDRINUSE
		}

		held.ref()
		p.k.sockets[key] = held
		name = &SocketName{k: p.k, key: key}

		return nil
	})

	if err == EEXIST {
		err = EADDRINUSE
	}

	return name, err
}

func (p *Process) boundAt(v Object, path string) (Object, error) {
	var st Stat

	if err := p.dirStat(v, path, true, &st); err != nil {
		return nil, err
	}

	if st.Mode&S_IFMT != S_IFSOCK {
		return nil, ECONNREFUSED
	}

	if !p.permits(&st, 2) {
		return nil, EACCES
	}

	p.k.mu.Lock()

	held := p.k.sockets[FileKey{st.Dev, st.Ino}]

	if held != nil {
		held.ref()
	}

	p.k.mu.Unlock()

	if held == nil {
		return nil, ECONNREFUSED
	}

	return held, nil
}

func (p *Process) connectUnix(v Object, path string, kind int, nonblock bool, to *atomic.Int64) (*PairEnd, error) {
	f, err := p.boundAt(v, path)

	if err != nil {
		return nil, err
	}

	defer f.unref()

	r := SockKnock{kind: kind, nonblock: nonblock, timeout: to}

	err = p.request(f, &r)

	return r.conn, err
}

func (p *Process) mouthOf(path string) (Object, error) {
	var mouth Object

	err := p.onPath(AT_FDCWD, path, func(v Object) error {
		f, err := p.boundAt(v, path)

		if err != nil {
			return err
		}

		if p.sockKind(f) != SOCK_DGRAM {
			f.unref()

			return EPROTOTYPE
		}

		mouth = f

		return nil
	})

	return mouth, err
}

func (p *Process) destination(addr, alen uint32) Address {
	if addr == 0 {
		return Address{}
	}

	return p.socketAddress(addr, alen)
}

func (p *Process) socketAddress(at, n uint32) Address {
	path, err := p.unixSocketPath(at, n)
	a := Address{given: true, path: path, err: err}

	if n <= sizeofSockaddrStorage {
		a.raw = slices.Clone(p.span(at, n))
	}

	return a
}

func (p *Process) libcEpoll_create(size uint32) uint32 {
	if int32(size) <= 0 {
		return p.result(0, EINVAL)
	}

	return p.result(p.install(newEpoll(), 0, false))
}

func (p *Process) libcEpoll_create1(flags uint32) uint32 {
	if flags&^O_CLOEXEC != 0 {
		return p.result(0, EINVAL)
	}

	return p.result(p.install(newEpoll(), 0, flags&O_CLOEXEC != 0))
}

func (p *Process) libcEpoll_ctl(epfd, op, fd, at uint32) uint32 {
	ep, err := p.fd(epfd)

	if err != nil {
		return p.result(0, err)
	}

	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	if fd == epfd {
		return p.result(0, EINVAL)
	}

	var events uint32
	var data uint64

	if op != EPOLL_CTL_DEL {
		if at == 0 {
			return p.result(0, EFAULT)
		}

		b := p.span(at, sizeofEpollEvent)

		events, data = binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint64(b[8:])
	}

	return p.result(0, p.epollCtl(ep, op, EpollKey{fd, obj}, events, data))
}

func (p *Process) epollWait(epfd, at, max uint32, timeout int) (int, error) {
	if int32(max) <= 0 || max > epollMaxEvents {
		return 0, EINVAL
	}

	ep, err := p.fd(epfd)

	if err != nil {
		return 0, err
	}

	return p.epollTake(ep, func() []byte {
		return p.span(at, max*sizeofEpollEvent)
	}, int(max), deadline(timeout))
}

func (p *Process) libcEpoll_wait(epfd, at, max, timeout uint32) uint32 {
	return p.result(p.epollWait(epfd, at, max, int(int32(timeout))))
}

func (p *Process) libcEpoll_pwait(epfd, at, max, timeout, mask uint32) uint32 {
	return p.result(p.withMask(mask, func() (int, error) {
		return p.epollWait(epfd, at, max, int(int32(timeout)))
	}))
}

func (p *Process) opened(pk *Packet) (Object, bool, error) {
	f := pk.files[0]

	if len(pk.data) == 0 {
		return f, false, nil
	}

	if len(pk.data) != sizeofOpened {
		f.unref()

		return nil, false, EIO
	}

	obj, err := p.servedOf(f, pk.data)

	return obj, err == nil && binary.LittleEndian.Uint32(pk.data) == openedTerminal, err
}

func (p *Process) servedOf(end Object, b []byte) (Object, error) {
	obj, err := served(p, binary.LittleEndian.Uint32(b), int(binary.LittleEndian.Uint32(b[4:])), binary.LittleEndian.Uint64(b[8:]),
		binary.LittleEndian.Uint64(b[16:]), binary.LittleEndian.Uint64(b[24:]), binary.LittleEndian.Uint32(b[32:]), end)

	if err != nil {
		end.unref()

		return nil, EIO
	}

	return obj, nil
}

func (p *Process) attached(pk *Packet) (Object, []byte, error) {
	if len(pk.files) == 0 || len(pk.data) < sizeofOpened {
		dropFiles(pk.files)

		return nil, nil, EIO
	}

	dropFiles(pk.files[1:])

	obj, err := p.servedOf(pk.files[0], pk.data[:sizeofOpened])

	return obj, pk.data[sizeofOpened:], err
}

func (p *Process) head(data []byte, op, arg, group uint32) {
	binary.LittleEndian.PutUint32(data, op)
	binary.LittleEndian.PutUint32(data[4:], arg)
	binary.LittleEndian.PutUint32(data[8:], p.cred.euid())
	binary.LittleEndian.PutUint32(data[12:], p.cred.egid())
	binary.LittleEndian.PutUint32(data[16:], group)
	binary.LittleEndian.PutUint32(data[20:], uint32(p.sid.Load()))
}

func (p *Process) permits(st *Stat, want uint32) bool {
	if p.cred.privileged() {
		return want&1 == 0 || st.Mode&S_IFMT == S_IFDIR || st.Mode&0o111 != 0
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

func (p *Process) sameTerminal(a, b Object) bool {
	return p.device(a) == p.device(b)
}

func (p *Process) isCtty(t Object) bool {
	p.k.mu.Lock()

	c := p.k.cttys[p.sid.Load()]

	p.k.mu.Unlock()

	return c != nil && p.sameTerminal(c, t)
}

func (p *Process) acquire(t Object) {
	p.k.mu.Lock()

	_, has := p.k.cttys[p.sid.Load()]
	leads := p.sid.Load() == p.pid

	p.k.mu.Unlock()

	if leads && !has {
		p.controls(t)
	}
}

func (p *Process) shrugs(sig int) bool {
	p.sig.mu.Lock()

	defer p.sig.mu.Unlock()

	return p.sig.actions[sig].handler == SIG_IGN || p.sig.blocked&sigBit(sig) != 0
}

func (p *Process) group() uint32 {
	return uint32(p.pgid.Load())
}

func (p *Process) background(sig int) (bool, error) {
	if p.shrugs(sig) {
		if sig == SIGTTIN {
			return false, EIO
		}

		return true, nil
	}

	pgid := p.pgid.Load()

	if p.k.orphaned(pgid) {
		return false, EIO
	}

	p.k.signalGroup(pgid, sig)

	if !p.interrupted() {
		return false, EINTR
	}

	return false, nil
}

func (p *Process) leaveSession(going bool) {
	p.k.mu.Lock()

	t := p.k.cttys[p.sid.Load()]
	leads := p.sid.Load() == p.pid

	if leads && t != nil {
		delete(p.k.cttys, p.sid.Load())
	}

	p.k.mu.Unlock()

	if !leads || t == nil {
		return
	}

	how := uint32(cttyDropped)

	if going {
		how = cttyGone
	}

	p.ttyControl(t, how)
	t.unref()
}

func (p *Process) libcIsatty(fd uint32) uint32 {
	t, err := p.fd(fd)

	if err == nil {
		_, err = p.ttyGetattr(t)
	}

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	return 1
}

func (p *Process) libcTcgetattr(fd, at uint32) uint32 {
	return p.result(0, p.tcgetattr(fd, at))
}

func (p *Process) tcgetattr(fd, at uint32) error {
	t, err := p.fd(fd)

	if err != nil {
		return err
	}

	settings, err := p.ttyGetattr(t)

	if err == nil {
		copy(p.span(at, sizeofTermios), settings)
	}

	return err
}

func (p *Process) libcTcsetattr(fd, action, at uint32) uint32 {
	return p.result(0, p.tcsetattr(fd, action, at))
}

func (p *Process) tcsetattr(fd, action, at uint32) error {
	if action > 2 {
		return EINVAL
	}

	t, err := p.fd(fd)

	if err != nil {
		return err
	}

	return p.ttySetattr(t, action, append([]byte(nil), p.span(at, sizeofTermios)...))
}

func (p *Process) libcTcflow(fd, action uint32) uint32 {
	t, err := p.fd(fd)

	if err == nil {
		err = p.ttyFlow(t, action)
	}

	return p.result(0, err)
}

func (p *Process) libcTcflush(fd, queue uint32) uint32 {
	t, err := p.fd(fd)

	if err == nil {
		err = p.ttyFlush(t, queue)
	}

	return p.result(0, err)
}

func (p *Process) libcTcdrain(fd uint32) uint32 {
	t, err := p.fd(fd)

	if err == nil {
		err = p.ttyDrain(t)
	}

	return p.result(0, err)
}

func (p *Process) libcTcsendbreak(fd, duration uint32) uint32 {
	t, err := p.fd(fd)

	if err == nil {
		err = p.ttyBreak(t)
	}

	return p.result(0, err)
}

func (p *Process) libcTcgetpgrp(fd uint32) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	group, _, err := p.ttyForeground(t)

	return p.result(int(group), err)
}

func (p *Process) libcTcsetpgrp(fd, pgrp uint32) uint32 {
	t, err := p.fd(fd)

	if err == nil && p.device(t) == (FileKey{}) {
		_, _, err = p.ttyForeground(t)
	}

	switch {
	case err != nil:
		return p.result(0, err)
	case int32(pgrp) < 0:
		return p.result(0, EINVAL)
	case !p.groupExists(int32(pgrp)):
		return p.result(0, EPERM)
	}

	return p.result(0, p.ttySetForeground(t, pgrp))
}

func (p *Process) libcTcgetsid(fd uint32) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	_, sid, err := p.ttyForeground(t)

	if err == nil && sid == 0 {
		err = ENOTTY
	}

	return p.result(int(sid), err)
}

func (p *Process) controls(t Object) error {
	p.k.mu.Lock()

	sid := p.sid.Load()
	leads := sid == p.pid
	c := p.k.cttys[sid]

	p.k.mu.Unlock()

	switch {
	case leads && c != nil && p.sameTerminal(c, t):
		return nil
	case !leads || c != nil:
		return EPERM
	}

	if err := p.ttyControl(t, cttyTake); err != nil {
		return err
	}

	t.ref()
	p.k.mu.Lock()
	p.k.cttys[sid] = t
	p.k.mu.Unlock()

	return nil
}

func (p *Process) notty(t Object) error {
	if !p.isCtty(t) {
		return ENOTTY
	}

	p.leaveSession(false)

	return nil
}

func (p *Process) terminalIoctl(t Object, fd, req, arg uint32) (uint32, bool) {
	switch req {
	case TCGETS:
		return p.result(0, p.tcgetattr(fd, arg)), true
	case TCSETS, TCSETS + 1, TCSETS + 2:
		return p.result(0, p.tcsetattr(fd, req-TCSETS, arg)), true
	case TCFLSH:
		return p.libcTcflush(fd, arg), true
	case TCXONC:
		return p.libcTcflow(fd, arg), true
	case TCSBRK:
		if arg == 0 {
			return p.result(0, p.ttyBreak(t)), true
		}

		return p.result(0, p.ttyDrain(t)), true
	case TIOCGWINSZ:
		size, err := p.ttyWindow(t)

		if err == nil {
			copy(p.span(arg, sizeofWinsize), size)
		}

		return p.result(0, err), true
	case TIOCSWINSZ:
		return p.result(0, p.ttySetWindow(t, append([]byte(nil), p.span(arg, sizeofWinsize)...))), true
	case FIONREAD, TIOCOUTQ:
		count := p.ttyPending

		if req == TIOCOUTQ {
			count = p.ttyUnsent
		}

		n, err := count(t)

		if err == nil {
			p.put32(arg, n)
		}

		return p.result(0, err), true
	case TIOCGPTN:
		n, err := p.slaveNumber(t)

		if err == nil {
			p.put32(arg, uint32(n))
		}

		return p.result(0, err), true
	case TIOCSPTLCK:
		_, err := p.ttyName(t, true)

		return p.result(0, err), true
	case TIOCSCTTY:
		return p.result(0, p.controls(t)), true
	case TIOCNOTTY:
		return p.result(0, p.notty(t)), true
	case TIOCGSID:
		r := p.libcTcgetsid(fd)

		if int32(r) >= 0 {
			p.put32(arg, r)
			r = 0
		}

		return r, true
	}

	return 0, false
}

func (p *Process) slaveNumber(t Object) (int, error) {
	name, err := p.ttyName(t, true)

	if err != nil {
		return 0, err
	}

	n, err := strconv.Atoi(strings.TrimPrefix(name, "/dev/pts/"))

	if err != nil {
		return 0, EIO
	}

	return n, nil
}

func (p *Process) terminalName(fd uint32) (string, Errno) {
	t, err := p.fd(fd)

	if err == nil {
		var name string

		if name, err = p.ttyName(t, false); err == nil {
			return name, 0
		}
	}

	return "", errnoOf(err)
}

func (p *Process) libcGrantpt(fd uint32) uint32 {
	_, err := p.fd(fd)

	return p.result(0, err)
}

func (p *Process) libcUnlockpt(fd uint32) uint32 {
	t, err := p.fd(fd)

	if err == nil {
		_, err = p.ttyName(t, true)
	}

	return p.result(0, err)
}

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
		return uint32(ERANGE)
	}

	p.putString(buf, name)

	return 0
}

func (p *Process) ptsname(fd uint32) (string, error) {
	t, err := p.fd(fd)

	if err != nil {
		return "", err
	}

	return p.ttyName(t, true)
}

func (p *Process) putTimespec(at uint32, d time.Duration) {
	p.put64(at, uint64(d/time.Second))
	p.put32(at+8, uint32(d%time.Second))
}

func (p *Process) timespec(at uint32) (time.Duration, error) {
	sec, nsec := int64(p.u64(at)), int64(int32(p.u32(at+8)))

	if sec < 0 || nsec < 0 || nsec >= 1e9 {
		return 0, EINVAL
	}

	return time.Duration(min(sec, 1<<33))*time.Second + time.Duration(nsec), nil
}

func (p *Process) timeval(at uint32) (time.Duration, error) {
	sec, usec := int64(p.u64(at)), int64(p.u64(at+8))

	if sec < 0 || usec < 0 || usec >= 1e6 {
		return 0, EINVAL
	}

	return time.Duration(min(sec, 1<<33))*time.Second + time.Duration(usec)*time.Microsecond, nil
}

func (p *Process) putTimeval(at uint32, d time.Duration) {
	p.put64(at, uint64(d/time.Second))
	p.put64(at+8, uint64(d%time.Second/time.Microsecond))
}

type Itimer struct {
	epoch    uint64
	timer    *time.Timer
	end      time.Time
	interval time.Duration
}

func (t *Itimer) left() time.Duration {
	if t.timer == nil {
		return 0
	}

	return max(time.Until(t.end), time.Microsecond)
}

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

func (p *Process) armTimer(which int, end time.Time) {
	t := &p.timers[which]

	t.end = end

	epoch := t.epoch
	e := &Event{kind: eventTimer, run: func(p *Process) { p.timerDue(which, epoch) }}

	t.timer = time.AfterFunc(time.Until(end), func() {
		p.post(e)
		p.nudge()
	})
}

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

func (p *Process) stopTimers() {
	for _, t := range p.posixTimers {
		p.disarmPosix(t)
	}

	p.posixTimers = nil

	for i := range p.timers {
		t := &p.timers[i]

		if t.timer != nil {
			t.timer.Stop()
		}

		t.epoch++
		t.timer = nil
	}
}

func (p *Process) location() *time.Location {
	if p.zone == nil {
		return time.UTC
	}

	return p.zone
}

func (p *Process) libc__kernel_zone(name, data, size uint32) uint32 {
	tz, loc := p.str(name), time.UTC

	switch {
	case data != 0:
		l, err := time.LoadLocationFromTZData(tz, bytes.Clone(p.span(data, size)))

		if err != nil {
			return ^uint32(0)
		}

		loc = l
	case tz != "":
		loc = posixZone(tz)
	}

	p.zone = loc
	p.tzset()

	return 0
}

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

func (p *Process) readTm(at uint32) TmFields {
	b := p.span(at, 44)

	f := func(i int) int {
		return int(int32(binary.LittleEndian.Uint32(b[4*i:])))
	}

	tm := TmFields{f(0), f(1), f(2), f(3), f(4), f(5), f(6), f(7), f(8), f(9), ""}

	if zone := binary.LittleEndian.Uint32(b[40:]); zone != 0 {
		tm.zone = p.str(zone)
	}

	return tm
}

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

func (p *Process) localtime(t, at uint32) uint32 {
	p.tzset()

	if !p.writeTm(at, time.Unix(int64(p.u64(t)), 0).In(p.location())) {
		p.setErrno(EOVERFLOW)

		return 0
	}

	return at
}

func (p *Process) libcAsctime(tm uint32) uint32 {
	s, ok := asctime(p.readTm(tm))

	if !ok {
		p.setErrno(EOVERFLOW)

		return 0
	}

	at := p.staticAt(staticCtime)

	p.putString(at, s)

	return at
}

func (p *Process) libcAsctime_r(tm, buf uint32) uint32 {
	s, ok := asctime(p.readTm(tm))

	if !ok {
		p.setErrno(EOVERFLOW)

		return 0
	}

	p.putString(buf, s)

	return buf
}

func (p *Process) libcDifftime(t1, t0 uint64) float64 {
	return float64(int64(t1)) - float64(int64(t0))
}

func (p *Process) libcTimegm(at uint32) uint64 {
	tm := p.readTm(at)
	t := time.Date(tm.year+1900, time.Month(tm.mon+1), tm.mday, tm.hour, tm.min, tm.sec, 0, time.UTC)

	if !p.writeTm(at, t) {
		p.setErrno(EOVERFLOW)

		return math.MaxUint64
	}

	return uint64(t.Unix())
}

func (p *Process) libcUsleep(us uint32) uint32 {
	left := p.sleep(time.Duration(us) * time.Microsecond)

	if left > 0 {
		return p.result(0, EINTR)
	}

	return p.result(0, nil)
}

func (p *Process) libcGmtime(t uint32) uint32 {
	return p.gmtime(t, p.staticAt(staticTm))
}

func (p *Process) libcClock() uint64 {
	return uint64(clockGettime(hostClockProcessCputime) / time.Microsecond)
}

func (p *Process) libcClock_getres(clock, res uint32) uint32 {
	host, ok := p.hostClock(clock)

	if !ok {
		return p.result(0, EINVAL)
	}

	var t syscall.Timespec
	syscall.Syscall(syscall.SYS_CLOCK_GETRES, host, uintptr(unsafe.Pointer(&t)), 0)

	if res != 0 {
		p.put64(res, uint64(t.Sec))
		p.put32(res+8, uint32(t.Nsec))
	}

	return p.result(0, nil)
}

func (p *Process) libcClock_nanosleep(clock, flags, req, rem uint32) uint32 {
	switch clock {
	case CLOCK_REALTIME, CLOCK_MONOTONIC, CLOCK_BOOTTIME:
	case CLOCK_PROCESS_CPUTIME_ID:
		return uint32(ENOTSUP)
	default:
		return uint32(EINVAL)
	}

	d, err := p.timespec(req)

	if err != nil {
		return uint32(EINVAL)
	}

	if flags&TIMER_ABSTIME != 0 {
		d -= clockGettime(hostClocks[clock])
	}

	left := p.sleep(d)

	if left > 0 {
		if rem != 0 && flags&TIMER_ABSTIME == 0 {
			p.putTimespec(rem, left)
		}

		return uint32(EINTR)
	}

	return 0
}

func (p *Process) libcSetitimer(which, value, old uint32) uint32 {
	if which > ITIMER_PROF {
		return p.result(0, EINVAL)
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
	if which > ITIMER_PROF {
		return p.result(0, EINVAL)
	}

	t := &p.timers[which]
	left, every := t.left(), t.interval

	p.putTimeval(at, every)
	p.putTimeval(at+16, left)

	return p.result(0, nil)
}

func (p *Process) libcAlarm(seconds uint32) uint32 {
	left, _ := p.setTimer(ITIMER_REAL, time.Duration(seconds)*time.Second, 0)

	return uint32((left + time.Second - 1) / time.Second)
}

func (p *Process) libcMktime(at uint32) uint64 {
	p.tzset()

	tm := p.readTm(at)
	t := time.Date(tm.year+1900, time.Month(tm.mon+1), tm.mday, tm.hour, tm.min, tm.sec, 0, p.location())

	if !p.writeTm(at, t) {
		p.setErrno(EOVERFLOW)

		return math.MaxUint64
	}

	return uint64(t.Unix())
}

func (p *Process) libcTimes(at uint32) uint64 {
	var t syscall.Tms

	ticks, err := syscall.Times(&t)

	if err != nil {
		return p.result64(0, hostErrno(err))
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

func (p *Process) hostClock(clock uint32) (uintptr, bool) {
	if host, ok := hostClock(clock); ok {
		return host, true
	}

	id := int32(clock)

	if id >= 0 || id&3 == 3 {
		return 0, false
	}

	pid := ^(id >> 3)

	if id&4 != 0 {
		return hostClockProcessCputime, pid == p.pid
	}

	if _, err := p.process(pid); err != nil {
		return 0, false
	}

	return hostClockProcessCputime, true
}

func (p *Process) libcClock_getcpuclockid(pid, out uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return uint32(ESRCH)
	}

	p.put32(out, uint32((-int32(pid)-1)*8+2))

	return 0
}

func (p *Process) clockNow(clock uint32) (int64, error) {
	host, ok := p.hostClock(clock)

	if !ok {
		return 0, EINVAL
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

		return p.result(0, EINTR)
	}

	return p.result(0, nil)
}

func (p *Process) gmtime(t, at uint32) uint32 {
	sec := int64(p.u64(t))
	u := time.Unix(sec, 0).UTC()

	if !p.writeTm(at, u) {
		p.setErrno(EOVERFLOW)

		return 0
	}

	return at
}

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
	return p.result(0, EPERM)
}

func (p *Process) libcSettimeofday(tv, tz uint32) uint32 {
	return p.result(0, EPERM)
}

func (p *Process) libcAdjtime(delta, old uint32) uint32 {
	return p.result(0, EPERM)
}

func (p *Process) libcGmtime_r(t, at uint32) uint32 {
	return p.gmtime(t, at)
}

func (p *Process) pathconf(name uint32) uint32 {
	if name >= uint32(len(pathconfValues)) {
		return p.result(0, EINVAL)
	}

	return p.result(int(pathconfValues[name]), nil)
}

func (p *Process) sysinfo(at uint32) error {
	var si syscall.Sysinfo_t

	if err := syscall.Sysinfo(&si); err != nil {
		return hostErrno(err)
	}

	sizes := []uint64{si.Totalram, si.Freeram, si.Sharedram, si.Bufferram, si.Totalswap, si.Freeswap, si.Totalhigh, si.Freehigh}
	unit := uint64(max(si.Unit, 1))
	scale := unit

	for _, s := range sizes {
		for s*unit/scale > math.MaxUint32 {
			scale *= 2
		}
	}

	b := p.span(at, sizeofSysinfo)

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

func (p *Process) limitOf(q *Process, res, set, old uint32) error {
	if res >= RLIM_NLIMITS {
		return EINVAL
	}

	var l *Rlimit

	if set != 0 {
		l = &Rlimit{p.u64(set), p.u64(set + 8)}
	}

	cur, err := p.limit(q, res, l)

	if err == nil && old != 0 {
		p.put64(old, cur.cur)
		p.put64(old+8, cur.max)
	}

	return err
}

func (p *Process) limit(q *Process, res uint32, set *Rlimit) (Rlimit, error) {
	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	cur := q.limits[res]

	if set != nil {
		if set.cur > set.max {
			return cur, EINVAL
		}

		if set.max > cur.max && !p.cred.privileged() {
			return cur, EPERM
		}

		q.limits[res] = *set
	}

	return cur, nil
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

func (p *Process) libcGetrusage(who, at uint32) uint32 {
	switch int32(who) {
	case 0, 1:
		var ru syscall.Rusage

		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
			return p.result(0, hostErrno(err))
		}

		p.putRusage(at, &ru)
	case -1:
		p.putRusage(at, nil)
	default:
		return p.result(0, EINVAL)
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

	return uint32(min(p.limits[RLIMIT_NOFILE].cur, math.MaxInt32))
}

func (p *Process) libcConfstr(name, buf, n uint32) uint32 {
	if name != 0 {
		p.setErrno(EINVAL)

		return 0
	}

	s := "/bin:/usr/bin"

	if n > 0 {
		p.putString(buf, s[:min(int(n-1), len(s))])
	}

	return uint32(len(s) + 1)
}

func (p *Process) libcPathconf(path, name uint32) uint32 {
	var st Stat

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

func (p *Process) libcGetlogin() uint32 {
	_, value, _ := p.lookupEnv([]byte("LOGNAME"))

	return value
}

func (p *Process) libcGetlogin_r(buf, n uint32) uint32 {
	name, ok := p.getenv("LOGNAME")

	switch {
	case !ok:
		return uint32(ENXIO)
	case uint32(len(name)) >= n:
		return uint32(ERANGE)
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

func (p *Process) libcTtyname_r(fd, buf, n uint32) uint32 {
	name, e := p.terminalName(fd)

	switch {
	case e != 0:
		return uint32(e)
	case uint32(len(name)) >= n:
		return uint32(ERANGE)
	}

	p.putString(buf, name)

	return 0
}

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

func temper(x uint32) uint32 {
	x ^= x >> 11
	x ^= x << 7 & 0x9d2c5680
	x ^= x << 15 & 0xefc60000
	x ^= x >> 18

	return x
}

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
	case PR_SET_PDEATHSIG:
		if arg != 0 && !validSignal(arg) {
			return p.result(0, EINVAL)
		}

		p.k.mu.Lock()
		p.pdeathsig = int(arg)
		p.k.mu.Unlock()
	case PR_GET_PDEATHSIG:
		p.k.mu.Lock()

		sig := p.pdeathsig

		p.k.mu.Unlock()
		p.put32(arg, uint32(sig))
	case PR_GET_DUMPABLE:
		return p.result(int(bit(!p.undumpable)), nil)
	case PR_SET_DUMPABLE:
		if arg > 1 {
			return p.result(0, EINVAL)
		}

		p.undumpable = arg == 0
	case PR_SET_NAME:
		name := truncate(string(p.cstr(arg)), 15)

		p.comm = &name
	case PR_GET_NAME:
		b := p.span(arg, 16)

		clear(b)
		copy(b, p.name())
	case PR_SET_NO_NEW_PRIVS:
		if arg != 1 {
			return p.result(0, EINVAL)
		}

		p.noNewPrivs = true
	case PR_GET_NO_NEW_PRIVS:
		return p.result(int(bit(p.noNewPrivs)), nil)
	default:
		return p.result(0, EINVAL)
	}

	return p.result(0, nil)
}

func (p *Process) libcSrandom(seed uint32) {
	p.srandom(p.randomState(), seed)
}

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

func (p *Process) libcSecure_getenv(name uint32) uint32 {
	_, value, _ := p.lookupEnv(p.cstr(name))

	return value
}

func (p *Process) libcGetentropy(buf, n uint32) uint32 {
	if n > 256 {
		return p.result(0, EIO)
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

	if t, err := p.fd(2); err == nil {
		p.fileWrite(t, bytesFile([]byte(msg)), len(msg), IoMode{})
	}

	p.abort()
}

func (p *Process) complain(prefix uint32, msg string) {
	if prefix != 0 && p.u8(prefix) != 0 {
		msg = p.str(prefix) + ": " + msg
	}

	if t, err := p.fd(2); err == nil {
		p.fileWrite(t, bytesFile([]byte(msg+"\n")), len(msg)+1, IoMode{})
	}
}

func (p *Process) libcRandom() uint32 {
	return p.random()
}

func (p *Process) libcGetsubopt(opt, keys, val uint32) uint32 {
	return p.getsubopt(opt, keys, val)
}

func (p *Process) libcPrctl(option, va uint32) uint32 {
	return p.prctl(option, va)
}

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
		return p.result(0, EINVAL)
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

func (p *Process) setName(to *string, name, n uint32) error {
	if !p.cred.privileged() {
		return EPERM
	}

	if n >= utsField {
		return EINVAL
	}

	text := string(p.span(name, n))

	p.k.names.mu.Lock()
	*to = text
	p.k.names.mu.Unlock()

	return nil
}

func (p *Process) staticResult(slot, size uint32) uint32 {
	at := p.staticAt(slot)

	p.free(p.u32(at))
	p.put32(at, 0)

	block := p.malloc(size)

	if block == 0 {
		p.setErrno(ENOMEM)

		return 0
	}

	p.put32(at, block)

	return block
}

func (p *Process) libcInet_aton(s, at uint32) uint32 {
	a, ok := inetAton(p.str(s))

	if ok {
		copy(p.span(at, 4), a[:])
	}

	return bit(ok)
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
	case AF_INET:
		a, err := netip.ParseAddr(text)

		if err != nil || !a.Is4() {
			return 0
		}

		v := a.As4()

		copy(p.span(dst, 4), v[:])
	case AF_INET6:
		a, err := netip.ParseAddr(text)

		if err != nil || !a.Is6() || a.Zone() != "" {
			return 0
		}

		v := a.As16()

		copy(p.span(dst, 16), v[:])
	default:
		p.setErrno(EAFNOSUPPORT)

		return ^uint32(0)
	}

	return 1
}

func (p *Process) libcInet_ntop(family, src, dst, size uint32) uint32 {
	var text string

	switch family {
	case AF_INET:
		text = netip.AddrFrom4([4]byte(p.span(src, 4))).String()
	case AF_INET6:
		text = netip.AddrFrom16([16]byte(p.span(src, 16))).String()
	default:
		p.setErrno(EAFNOSUPPORT)

		return 0
	}

	if uint32(len(text)) >= size {
		p.setErrno(ENOSPC)

		return 0
	}

	p.putString(dst, text)

	return dst
}

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

func (p *Process) set(at uint32) [256]bool {
	var s [256]bool

	for _, c := range p.cstr(at) {
		s[c] = true
	}

	return s
}

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
		p.setErrno(ENOMEM)

		return 0
	}

	copy(p.span(at, l), p.span(s, l))
	p.span(at+l, 1)[0] = 0

	return at
}

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

func (p *Process) numberEnd(s, end uint32, n int, size uint32, erange bool) {
	if end != 0 {
		p.put32(end, s+uint32(n)*size)
	}

	if erange {
		p.setErrno(ERANGE)
	}
}

func (p *Process) libcStrtod(s, end uint32) float64 {
	v, n, erange := scanFloat(p.cstr(s), 64)

	p.numberEnd(s, end, n, 1, erange)

	return v
}

func (p *Process) libcStrtof(s, end uint32) float32 {
	v, n, erange := scanFloat(p.cstr(s), 32)

	p.numberEnd(s, end, n, 1, erange)

	return float32(v)
}

func (p *Process) libcWcstod(s, end uint32) float64 {
	v, n, erange := scanWideFloat(p.wstring(s), 64)

	p.numberEnd(s, end, n, 4, erange)

	return v
}

func (p *Process) libcWcstof(s, end uint32) float32 {
	v, n, erange := scanWideFloat(p.wstring(s), 32)

	p.numberEnd(s, end, n, 4, erange)

	return float32(v)
}

func (p *Process) integerOf(s, end, base uint32, signed bool, bits int, wide bool) uint64 {
	if b := int32(base); b != 0 && (b < 2 || b > 36) {
		p.numberEnd(s, end, 0, 1, false)
		p.setErrno(EINVAL)

		return 0
	}

	var (
		v        uint64
		negative bool
		n        int
		over     bool
	)

	size := uint32(1)

	if wide {
		v, negative, n, over = scanWideInteger(p.wstring(s), int(base))
		size = 4
	} else {
		v, negative, n, over = scanInteger(p.cstr(s), int(base))
	}

	r, erange := integerIn(v, negative, over, signed, bits)

	p.numberEnd(s, end, n, size, erange)

	return r
}

func (p *Process) libcStrtol(s, end, base uint32) uint32 {
	return uint32(p.integerOf(s, end, base, true, 32, false))
}

func (p *Process) libcStrtoul(s, end, base uint32) uint32 {
	return uint32(p.integerOf(s, end, base, false, 32, false))
}

func (p *Process) libcStrtoll(s, end, base uint32) uint64 {
	return p.integerOf(s, end, base, true, 64, false)
}

func (p *Process) libcStrtoull(s, end, base uint32) uint64 {
	return p.integerOf(s, end, base, false, 64, false)
}

func (p *Process) libcWcstol(s, end, base uint32) uint32 {
	return uint32(p.integerOf(s, end, base, true, 32, true))
}

func (p *Process) libcWcstoul(s, end, base uint32) uint32 {
	return uint32(p.integerOf(s, end, base, false, 32, true))
}

func (p *Process) libcWcstoll(s, end, base uint32) uint64 {
	return p.integerOf(s, end, base, true, 64, true)
}

func (p *Process) libcWcstoull(s, end, base uint32) uint64 {
	return p.integerOf(s, end, base, false, 64, true)
}

func (p *Process) libcAtol(s uint32) uint32 {
	v, negative, _, over := scanInteger(p.cstr(s), 10)
	r, _ := integerIn(v, negative, over, true, 32)

	return uint32(r)
}

func (p *Process) libcAtoll(s uint32) uint64 {
	v, negative, _, over := scanInteger(p.cstr(s), 10)
	r, _ := integerIn(v, negative, over, true, 64)

	return r
}

func (p *Process) libcAtof(s uint32) float64 {
	v, _, _ := scanFloat(p.cstr(s), 64)

	return v
}

func (p *Process) digitsOut(text string, point int, negative bool, decpt, sign, end uint32) uint32 {
	at := p.newString(text)

	if at == 0 {
		p.setErrno(ENOMEM)

		return 0
	}

	p.put32(decpt, uint32(int32(point)))
	p.put32(sign, 0)

	if negative {
		p.put32(sign, 1)
	}

	if end != 0 {
		p.put32(end, at+uint32(len(text)))
	}

	return at
}

func (p *Process) libc__dtoa(v float64, mode, n, decpt, sign, end uint32) uint32 {
	text, point := "NaN", 9999

	switch {
	case math.IsInf(v, 0):
		text = "Infinity"
	case v == 0:
		text, point = "0", 1
	case !math.IsNaN(v):
		text, point = decimalDigits(math.Abs(v), int(int32(mode)), int(int32(n)))
	}

	return p.digitsOut(text, point, math.Signbit(v), decpt, sign, end)
}

func (p *Process) libc__ldtoa(v, mode, n, decpt, sign, end uint32) uint32 {
	return p.libc__dtoa(math.Float64frombits(p.u64(v)), mode, n, decpt, sign, end)
}

func (p *Process) libc__hdtoa(v float64, xdigits, n, decpt, sign, end uint32) uint32 {
	text, point := "NaN", math.MaxInt32

	switch {
	case math.IsInf(v, 0):
		text = "Infinity"
	case v == 0:
		text, point = "0", 1
	case !math.IsNaN(v):
		var digits []byte

		digits, point = hexDigits(math.Abs(v), int(int32(n)))

		set := p.span(xdigits, 16)

		for i, d := range digits {
			digits[i] = set[d]
		}

		text = string(digits)
	}

	return p.digitsOut(text, point, math.Signbit(v), decpt, sign, end)
}

func (p *Process) libcAbs(n uint32) uint32 {
	if int32(n) < 0 {
		return uint32(-int32(n))
	}

	return n
}

func (p *Process) libcStrerror(e uint32) uint32 {
	at := p.staticAt(staticStrerror)

	p.putString(at, truncate(Errno(e).message(), staticStrsignal-staticStrerror-1))

	return at
}

func (p *Process) libcStrsignal(sig uint32) uint32 {
	at := p.staticAt(staticStrsignal)

	p.putString(at, truncate(signalMessage(int(sig)), staticStrtok-staticStrsignal-1))

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

func (p *Process) libcStrerror_r(e, buf, n uint32) uint32 {
	msg := Errno(e).message()

	if uint32(len(msg)) >= n {
		if n > 0 {
			p.putString(buf, msg[:n-1])
		}

		return uint32(ERANGE)
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

func (p *Process) wide(at, n uint32) []byte {
	m := p.mem()
	end := uint64(at) + 4*uint64(n)

	if end > uint64(len(m)) {
		oob()
	}

	return m[at:end]
}

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

func (p *Process) wstring(at uint32) []uint32 {
	n := p.wcslen(at)
	b := p.wide(at, n)
	out := make([]uint32, n)

	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[4*i:])
	}

	return out
}

func (p *Process) libcIconv_open(to, from uint32) uint32 {
	c, ok := newIconv(p.str(to), p.str(from))

	if !ok {
		p.setErrno(EINVAL)

		return ^uint32(0)
	}

	p.nextIconv++
	p.iconvs[p.nextIconv] = c

	return p.nextIconv
}

func (p *Process) libcIconv(cd, inbuf, inleft, outbuf, outleft uint32) uint32 {
	c, ok := p.iconvs[cd]

	if !ok {
		p.setErrno(EBADF)

		return ^uint32(0)
	}

	var out []byte

	outAt := uint32(0)

	if outbuf != 0 && outleft != 0 {
		if outAt = p.u32(outbuf); outAt != 0 {
			out = p.span(outAt, p.u32(outleft))
		}
	}

	nIn, nOut, count, e := 0, 0, 0, Errno(0)

	if inbuf == 0 || p.u32(inbuf) == 0 {
		nOut, e = c.reset(out, out != nil)
	} else {
		inAt := p.u32(inbuf)

		nIn, nOut, count, e = c.convert(p.span(inAt, p.u32(inleft)), out)

		p.put32(inbuf, inAt+uint32(nIn))
		p.put32(inleft, p.u32(inleft)-uint32(nIn))
	}

	if out != nil {
		p.put32(outbuf, outAt+uint32(nOut))
		p.put32(outleft, p.u32(outleft)-uint32(nOut))
	}

	if e != 0 {
		p.setErrno(e)

		return ^uint32(0)
	}

	return uint32(count)
}

func (p *Process) libcIconv_close(cd uint32) uint32 {
	if _, ok := p.iconvs[cd]; !ok {
		return p.result(0, EBADF)
	}

	delete(p.iconvs, cd)

	return p.result(0, nil)
}

func (p *Process) glob(pattern, flags, errfunc, out, home uint32) uint32 {
	g := &Globber{p: p, flags: flags, errfunc: errfunc}

	if home != 0 {
		g.home = p.str(home)
	}

	offs := uint32(0)

	if flags&GLOB_DOOFFS != 0 {
		offs = p.u32(out + 8)
	}

	if flags&GLOB_APPEND == 0 {
		p.put32(out, 0)
		p.put32(out+4, 0)
		p.put32(out+8, offs)
	}

	pat, ok := g.tilde(p.str(pattern))

	if !ok || pat == "" {
		return GLOB_NOMATCH
	}

	if strings.HasPrefix(pat, "/") {
		g.walk("/", strings.Split(pat[1:], "/"))
	} else {
		g.walk("", strings.Split(pat, "/"))
	}

	if g.aborted {
		return GLOB_ABORTED
	}

	if len(g.found) == 0 {
		if flags&GLOB_NOCHECK == 0 {
			return GLOB_NOMATCH
		}

		g.found = []string{p.str(pattern)}
	}

	if flags&GLOB_NOSORT == 0 {
		slices.Sort(g.found)
	}

	if flags&GLOB_MARK != 0 {
		for i, name := range g.found {
			var st Stat

			isDir := p.statPath(name, true, &st) == nil && st.Mode&S_IFMT == S_IFDIR

			if isDir && !strings.HasSuffix(name, "/") {
				g.found[i] = name + "/"
			}
		}
	}

	count, old := p.u32(out), p.u32(out+4)
	total := uint64(offs) + uint64(count) + uint64(len(g.found)) + 1

	if total*4 > heapMax {
		return GLOB_NOSPACE
	}

	pathv := p.malloc(uint32(total * 4))

	if pathv == 0 {
		return GLOB_NOSPACE
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

			return GLOB_NOSPACE
		}

		p.put32(pathv+(offs+count+uint32(i))*4, s)
	}

	p.put32(pathv+(offs+count+uint32(len(g.found)))*4, 0)
	p.free(old)
	p.put32(out, count+uint32(len(g.found)))
	p.put32(out+4, pathv)

	return 0
}

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

func (p *Process) libc__glob_home(pattern, flags, errfunc, out, home uint32) uint32 {
	return p.glob(pattern, flags, errfunc, out, home)
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
		p.setErrno(EINVAL)

		return 0
	}

	block := p.staticResult(staticCrypt, uint32(len(out))+1)

	if block != 0 {
		p.putString(block, out)
	}

	return block
}

func (p *Process) sized(call string, sizes ...uint32) {
	for k := 0; k+1 < len(sizes); k += 2 {
		if sizes[k] != sizes[k+1] {
			p.crash(fmt.Sprintf("%s: a structure of %d bytes, which this kernel knows of %d", call, sizes[k], sizes[k+1]), SIGSYS)
		}
	}
}

func (p *Process) job(fn func(group uint32) (int, error)) (int, error) {
	free := false

	for {
		group := uint32(0)

		if !free {
			group = p.group()
		}

		n, err := fn(group)
		sig, held := err.(Background)

		if !held {
			return n, err
		}

		if free, err = p.background(int(sig)); err != nil {
			return 0, err
		}
	}
}

func (p *Process) request(o Object, r Request) error {
	_, err := o.call(p, r, p.group(), false)

	if err == errUnserved {
		err = r.unserved()
	}

	return err
}

func (p *Process) vfsRoot(d Object) uint64 {
	id, _ := p.handleID(d)

	if root, err := p.handleID(p.root); err == nil {
		id = root
	}

	return id
}

func flagOf(follow bool) uint32 {
	if follow {
		return 0
	}

	return AT_SYMLINK_NOFOLLOW
}

func (p *Process) dirOpen(d Object, path string, flags int, mode uint32) (Object, error) {
	if p.chaosFail("openat") {
		return nil, EIO
	}

	r := VfsOpen{root: p.vfsRoot(d), path: path, flags: flags, mode: mode}

	if err := p.request(d, &r); err != nil {
		return nil, err
	}

	switch {
	case r.fifo:
		return p.openFifo(&r.st, flags)
	case r.terminal && flags&O_NOCTTY == 0:
		p.acquire(r.file)
	}

	return r.file, nil
}

func (p *Process) dirStat(d Object, path string, follow bool, st *Stat) error {
	if p.chaosFail("file-stat") {
		return EIO
	}

	r := VfsStat{root: p.vfsRoot(d), path: path, follow: follow}
	err := p.request(d, &r)

	*st = r.st

	return err
}

func (p *Process) dirReadlink(d Object, path string) (string, error) {
	r := VfsReadlink{root: p.vfsRoot(d), path: path}
	err := p.request(d, &r)

	return r.target, err
}

func (p *Process) dirMkdir(d Object, path string, mode uint32) error {
	return p.request(d, &VfsMkdir{root: p.vfsRoot(d), path: path, mode: mode})
}

func (p *Process) dirMknod(d Object, path string, mode uint32) error {
	return p.request(d, &VfsMknod{root: p.vfsRoot(d), path: path, mode: mode})
}

func (p *Process) dirSymlink(d Object, target, path string) error {
	return p.request(d, &VfsSymlink{root: p.vfsRoot(d), target: target, path: path})
}

func (p *Process) dirUnlink(d Object, path string, dir bool) error {
	return p.request(d, &VfsUnlink{root: p.vfsRoot(d), path: path, dir: dir})
}

func (p *Process) dirRename(d Object, old string, to Object, new string) error {
	id, err := p.handleID(to)

	if err != nil {
		return err
	}

	return p.request(d, &VfsRename{root: p.vfsRoot(d), old: old, to: id, new: new})
}

func (p *Process) dirLink(d Object, old string, to Object, new string, follow bool) error {
	id, err := p.handleID(to)

	if err != nil {
		return err
	}

	return p.request(d, &VfsLink{root: p.vfsRoot(d), old: old, to: id, new: new, follow: follow})
}

func (p *Process) dirChmod(d Object, path string, mode uint32, follow bool) error {
	return p.request(d, &VfsChmod{root: p.vfsRoot(d), path: path, mode: mode, follow: follow})
}

func (p *Process) dirChown(d Object, path string, uid, gid int, follow bool) error {
	return p.request(d, &VfsChown{root: p.vfsRoot(d), path: path, uid: uid, gid: gid, follow: follow})
}

func (p *Process) dirUtimes(d Object, path string, ts []Timespec, follow bool) error {
	return p.request(d, &VfsUtimes{root: p.vfsRoot(d), path: path, ts: ts, follow: follow})
}

func (p *Process) dirTruncate(d Object, path string, size int64) error {
	return p.request(d, &VfsTruncate{root: p.vfsRoot(d), path: path, size: size})
}

func (p *Process) dirStatfs(d Object, path string, st *Statfs) error {
	r := VfsStatfs{root: p.vfsRoot(d), path: path}
	err := p.request(d, &r)

	*st = r.st

	return err
}

func (p *Process) dirRealpath(d Object, path string) (string, error) {
	r := VfsRealpath{root: p.vfsRoot(d), path: path}
	err := p.request(d, &r)

	return r.out, err
}

func (p *Process) dirPath(d Object) (string, error) {
	r := VfsPath{root: p.vfsRoot(d)}
	err := p.request(d, &r)

	return r.out, err
}

func (p *Process) dirMount(d Object, target, source string, conn Object) error {
	return p.request(d, &VfsMount{root: p.vfsRoot(d), target: target, source: source, conn: conn})
}

func (p *Process) dirUnmount(d Object, path string, flags uint32) error {
	return p.request(d, &VfsUnmount{root: p.vfsRoot(d), path: path, flags: flags})
}

func (p *Process) ttyChange(t Object, r Request, waits bool) error {
	_, err := p.job(func(group uint32) (int, error) {
		for {
			cancelled, err := t.call(p, r, group, waits)

			switch {
			case waits && err == EINTR && cancelled && !p.interrupts(true):
				continue
			case err == errUnserved:
				err = r.unserved()
			}

			return 0, err
		}
	})

	return err
}

func (p *Process) ttyGetattr(t Object) ([]byte, error) {
	r := TtyGetattr{}
	err := p.request(t, &r)

	return r.termios, err
}

func (p *Process) ttySetattr(t Object, when uint32, termios []byte) error {
	return p.ttyChange(t, &TtySetattr{when: when, termios: termios}, when != 0)
}

func (p *Process) ttyFlush(t Object, queue uint32) error {
	return p.ttyChange(t, &TtyFlush{queue: queue}, false)
}

func (p *Process) ttyFlow(t Object, action uint32) error {
	return p.ttyChange(t, &TtyFlow{action: action}, false)
}

func (p *Process) ttyDrain(t Object) error {
	return p.ttyChange(t, &TtyDrain{}, true)
}

func (p *Process) ttyBreak(t Object) error {
	return p.ttyChange(t, &TtyBreak{}, false)
}

func (p *Process) ttyWindow(t Object) ([]byte, error) {
	r := TtyWindow{}
	err := p.request(t, &r)

	return r.size, err
}

func (p *Process) ttySetWindow(t Object, size []byte) error {
	return p.request(t, &TtySetWindow{size: size})
}

func (p *Process) ttyPending(t Object) (uint32, error) {
	r := TtyQueue{}
	err := p.request(t, &r)

	return r.n, err
}

func (p *Process) ttyUnsent(t Object) (uint32, error) {
	r := TtyQueue{out: true}
	err := p.request(t, &r)

	return r.n, err
}

func (p *Process) ttyForeground(t Object) (uint32, uint32, error) {
	r := TtyForeground{}
	err := p.request(t, &r)

	return r.group, r.sid, err
}

func (p *Process) ttySetForeground(t Object, pgrp uint32) error {
	return p.ttyChange(t, &TtySetForeground{group: pgrp}, false)
}

func (p *Process) ttyControl(t Object, how uint32) error {
	return p.request(t, &TtyControl{how: how})
}

func (p *Process) ttyName(t Object, peer bool) (string, error) {
	r := TtyName{peer: peer}
	err := p.request(t, &r)

	return r.name, err
}

func (p *Process) sockKind(s Object) int {
	r := SockType{}

	if p.request(s, &r) != nil {
		return 0
	}

	return r.kind
}

func (p *Process) sockSend(s Object, pk *Packet, to Address, mode IoMode) (int, error) {
	r := p.sends.get()

	r.pk, r.to, r.mode = pk, to, mode

	err := p.request(s, r)
	n := r.n

	p.sends.put(r)

	return n, err
}

func (p *Process) sockAccept(s Object, flags uint32) (Object, []byte, error) {
	r := SockAccept{flags: flags}
	err := p.request(s, &r)

	return r.conn, r.peer, err
}

func (p *Process) sockName(s Object, peer bool) ([]byte, error) {
	r := SockName{peer: peer}
	err := p.request(s, &r)

	return r.addr, err
}

func (p *Process) sockGetopt(s Object, level, name uint32) ([]byte, error) {
	r := SockGetopt{level: level, name: name}
	err := p.request(s, &r)

	return r.value, err
}

func (p *Process) fileCall(o Object, r Request, group uint32) error {
	for {
		cancelled, err := o.call(p, r, group, true)

		switch {
		case err == EINTR && cancelled && !p.interrupts(true) && p.interrupted():
			continue
		case err == errUnserved:
			err = r.unserved()
		}

		return err
	}
}

func (p *Process) fileJob(o Object, r Request) error {
	free := false

	for {
		group := uint32(0)

		if !free {
			group = p.group()
		}

		err := p.fileCall(o, r, group)
		sig, held := err.(Background)

		if !held {
			return err
		}

		if free, err = p.background(int(sig)); err != nil {
			return err
		}
	}
}

func (p *Process) fileRead(o Object, into *MemfdFile, n int, mode IoMode) (int, error) {
	r := p.reads.get()

	r.into, r.n, r.mode = into, n, mode

	err := p.fileJob(o, r)
	count := r.count

	p.reads.put(r)

	return count, err
}

func (p *Process) filePread(o Object, into *MemfdFile, n int, off int64) (int, error) {
	r := p.reads.get()

	r.into, r.n, r.at, r.explicit = into, n, off, true

	err := p.fileCall(o, r, p.group())
	count := r.count

	p.reads.put(r)

	return count, err
}

func (p *Process) fileWrite(o Object, from *MemfdFile, n int, mode IoMode) (int, error) {
	r := p.writes.get()

	r.from, r.n, r.mode = from, n, mode

	err := p.fileJob(o, r)
	count := r.count

	p.writes.put(r)

	return count, err
}

func (p *Process) filePwrite(o Object, from *MemfdFile, n int, off int64) (int, error) {
	r := p.writes.get()

	r.from, r.n, r.at, r.explicit = from, n, off, true

	err := p.fileCall(o, r, p.group())
	count := r.count

	p.writes.put(r)

	return count, err
}

func (p *Process) fileSeek(o Object, off int64, whence int) (int64, error) {
	r := FileSeek{off: off, whence: whence}
	err := p.request(o, &r)

	return r.pos, err
}

func (p *Process) fileReaddir(o Object, into *MemfdFile, n int) (int, error) {
	r := FileReaddir{into: into, n: n}
	err := p.request(o, &r)

	return r.count, err
}

func (p *Process) fileFlags(o Object) (int, error) {
	r := FileGetfl{}
	err := p.request(o, &r)

	return r.flags, err
}

func (p *Process) fileSetFlags(o Object, fl int) error {
	return p.request(o, &FileSetfl{flags: fl})
}

func (p *Process) fileCopyTo(o, out Object, from, to int64, n int) (int, error) {
	r := FileCopyTo{out: out, from: from, to: to, n: n}
	err := p.request(o, &r)

	return r.count, err
}

func (p *Process) fileCopyFrom(o, in Object, from, to int64, n int) (int, error) {
	r := FileCopyFrom{in: in, from: from, to: to, n: n}
	err := p.request(o, &r)

	return r.count, err
}

func (p *Process) fileIoctl(o Object, req uintptr, arg unsafe.Pointer) error {
	return p.request(o, &FileIoctl{req: req, arg: arg})
}

func (p *Process) epollCtl(o Object, op uint32, key EpollKey, events uint32, data uint64) error {
	return p.request(o, &EpollCtl{ctl: op, key: key, events: events, data: data})
}

func (p *Process) epollTake(o Object, buf func() []byte, max int, end time.Time) (int, error) {
	r := p.epolls.get()

	r.buf, r.max, r.end = buf, max, end

	err := p.request(o, r)
	count := r.count

	p.epolls.put(r)

	return count, err
}

func (p *Process) fileStat(o Object, st *Stat) error {
	r := p.stats.get()

	r.st = st

	err := p.request(o, r)

	p.stats.put(r)

	return err
}

func (p *Process) identity(o Object) (FileIdentity, error) {
	r := FileIdentity{}
	err := p.request(o, &r)

	return r, err
}

func (p *Process) handleID(o Object) (uint64, error) {
	r, err := p.identity(o)

	return r.id, err
}

func (p *Process) device(o Object) FileKey {
	r, _ := p.identity(o)

	return r.key
}

func (p *Process) lockable(o Object) (FileKey, int, error) {
	r, err := p.identity(o)

	switch {
	case err != nil || r.access&O_PATH != 0:
		return FileKey{}, 0, EBADF
	case r.key.ino == 0:
		return FileKey{}, 0, EINVAL
	}

	return r.key, r.access, nil
}

func (p *Process) fileFlock(o Object, op int) error {
	key, _, err := p.lockable(o)

	if err != nil {
		return err
	}

	switch kind := op &^ LOCK_NB; kind {
	case LOCK_UN:
		p.k.locks.unlock(o, key, false, 0, 0)

		return nil
	case LOCK_SH, LOCK_EX:
		return p.k.locks.lock(p, &FileLock{owner: o, key: key, write: kind == LOCK_EX}, op&LOCK_NB == 0)
	}

	return EINVAL
}

func (p *Process) fileLock(o Object, cmd int, lk *Flock) error {
	key, fl, err := p.lockable(o)

	if err != nil {
		return err
	}

	start, length := lk.Start, lk.Len

	switch lk.Whence {
	case 0:
	case 1:
		pos, err := p.fileSeek(o, 0, 1)

		if err != nil {
			return err
		}

		start += pos
	case 2:
		var st Stat

		if err := p.fileStat(o, &st); err != nil {
			return err
		}

		start += st.Size
	default:
		return EINVAL
	}

	if length < 0 {
		start, length = start+length, -length
	}

	if start < 0 || lk.Type != F_RDLCK && lk.Type != F_WRLCK && lk.Type != F_UNLCK {
		return EINVAL
	}

	want := &FileLock{owner: o, key: key, ranged: true, write: lk.Type == F_WRLCK, start: uint64(start), stop: lockPast}

	if length != 0 && want.start <= lockPast-uint64(length) {
		want.stop = want.start + uint64(length)
	}

	accmode := fl & O_ACCMODE

	switch {
	case cmd != fOfdGetlk && (lk.Type == F_RDLCK && accmode == O_WRONLY || lk.Type == F_WRLCK && accmode == O_RDONLY):
		return EBADF
	case cmd == fOfdGetlk:
		lk.Whence, lk.Pid = 0, -1

		way, found := p.k.locks.way(want)

		if !found {
			lk.Type, lk.Start, lk.Len = F_UNLCK, start, length

			return nil
		}

		lk.Type, lk.Start, lk.Len = F_RDLCK, int64(way.start), 0

		if way.write {
			lk.Type = F_WRLCK
		}

		if way.stop != lockPast {
			lk.Len = int64(way.stop - way.start)
		}

		return nil
	case lk.Type == F_UNLCK:
		p.k.locks.unlock(o, key, true, want.start, want.stop)

		return nil
	}

	return p.k.locks.lock(p, want, cmd == fOfdSetlkw)
}

func codeOf(err error) uint32 {
	return uint32(errnoOf(err))
}

func threadsCode(err error) uint32 {
	switch err {
	case nil:
		return thrd_success
	case EBUSY:
		return thrd_busy
	case ETIMEDOUT:
		return thrd_timedout
	case EAGAIN, ENOMEM:
		return thrd_nomem
	}

	return thrd_error
}

func (p *Process) sleepUntil(end time.Time) bool {
	for {
		left := time.Until(end)

		if left <= 0 {
			return false
		}

		p.sleep(left)
	}
}

func (p *Process) deadlineOf(at, clock uint32) (time.Time, error) {
	d, err := p.timespec(at)

	if err != nil {
		return time.Time{}, EINVAL
	}

	host, ok := hostClocks[clock]

	if !ok {
		return time.Time{}, EINVAL
	}

	return time.Now().Add(d - clockGettime(host)), nil
}

func (p *Process) addSync(o Object) int32 {
	for {
		p.nextSync++

		if p.nextSync <= 0 {
			p.nextSync = 1
		}

		if p.syncs[p.nextSync] == nil {
			break
		}
	}

	p.syncs[p.nextSync] = o

	return p.nextSync
}

func (p *Process) syncAt(at uint32, made func() Object) (Object, error) {
	if at == 0 {
		return nil, EINVAL
	}

	h := int32(p.u32(at))

	if h == 0 && made != nil {
		o := made()

		p.put32(at, uint32(p.addSync(o)))

		return o, nil
	}

	if o := p.syncs[h]; o != nil {
		return o, nil
	}

	return nil, EINVAL
}

func (p *Process) dropSync(at uint32) {
	if at == 0 {
		return
	}

	h := int32(p.u32(at))

	if o := p.syncs[h]; o != nil {
		delete(p.syncs, h)
		o.unref()
	}

	p.put32(at, 0)
}

func (p *Process) syncCall(at uint32, made func() Object, r Request) error {
	o, err := p.syncAt(at, made)

	if err != nil {
		return err
	}

	return p.request(o, r)
}

func (p *Process) timedLock(at uint32, made func() Object, shared bool, deadline uint32, clock uint32) error {
	r := SyncLock{thread: p.pid, try: true, shared: shared}

	if err := p.syncCall(at, made, &r); err != EBUSY {
		return err
	}

	end, err := p.deadlineOf(deadline, clock)

	if err != nil {
		return err
	}

	r.try, r.end = false, end

	return p.syncCall(at, made, &r)
}

func defaultMutex() Object {
	return newMutex(PTHREAD_MUTEX_NORMAL)
}

func defaultCond() Object {
	return newCond(CLOCK_REALTIME)
}

func defaultRWLock() Object {
	return newRWLock()
}

func defaultSpinLock() Object {
	return newSpinLock()
}

func notRunOnce() Object {
	return newOnce(onceNotRun)
}

func (p *Process) self(thread uint32) bool {
	return int32(thread) == p.pid
}

func (p *Process) libcGettid() uint32 {
	return uint32(p.pid)
}

func (p *Process) libcPthread_create(thread, attr, fn, arg uint32) uint32 {
	return uint32(EAGAIN)
}

func (p *Process) libcPthread_join(thread, out uint32) uint32 {
	if p.self(thread) {
		return uint32(EDEADLK)
	}

	return uint32(ESRCH)
}

func (p *Process) libcPthread_tryjoin_np(thread, out uint32) uint32 {
	if p.self(thread) {
		return uint32(EBUSY)
	}

	return uint32(ESRCH)
}

func (p *Process) libcPthread_timedjoin_np(thread, out, at uint32) uint32 {
	if !p.self(thread) {
		return uint32(ESRCH)
	}

	end, err := p.deadlineOf(at, CLOCK_REALTIME)

	if err != nil {
		return codeOf(err)
	}

	p.sleepUntil(end)

	return uint32(ETIMEDOUT)
}

func (p *Process) libcPthread_detach(thread uint32) uint32 {
	if !p.self(thread) {
		return uint32(ESRCH)
	}

	p.thread.detached = true

	return 0
}

func (p *Process) libcPthread_self() uint32 {
	return uint32(p.pid)
}

func (p *Process) libcPthread_equal(a, b uint32) uint32 {
	return bit(a == b)
}

func (p *Process) libcPthread_kill(thread, sig uint32) uint32 {
	switch {
	case !p.self(thread):
		return uint32(ESRCH)
	case sig == 0:
		return 0
	case !validSignal(sig):
		return uint32(EINVAL)
	}

	p.sendSelf(int(sig))

	return 0
}

func (p *Process) libcPthread_cancel(thread uint32) uint32 {
	if !p.self(thread) {
		return uint32(ESRCH)
	}

	p.thread.cancelled = true

	return 0
}

func (p *Process) libcPthread_setcancelstate(value, out uint32) uint32 {
	if value > PTHREAD_CANCEL_MASKED {
		return uint32(EINVAL)
	}

	if out != 0 {
		p.put32(out, p.thread.cancelState)
	}

	p.thread.cancelState = value

	return 0
}

func (p *Process) libcPthread_setcanceltype(value, out uint32) uint32 {
	if value > PTHREAD_CANCEL_ASYNCHRONOUS {
		return uint32(EINVAL)
	}

	if out != 0 {
		p.put32(out, p.thread.cancelType)
	}

	p.thread.cancelType = value

	return 0
}

func (p *Process) libc__pthread_cancelled(asynchronous uint32) uint32 {
	t := &p.thread
	acts := t.cancelled && t.cancelState == PTHREAD_CANCEL_ENABLE && (asynchronous == 0 || t.cancelType == PTHREAD_CANCEL_ASYNCHRONOUS)

	if acts {
		t.cancelled = false
	}

	return bit(acts)
}

func (p *Process) libcPthread_getschedparam(thread, out, param uint32) uint32 {
	if !p.self(thread) {
		return uint32(ESRCH)
	}

	p.k.mu.Lock()

	policy := p.policy

	p.k.mu.Unlock()
	p.put32(out, uint32(policy))
	p.put32(param, 0)

	return 0
}

func (p *Process) libcPthread_setschedparam(thread, policy, param uint32) uint32 {
	if !p.self(thread) {
		return uint32(ESRCH)
	}

	return codeOf(p.scheduler(p, policy, param))
}

func (p *Process) libcPthread_setschedprio(thread, value uint32) uint32 {
	switch {
	case !p.self(thread):
		return uint32(ESRCH)
	case value != 0:
		return uint32(EINVAL)
	}

	return 0
}

func (p *Process) libcPthread_getcpuclockid(thread, out uint32) uint32 {
	if !p.self(thread) {
		return uint32(ESRCH)
	}

	p.put32(out, uint32((-p.pid-1)*8+6))

	return 0
}

func (p *Process) libcPthread_getname_np(thread, out, n uint32) uint32 {
	name := p.name()

	switch {
	case !p.self(thread):
		return uint32(ESRCH)
	case n < 16:
		return uint32(ERANGE)
	}

	b := p.span(out, uint32(len(name))+1)

	copy(b, name)
	b[len(name)] = 0

	return 0
}

func (p *Process) libcPthread_setname_np(thread, name uint32) uint32 {
	s := p.str(name)

	switch {
	case !p.self(thread):
		return uint32(ESRCH)
	case len(s) > 15:
		return uint32(ERANGE)
	}

	p.comm = &s

	return 0
}

func (p *Process) affinity(size, set uint32, get bool) error {
	n := uint32(runtime.NumCPU())

	if get {
		if size*8 < n {
			return EINVAL
		}

		b := p.span(set, size)

		clear(b)

		for i := range n {
			b[i/8] |= 1 << (i % 8)
		}

		return nil
	}

	b := p.span(set, size)

	for i := uint32(0); i < n && i/8 < size; i++ {
		if b[i/8]&(1<<(i%8)) != 0 {
			return nil
		}
	}

	return EINVAL
}

func (p *Process) libc__sched_cpucount(size, set uint32) uint32 {
	n := 0

	for _, b := range p.span(set, size) {
		n += bits.OnesCount8(b)
	}

	return uint32(n)
}

func (p *Process) libcSched_getaffinity(pid, size, set uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return p.result(0, err)
	}

	return p.result(0, p.affinity(size, set, true))
}

func (p *Process) libcSched_setaffinity(pid, size, set uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return p.result(0, err)
	}

	return p.result(0, p.affinity(size, set, false))
}

func (p *Process) libcSched_getcpu() uint32 {
	return 0
}

func (p *Process) libcPthread_getaffinity_np(thread, size, set uint32) uint32 {
	if !p.self(thread) {
		return uint32(ESRCH)
	}

	return codeOf(p.affinity(size, set, true))
}

func (p *Process) libcPthread_setaffinity_np(thread, size, set uint32) uint32 {
	if !p.self(thread) {
		return uint32(ESRCH)
	}

	return codeOf(p.affinity(size, set, false))
}

func (p *Process) threadDefaults() (uint32, uint32) {
	stack, guard := p.defaultStack, p.defaultGuard

	if stack == 0 {
		stack = defaultThreadStack
	}

	if guard == 0 {
		guard = defaultThreadGuard
	}

	return stack, guard
}

func (p *Process) libcPthread_attr_init(attr uint32) uint32 {
	stack, guard := p.threadDefaults()

	clear(p.span(attr, sizeofPthreadAttr))
	p.put32(attr+20, stack)
	p.put32(attr+24, guard)

	return 0
}

func (p *Process) libcPthread_attr_destroy(attr uint32) uint32 {
	return 0
}

func (p *Process) libcPthread_attr_setdetachstate(attr, value uint32) uint32 {
	if value > PTHREAD_CREATE_DETACHED {
		return uint32(EINVAL)
	}

	p.put32(attr, value)

	return 0
}

func (p *Process) libcPthread_attr_getdetachstate(attr, out uint32) uint32 {
	p.put32(out, p.u32(attr))

	return 0
}

func (p *Process) libcPthread_attr_setinheritsched(attr, value uint32) uint32 {
	if value > PTHREAD_EXPLICIT_SCHED {
		return uint32(EINVAL)
	}

	p.put32(attr+4, value)

	return 0
}

func (p *Process) libcPthread_attr_getinheritsched(attr, out uint32) uint32 {
	p.put32(out, p.u32(attr+4))

	return 0
}

func (p *Process) libcPthread_attr_setschedpolicy(attr, value uint32) uint32 {
	p.put32(attr+8, value)

	return 0
}

func (p *Process) libcPthread_attr_getschedpolicy(attr, out uint32) uint32 {
	p.put32(out, p.u32(attr+8))

	return 0
}

func (p *Process) libcPthread_attr_setschedparam(attr, param uint32) uint32 {
	p.put32(attr+12, p.u32(param))

	return 0
}

func (p *Process) libcPthread_attr_getschedparam(attr, param uint32) uint32 {
	p.put32(param, p.u32(attr+12))

	return 0
}

func (p *Process) libcPthread_attr_setscope(attr, value uint32) uint32 {
	switch value {
	case PTHREAD_SCOPE_SYSTEM:
		return 0
	case PTHREAD_SCOPE_PROCESS:
		return uint32(ENOTSUP)
	}

	return uint32(EINVAL)
}

func (p *Process) libcPthread_attr_getscope(attr, out uint32) uint32 {
	p.put32(out, PTHREAD_SCOPE_SYSTEM)

	return 0
}

func stackSizeBad(size uint32) bool {
	return size-PTHREAD_STACK_MIN > math.MaxUint32/4
}

func (p *Process) libcPthread_attr_setstacksize(attr, value uint32) uint32 {
	if stackSizeBad(value) {
		return uint32(EINVAL)
	}

	p.put32(attr+20, value)
	p.put32(attr+28, 0)

	return 0
}

func (p *Process) libcPthread_attr_getstacksize(attr, out uint32) uint32 {
	p.put32(out, p.u32(attr+20))

	return 0
}

func (p *Process) libcPthread_attr_setguardsize(attr, value uint32) uint32 {
	if value > math.MaxUint32/8 {
		return uint32(EINVAL)
	}

	p.put32(attr+24, value)

	return 0
}

func (p *Process) libcPthread_attr_getguardsize(attr, out uint32) uint32 {
	p.put32(out, p.u32(attr+24))

	return 0
}

func (p *Process) libcPthread_attr_setstack(attr, stack, size uint32) uint32 {
	if stackSizeBad(size) {
		return uint32(EINVAL)
	}

	p.put32(attr+20, size)
	p.put32(attr+28, stack+size)

	return 0
}

func (p *Process) libcPthread_attr_getstack(attr, stack, size uint32) uint32 {
	top, n := p.u32(attr+28), p.u32(attr+20)

	if top == 0 {
		return uint32(EINVAL)
	}

	p.put32(size, n)
	p.put32(stack, top-n)

	return 0
}

func (p *Process) libcPthread_getattr_np(thread, attr uint32) uint32 {
	if !p.self(thread) {
		return uint32(ESRCH)
	}

	p.libcPthread_attr_init(attr)
	p.put32(attr, bit(p.thread.detached))
	p.k.mu.Lock()
	p.put32(attr+8, uint32(p.policy))
	p.k.mu.Unlock()

	return 0
}

func (p *Process) libcPthread_key_create(key, fn uint32) uint32 {
	for k := range p.keys {
		if !p.keys[k].used {
			p.keys[k] = Key{used: true, destructor: fn}
			p.thread.values[k] = 0
			p.put32(key, uint32(k))

			return 0
		}
	}

	return uint32(EAGAIN)
}

func (p *Process) keyUsed(key uint32) bool {
	return key < PTHREAD_KEYS_MAX && p.keys[key].used
}

func (p *Process) libcPthread_key_delete(key uint32) uint32 {
	if !p.keyUsed(key) {
		return uint32(EINVAL)
	}

	p.keys[key] = Key{}
	p.thread.values[key] = 0

	return 0
}

func (p *Process) libcPthread_getspecific(key uint32) uint32 {
	if !p.keyUsed(key) {
		return 0
	}

	return p.thread.values[key]
}

func (p *Process) libcPthread_setspecific(key, value uint32) uint32 {
	if !p.keyUsed(key) {
		return uint32(EINVAL)
	}

	p.thread.values[key] = value

	return 0
}

func (p *Process) libc__pthread_key_take(key, fn uint32) uint32 {
	for k := p.u32(key); k < PTHREAD_KEYS_MAX; k++ {
		if v := p.thread.values[k]; v != 0 && p.keys[k].used && p.keys[k].destructor != 0 {
			p.thread.values[k] = 0
			p.put32(key, k+1)
			p.put32(fn, p.keys[k].destructor)

			return v
		}
	}

	p.put32(key, PTHREAD_KEYS_MAX)

	return 0
}

func (p *Process) libc__pthread_once_begin(once uint32) uint32 {
	r := SyncLock{thread: p.pid}

	if p.syncCall(once, notRunOnce, &r) != nil {
		return 0
	}

	return bit(r.run)
}

func (p *Process) libc__pthread_once_end(once uint32) {
	p.syncCall(once, notRunOnce, &SyncUnlock{thread: p.pid})
}

func (p *Process) attrSet(attr, at, value, most uint32) uint32 {
	if value > most {
		return uint32(EINVAL)
	}

	p.put32(attr+at, value)

	return 0
}

func (p *Process) attrGet(attr, at, out uint32) uint32 {
	p.put32(out, p.u32(attr+at))

	return 0
}

func (p *Process) libcPthread_mutexattr_init(attr uint32) uint32 {
	clear(p.span(attr, sizeofPthreadMutexattr))

	return 0
}

func (p *Process) libcPthread_mutexattr_destroy(attr uint32) uint32 {
	return 0
}

func (p *Process) libcPthread_mutexattr_settype(attr, value uint32) uint32 {
	return p.attrSet(attr, 0, value, PTHREAD_MUTEX_ERRORCHECK)
}

func (p *Process) libcPthread_mutexattr_gettype(attr, out uint32) uint32 {
	return p.attrGet(attr, 0, out)
}

func (p *Process) libcPthread_mutexattr_setprotocol(attr, value uint32) uint32 {
	if value == PTHREAD_PRIO_PROTECT {
		return uint32(ENOTSUP)
	}

	return p.attrSet(attr, 4, value, PTHREAD_PRIO_INHERIT)
}

func (p *Process) libcPthread_mutexattr_getprotocol(attr, out uint32) uint32 {
	return p.attrGet(attr, 4, out)
}

func (p *Process) libcPthread_mutexattr_setrobust(attr, value uint32) uint32 {
	return p.attrSet(attr, 8, value, PTHREAD_MUTEX_ROBUST)
}

func (p *Process) libcPthread_mutexattr_getrobust(attr, out uint32) uint32 {
	return p.attrGet(attr, 8, out)
}

func (p *Process) libcPthread_mutexattr_setpshared(attr, value uint32) uint32 {
	return p.attrSet(attr, 12, value, PTHREAD_PROCESS_SHARED)
}

func (p *Process) libcPthread_mutexattr_getpshared(attr, out uint32) uint32 {
	return p.attrGet(attr, 12, out)
}

func (p *Process) libcPthread_mutexattr_setprioceiling(attr, value uint32) uint32 {
	return p.attrSet(attr, 16, value, math.MaxInt32)
}

func (p *Process) libcPthread_mutexattr_getprioceiling(attr, out uint32) uint32 {
	return p.attrGet(attr, 16, out)
}

func (p *Process) libcPthread_mutex_init(mutex, attr uint32) uint32 {
	kind := uint32(PTHREAD_MUTEX_NORMAL)

	if attr != 0 {
		kind = p.u32(attr)
	}

	p.put32(mutex, uint32(p.addSync(newMutex(kind))))

	return 0
}

func (p *Process) libcPthread_mutex_destroy(mutex uint32) uint32 {
	p.dropSync(mutex)

	return 0
}

func (p *Process) libcPthread_mutex_lock(mutex uint32) uint32 {
	return codeOf(p.syncCall(mutex, defaultMutex, &SyncLock{thread: p.pid}))
}

func (p *Process) libcPthread_mutex_trylock(mutex uint32) uint32 {
	return codeOf(p.syncCall(mutex, defaultMutex, &SyncLock{thread: p.pid, try: true}))
}

func (p *Process) libcPthread_mutex_timedlock(mutex, at uint32) uint32 {
	return codeOf(p.timedLock(mutex, defaultMutex, false, at, CLOCK_REALTIME))
}

func (p *Process) libcPthread_mutex_unlock(mutex uint32) uint32 {
	return codeOf(p.syncCall(mutex, defaultMutex, &SyncUnlock{thread: p.pid}))
}

func (p *Process) libcPthread_mutex_consistent(mutex uint32) uint32 {
	return uint32(EINVAL)
}

func (p *Process) libcPthread_mutex_getprioceiling(mutex, out uint32) uint32 {
	return uint32(EINVAL)
}

func (p *Process) libcPthread_mutex_setprioceiling(mutex, value, out uint32) uint32 {
	return uint32(EINVAL)
}

func (p *Process) libcPthread_condattr_init(attr uint32) uint32 {
	clear(p.span(attr, sizeofPthreadCondattr))

	return 0
}

func (p *Process) libcPthread_condattr_destroy(attr uint32) uint32 {
	return 0
}

func (p *Process) libcPthread_condattr_setclock(attr, clock uint32) uint32 {
	if int32(clock) < 0 || clock == CLOCK_PROCESS_CPUTIME_ID || clock == CLOCK_THREAD_CPUTIME_ID {
		return uint32(EINVAL)
	}

	p.put32(attr, clock)

	return 0
}

func (p *Process) libcPthread_condattr_getclock(attr, out uint32) uint32 {
	return p.attrGet(attr, 0, out)
}

func (p *Process) libcPthread_condattr_setpshared(attr, value uint32) uint32 {
	return p.attrSet(attr, 4, value, PTHREAD_PROCESS_SHARED)
}

func (p *Process) libcPthread_condattr_getpshared(attr, out uint32) uint32 {
	return p.attrGet(attr, 4, out)
}

func (p *Process) libcPthread_cond_init(cond, attr uint32) uint32 {
	clock := uint32(CLOCK_REALTIME)

	if attr != 0 {
		clock = p.u32(attr)
	}

	p.put32(cond, uint32(p.addSync(newCond(clock))))

	return 0
}

func (p *Process) libcPthread_cond_destroy(cond uint32) uint32 {
	p.dropSync(cond)

	return 0
}

func (p *Process) libcPthread_cond_signal(cond uint32) uint32 {
	return codeOf(p.syncCall(cond, defaultCond, &SyncWake{}))
}

func (p *Process) libcPthread_cond_broadcast(cond uint32) uint32 {
	return codeOf(p.syncCall(cond, defaultCond, &SyncWake{all: true}))
}

func (p *Process) condWaitForever(call string) {
	msg := call + ": no other thread to signal\n"

	if t, err := p.fd(2); err == nil {
		p.fileWrite(t, bytesFile([]byte(msg)), len(msg), IoMode{})
	}

	p.abort()
}

func (p *Process) libcPthread_cond_wait(cond, mutex uint32) uint32 {
	p.condWaitForever("pthread_cond_wait")

	return 0
}

func (p *Process) condTimedwait(cond, mutex, at uint32, clock *uint32) error {
	c, err := p.syncAt(cond, defaultCond)

	if err != nil {
		return err
	}

	m, err := p.syncAt(mutex, defaultMutex)

	if err != nil {
		return err
	}

	return p.request(c, &SyncWait{thread: p.pid, lock: m, at: at, clock: clock})
}

func (p *Process) libcPthread_cond_timedwait(cond, mutex, at uint32) uint32 {
	return codeOf(p.condTimedwait(cond, mutex, at, nil))
}

func (p *Process) libcPthread_rwlockattr_init(attr uint32) uint32 {
	clear(p.span(attr, sizeofPthreadRwlockattr))

	return 0
}

func (p *Process) libcPthread_rwlockattr_destroy(attr uint32) uint32 {
	return 0
}

func (p *Process) libcPthread_rwlockattr_setpshared(attr, value uint32) uint32 {
	return p.attrSet(attr, 0, value, PTHREAD_PROCESS_SHARED)
}

func (p *Process) libcPthread_rwlockattr_getpshared(attr, out uint32) uint32 {
	return p.attrGet(attr, 0, out)
}

func (p *Process) libcPthread_rwlock_init(lock, attr uint32) uint32 {
	p.put32(lock, uint32(p.addSync(newRWLock())))

	return 0
}

func (p *Process) libcPthread_rwlock_destroy(lock uint32) uint32 {
	p.dropSync(lock)

	return 0
}

func (p *Process) libcPthread_rwlock_rdlock(lock uint32) uint32 {
	return codeOf(p.syncCall(lock, defaultRWLock, &SyncLock{thread: p.pid, shared: true}))
}

func (p *Process) libcPthread_rwlock_tryrdlock(lock uint32) uint32 {
	return codeOf(p.syncCall(lock, defaultRWLock, &SyncLock{thread: p.pid, shared: true, try: true}))
}

func (p *Process) libcPthread_rwlock_timedrdlock(lock, at uint32) uint32 {
	return codeOf(p.timedLock(lock, defaultRWLock, true, at, CLOCK_REALTIME))
}

func (p *Process) libcPthread_rwlock_wrlock(lock uint32) uint32 {
	return codeOf(p.syncCall(lock, defaultRWLock, &SyncLock{thread: p.pid}))
}

func (p *Process) libcPthread_rwlock_trywrlock(lock uint32) uint32 {
	return codeOf(p.syncCall(lock, defaultRWLock, &SyncLock{thread: p.pid, try: true}))
}

func (p *Process) libcPthread_rwlock_timedwrlock(lock, at uint32) uint32 {
	return codeOf(p.timedLock(lock, defaultRWLock, false, at, CLOCK_REALTIME))
}

func (p *Process) libcPthread_rwlock_unlock(lock uint32) uint32 {
	return codeOf(p.syncCall(lock, defaultRWLock, &SyncUnlock{thread: p.pid}))
}

func (p *Process) libcPthread_barrierattr_init(attr uint32) uint32 {
	clear(p.span(attr, sizeofPthreadBarrierattr))

	return 0
}

func (p *Process) libcPthread_barrierattr_destroy(attr uint32) uint32 {
	return 0
}

func (p *Process) libcPthread_barrierattr_setpshared(attr, value uint32) uint32 {
	return p.attrSet(attr, 0, value, PTHREAD_PROCESS_SHARED)
}

func (p *Process) libcPthread_barrierattr_getpshared(attr, out uint32) uint32 {
	return p.attrGet(attr, 0, out)
}

func (p *Process) libcPthread_barrier_init(barrier, attr, count uint32) uint32 {
	if count == 0 || count > math.MaxInt32 {
		return uint32(EINVAL)
	}

	p.put32(barrier, uint32(p.addSync(newBarrier(count))))

	return 0
}

func (p *Process) libcPthread_barrier_destroy(barrier uint32) uint32 {
	p.dropSync(barrier)

	return 0
}

func (p *Process) libcPthread_barrier_wait(barrier uint32) uint32 {
	r := SyncArrive{}

	if err := p.syncCall(barrier, nil, &r); err != nil {
		return codeOf(err)
	}

	if r.serial {
		return math.MaxUint32
	}

	return 0
}

func (p *Process) libcPthread_spin_init(lock, value uint32) uint32 {
	p.put32(lock, uint32(p.addSync(newSpinLock())))

	return 0
}

func (p *Process) libcPthread_spin_destroy(lock uint32) uint32 {
	p.dropSync(lock)

	return 0
}

func (p *Process) libcPthread_spin_lock(lock uint32) uint32 {
	return codeOf(p.syncCall(lock, defaultSpinLock, &SyncLock{thread: p.pid}))
}

func (p *Process) libcPthread_spin_trylock(lock uint32) uint32 {
	return codeOf(p.syncCall(lock, defaultSpinLock, &SyncLock{thread: p.pid, try: true}))
}

func (p *Process) libcPthread_spin_unlock(lock uint32) uint32 {
	return codeOf(p.syncCall(lock, defaultSpinLock, &SyncUnlock{thread: p.pid}))
}

func (p *Process) libcSem_init(sem, shared, value uint32) uint32 {
	if value > SEM_VALUE_MAX {
		return p.result(0, EINVAL)
	}

	p.put32(sem, uint32(p.addSync(newSemaphore(value))))

	return p.result(0, nil)
}

func (p *Process) libcSem_destroy(sem uint32) uint32 {
	p.dropSync(sem)

	return p.result(0, nil)
}

func (p *Process) libcSem_wait(sem uint32) uint32 {
	return p.result(0, p.syncCall(sem, nil, &SyncLock{thread: p.pid}))
}

func (p *Process) libcSem_trywait(sem uint32) uint32 {
	return p.result(0, p.syncCall(sem, nil, &SyncLock{thread: p.pid, try: true}))
}

func (p *Process) libcSem_timedwait(sem, at uint32) uint32 {
	err := p.syncCall(sem, nil, &SyncLock{thread: p.pid, try: true})

	if err != EAGAIN {
		return p.result(0, err)
	}

	end, err := p.deadlineOf(at, CLOCK_REALTIME)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(0, p.syncCall(sem, nil, &SyncLock{thread: p.pid, end: end}))
}

func (p *Process) libcSem_post(sem uint32) uint32 {
	return p.result(0, p.syncCall(sem, nil, &SyncPost{}))
}

func (p *Process) libcSem_getvalue(sem, out uint32) uint32 {
	r := SyncValue{}

	if err := p.syncCall(sem, nil, &r); err != nil {
		return p.result(0, err)
	}

	p.put32(out, r.value)

	return p.result(0, nil)
}

func semaphoreName(name string) (string, error) {
	key := strings.TrimLeft(name, "/")

	switch {
	case key == "" || key == "." || key == ".." || strings.Contains(key, "/"):
		return "", EINVAL
	case len(key) > NAME_MAX:
		return "", ENAMETOOLONG
	}

	return key, nil
}

func (p *Process) semOpen(name string, flags, va uint32) (uint32, error) {
	key, err := semaphoreName(name)

	if err != nil {
		return 0, err
	}

	if o := p.semOpens[key]; o != nil {
		if flags&(O_CREAT|O_EXCL) == O_CREAT|O_EXCL {
			return 0, EEXIST
		}

		o.count++

		return o.at, nil
	}

	p.k.mu.Lock()

	s := p.k.semaphores[key]

	switch {
	case s != nil && flags&(O_CREAT|O_EXCL) == O_CREAT|O_EXCL:
		p.k.mu.Unlock()

		return 0, EEXIST
	case s == nil && flags&O_CREAT == 0:
		p.k.mu.Unlock()

		return 0, ENOENT
	case s == nil:
		value := p.u32(va + 4)

		if value > SEM_VALUE_MAX {
			p.k.mu.Unlock()

			return 0, EINVAL
		}

		s = newSemaphore(value)
		s.name, s.mode, s.uid, s.gid = key, p.u32(va)&0o777&^p.umask, p.cred.euid(), p.cred.egid()

		p.k.semaphores[key] = s
	}

	p.k.mu.Unlock()

	if !p.permits(&Stat{Mode: s.mode, Uid: s.uid, Gid: s.gid}, 6) {
		return 0, EACCES
	}

	at := p.malloc(sizeofSem)

	if at == 0 {
		return 0, ENOMEM
	}

	s.ref()
	p.put32(at, uint32(p.addSync(s)))
	p.semOpens[key] = &SemOpen{at: at, count: 1}

	return at, nil
}

func (p *Process) libcSem_open(name, flags, va uint32) uint32 {
	at, err := p.semOpen(p.str(name), flags, va)

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	return at
}

func (p *Process) libcSem_close(sem uint32) uint32 {
	for key, o := range p.semOpens {
		if o.at != sem {
			continue
		}

		if o.count--; o.count == 0 {
			p.dropSync(sem)
			p.free(sem)
			delete(p.semOpens, key)
		}

		return p.result(0, nil)
	}

	return p.result(0, EINVAL)
}

func (p *Process) libcSem_unlink(name uint32) uint32 {
	key, err := semaphoreName(p.str(name))

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	s := p.k.semaphores[key]

	switch {
	case s == nil:
		return p.result(0, ENOENT)
	case !p.cred.privileged() && p.cred.euid() != s.uid:
		return p.result(0, EACCES)
	}

	delete(p.k.semaphores, key)

	return p.result(0, nil)
}

func (p *Process) libcThrd_create(thread, fn, arg uint32) uint32 {
	return threadsCode(EAGAIN)
}

func (p *Process) libcThrd_join(thread, out uint32) uint32 {
	return thrd_error
}

func (p *Process) libcThrd_detach(thread uint32) uint32 {
	return threadsCode(errnoOrNil(p.libcPthread_detach(thread)))
}

func (p *Process) libcThrd_current() uint32 {
	return uint32(p.pid)
}

func (p *Process) libcThrd_equal(a, b uint32) uint32 {
	return bit(a == b)
}

func (p *Process) libcThrd_sleep(at, rem uint32) uint32 {
	d, err := p.timespec(at)

	if err != nil {
		return math.MaxUint32 - 1
	}

	if left := p.sleep(d); left > 0 {
		if rem != 0 {
			p.putTimespec(rem, left)
		}

		return math.MaxUint32
	}

	return 0
}

func (p *Process) libcThrd_yield() {
	runtime.Gosched()
}

func (p *Process) libcMtx_init(mutex, kind uint32) uint32 {
	m := uint32(PTHREAD_MUTEX_NORMAL)

	if kind&mtx_recursive != 0 {
		m = PTHREAD_MUTEX_RECURSIVE
	}

	p.put32(mutex, uint32(p.addSync(newMutex(m))))

	return thrd_success
}

func (p *Process) libcMtx_destroy(mutex uint32) {
	p.dropSync(mutex)
}

func (p *Process) libcMtx_lock(mutex uint32) uint32 {
	return threadsCode(errnoOrNil(p.libcPthread_mutex_lock(mutex)))
}

func (p *Process) libcMtx_trylock(mutex uint32) uint32 {
	return threadsCode(errnoOrNil(p.libcPthread_mutex_trylock(mutex)))
}

func (p *Process) libcMtx_timedlock(mutex, at uint32) uint32 {
	return threadsCode(errnoOrNil(p.libcPthread_mutex_timedlock(mutex, at)))
}

func (p *Process) libcMtx_unlock(mutex uint32) uint32 {
	return threadsCode(errnoOrNil(p.libcPthread_mutex_unlock(mutex)))
}

func (p *Process) libcCnd_init(cond uint32) uint32 {
	p.put32(cond, uint32(p.addSync(newCond(CLOCK_REALTIME))))

	return thrd_success
}

func (p *Process) libcCnd_destroy(cond uint32) {
	p.dropSync(cond)
}

func (p *Process) libcCnd_broadcast(cond uint32) uint32 {
	return threadsCode(errnoOrNil(p.libcPthread_cond_broadcast(cond)))
}

func (p *Process) libcCnd_signal(cond uint32) uint32 {
	return threadsCode(errnoOrNil(p.libcPthread_cond_signal(cond)))
}

func (p *Process) libcCnd_wait(cond, mutex uint32) uint32 {
	p.condWaitForever("cnd_wait")

	return thrd_error
}

func (p *Process) libcCnd_timedwait(cond, mutex, at uint32) uint32 {
	return threadsCode(p.condTimedwait(cond, mutex, at, nil))
}

func (p *Process) libcTss_create(key, fn uint32) uint32 {
	if p.libcPthread_key_create(key, fn) != 0 {
		return thrd_error
	}

	return thrd_success
}

func (p *Process) libcTss_delete(key uint32) {
	p.libcPthread_key_delete(key)
}

func (p *Process) libcTss_set(key, value uint32) uint32 {
	if p.libcPthread_setspecific(key, value) != 0 {
		return thrd_error
	}

	return thrd_success
}

func (p *Process) libcTss_get(key uint32) uint32 {
	return p.libcPthread_getspecific(key)
}

func errnoOrNil(code uint32) error {
	if code == 0 {
		return nil
	}

	return Errno(code)
}

func (p *Process) libcMkostemps(template, suffix, flags uint32) uint32 {
	return p.mkstemp(template, suffix, flags)
}

func (p *Process) libcLchmod(path, mode uint32) uint32 {
	return p.fchmodat(AT_FDCWD, path, mode, AT_SYMLINK_NOFOLLOW)
}

func (p *Process) libcPosix_close(fd, flags uint32) uint32 {
	return p.libcClose(fd)
}

func (p *Process) libcIssetugid() uint32 {
	return 0
}

func (p *Process) libcTimespec_get(ts, base uint32) uint32 {
	if base != TIME_UTC {
		return 0
	}

	t, err := p.clockNow(CLOCK_REALTIME)

	if err != nil {
		return 0
	}

	p.put64(ts, uint64(t/1e9))
	p.put32(ts+8, uint32(t%1e9))

	return base
}

func (p *Process) libcEventfd(count, flags uint32) uint32 {
	if flags&^(EFD_SEMAPHORE|O_CLOEXEC|O_NONBLOCK) != 0 {
		return p.result(0, EINVAL)
	}

	e := newEventFd(uint64(count), flags&EFD_SEMAPHORE != 0, flags&O_NONBLOCK != 0)

	return p.result(p.install(e, 0, flags&O_CLOEXEC != 0))
}

func (p *Process) libcEventfd_read(fd, value uint32) uint32 {
	if p.libcRead(fd, value, 8) != 8 {
		return math.MaxUint32
	}

	return 0
}

func (p *Process) libcEventfd_write(fd uint32, value uint64) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	n, err := p.fileWrite(t, bytesFile(binary.LittleEndian.AppendUint64(nil, value)), 8, IoMode{})

	if err == nil && n != 8 {
		err = EIO
	}

	return p.result(0, err)
}

func (p *Process) libcTimerfd_create(clock, flags uint32) uint32 {
	if clock != CLOCK_REALTIME && clock != CLOCK_MONOTONIC && clock != CLOCK_BOOTTIME || flags&^(O_CLOEXEC|O_NONBLOCK) != 0 {
		return p.result(0, EINVAL)
	}

	return p.result(p.install(newTimerFd(clock, flags&O_NONBLOCK != 0), 0, flags&O_CLOEXEC != 0))
}

func (p *Process) libcTimerfd_settime(fd, flags, value, old uint32) uint32 {
	if flags&^(TFD_TIMER_ABSTIME|TFD_TIMER_CANCEL_ON_SET) != 0 {
		return p.result(0, EINVAL)
	}

	interval, err := p.timespec(value)

	if err != nil {
		return p.result(0, err)
	}

	first, err := p.timespec(value + sizeofTimespec)

	if err != nil {
		return p.result(0, err)
	}

	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	r := TimerSet{value: first, interval: interval, absolute: flags&TFD_TIMER_ABSTIME != 0}

	if err := p.request(t, &r); err != nil {
		return p.result(0, err)
	}

	if old != 0 {
		p.putTimespec(old, r.every)
		p.putTimespec(old+sizeofTimespec, r.left)
	}

	return p.result(0, nil)
}

func (p *Process) libcTimerfd_gettime(fd, at uint32) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	r := TimerGet{}

	if err := p.request(t, &r); err != nil {
		return p.result(0, err)
	}

	p.putTimespec(at, r.every)
	p.putTimespec(at+sizeofTimespec, r.left)

	return p.result(0, nil)
}

func (p *Process) libcInotify_init() uint32 {
	return p.libcInotify_init1(0)
}

func (p *Process) libcInotify_init1(flags uint32) uint32 {
	if flags&^(O_CLOEXEC|O_NONBLOCK) != 0 {
		return p.result(0, EINVAL)
	}

	return p.result(p.install(newInotify(flags&O_NONBLOCK != 0), 0, flags&O_CLOEXEC != 0))
}

func (p *Process) libcInotify_add_watch(fd, path, mask uint32) uint32 {
	if mask&IN_ALL_EVENTS == 0 || mask&IN_MASK_ADD != 0 && mask&IN_MASK_CREATE != 0 {
		return p.result(0, EINVAL)
	}

	in, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	r := InotifyAdd{path: p.str(path), mask: mask}

	err = p.request(in, &r)

	return p.result(int(r.wd), err)
}

func (p *Process) libcInotify_rm_watch(fd, wd uint32) uint32 {
	in, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(0, p.request(in, &InotifyRemove{wd: int32(wd)}))
}

func (p *Process) libcTimer_create(clock, event, at uint32) uint32 {
	switch clock {
	case CLOCK_REALTIME, CLOCK_MONOTONIC, CLOCK_BOOTTIME:
	case CLOCK_PROCESS_CPUTIME_ID, CLOCK_THREAD_CPUTIME_ID:
		return p.result(0, ENOTSUP)
	default:
		if _, ok := p.hostClock(clock); ok && int32(clock) < 0 {
			return p.result(0, ENOTSUP)
		}

		return p.result(0, EINVAL)
	}

	id := int32(0)

	for p.posixTimers[id] != nil {
		id++
	}

	t := &PosixTimer{id: id, clock: clock, notify: SIGEV_SIGNAL, signal: SIGALRM, value: uint32(id)}

	if event != 0 {
		t.value, t.signal, t.notify = p.u32(event), int(p.u32(event+4)), p.u32(event+8)

		switch t.notify {
		case SIGEV_NONE:
		case SIGEV_THREAD:
			return p.result(0, EAGAIN)
		case SIGEV_THREAD_ID:
			if p.u32(event+12) != uint32(p.pid) || !validSignal(uint32(t.signal)) {
				return p.result(0, EINVAL)
			}
		case SIGEV_SIGNAL:
			if !validSignal(uint32(t.signal)) {
				return p.result(0, EINVAL)
			}
		default:
			return p.result(0, EINVAL)
		}
	}

	if p.posixTimers == nil {
		p.posixTimers = map[int32]*PosixTimer{}
	}

	p.posixTimers[id] = t
	p.put32(at, uint32(id))

	return p.result(0, nil)
}

func (p *Process) posixTimer(timer uint32) (*PosixTimer, error) {
	if t := p.posixTimers[int32(timer)]; t != nil {
		return t, nil
	}

	return nil, EINVAL
}

func (p *Process) disarmPosix(t *PosixTimer) {
	if t.timer != nil {
		t.timer.Stop()
	}

	t.epoch++
	t.timer, t.interval = nil, 0
}

func (p *Process) armPosix(t *PosixTimer, end time.Time) {
	t.end = end

	epoch, id := t.epoch, t.id
	e := &Event{kind: eventTimer, run: func(p *Process) { p.posixDue(id, epoch) }}

	t.timer = time.AfterFunc(time.Until(end), func() {
		p.post(e)
		p.nudge()
	})
}

func (p *Process) timerQueued(t *PosixTimer) bool {
	p.sig.mu.Lock()

	defer p.sig.mu.Unlock()

	for _, info := range p.sig.infos[t.signal] {
		if info.code == SI_TIMER && info.timer == t.id {
			return true
		}
	}

	return false
}

func (p *Process) posixDue(id int32, epoch uint64) {
	t := p.posixTimers[id]

	if t == nil || t.epoch != epoch {
		return
	}

	t.epoch++

	passed := int64(1)

	if t.interval > 0 {
		passed += int64(max(time.Since(t.end), 0) / t.interval)
		p.armPosix(t, t.end.Add(time.Duration(passed)*t.interval))
	} else {
		t.timer = nil
	}

	if t.notify == SIGEV_NONE {
		return
	}

	if p.timerQueued(t) {
		t.overrun = int32(min(int64(t.overrun)+passed, DELAYTIMER_MAX))

		return
	}

	t.overrun = int32(min(passed-1, DELAYTIMER_MAX))
	p.sendSelfInfo(t.signal, SigInfo{code: SI_TIMER, value: t.value, timer: t.id})
}

func (p *Process) libcTimer_delete(timer uint32) uint32 {
	t, err := p.posixTimer(timer)

	if err != nil {
		return p.result(0, err)
	}

	p.disarmPosix(t)
	delete(p.posixTimers, t.id)

	return p.result(0, nil)
}

func (p *Process) libcTimer_settime(timer, flags, value, old uint32) uint32 {
	t, err := p.posixTimer(timer)

	if err != nil {
		return p.result(0, err)
	}

	interval, err := p.timespec(value)

	if err != nil {
		return p.result(0, err)
	}

	first, err := p.timespec(value + sizeofTimespec)

	if err != nil {
		return p.result(0, err)
	}

	left, every := t.left(), t.interval

	p.disarmPosix(t)
	t.overrun = 0

	if first > 0 {
		if host, ok := hostClock(t.clock); ok && flags&TIMER_ABSTIME != 0 {
			first = max(first-clockGettime(host), time.Nanosecond)
		}

		t.interval = interval
		p.armPosix(t, time.Now().Add(first))
	}

	if old != 0 {
		p.putTimespec(old, every)
		p.putTimespec(old+sizeofTimespec, left)
	}

	return p.result(0, nil)
}

func (p *Process) libcTimer_gettime(timer, at uint32) uint32 {
	t, err := p.posixTimer(timer)

	if err != nil {
		return p.result(0, err)
	}

	p.putTimespec(at, t.interval)
	p.putTimespec(at+sizeofTimespec, t.left())

	return p.result(0, nil)
}

func (p *Process) libcTimer_getoverrun(timer uint32) uint32 {
	t, err := p.posixTimer(timer)

	if err != nil {
		return p.result(0, err)
	}

	return uint32(t.last)
}
