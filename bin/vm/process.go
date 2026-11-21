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
	"reflect"
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

var (
	itimerSignals  = [3]int{SIGALRM, SIGVTALRM, SIGPROF}
	zoneDirs       = []string{"/usr/share/zoneinfo/", "/share/zoneinfo/", "/etc/zoneinfo/"}
	pathconfValues = []int32{8, 255, 255, 255, 4096, 4096, 1, 1, 0, 1, -1, -1, -1, 64, 4096, 4096, 4096, 4096, 4096, -1, 1}
	hostMessages   = []string{"Host not found", "Try again", "Non-recoverable error", "Address not available"}
	localeVars     = [LC_ALL]string{"LC_CTYPE", "LC_NUMERIC", "LC_TIME", "LC_COLLATE", "LC_MONETARY", "LC_MESSAGES"}
	wcClassNames   = []string{"alnum", "alpha", "blank", "cntrl", "digit", "graph", "lower", "print", "punct", "space", "upper", "xdigit"}
)

var randomInit = [32]uint32{
	0x00000000, 0x5851f42d, 0xc0b18ccf, 0xcbb5f646, 0xc7033129, 0x30705b04, 0x20fd5db4, 0x9a8b7f78,
	0x502959d8, 0xab894868, 0x6c0356a7, 0x88cdb7ff, 0xb477d43f, 0x70a3a52b, 0xa8e4baf1, 0xfd8341fc,
	0x8ae16fd9, 0x742d2f7a, 0x0d1f0796, 0x76035e09, 0x40f7702c, 0x6fa72ca5, 0xaaa84157, 0x58a0df74,
	0xc74a0364, 0xae533cc4, 0x04185faf, 0x6de3b115, 0x0cab8628, 0xf043bfa4, 0x398150e9, 0x37521657,
}

var gaiMessages = []string{"Invalid flags", "Name does not resolve", "Try again", "Non-recoverable error", "Name has no usable address",
	"Unrecognized address family or invalid length", "Unrecognized socket type", "Unrecognized service", "Unknown error",
	"Out of memory", "System error", "Overflow"}

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

var wellKnown = []ServiceEntry{
	{"echo", 7, "tcp"}, {"echo", 7, "udp"}, {"discard", 9, "tcp"}, {"discard", 9, "udp"}, {"daytime", 13, "tcp"},
	{"ftp-data", 20, "tcp"}, {"ftp", 21, "tcp"}, {"ssh", 22, "tcp"}, {"telnet", 23, "tcp"}, {"smtp", 25, "tcp"},
	{"time", 37, "tcp"}, {"domain", 53, "tcp"}, {"domain", 53, "udp"}, {"tftp", 69, "udp"}, {"finger", 79, "tcp"},
	{"http", 80, "tcp"}, {"pop3", 110, "tcp"}, {"sunrpc", 111, "tcp"}, {"sunrpc", 111, "udp"}, {"nntp", 119, "tcp"},
	{"ntp", 123, "udp"}, {"imap", 143, "tcp"}, {"snmp", 161, "udp"}, {"ldap", 389, "tcp"}, {"https", 443, "tcp"},
	{"syslog", 514, "udp"}, {"submission", 587, "tcp"}, {"imaps", 993, "tcp"}, {"pop3s", 995, "tcp"},
}

var langItems = map[int][]string{
	LC_NUMERIC: {".", ""},
	LC_TIME: {
		"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat",
		"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday",
		"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
		"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December",
		"AM", "PM", "%a %b %e %T %Y", "%m/%d/%y", "%H:%M:%S", "%I:%M:%S %p",
		"", "", "%m/%d/%y", "0123456789", "%a %b %e %T %Y", "%H:%M:%S",
	},
	LC_MESSAGES: {"^[yY]", "^[nN]", "yes", "no"},
}

var regMessages = map[uint32]string{
	0: "No error", REG_NOMATCH: "No match", REG_BADPAT: "Invalid regexp", 3: "Unknown collating element",
	4: "Unknown character class name", REG_EESCAPE: "Trailing backslash", REG_ESUBREG: "Invalid back reference",
	REG_EBRACK: "Missing ']'", REG_EPAREN: "Missing ')'", REG_EBRACE: "Missing '}'", REG_BADBR: "Invalid contents of {}",
	11: "Invalid character range", 12: "Out of memory", REG_BADRPT: "Repetition not preceded by valid expression",
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
	passwordSize     = 128
	zoneMax          = 1 << 20
	logIdentSize     = 32
	utsSysname       = "pizdos"
	utsRelease       = "1"
	utsVersion       = "#1"
	utsMachine       = "wasm32"
	databaseMax      = 16 << 20
	resolveWait      = 10 * time.Second
	localeNameMax    = 23
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

	errnoAt    uint32
	environAt  uint32
	tzAt       [3]uint32
	herrnoAt   uint32
	passwordAt uint32

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
	regexes   map[uint32]*PosixRegex
	nextRegex uint32

	constants map[string]uint32
	siginfo   uint32

	locale     [LC_ALL]string
	zone       ZoneCache
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
		p.crash(err.error(), nil)
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
		p.tzAt, p.herrnoAt, p.passwordAt = [3]uint32{}, 0, 0
		p.regexes, p.constants, p.locale = map[uint32]*PosixRegex{}, map[string]uint32{}, [LC_ALL]string{}
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
			}

			who.crash(msg, t.stack)
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

func (p *Process) libc__kernel_start(errnoAt, environAt, argcAt, argvAt, tznameAt, timezoneAt, daylightAt, herrnoAt uint32) {
	p.errnoAt, p.environAt = errnoAt, environAt
	p.tzAt, p.herrnoAt = [3]uint32{tznameAt, timezoneAt, daylightAt}, herrnoAt

	argv := p.stringArray(p.image.argv)

	p.env = p.stringArray(p.image.envp)

	if argv == 0 || p.env == 0 {
		p.exitWith(SIGABRT)
	}

	p.put32(argcAt, uint32(len(p.image.argv)))
	p.put32(argvAt, argv)
	p.put32(environAt, p.env)
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

	return p.inst.memfd.splice(int64(at), int(n), mode)
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

func (p *Process) loadProgram(path string, argv []string) (*ExecImage, error) {
	return p.loadFrom(path, nil, argv)
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

func (p *Process) execPath(file string, argv, envp []string) error {
	if file == "" {
		return ENOENT
	}

	if strings.Contains(file, "/") {
		return p.exec(file, argv, envp)
	}

	path, ok := p.getenv("PATH")

	if !ok {
		path = "/usr/local/bin:/bin:/usr/bin"
	}

	err := error(ENOENT)

	for _, dir := range strings.Split(path, ":") {
		if dir == "" {
			dir = "."
		}

		switch e := p.exec(dir+"/"+file, argv, envp); e {
		case ENOENT, ENOTDIR:
		case EACCES:
			err = e
		default:
			return e
		}
	}

	return err
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

func (p *Process) libcExecvp(file, argv uint32) uint32 {
	return p.result(0, p.execPath(p.str(file), p.strings(argv), p.environment()))
}

func (p *Process) libcExecv(path, argv uint32) uint32 {
	return p.result(0, p.exec(p.str(path), p.strings(argv), p.environment()))
}

func (p *Process) libcExecvpe(file, argv, envp uint32) uint32 {
	return p.result(0, p.execPath(p.str(file), p.strings(argv), p.strings(envp)))
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

	go func() {
		c.run(img)
		ended.Store(true)
		p.post(&p.own.child)
	}()

	intr := p.setAction(SIGINT, SigAction{handler: SIG_IGN})
	quit := p.setAction(SIGQUIT, SigAction{handler: SIG_IGN})
	mask := p.setMask(p.mask() | sigBit(SIGCHLD))

	p.wait(time.Time{}, func(*Event) (bool, error) {
		return ended.Load(), nil
	})

	p.setAction(SIGINT, intr)
	p.setAction(SIGQUIT, quit)
	p.setMask(mask)

	status := c.status

	p.k.mu.Lock()
	delete(p.k.procs, c.pid)
	p.k.mu.Unlock()

	return uint32(int32(status))
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

	switch policy {
	case SCHED_OTHER, SCHED_BATCH, SCHED_IDLE:
		if p.u32(param) != 0 {
			return p.result(0, EINVAL)
		}

		p.k.mu.Lock()
		q.policy = int32(policy)
		p.k.mu.Unlock()

		return p.result(0, nil)
	case SCHED_FIFO, SCHED_RR:
		return p.result(0, EPERM)
	}

	return p.result(0, EINVAL)
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
	p.fatal("vfork: the function that called vfork has returned")
}

func (p *Process) startVfork() uint32 {
	v := &VforkFrame{parent: p}

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
	c.heap, c.stack, c.static, c.env, c.errnoAt, c.environAt, c.siginfo = p.heap, p.stack, p.static, p.env, p.errnoAt, p.environAt, p.siginfo
	c.regexes, c.nextRegex, c.constants, c.jumps, c.stackAt = p.regexes, p.nextRegex, p.constants, p.jumps, p.stackAt
	c.locale, c.envStrings, c.mappings, c.tzAt, c.herrnoAt, c.passwordAt = p.locale, p.envStrings, p.mappings, p.tzAt, p.herrnoAt, p.passwordAt
	c.token.Store(p.token.Load())
	c.inherit(p)
	p.inst.attach(c)
	c.trace(TRACE_PROCESS, fmt.Sprintf("child of %d", p.pid))

	return c.result(0, nil)
}

func (p *Process) fork(setup, begin func(c *Process)) (int32, error) {
	if p.chaosFail("fork") {
		return 0, EAGAIN
	}

	j := new(Jump)

	if !p.inst.setjmp(j) {
		p.fatal("fork: called through a pointer")
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
	c.nextRegex, c.stackAt, c.locale, c.tzAt, c.herrnoAt, c.passwordAt = p.nextRegex, p.stackAt, p.locale, p.tzAt, p.herrnoAt, p.passwordAt
	c.regexes, c.constants, c.jumps = map[uint32]*PosixRegex{}, map[string]uint32{}, map[uint32]*JmpBuf{}

	for id, r := range p.regexes {
		copied := *r

		c.regexes[id] = &copied
	}

	for s, at := range p.constants {
		c.constants[s] = at
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

	if setup != nil {
		setup(c)
	}

	parent := p.pid

	go c.runFrom(p.image, inst, func() {
		c.trace(TRACE_PROCESS, fmt.Sprintf("child of %d", parent))

		if begin != nil {
			begin(c)
		}

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
		clear(p.span(info, 36))

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

func (p *Process) libcFork() uint32 {
	pid, err := p.fork(nil, nil)

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
		s.pending &^= stopSignals
	case sigBit(sig)&stopSignals != 0:
		s.pending &^= sigBit(SIGCONT)
	}
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
	if sig == SIGCONT {
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

	if p == p.k.first && !blocked && act.handler == SIG_DFL && defaultAction(sig) == actTerminate {
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
	p.sig.mu.Lock()
	p.sig.sent(sig)

	act := p.sig.actions[sig]

	if !act.ignored(sig) || p.sig.blocked&sigBit(sig) != 0 {
		p.sig.pending |= sigBit(sig)
	}

	p.sig.mu.Unlock()
	p.signalled()
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

func (p *Process) take() (int, SigAction) {
	p.sig.mu.Lock()

	defer p.sig.mu.Unlock()

	ready := p.sig.pending &^ p.sig.blocked

	if ready == 0 {
		return 0, SigAction{}
	}

	sig := bits.TrailingZeros64(ready) + 1

	p.sig.pending &^= sigBit(sig)

	act := p.sig.actions[sig]

	if act.handler != SIG_DFL && act.handler != SIG_IGN && act.flags&SA_RESETHAND != 0 {
		p.sig.actions[sig] = SigAction{}
	}

	return sig, act
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
		sig, act := p.take()

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

			p.runHandler(sig, act)
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

func (p *Process) runHandler(sig int, act SigAction) {
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
			p.siginfo = p.malloc(36)
		}

		info := p.span(p.siginfo, 36)

		clear(info)
		p.put32(p.siginfo, uint32(sig))
		p.put32(p.siginfo+12, uint32(p.pid))

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

func (p *Process) sigwait(set uint64, timeout time.Duration) (int, error) {
	set &^= unblockable
	var end time.Time

	if timeout >= 0 {
		end = time.Now().Add(timeout)
	}

	old := p.setMask(p.mask() | set)

	defer p.setMask(old)

	sig := 0

	timedOut, err := p.wait(end, func(*Event) (bool, error) {
		p.sig.mu.Lock()

		if ready := p.sig.pending & set; ready != 0 {
			sig = bits.TrailingZeros64(ready) + 1
			p.sig.pending &^= sigBit(sig)
		}

		p.sig.mu.Unlock()

		if sig != 0 {
			return true, nil
		}

		return p.interrupts(false), EINTR
	})

	if timedOut {
		return 0, EAGAIN
	}

	return sig, err
}

func (p *Process) putSiginfo(at uint32, sig int) {
	if at != 0 {
		clear(p.span(at, 36))
		p.put32(at, uint32(sig))
	}
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
		p.sig.pending &^= sigBit(sig)
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
	if sig != 0 && !validSignal(sig) {
		return EINVAL
	}

	targets := p.k.signalTargets(p, pid)

	if len(targets) == 0 {
		return ESRCH
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

func (p *Process) libcSiginterrupt(sig, flag uint32) uint32 {
	if !validSignal(sig) {
		return p.result(0, EINVAL)
	}

	p.sig.mu.Lock()

	if flag != 0 {
		p.sig.actions[sig].flags &^= SA_RESTART
	} else {
		p.sig.actions[sig].flags |= SA_RESTART
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

	return p.result(0, EINTR)
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

	return p.result(0, EINTR)
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

	return p.result(0, p.kill(int32(pid), sig))
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
			p.fatal("setjmp: no __stack_pointer")
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
	select {
	case p.events.bell <- struct{}{}:
	default:
	}
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
		p.fatal("no __heap_base")
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
	p.fatal(fmt.Sprintf("%s(): invalid pointer %#x", what, ptr))
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

func (p *Process) fatal(msg string) {
	p.crash(msg, callers(1))
	p.exitWith(SIGABRT)
}

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

	if p.traces(TRACE_FAULTS) {
		p.sinkWrite([]byte(b.String()))

		if p.sink != nil {
			return
		}
	}

	if t, err := p.fd(2); err == nil {
		if _, err := p.fileWrite(t, bytesFile([]byte(b.String())), b.Len(), IoMode{quiet: true}); err == nil {
			return
		}
	}

	p.k.report(b.String())
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

		w.unref()

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

				w.unref()

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
		w.unref()
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

func (p *Process) libcValloc(n uint32) uint32 {
	at := p.memalign(4096, n)

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

func (p *Process) libcMalloc_trim(pad uint32) uint32 {
	return 0
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
		p.fatal(p.asanReport(&Trap{code: trapAsan, addr: uint64(at) + uint64(ok), kind: asanRange}))
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

	table := p.span(iovs, 8*count)
	total := 0

	for i := range count {
		total += len(p.span(binary.LittleEndian.Uint32(table[8*i:]), binary.LittleEndian.Uint32(table[8*i+4:])))
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
		at += copy(data[at:], p.span(binary.LittleEndian.Uint32(table[8*i:]), binary.LittleEndian.Uint32(table[8*i+4:])))
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

	w.unref()

	return p.result(count, err)
}

func (p *Process) libcWrite(fd, buf, n uint32) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	w := p.memory(buf, n, O_RDONLY)
	count, err := p.fileWrite(t, w, int(n), IoMode{})

	w.unref()

	return p.result(count, err)
}

func (p *Process) libcPread(fd, buf, n uint32, off uint64) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	w := p.memory(buf, n, O_WRONLY)
	count, err := p.filePread(t, w, int(n), int64(off))

	w.unref()

	return p.result(count, err)
}

func (p *Process) libcPwrite(fd, buf, n uint32, off uint64) uint32 {
	t, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	w := p.memory(buf, n, O_RDONLY)
	count, err := p.filePwrite(t, w, int(n), int64(off))

	w.unref()

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
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	if p.device(obj) != (FileKey{}) {
		if r, done := p.terminalIoctl(obj, fd, req, va); done {
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

		if p.u32(p.varArg(va, 0)) != 0 {
			fl |= O_NONBLOCK
		}

		return p.result(0, p.fileSetFlags(obj, fl))
	case FIOCLEX, FIONCLEX:
		p.files.slots[fd].cloexec = req == FIOCLEX

		return p.result(0, nil)
	case TIOCGPGRP:
		r := p.libcTcgetpgrp(fd)

		if int32(r) >= 0 {
			p.put32(p.varArg(va, 0), r)
			r = 0
		}

		return r
	case TIOCSPGRP:
		return p.libcTcsetpgrp(fd, p.u32(p.varArg(va, 0)))
	case TIOCGWINSZ:
		w, err := p.ttyWindow(obj)

		if err == ENOTTY {
			break
		}

		if err == nil {
			copy(p.span(p.varArg(va, 0), sizeofWinsize), w)
		}

		return p.result(0, err)
	case FIONREAD:
		var n int32

		if err := p.fileIoctl(obj, FIONREAD, unsafe.Pointer(&n)); err == ENOTTY {
			break
		} else if err != nil {
			return p.result(0, err)
		}

		p.put32(p.varArg(va, 0), uint32(n))

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

func (p *Process) readPath(path string, max int) ([]byte, error) {
	f, err := p.openPath(path, O_RDONLY, 0)

	if err != nil {
		return nil, err
	}

	defer f.unref()

	t := f

	var data []byte

	buf := make([]byte, 65536)
	file := bytesFile(buf)

	for len(data) < max {
		n, err := p.fileRead(t, file, len(buf), IoMode{})

		if err != nil || n == 0 {
			break
		}

		data = append(data, buf[:n]...)
	}

	return data, nil
}

func (p *Process) mkdir(dirfd int32, path string, mode uint32) error {
	return p.onPath(dirfd, path, func(d Object) error {
		return p.dirMkdir(d, path, mode&0o7777&^p.umask)
	})
}

func (p *Process) mknod(dirfd int32, path string, mode uint32, dev uint64) error {
	switch mode & S_IFMT {
	case 0, S_IFREG, S_IFIFO:
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

func (p *Process) libcFchdir(fd uint32) uint32 {
	obj, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
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

		return p.result(0, err)
	}

	p.setCwd(d)

	return p.result(0, nil)
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

func (p *Process) libcCanonicalize_file_name(path uint32) uint32 {
	guest, err := p.realpath(p.str(path))

	if err != nil {
		return p.pointer(0, err)
	}

	at := p.newString(guest)

	if at == 0 {
		return p.pointer(0, ENOMEM)
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
		return p.pointer(0, ENOMEM)
	}

	return p.pointer(at, nil)
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

func (p *Process) libcEuidaccess(path, mode uint32) uint32 {
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
	return p.result(0, p.onAt(int32(dirfd), p.str(path), flags, func(a Object) error {
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

		w.unref()

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

func (p *Process) libcMemfd_create(at, n, flags uint32) uint32 {
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
	s, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	h := p.span(msg, sizeofMsghdr)
	to := p.destination(binary.LittleEndian.Uint32(h), binary.LittleEndian.Uint32(h[4:]))
	files, cred, err := p.rights(binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:]))

	if err != nil {
		return p.result(0, err)
	}

	most := packetMax

	if p.sockKind(s) == SOCK_STREAM {
		most = math.MaxInt
	}

	data, err := p.gather(binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:]), most)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(p.sockSend(s, &Packet{data: data, files: files, cred: cred}, to, sendMode(flags)))
}

func sendMode(flags uint32) IoMode {
	return IoMode{nonblock: flags&MSG_DONTWAIT != 0, quiet: flags&MSG_NOSIGNAL != 0}
}

func (p *Process) libcRecvmsg(fd, msg, flags uint32) uint32 {
	var few [4][]byte

	s, err := p.fd(fd)

	if err != nil {
		return p.result(0, err)
	}

	iov, count := p.u32(msg+8), p.u32(msg+12)
	spans, err := p.ioSpansIn(few[:0], iov, count)

	if err != nil {
		return p.result(0, err)
	}

	r := p.recvs.get()

	defer p.recvs.put(r)

	r.into, r.want, r.flags = Into{p: p, at: iov, n: count, vector: true}, totalLength(spans), flags

	if err := p.request(s, r); err != nil {
		return p.result(0, err)
	}

	written, cut := p.putControl(r.files, r.cred, p.u32(msg+16), p.u32(msg+20), flags&MSG_CMSG_CLOEXEC != 0)

	if name := p.u32(msg); name != 0 {
		addr := senderAddress(r.addr)

		copy(p.span(name, min(p.u32(msg+4), uint32(len(addr)))), addr)
		p.put32(msg+4, uint32(len(addr)))
	}

	p.put32(msg+20, written)
	p.put32(msg+24, r.out|cut|flags&MSG_CMSG_CLOEXEC)

	return p.result(r.n, nil)
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
		p.putAddress(senderAddress(r.addr), addr, lenAt)
	}

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

	at := control + written

	p.put32(at, size)
	p.put32(at+4, SOL_SOCKET)
	p.put32(at+8, SCM_CREDENTIALS)
	p.put32(at+12, cred.pid)
	p.put32(at+16, cred.uid)
	p.put32(at+20, cred.gid)

	return written + min(guestAlign(size), room-written), cut
}

func (p *Process) ucred() *Ucred {
	return &Ucred{uint32(p.pid), p.cred.uid(), p.cred.gid()}
}

func (p *Process) rights(at, n uint32) ([]Object, *Ucred, error) {
	var files []Object
	var cred *Ucred

	for off := uint32(0); off+sizeofCmsghdr <= n; {
		length := p.u32(at + off)

		if length < sizeofCmsghdr || off+length > n {
			return nil, nil, EINVAL
		}

		level, kind := p.u32(at+off+4), p.u32(at+off+8)
		data := p.span(at+off+sizeofCmsghdr, length-sizeofCmsghdr)

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

	var fds []byte

	flags := uint32(0)

	for _, f := range files {
		if sizeofCmsghdr+uint32(len(fds))+4 > room {
			f.unref()
			flags |= MSG_CTRUNC

			continue
		}

		fd, err := p.install(f, 0, cloexec)

		if err != nil {
			flags |= MSG_CTRUNC

			continue
		}

		fds = binary.LittleEndian.AppendUint32(fds, uint32(fd))
	}

	if len(fds) == 0 {
		return 0, flags
	}

	size := sizeofCmsghdr + uint32(len(fds))

	p.put32(at, size)
	p.put32(at+4, SOL_SOCKET)
	p.put32(at+8, SCM_RIGHTS)
	copy(p.span(at+sizeofCmsghdr, uint32(len(fds))), fds)

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

func (p *Process) connectUnix(v Object, path string, kind int, nonblock bool, to *atomic.Int64) (*PairEnd, error) {
	f, err := p.dirConnect(v, path)

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
		f, err := p.dirConnect(v, path)

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

	end := f

	if len(pk.data) != sizeofOpened {
		f.unref()

		return nil, false, EIO
	}

	b := pk.data
	kind := binary.LittleEndian.Uint32(b)

	obj, err := served(p, kind, int(binary.LittleEndian.Uint32(b[4:])), binary.LittleEndian.Uint64(b[8:]), binary.LittleEndian.Uint64(b[16:]),
		binary.LittleEndian.Uint64(b[24:]), binary.LittleEndian.Uint32(b[32:]), end)

	if err != nil {
		f.unref()

		return nil, false, EIO
	}

	return obj, kind == openedTerminal, nil
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

func (p *Process) openControlling(flags int) (Object, error) {
	p.k.mu.Lock()

	c := p.k.cttys[p.sid.Load()]

	if c != nil {
		c.ref()
	}

	p.k.mu.Unlock()

	if c == nil {
		return nil, ENXIO
	}

	defer c.unref()

	return p.ttyReopen(c, flags)
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

func (p *Process) terminalIoctl(t Object, fd, req, va uint32) (uint32, bool) {
	arg := p.varArg(va, 0)

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

func (p *Process) libcPosix_openpt(flags uint32) uint32 {
	return p.result(p.open(AT_FDCWD, "/dev/pty", flags, 0))
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

func (p *Process) openPty(termp, winp uint32) (int, int, string, error) {
	master, err := p.open(AT_FDCWD, "/dev/pty", O_RDWR|O_NOCTTY, 0)

	if err != nil {
		return 0, 0, "", err
	}

	name, err := p.ptsname(uint32(master))
	slave := 0

	if err == nil {
		slave, err = p.open(AT_FDCWD, name, O_RDWR|O_NOCTTY, 0)
	}

	if err == nil && termp != 0 {
		err = p.tcsetattr(uint32(slave), 0, termp)
	}

	if err == nil && winp != 0 {
		var t Object

		if t, err = p.fd(uint32(slave)); err == nil {
			err = p.ttySetWindow(t, append([]byte(nil), p.span(winp, sizeofWinsize)...))
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

func (p *Process) libcForkpty(amaster, name, termp, winp uint32) uint32 {
	master, slave, path, err := p.openPty(termp, winp)

	if err != nil {
		return p.result(0, err)
	}

	t, err := p.fd(uint32(slave))

	if err != nil {
		return p.result(0, err)
	}

	pid, err := p.fork(func(c *Process) {
		c.k.mu.Lock()
		c.sid.Store(c.pid)
		c.pgid.Store(c.pid)
		c.k.mu.Unlock()

		for fd := range 3 {
			c.closeFd(fd)
			t.ref()
			c.files.put(fd, t, false)
		}

		c.closeFd(master)
		c.closeFd(slave)
	}, func(c *Process) {
		c.controls(t)
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

func (p *Process) libcGetpass(prompt uint32) uint32 {
	fd, err := p.open(AT_FDCWD, "/dev/tty", O_RDWR|O_NOCTTY|O_CLOEXEC, 0)

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	defer p.closeFd(fd)

	t, err := p.fd(uint32(fd))

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	if p.passwordAt == 0 {
		p.passwordAt = p.malloc(passwordSize + 1)
	}

	if p.passwordAt == 0 {
		p.setErrno(ENOMEM)

		return 0
	}

	at := p.passwordAt
	saved, err := p.ttyGetattr(t)

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	quiet := append([]byte(nil), saved...)
	le := binary.LittleEndian

	le.PutUint32(quiet, le.Uint32(quiet)&^(INLCR|IGNCR)|ICRNL)
	le.PutUint32(quiet[12:], le.Uint32(quiet[12:])&^(ECHO|ISIG)|ICANON)
	p.ttySetattr(t, 2, quiet)

	asked := uint32(len(p.str(prompt)))
	shown := p.memory(prompt, asked, O_RDONLY)

	p.fileWrite(t, shown, int(asked), IoMode{})
	shown.unref()

	typed := p.memory(at, passwordSize, O_WRONLY)
	n, err := p.fileRead(t, typed, passwordSize, IoMode{})

	typed.unref()

	if err == nil {
		if n > 0 && p.span(at, passwordSize)[n-1] == '\n' || n == passwordSize {
			n--
		}

		p.span(at, passwordSize)[n] = 0
	}

	p.ttySetattr(t, 2, saved)

	p.fileWrite(t, bytesFile([]byte{'\n'}), 1, IoMode{})

	if err != nil {
		p.setErrno(errnoOf(err))

		return 0
	}

	return at
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
	for i := range p.timers {
		t := &p.timers[i]

		if t.timer != nil {
			t.timer.Stop()
		}

		t.epoch++
		t.timer = nil
	}
}

type ZoneCache struct {
	tz  string
	loc *time.Location
}

func (p *Process) location() *time.Location {
	tz, ok := p.getenv("TZ")

	if !ok {
		tz = "/etc/localtime"
	}

	if p.zone.loc == nil || p.zone.tz != tz {
		p.zone = ZoneCache{tz, p.parseZone(tz)}
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

func (p *Process) libcLocaltime(t uint32) uint32 {
	return p.localtime(t, p.staticAt(staticTm))
}

func (p *Process) libcLocaltime_r(t, at uint32) uint32 {
	return p.localtime(t, at)
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

func (p *Process) libcCtime_r(t, buf uint32) uint32 {
	s, ok := asctime(tmOf(time.Unix(int64(p.u64(t)), 0).In(p.location())))

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

func (p *Process) libcUalarm(value, interval uint32) uint32 {
	left, _ := p.setTimer(ITIMER_REAL, time.Duration(value)*time.Microsecond, time.Duration(interval)*time.Microsecond)

	return uint32(min(left/time.Microsecond, math.MaxUint32))
}

func (p *Process) libcGmtime(t uint32) uint32 {
	return p.gmtime(t, p.staticAt(staticTm))
}

func (p *Process) libcClock() uint64 {
	return uint64(clockGettime(hostClockProcessCputime) / time.Microsecond)
}

func (p *Process) libcClock_getres(clock, res uint32) uint32 {
	host, ok := hostClock(clock)

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

func (p *Process) clockNow(clock uint32) (int64, error) {
	host, ok := hostClock(clock)

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

func (p *Process) libcTzset() {
	p.tzset()
}

func (p *Process) libcSettimeofday(tv, tz uint32) uint32 {
	return p.result(0, EPERM)
}

func (p *Process) libcStime(t uint32) uint32 {
	return p.result(0, EPERM)
}

func (p *Process) libcAdjtime(delta, old uint32) uint32 {
	return p.result(0, EPERM)
}

func (p *Process) libcGmtime_r(t, at uint32) uint32 {
	return p.gmtime(t, at)
}

func (p *Process) syslog(priority, message uint32) {
	if p.u32(p.staticAt(staticLogMask))&(1<<(priority&7)) != 0 {
		return
	}

	option := p.u32(p.staticAt(staticLogOption))

	if option&LOG_PERROR == 0 {
		return
	}

	line := p.str(p.staticAt(staticLogIdent))

	if option&LOG_PID != 0 {
		line += fmt.Sprintf("[%d]", p.pid)
	}

	line += ": " + p.str(message)

	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}

	if t, err := p.fd(2); err == nil {
		p.fileWrite(t, bytesFile([]byte(line)), len(line), IoMode{})
	}
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

	p.k.mu.Lock()

	defer p.k.mu.Unlock()

	cur := q.limits[res]

	if set != 0 {
		l := Rlimit{p.u64(set), p.u64(set + 8)}

		if l.cur > l.max {
			return EINVAL
		}

		if l.max > cur.max && !p.cred.privileged() {
			return EPERM
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

func (p *Process) libc__kernel_syslog(priority, message uint32) {
	p.syslog(priority, message)
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

func (p *Process) daemon(nochdir, noclose uint32) uint32 {
	if nochdir == 0 {
		p.root.ref()
		p.setCwd(p.root)
	}

	if noclose == 0 {
		obj, err := p.openPath("/dev/null", O_RDWR, 0)

		if err != nil {
			return p.result(0, err)
		}

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
	p.pid, p.ppid = k.lastPid, k.first.pid
	p.pgid.Store(k.lastPid)
	p.sid.Store(k.lastPid)
	k.procs[p.pid] = p

	for _, q := range k.procs {
		if q.ppid == old {
			q.ppid = k.first.pid
		}
	}

	k.mu.Unlock()

	if parent != nil && !parent.discardsChildren() {
		k.mu.Lock()

		zombie := &Process{k: k, pid: old, ppid: parent.pid, zombie: true}

		zombie.pgid.Store(parent.pgid.Load())
		zombie.sid.Store(parent.sid.Load())
		k.procs[old] = zombie
		k.mu.Unlock()
		parent.childChanged(SIGCHLD)
	}

	return p.result(0, nil)
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

func (p *Process) libcDaemon(nochdir, noclose uint32) uint32 {
	return p.daemon(nochdir, noclose)
}

func (p *Process) libcPrctl(option, va uint32) uint32 {
	return p.prctl(option, va)
}

func (p *Process) libcVhangup() uint32 {
	return p.result(0, EPERM)
}

func (p *Process) libcSethostid(id uint32) uint32 {
	return p.result(0, EPERM)
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

type PasswdEntry struct {
	name, passwd      string
	uid, gid          uint32
	gecos, dir, shell string
}

type GroupEntry struct {
	name, passwd string
	gid          uint32
	members      []string
}

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

func (p *Process) passwdEntries() []PasswdEntry {
	var out []PasswdEntry

	for _, f := range p.databaseLines("/etc/passwd", 7) {
		uid, ok1 := parseId(f[2])
		gid, ok2 := parseId(f[3])

		if ok1 && ok2 {
			out = append(out, PasswdEntry{f[0], f[1], uid, gid, f[4], f[5], f[6]})
		}
	}

	return out
}

func (p *Process) groupEntries() []GroupEntry {
	var out []GroupEntry

	for _, f := range p.databaseLines("/etc/group", 4) {
		gid, ok := parseId(f[2])

		if !ok {
			continue
		}

		var members []string

		if f[3] != "" {
			members = strings.Split(f[3], ",")
		}

		out = append(out, GroupEntry{f[0], f[1], gid, members})
	}

	return out
}

func (e *PasswdEntry) size() uint32 {
	return uint32(len(e.name) + len(e.passwd) + len(e.gecos) + len(e.dir) + len(e.shell) + 5)
}

func (p *Process) putPasswd(at, buf uint32, e *PasswdEntry) {
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

func (e *GroupEntry) size(align uint32) uint32 {
	n := (4-align%4)%4 + 4*uint32(len(e.members)+1) + uint32(len(e.name)+len(e.passwd)+2)

	for _, m := range e.members {
		n += uint32(len(m) + 1)
	}

	return n
}

func (p *Process) putGroup(at, buf uint32, e *GroupEntry) {
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

func (p *Process) passwdResult(e *PasswdEntry, pwd, buf, size, result uint32) uint32 {
	p.put32(result, 0)

	if e == nil {
		return 0
	}

	if e.size() > size {
		return uint32(ERANGE)
	}

	p.putPasswd(pwd, buf, e)
	p.put32(result, pwd)

	return 0
}

func (p *Process) groupResult(e *GroupEntry, grp, buf, size, result uint32) uint32 {
	p.put32(result, 0)

	if e == nil {
		return 0
	}

	if e.size(buf) > size {
		return uint32(ERANGE)
	}

	p.putGroup(grp, buf, e)
	p.put32(result, grp)

	return 0
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

func (p *Process) staticPasswd(e *PasswdEntry) uint32 {
	if e == nil {
		return 0
	}

	block := p.staticResult(staticPasswd, 28+e.size())

	if block != 0 {
		p.putPasswd(block, block+28, e)
	}

	return block
}

func (p *Process) staticGroup(e *GroupEntry) uint32 {
	if e == nil {
		return 0
	}

	block := p.staticResult(staticGroup, 16+e.size(16))

	if block != 0 {
		p.putGroup(block, block+16, e)
	}

	return block
}

func (p *Process) passwdBy(match func(*PasswdEntry) bool) *PasswdEntry {
	entries := p.passwdEntries()

	if i := slices.IndexFunc(entries, func(e PasswdEntry) bool { return match(&e) }); i >= 0 {
		return &entries[i]
	}

	return nil
}

func (p *Process) groupBy(match func(*GroupEntry) bool) *GroupEntry {
	entries := p.groupEntries()

	if i := slices.IndexFunc(entries, func(e GroupEntry) bool { return match(&e) }); i >= 0 {
		return &entries[i]
	}

	return nil
}

func nextEntry[E any](p *Process, slot uint32, entries []E) *E {
	at := p.staticAt(slot)
	i := p.u32(at)

	if i >= uint32(len(entries)) {
		return nil
	}

	p.put32(at, i+1)

	return &entries[i]
}

func byName(p *Process, name uint32) func(*PasswdEntry) bool {
	s := p.str(name)

	return func(e *PasswdEntry) bool { return e.name == s }
}

func byUid(uid uint32) func(*PasswdEntry) bool {
	return func(e *PasswdEntry) bool { return e.uid == uid }
}

func groupName(p *Process, name uint32) func(*GroupEntry) bool {
	s := p.str(name)

	return func(e *GroupEntry) bool { return e.name == s }
}

func byGid(gid uint32) func(*GroupEntry) bool {
	return func(e *GroupEntry) bool { return e.gid == gid }
}

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

type ServiceEntry struct {
	name  string
	port  int
	proto string
}

type HostEntry struct {
	addr  netip.Addr
	names []string
}

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

func (p *Process) hostEntries() []HostEntry {
	lines, _ := p.fileLines("/etc/hosts")

	var out []HostEntry

	for _, line := range lines {
		fields := strings.Fields(line)

		if addr, err := netip.ParseAddr(fields[0]); err == nil && len(fields) > 1 {
			out = append(out, HostEntry{addr.WithZone(""), fields[1:]})
		}
	}

	return out
}

func (p *Process) services() []ServiceEntry {
	lines, ok := p.fileLines("/etc/services")

	if !ok {
		return wellKnown
	}

	var out []ServiceEntry

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
			out = append(out, ServiceEntry{name, n, proto})
		}
	}

	return out
}

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
			return nil, "", EAI_AGAIN
		}

		if errors.As(err, &dns) && dns.IsNotFound {
			return nil, "", EAI_NONAME
		}

		return nil, "", EAI_FAIL
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

func (p *Process) servicePort(name, proto string) (int, bool) {
	for _, s := range p.services() {
		if s.name == name && (proto == "" || s.proto == proto) {
			return s.port, true
		}
	}

	return 0, false
}

func (p *Process) serviceName(port int, proto string) (ServiceEntry, bool) {
	for _, s := range p.services() {
		if s.port == port && (proto == "" || s.proto == proto) {
			return s, true
		}
	}

	return ServiceEntry{}, false
}

type SocketKind struct {
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
		return EAI_NONAME
	case flags&^(AI_PASSIVE|AI_CANONNAME|AI_NUMERICHOST|AI_V4MAPPED|AI_ALL|AI_ADDRCONFIG|AI_NUMERICSERV) != 0:
		return EAI_BADFLAGS
	case family != 0 && family != AF_INET && family != AF_INET6:
		return EAI_FAMILY
	case socktype != 0 && socktype != SOCK_STREAM && socktype != SOCK_DGRAM && socktype != SOCK_RAW:
		return EAI_SOCKTYPE
	}

	kinds := []SocketKind{{SOCK_STREAM, IPPROTO_TCP}, {SOCK_DGRAM, IPPROTO_UDP}}

	if socktype != 0 || protocol != 0 {
		kinds = nil

		for _, k := range []SocketKind{{SOCK_STREAM, IPPROTO_TCP}, {SOCK_DGRAM, IPPROTO_UDP}, {SOCK_RAW, protocol}} {
			if (socktype == 0 || k.socktype == socktype) && (protocol == 0 || k.protocol == protocol) {
				kinds = append(kinds, k)
			}
		}

		if len(kinds) == 0 {
			return EAI_SOCKTYPE
		}
	}

	port := 0

	if service != 0 {
		name := p.str(service)
		n, err := strconv.ParseUint(name, 10, 16)

		switch {
		case err == nil:
			port = int(n)
		case flags&AI_NUMERICSERV != 0:
			return EAI_NONAME
		default:
			var kept []SocketKind

			for _, k := range kinds {
				proto := "tcp"

				if k.socktype == SOCK_DGRAM {
					proto = "udp"
				}

				if found, ok := p.servicePort(name, proto); ok && k.socktype != SOCK_RAW {
					port = found
					kept = append(kept, k)
				}
			}

			if len(kept) == 0 {
				return EAI_SERVICE
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

		if flags&AI_PASSIVE == 0 {
			any4, any6 = netip.AddrFrom4([4]byte{127, 0, 0, 1}), netip.IPv6Loopback()
		}

		addrs = []address{{any4, 0}, {any6, 0}}
	} else {
		name := p.str(node)

		if a, scope, ok := numericHost(name); ok {
			addrs, canonical = []address{{a, scope}}, name
		} else if flags&AI_NUMERICHOST != 0 {
			return EAI_NONAME
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

	usable := flags&AI_ADDRCONFIG == 0
	usable4, usable6 := usable, usable

	for _, a := range addrs {
		switch {
		case family == AF_INET && !a.addr.Is4():
		case family == AF_INET6 && a.addr.Is4():
			if flags&AI_V4MAPPED != 0 && (!have6 || flags&AI_ALL != 0) {
				kept = append(kept, address{netip.AddrFrom16(a.addr.As16()), 0})
			}
		case a.addr.Is4() && !usable4, a.addr.Is6() && !usable6:
		default:
			kept = append(kept, a)
		}
	}

	if len(kept) == 0 {
		return EAI_NONAME
	}

	first, prev := uint32(0), uint32(0)

	for _, a := range kept {
		for _, k := range kinds {
			addr := sockaddr(a.addr, port, a.scope)
			name := ""

			if first == 0 && flags&AI_CANONNAME != 0 {
				name = canonical
			}

			size := 32 + uint32(len(addr))

			if name != "" {
				size += uint32(len(name)) + 1
			}

			node := p.malloc(size)

			if node == 0 {
				p.freeaddrinfo(first)

				return EAI_MEMORY
			}

			clear(p.span(node, size))

			family := AF_INET6

			if a.addr.Is4() {
				family = AF_INET
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
		return EAI_FAMILY
	}

	var addr netip.Addr

	scope := uint32(0)

	switch p.u16(sa) {
	case AF_INET:
		if salen < 16 {
			return EAI_FAMILY
		}

		addr = netip.AddrFrom4([4]byte(p.span(sa+4, 4)))
	case AF_INET6:
		if salen < 28 {
			return EAI_FAMILY
		}

		addr, scope = netip.AddrFrom16([16]byte(p.span(sa+8, 16))), p.u32(sa+24)
	default:
		return EAI_FAMILY
	}

	if host != 0 && hostlen > 0 {
		name, found := "", false

		if flags&NI_NUMERICHOST == 0 {
			name, found = p.reverse(addr)

			if found && flags&NI_NOFQDN != 0 {
				name, _, _ = strings.Cut(name, ".")
			}
		}

		if !found {
			if flags&NI_NAMEREQD != 0 {
				return EAI_NONAME
			}

			name = addr.String()

			if scope != 0 {
				name += "%" + strconv.Itoa(int(scope))
			}
		}

		if uint32(len(name)) >= hostlen {
			return EAI_OVERFLOW
		}

		p.putString(host, name)
	}

	if serv != 0 && servlen > 0 {
		port := int(binary.BigEndian.Uint16(p.span(sa+2, 2)))
		name := strconv.Itoa(port)

		if flags&NI_NUMERICSERV == 0 {
			proto := "tcp"

			if flags&NI_DGRAM != 0 {
				proto = "udp"
			}

			if s, ok := p.serviceName(port, proto); ok {
				name = s.name
			}
		}

		if uint32(len(name)) >= servlen {
			return EAI_OVERFLOW
		}

		p.putString(serv, name)
	}

	return 0
}

func (p *Process) setHerrno(code int) {
	if p.herrnoAt != 0 {
		p.put32(p.herrnoAt, uint32(code))
	}
}

func (p *Process) hostent(name string, family int, addrs []netip.Addr) uint32 {
	length := 4

	if family == AF_INET6 {
		length = 16
	}

	size := 20 + 4 + 4*uint32(len(addrs)+1) + uint32(length*len(addrs)) + uint32(len(name)) + 1
	block := p.staticResult(staticHostent, size)

	if block == 0 {
		p.setHerrno(NO_RECOVERY)

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

		if family == AF_INET {
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
		case EAI_AGAIN:
			p.setHerrno(TRY_AGAIN)

			return 0
		case EAI_FAIL:
			p.setHerrno(NO_RECOVERY)

			return 0
		default:
			p.setHerrno(HOST_NOT_FOUND)

			return 0
		}

		found, canonical = addrs, canon
	}

	var kept []netip.Addr

	for _, a := range found {
		if a.Is4() == (family == AF_INET) {
			kept = append(kept, a)
		}
	}

	if len(kept) == 0 {
		p.setHerrno(HOST_NOT_FOUND)

		return 0
	}

	return p.hostent(canonical, family, kept)
}

func (p *Process) servent(s ServiceEntry) uint32 {
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
	return p.gethostbyname(p.str(name), AF_INET)
}

func (p *Process) libcGethostbyname2(name, family uint32) uint32 {
	if family != AF_INET && family != AF_INET6 {
		p.setHerrno(NO_RECOVERY)

		return 0
	}

	return p.gethostbyname(p.str(name), int(family))
}

func (p *Process) libcGethostbyaddr(addr, n, family uint32) uint32 {
	var a netip.Addr

	switch {
	case family == AF_INET && n == 4:
		a = netip.AddrFrom4([4]byte(p.span(addr, 4)))
	case family == AF_INET6 && n == 16:
		a = netip.AddrFrom16([16]byte(p.span(addr, 16)))
	default:
		p.setHerrno(NO_RECOVERY)

		return 0
	}

	name, ok := p.reverse(a)

	if !ok {
		p.setHerrno(HOST_NOT_FOUND)

		return 0
	}

	return p.hostent(name, int(family), []netip.Addr{a})
}

func (p *Process) libcGethostent() uint32 {
	e := nextEntry(p, staticHostIndex, p.hostEntries())

	if e == nil {
		return 0
	}

	family := AF_INET6

	if e.addr.Is4() {
		family = AF_INET
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
		p.setErrno(ENXIO)
	}

	return index
}

func (p *Process) libcIf_indextoname(index, buf uint32) uint32 {
	if index == 0 || int(index) > len(interfaces) {
		p.setErrno(ENXIO)

		return 0
	}

	p.putString(buf, interfaces[index-1])

	return buf
}

func (p *Process) libcIf_nameindex() uint32 {
	size := 8 * uint32(len(interfaces)+1)

	for _, name := range interfaces {
		size += uint32(len(name)) + 1
	}

	block := p.malloc(size)

	if block == 0 {
		p.setErrno(ENOBUFS)

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
	case name == "C.UTF-8" && cat != LC_CTYPE:
		return ""
	}

	return name
}

func (p *Process) setlocale(cat uint32, name uint32) uint32 {
	if cat > LC_ALL {
		return 0
	}

	if name != 0 {
		s := p.str(name)
		parts := strings.Split(s, ";")

		var names [LC_ALL]string

		for i := range LC_ALL {
			part := s

			if cat == LC_ALL {
				part = parts[min(i, len(parts)-1)]
			} else if i != int(cat) {
				continue
			}

			names[i] = p.localeName(i, part)

			if !utf8Codeset(names[i]) {
				return 0
			}
		}

		for i := range LC_ALL {
			if cat == LC_ALL || i == int(cat) {
				p.locale[i] = names[i]
			}
		}
	}

	if cat < LC_ALL {
		return p.constant(localeShown(p.locale[cat]))
	}

	names := make([]string, LC_ALL)

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

func (p *Process) langinfo(item uint32) string {
	cat, idx := int(item>>16), int(item&0xffff)

	switch {
	case item == CODESET:
		return "UTF-8"
	case idx == localeNameItem && cat < LC_ALL:
		return localeShown(p.locale[cat])
	case idx < len(langItems[cat]):
		return langItems[cat][idx]
	}

	return ""
}

func (p *Process) localeconv() uint32 {
	at := p.staticAt(staticLconv)

	p.put32(at, p.staticAt(staticDot))

	for i := uint32(1); i < lconvStrings; i++ {
		p.put32(at+4*i, p.staticAt(staticEmpty))
	}

	chars := p.span(at+4*lconvStrings, lconvChars)

	for i := range chars {
		chars[i] = CHAR_MAX
	}

	return at
}

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

func wcmp(a, b uint32) uint32 {
	switch {
	case int32(a) < int32(b):
		return math.MaxUint32
	case int32(a) > int32(b):
		return 1
	}

	return 0
}

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
		p.setErrno(ENOMEM)

		return 0
	}

	at := p.malloc(4 * n)

	if at == 0 {
		p.setErrno(ENOMEM)

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

func (p *Process) libcRegcomp(preg, pattern, flags uint32) uint32 {
	pat := bytes.Clone(p.cstr(pattern))
	src, nsub, code := translateRegex(pat, flags, false, false)

	if code != 0 {
		return code
	}

	re, err := regexp.Compile(src)

	if err != nil {
		if parsed, ok := err.(*syntax.Error); ok && parsed.Code == syntax.ErrInvalidCharRange {
			return REG_ERANGE
		}

		return REG_BADPAT
	}

	re.Longest()
	p.nextRegex++

	r := &PosixRegex{pattern: pat, flags: flags, nsub: nsub}

	r.variants[0] = re
	p.regexes[p.nextRegex] = r
	p.put32(preg, uint32(nsub))
	p.put32(preg+4, p.nextRegex)

	return 0
}

func (p *Process) libcRegexec(preg, s, nmatch, pmatch, eflags uint32) uint32 {
	r, ok := p.regexes[p.u32(preg+4)]

	if !ok {
		return REG_BADPAT
	}

	var subject []byte

	base := uint32(0)

	if eflags&REG_STARTEND != 0 {
		so, eo := p.u32(pmatch), p.u32(pmatch+4)

		if eo < so {
			return REG_BADPAT
		}

		subject, base = p.span(s+so, eo-so), so
	} else {
		subject = p.cstr(s)
	}

	boundary := r.flags&REG_NEWLINE != 0 && eflags&(REG_NOTBOL|REG_NOTEOL) != 0
	prefix := 0

	if boundary {
		suffix := 0

		if eflags&REG_NOTBOL != 0 {
			prefix = 1
		}

		if eflags&REG_NOTEOL != 0 {
			suffix = 1
		}

		padded := make([]byte, prefix+len(subject)+suffix)

		copy(padded[prefix:], subject)
		subject = padded
	}

	m := r.variant(eflags).FindSubmatchIndex(subject)

	if m == nil {
		return REG_NOMATCH
	}

	if boundary {
		m = m[2:]

		for i, at := range m {
			if at >= 0 {
				m[i] = at - prefix
			}
		}
	}

	if r.flags&REG_NOSUB == 0 {
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
	g := &Globber{p: p, flags: flags, errfunc: errfunc}
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
			p.crash(fmt.Sprintf("%s: a structure of %d bytes, which this kernel knows of %d", call, sizes[k], sizes[k+1]), callers(1))
			p.exitWith(SIGSYS)
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
	case r.controlling:
		return p.openControlling(flags)
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

func (p *Process) dirBind(d Object, path string, mode uint32, held Object) error {
	return p.request(d, &VfsBind{root: p.vfsRoot(d), path: path, mode: mode, held: held})
}

func (p *Process) dirConnect(d Object, path string) (Object, error) {
	r := VfsConnect{root: p.vfsRoot(d), path: path}
	err := p.request(d, &r)

	return r.file, err
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

func (p *Process) ttyReopen(t Object, flags int) (Object, error) {
	r := TtyReopen{flags: flags}
	err := p.request(t, &r)

	return r.file, err
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
