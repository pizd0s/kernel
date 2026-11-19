//go:build linux && amd64

package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// The loader links an object's code once, executable, with its GOT filled,
// and tells Go about it; instances of the object share that code. An
// instance is the instance block, linear memory and tables, filled by the
// init thunk. An export is a Go function value whose code is its thunk:
// calling it runs the module on the caller's goroutine stack.

// runStack is how much goroutine stack a command may take; deeper, it
// traps, before Go's own limit of 1 GiB would end the program.
const runStack = 64 << 20

type Instance struct {
	name string
	obj  *ElfObject
	code *Code
	// region is the instance's mapping (newRegion): block is its start.
	// holds counts the instance itself and the memfds of its memory: the
	// last to go unmaps the region (unhold).
	region []byte
	holds  atomic.Int32
	block  []byte
	memory *LinearMemory
	tables [][]uintptr
	// mem is the accessible memory, the tail included: setMemory keeps it.
	mem []byte
	// host is what the embedder attaches: the kernel's process, the WASI;
	// proc is the process when it is one.
	host any
	proc *Process
}

// attach attaches a host to the instance.
func (i *Instance) attach(host any) {
	i.host = host
	i.proc, _ = host.(*Process)
}

func (i *Instance) memoryBytes() []byte {
	return i.mem
}

func (i *Instance) ctx() uintptr {
	return uintptr(unsafe.Pointer(unsafe.SliceData(i.block)))
}

func (i *Instance) put64(at uint32, v uint64) {
	binary.LittleEndian.PutUint64(i.block[at:], v)
}

func (i *Instance) setMemory() {
	size := i.memory.pages * pageSize
	i.mem = i.memory.data[:size+i.obj.meta.memory.tail]
	i.put64(offMemSize, size)
	i.put64(offMemBound, uint64(int64(size+i.obj.meta.memory.tail)-boundSlack))

	if i.obj.meta.memory.tail != 0 {
		i.put64(i.obj.meta.memory.tailEnd, size+i.obj.meta.memory.tail)
	}

	if i.obj.meta.memory.shadow != 0 {
		i.put64(i.obj.meta.memory.shadow, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(i.memory.shadow)))))
	}
}

// instanceAt is the instance of a context the code passes: its header holds
// the address of the Go value, which the caller of the code keeps alive.
//
//go:nocheckptr
func instanceAt(ctx uintptr) *Instance {
	return *(**Instance)(unsafe.Pointer(ctx + offHost))
}

// instantiate links an object and makes an instance of it that may take
// stack bytes of goroutine stack; start runs its init.
func instantiate(name string, obj *ElfObject, imports Imports, stack uint64) *Instance {
	return newInstance(link(name, obj, imports), imports, stack)
}

// newInstance makes an instance of linked code: its imports other than
// functions come from imports.
func newInstance(c *Code, imports Imports, stack uint64) *Instance {
	meta := c.obj.meta
	region := newRegion(meta.memory)
	i := &Instance{name: c.name, obj: c.obj, code: c, region: region, block: region[:max(meta.instanceSize, headerSize)]}
	i.holds.Store(1)
	initialized := false

	defer func() {
		if !initialized {
			i.release()
		}
	}()

	i.put64(offMem, uint64(i.ctx())+instanceDistance)
	i.put64(offHost, uint64(uintptr(unsafe.Pointer(i))))
	i.put64(offStackLimit, stack)

	for k := range meta.imports {
		imp := &meta.imports[k]

		switch imp.kind {
		case externGlobal:
			at := imports.global(imp)

			if at == 0 {
				throwFmt("unresolved import %s.%s", imp.module, imp.name)
			}

			i.put64(meta.globals[imp.index].offset, uint64(at))
		case externTable:
			at := imports.table(imp)

			if at == 0 {
				throwFmt("unresolved import %s.%s", imp.module, imp.name)
			}

			copy(i.block[meta.tables[imp.index].offset:], unsafe.Slice((*byte)(unsafe.Pointer(at)), tableBytes))
		}
	}

	if meta.memory != nil {
		i.memory = newMemory(region[instanceDistance:], meta.memory.min, meta.memory.max, meta.memory.shadow != 0)

		if meta.memory.tail != 0 {
			i.memory.tailUsed = true
		}

		i.memory.owner = i
		i.setMemory()
	}

	for _, t := range meta.tables {
		if t.imported {
			continue
		}

		if t.min > 1<<24 {
			throwFmt("table of %d elements", t.min)
		}

		entries := make([]uintptr, t.min+1)
		i.tables = append(i.tables, entries)
		i.put64(t.offset+tableEntries, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(entries)))))
		binary.LittleEndian.PutUint32(i.block[t.offset+tableSize:], uint32(t.min))
		binary.LittleEndian.PutUint32(i.block[t.offset+tableMax:], uint32(min(t.max, math.MaxUint32)))
	}

	initialized = true

	return i
}

// clone makes a kernel instance that is a copy of i: its block, the
// tables' entries moved to its own, its globals, and of linear memory the
// bytes [from, to), the rest zero.
func (i *Instance) clone(imports *Kernel, stack uint64, from, to uint64) *Instance {
	c := newInstance(i.code, imports, stack)

	if i.memory != nil {
		c.memory.grow(uint32(i.memory.pages - c.memory.pages))
		copy(c.memory.data[from:to], i.memory.data[from:to])

		if i.memory.tailUsed {
			c.memory.tailUsed = true
			start := i.memory.pages * pageSize
			copy(c.memory.data[start:start+boundSlack], i.memory.data[start:start+boundSlack])
		}

		if i.memory.shadow != nil {
			// The whole map up to the copied memory's end: what the stack
			// below the copy may access is marked as in the parent.
			copy(c.memory.shadow[:to], i.memory.shadow[:to])
		}
	}

	copy(c.block[headerSize:], i.block[headerSize:])

	if meta := i.obj.meta.memory; meta != nil && meta.profile != 0 {
		clear(c.block[meta.profile : uint64(meta.profile)+uint64(len(meta.sites))*profileCounterBytes])
	}

	if meta := i.obj.meta.memory; meta != nil && meta.shadow != 0 {
		// The copy of the block brought the parent's shadow map.
		c.put64(meta.shadow, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(c.memory.shadow)))))
	}

	c.put64(offTokens, binary.LittleEndian.Uint64(i.block[offTokens:]))
	moved := c.ctx() - i.ctx()
	// The kernel resolves no imported tables, so every table is owned here.
	for k, t := range i.obj.meta.tables {
		entries := c.tables[k]

		for e, v := range i.tables[k] {
			if v != 0 {
				entries[e] = v + moved
			}
		}

		c.put64(t.offset+tableEntries, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(entries)))))
	}

	return c
}

// release unmaps the memory's shadow map and drops the instance's hold on
// its region, its block and memory, which a memfd of the memory keeps
// mapped; the instance is not used again.
func (i *Instance) release() {
	compilerCoverage(i)
	i.writeTailProfile()

	if i.memory != nil && i.memory.shadow != nil {
		throw(syscall.Munmap(i.memory.shadow))
	}

	i.block, i.memory, i.tables = nil, nil, nil
	i.unhold()
}

// unhold drops a hold on the instance's region, the instance's own or a
// memfd's: the last unmaps it.
func (i *Instance) unhold() {
	if i.holds.Add(-1) == 0 {
		throw(syscall.Munmap(i.region))
	}
}

// nativeFunc is a Go function value of type typ whose code is at code.
func nativeFunc(typ reflect.Type, code uintptr) reflect.Value {
	closure := new(uintptr)
	*closure = code

	return reflect.NewAt(typ, unsafe.Pointer(&closure)).Elem()
}

func (i *Instance) symbol(name string) uintptr {
	at, ok := i.obj.defined[name]

	if !ok {
		throwFmt("no symbol %s", name)
	}

	return i.code.base + uintptr(at)
}

// start runs the init thunk: the segments and the start function.
func (i *Instance) start() {
	nativeFunc(reflect.TypeFor[func(uintptr)](), i.symbol(i.obj.meta.init)).Interface().(func(uintptr))(i.ctx())
}

// startCommand calls a command's entry with the parameterless Wasm ABI.
func (i *Instance) startCommand() {
	start, entry := i.export("_start")

	if len(entry.params) != 0 || len(entry.results) != 0 {
		throwFmt("_start must have no parameters or results")
	}

	start.Interface().(func(uintptr))(i.ctx())
}

// export returns an exported function as a Go function value,
// func(instance uintptr, params...) (results...).
func (i *Instance) export(name string) (reflect.Value, *ExportMeta) {
	for k := range i.obj.meta.exports {
		e := &i.obj.meta.exports[k]

		if e.name == name && e.kind == externFunc {
			return nativeFunc(goFuncType(e.params, e.results), i.symbol(e.sym)), e
		}
	}

	throwFmt("no exported function %s", name)

	return reflect.Value{}, nil
}

// setjmp takes the jump frame of the call in progress, the one Go is in;
// false tells that the call came through a table or an export, whose
// adapter writes no frame.
func (i *Instance) setjmp(j *Jump) bool {
	for k := range j.frame {
		j.frame[k] = binary.LittleEndian.Uint64(i.block[offJump+8*k:])
	}

	clear(i.block[offJump : offJump+8*jumpWords])

	return j.frame[jumpPC] != 0
}

// longjmp returns from the call of j with val; it returns itself only when
// j's frame is not on the stack any more.
func (i *Instance) longjmp(j *Jump, val uint32) {
	regions := i.obj.meta.regions
	pc := j.frame[jumpPC] - uint64(i.code.base)
	// Only direct returns-twice adapters create these private frames. Their
	// saved PC follows a call inside this Code's function region; the guest's
	// jmp_buf never contains or supplies the native frame (process.go).
	k := sort.Search(len(regions), func(k int) bool { return uint64(regions[k].end) > pc })
	j.code = [2]uint64{uint64(i.code.base) + uint64(regions[k].start), uint64(i.code.base) + uint64(regions[k].end)}
	land := nativeFunc(reflect.TypeFor[func(uintptr, uint32, uintptr) uint32](), landStub())
	land.Interface().(func(uintptr, uint32, uintptr) uint32)(uintptr(unsafe.Pointer(j)), val, i.ctx())
	runtime.KeepAlive(j)
}

// fork: the frames of a call that returns twice, copied by the frames stub
// from its caller up to the process's entry, run again on another
// goroutine by the resume stub (stubs_amd64.s).

type framesRequest struct {
	ctx, sp, bp          uint64
	funcs, entry, resume [2]uint64
	buf, cap             uint64
	size, top            uint64
}

type resumeRequest struct {
	ctx, buf, size, top, bp, sp, pc uint64
	regs                            [jumpSaved]uint64
}

// Frames are copied frames: top is the offset of the top one's saved RBP.
// The resume stub's request is in the heap with them: the stub grows the
// stack, which a request on it would move under the stub.
type Frames struct {
	bytes []byte
	top   uint64
	jump  Jump
	req   resumeRequest
}

func isFunctionRegion(name string) bool {
	return len(name) > 1 && name[0] == 'f' && name[1] >= '0' && name[1] <= '9'
}

// codeRange is the code of regions first to last.
func (i *Instance) codeRange(first, last int) [2]uint64 {
	regions := i.obj.meta.regions
	base := uint64(i.code.base)

	return [2]uint64{base + uint64(regions[first].start), base + uint64(regions[last].end)}
}

// functionCode is the code of the module's functions, whose regions follow
// one another (compile.go).
func (i *Instance) functionCode() ([2]uint64, bool) {
	first, last := -1, -1

	for k, r := range i.obj.meta.regions {
		switch {
		case !isFunctionRegion(r.name):
		case first < 0:
			first, last = k, k
		case last != k-1:
			return [2]uint64{}, false
		default:
			last = k
		}
	}

	if first < 0 {
		return [2]uint64{}, false
	}

	return i.codeRange(first, last), true
}

// exportCode is the code of an exported function's thunk.
func (i *Instance) exportCode(name string) ([2]uint64, bool) {
	_, e := i.export(name)

	return i.symbolCode(e.sym)
}

// symbolCode is the code of the region a symbol starts.
func (i *Instance) symbolCode(sym string) ([2]uint64, bool) {
	at := i.obj.defined[sym]

	for k, r := range i.obj.meta.regions {
		if r.start == at {
			return i.codeRange(k, k), true
		}
	}

	return [2]uint64{}, false
}

// frames copies the frames of j's call up to the export entry the process
// entered by, or the resume stub a child of fork did; false tells that
// they lead elsewhere, as from a signal handler, through Go.
func (i *Instance) frames(j *Jump, entry string) (*Frames, bool) {
	funcs, ok := i.functionCode()
	thunk, found := i.exportCode(entry)

	if !ok || !found {
		return nil, false
	}

	// The copied frames return into the resume stub's continuation.
	lo, hi := resumeReturnRange()
	resume := [2]uint64{uint64(lo), uint64(hi)}

	buf := make([]byte, 16<<10)
	req := &framesRequest{ctx: uint64(i.ctx()), sp: j.frame[jumpSP], bp: j.frame[jumpBP], funcs: funcs, entry: thunk, resume: resume}
	stub := nativeFunc(reflect.TypeFor[func(uintptr)](), framesStub()).Interface().(func(uintptr))

	for {
		req.buf, req.cap = uint64(uintptr(unsafe.Pointer(unsafe.SliceData(buf)))), uint64(len(buf))
		stub(uintptr(unsafe.Pointer(req)))
		runtime.KeepAlive(buf)

		if req.size == 0 {
			return nil, false
		}

		if req.size <= req.cap {
			return &Frames{bytes: buf[:req.size], top: req.top, jump: *j}, true
		}

		buf = make([]byte, req.size)
	}
}

// resume runs copied frames in i, on this goroutine: their call returns 0.
// It returns when the top frame does, with what it left in RAX.
func (i *Instance) resume(f *Frames) uint64 {
	j := &f.jump.frame
	req := &f.req
	*req = resumeRequest{ctx: uint64(i.ctx()), buf: uint64(uintptr(unsafe.Pointer(unsafe.SliceData(f.bytes)))), size: uint64(len(f.bytes)),
		top: f.top, bp: j[jumpSP] - j[jumpBP], sp: j[jumpSP], pc: j[jumpPC], regs: [jumpSaved]uint64(j[jumpRegs : jumpRegs+jumpSaved])}
	stub := nativeFunc(reflect.TypeFor[func(uintptr) uint64](), resumeStub()).Interface().(func(uintptr) uint64)
	r := stub(uintptr(unsafe.Pointer(req)))
	runtime.KeepAlive(f)

	return r
}

// globalAt is where an exported global of the module keeps its value in
// the instance block.
func (i *Instance) globalAt(name string) (uint32, bool) {
	for _, e := range i.obj.meta.exports {
		if e.kind == externGlobal && e.name == name && !i.obj.meta.globals[e.index].imported {
			return i.obj.meta.globals[e.index].offset, true
		}
	}

	return 0, false
}

// globalExport is the value of an exported i32 global.
func (i *Instance) globalExport(name string) (uint32, bool) {
	at, ok := i.globalAt(name)

	if !ok {
		return 0, false
	}

	return binary.LittleEndian.Uint32(i.block[at:]), true
}

// specFunctionRef resolves a module's numeric function identity. An import
// without an addressable adapter cannot be passed as a reference.
func (i *Instance) specFunctionRef(index uint64) (uintptr, bool) {
	m := i.obj.meta

	if index >= uint64(m.functionCount) {
		return 0, false
	}

	at := uint64(m.functions) + index*descBytes

	if binary.LittleEndian.Uint64(i.block[at:]) == 0 {
		return 0, false
	}

	return i.ctx() + uintptr(at), true
}

// The tail profile of an object compiled with its counters (W2X_TAIL_PROFILE)
// goes, when an instance is released, to the file W2O_TAIL_PROFILE names: a
// line of JSON for the instance, with each function that counted anything.
// A function has its totals and the sites that counted, in the order of
// their offsets: a site's kind, the offset in the module of the wasm
// instruction it is of (0 for none) and its count. A check counts its
// executions; a guard of a checked span its own, a fallback the guard's
// failures and omitted the checks the guard stood for; loop a loop's
// iterations and entry its entries, and fast_loop and fast_entry those of a
// copy of the loop whose guard proved its accesses, which its entries took.

type TailProfileSite struct {
	Kind   string `json:"kind"`
	Offset uint32 `json:"offset"`
	Count  uint64 `json:"count"`
}

type TailProfileRow struct {
	Function  int               `json:"function"`
	Name      string            `json:"name"`
	Guards    uint64            `json:"guards"`
	Fallbacks uint64            `json:"fallbacks"`
	Omitted   uint64            `json:"omitted_checks"`
	Checks    uint64            `json:"ordinary_checks"`
	Sites     []TailProfileSite `json:"sites"`
}

type TailProfile struct {
	Module    string           `json:"module"`
	Functions []TailProfileRow `json:"functions"`
}

func (i *Instance) tailProfile() TailProfile {
	out := TailProfile{Module: i.name}
	meta := i.obj.meta.memory

	names := make(map[int]string)

	for _, region := range i.obj.meta.regions {
		var fn int

		if strings.HasPrefix(region.name, "f") {
			if _, err := fmt.Sscanf(region.name, "f%d", &fn); err == nil {
				names[fn] = region.name
			}
		}
	}

	rows := make(map[int]*TailProfileRow)

	for k, site := range meta.sites {
		p := int(meta.profile) + k*profileCounterBytes
		count := binary.LittleEndian.Uint64(i.block[p : p+profileCounterBytes])

		if count == 0 {
			continue
		}

		fn := int(site.function)
		r := rows[fn]

		if r == nil {
			r = &TailProfileRow{Function: fn, Name: names[fn]}
			rows[fn] = r
		}

		switch site.kind {
		case siteCheck:
			r.Checks += count
		case siteGuard:
			r.Guards += count
		case siteFallback:
			r.Fallbacks += count
		case siteOmitted:
			r.Omitted += count
		}

		r.Sites = append(r.Sites, TailProfileSite{Kind: siteKinds[site.kind], Offset: site.offset, Count: count})
	}

	for _, fn := range slices.Sorted(maps.Keys(rows)) {
		r := rows[fn]

		slices.SortStableFunc(r.Sites, func(a, b TailProfileSite) int {
			return int(a.Offset) - int(b.Offset)
		})

		out.Functions = append(out.Functions, *r)
	}

	return out
}

func (i *Instance) writeTailProfile() {
	meta := i.obj.meta.memory
	path := os.Getenv("W2O_TAIL_PROFILE")

	if path == "" || meta == nil || meta.profile == 0 {
		return
	}

	f := throw2(os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644))

	defer f.Close()

	throw(json.NewEncoder(f).Encode(i.tailProfile()))
}
