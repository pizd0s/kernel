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

const runStack = 64 << 20

type Instance struct {
	memBase  uintptr
	memBound uint64
	blockAt  uintptr

	name string
	obj  *ElfObject
	code *Code

	pool       *Pool
	extent     *Extent
	relocation Event
	region     []byte
	below      uint64
	holds      atomic.Int32
	block      []byte
	memory     *LinearMemory
	tables     [][]uintptr

	mem     []byte
	windows []*Description

	host any
	proc *Process
}

func (i *Instance) attach(host any) {
	q, _ := host.(*Process)

	i.host = host

	if was := i.pool.runner(i, q); was != nil && was != q && was.events.drop(&i.relocation) && q != nil {
		q.post(&i.relocation)
		q.raiseInterrupt()
	}
}

func (i *Instance) memoryBytes() []byte {
	return i.mem
}

func (i *Instance) anchor() {
	distance := distanceOf(i.obj.meta.instanceSize)
	ctx := i.ctx()

	i.memBase, i.blockAt = ctx+uintptr(distance), ctx
	i.put64(offMem, uint64(i.memBase))
	binary.LittleEndian.PutUint64(i.region[i.below+distance-8:], uint64(ctx))
}

func (i *Instance) handle() uintptr {
	return uintptr(unsafe.Pointer(i))
}

func (i *Instance) usedFor(pages uint64) uint64 {
	n := i.below + distanceOf(i.obj.meta.instanceSize)

	if i.obj.meta.memory != nil {
		n += pages*pageSize + boundSlack + 1
	}

	return grain(n)
}

func (i *Instance) shift(at, keep, used uint64) {
	m := i.memory
	region := i.pool.mem[at : at+used : at+used]

	if m != nil {
		m.shut()
	}

	copy(region, i.region[:keep])

	i.region = region
	i.block = region[i.below : i.below+uint64(len(i.block)) : i.below+uint64(len(i.block))]

	i.anchor()

	if m != nil {
		m.data = region[i.below+distanceOf(i.obj.meta.instanceSize):]

		i.setMemory()
		m.open()
	}
}

func (i *Instance) relocate() {
	at := i.pool.target(i.extent)

	if at == noMove || i.block == nil {
		return
	}

	used := uint64(len(i.region))

	i.shift(at, used, used)
	i.pool.settle(i.extent, used)
}

func (i *Instance) move() {
	if at, ok := i.pool.elsewhere(i.extent); ok {
		used := uint64(len(i.region))

		i.shift(at, used, used)
		i.pool.settle(i.extent, used)
	}
}

func (i *Instance) reach(pages uint64, by *Process, zero bool) bool {
	keep, used := uint64(len(i.region)), i.usedFor(pages)

	if used <= keep {
		if i.proc.chaosMoves() {
			i.move()
		}

		return true
	}

	at, moving, err := i.pool.grow(by, i.extent, used, i.usedFor(i.memory.max))

	if err != nil {
		return false
	}

	keep = uint64(len(i.region))

	if moving {
		i.shift(at, keep, used)
		i.pool.settle(i.extent, used)
	} else {
		i.region = i.pool.mem[at : at+used : at+used]
		i.memory.data = i.region[i.below+distanceOf(i.obj.meta.instanceSize):]
	}

	if zero {
		clear(i.region[keep:])
	}

	return true
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
	i.memBound = uint64(int64(size+i.obj.meta.memory.tail) - boundSlack)
	i.put64(offMemSize, size)
	i.put64(offMemBound, i.memBound)

	if i.obj.meta.memory.tail != 0 {
		i.put64(i.obj.meta.memory.tailEnd, size+i.obj.meta.memory.tail)
	}

	if i.obj.meta.memory.shadow != 0 {
		i.put64(i.obj.meta.memory.shadow, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(i.memory.shadow)))))
	}
}

//go:nocheckptr
func current() *Instance {
	return (*Instance)(unsafe.Pointer(runtime.Instance()))
}

func instantiate(pool *Pool, name string, obj *ElfObject, imports Imports, stack uint64) *Instance {
	return throw2(newInstance(pool, nil, link(name, obj, imports), stack))
}

func newInstance(pool *Pool, by *Process, c *Code, stack uint64) (*Instance, error) {
	meta := c.obj.meta
	below, distance := profileBytes(meta), distanceOf(meta.instanceSize)
	pages := uint64(0)

	if memory := meta.memory; memory != nil {
		if memory.max > 65536 || memory.min > memory.max {
			throwFmt("memory of %d..%d pages", memory.min, memory.max)
		}

		pages = memory.min
	}

	i := &Instance{name: c.name, obj: c.obj, code: c, pool: pool, below: below}

	i.relocation = Event{kind: eventRelocate, run: func(*Process) { i.relocate() }}

	used := i.usedFor(pages)
	x, err := pool.take(by, i, used)

	if err != nil {
		return nil, err
	}

	size := below + uint64(max(meta.instanceSize, headerSize))

	i.extent = x
	i.region = pool.mem[x.at : x.at+used : x.at+used]
	i.block = i.region[below:size:size]

	clear(i.region)
	i.holds.Store(1)

	initialized := false

	defer func() {
		if !initialized {
			i.release()
		}
	}()

	i.anchor()
	i.put64(offHost, uint64(uintptr(unsafe.Pointer(i))))
	i.put64(offStackLimit, stack)

	if meta.memory != nil {
		i.memory = newMemory(i.region[below+distance:], meta.memory.min, meta.memory.max, meta.memory.shadow != 0)

		if meta.memory.tail != 0 {
			i.memory.tailUsed = true
		}

		i.memory.owner = i
		i.setMemory()
	}

	for _, t := range meta.tables {
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

	return i, nil
}

func (i *Instance) clone(by *Process, stack uint64, from, to uint64) (*Instance, error) {
	c, err := newInstance(i.pool, by, i.code, stack)

	if err != nil {
		return nil, err
	}

	if i.memory != nil {
		clean := uint64(len(c.memory.data))

		if !c.reach(i.memory.pages, by, false) {
			c.release()

			return nil, ENOMEM
		}

		c.memory.pages = i.memory.pages
		c.setMemory()

		copy(c.memory.data[from:to], i.memory.data[from:to])
		clear(c.memory.data[min(clean, from):from])
		clear(c.memory.data[max(clean, to):])

		if i.memory.tailUsed {
			c.memory.tailUsed = true

			start := i.memory.pages * pageSize

			copy(c.memory.data[start:start+boundSlack], i.memory.data[start:start+boundSlack])
		}

		if i.memory.shadow != nil {
			copy(c.memory.shadow[:to], i.memory.shadow[:to])
		}
	}

	copy(c.block[headerSize:], i.block[headerSize:])

	if meta := i.obj.meta.memory; meta != nil && meta.shadow != 0 {
		c.put64(meta.shadow, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(c.memory.shadow)))))
	}

	c.put64(offTokens, binary.LittleEndian.Uint64(i.block[offTokens:]))

	for k, t := range i.obj.meta.tables {
		entries := c.tables[k]

		copy(entries, i.tables[k])
		c.put64(t.offset+tableEntries, uint64(uintptr(unsafe.Pointer(unsafe.SliceData(entries)))))
	}

	return c, nil
}

func (i *Instance) release() {
	compilerCoverage(i)
	i.writeTailProfile()

	if i.memory != nil && i.memory.shadow != nil {
		throw(syscall.Munmap(i.memory.shadow))
	}

	if was := i.pool.runner(i, nil); was != nil {
		was.events.drop(&i.relocation)
	}

	i.block, i.memory, i.tables, i.windows = nil, nil, nil, nil
	i.unhold()
}

func (i *Instance) window(at, n int64, mode int) *Description {
	var w *Description

	if k := len(i.windows); k > 0 {
		w, i.windows = i.windows[k-1], i.windows[:k-1]
	} else {
		w = opened(&MemfdFile{ObjectIdent: newObjectIdent(), ino: memfdInodes.Add(1)}, mode)
	}

	f := w.memory()

	f.mem, f.at, f.n, f.held = i.memory, at, n, true
	w.offset = 0
	w.flags.Store(int32(mode))
	i.holds.Add(1)

	return w
}

func (i *Instance) windowDone(w *Description) {
	f := w.memory()

	if f.mem != i.memory || w.holders.Load() != 1 || len(i.windows) >= windowsKept {
		w.release()

		return
	}

	f.mem = nil
	i.windows = append(i.windows, w)
	i.unhold()
}

func (i *Instance) unhold() {
	if i.holds.Add(-1) == 0 {
		i.pool.give(i.extent)
	}
}

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

func (i *Instance) start() {
	nativeFunc(reflect.TypeFor[func(uintptr)](), i.symbol(i.obj.meta.init)).Interface().(func(uintptr))(i.handle())
}

func (i *Instance) startCommand() {
	start, entry := i.export("_start")

	if len(entry.params) != 0 || len(entry.results) != 0 {
		throwFmt("_start must have no parameters or results")
	}

	start.Interface().(func(uintptr))(i.handle())
}

func (i *Instance) export(name string) (reflect.Value, *ExportMeta) {
	for k := range i.obj.meta.exports {
		e := &i.obj.meta.exports[k]

		if e.name == name && e.kind == externFunc {
			return nativeFunc(exportType(e.params, e.results), i.symbol(e.sym)), e
		}
	}

	throwFmt("no exported function %s", name)

	return reflect.Value{}, nil
}

func (i *Instance) setjmp(j *Jump) bool {
	for k := range j.frame {
		j.frame[k] = binary.LittleEndian.Uint64(i.block[offJump+8*k:])
	}

	clear(i.block[offJump : offJump+8*jumpWords])

	return j.frame[jumpPC] != 0
}

func (i *Instance) longjmp(j *Jump, val uint32) {
	regions := i.obj.meta.regions
	pc := j.frame[jumpPC] - uint64(i.code.base)
	k := sort.Search(len(regions), func(k int) bool { return uint64(regions[k].end) > pc })

	j.code = [2]uint64{uint64(i.code.base) + uint64(regions[k].start), uint64(i.code.base) + uint64(regions[k].end)}

	land := nativeFunc(reflect.TypeFor[func(uintptr, uint32, uintptr) uint32](), landStub())

	land.Interface().(func(uintptr, uint32, uintptr) uint32)(uintptr(unsafe.Pointer(j)), val, i.handle())
	runtime.KeepAlive(j)
}

type FramesRequest struct {
	ctx, sp, bp          uint64
	funcs, entry, resume [2]uint64
	buf, cap             uint64
	size, top            uint64
}

type ResumeRequest struct {
	inst, buf, size, top, bp, sp, pc uint64
	regs                             [jumpSaved]uint64
}

type Frames struct {
	bytes []byte
	top   uint64
	jump  Jump
	req   ResumeRequest
}

func isFunctionRegion(name string) bool {
	return len(name) > 1 && name[0] == 'f' && name[1] >= '0' && name[1] <= '9'
}

func (i *Instance) codeRange(first, last int) [2]uint64 {
	regions := i.obj.meta.regions
	base := uint64(i.code.base)

	return [2]uint64{base + uint64(regions[first].start), base + uint64(regions[last].end)}
}

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

func (i *Instance) exportCode(name string) ([2]uint64, bool) {
	_, e := i.export(name)

	return i.symbolCode(e.sym)
}

func (i *Instance) symbolCode(sym string) ([2]uint64, bool) {
	at := i.obj.defined[sym]

	for k, r := range i.obj.meta.regions {
		if r.start == at {
			return i.codeRange(k, k), true
		}
	}

	return [2]uint64{}, false
}

func (i *Instance) frames(j *Jump, entry string) (*Frames, bool) {
	funcs, ok := i.functionCode()
	thunk, found := i.exportCode(entry)

	if !ok || !found {
		return nil, false
	}

	lo, hi := resumeReturnRange()
	resume := [2]uint64{uint64(lo), uint64(hi)}
	buf := make([]byte, 16<<10)
	req := &FramesRequest{ctx: uint64(i.ctx()), sp: j.frame[jumpSP], bp: j.frame[jumpBP], funcs: funcs, entry: thunk, resume: resume}
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

func (i *Instance) resume(f *Frames) uint64 {
	j := &f.jump.frame
	req := &f.req

	*req = ResumeRequest{inst: uint64(i.handle()), buf: uint64(uintptr(unsafe.Pointer(unsafe.SliceData(f.bytes)))), size: uint64(len(f.bytes)),
		top: f.top, bp: j[jumpSP] - j[jumpBP], sp: j[jumpSP], pc: j[jumpPC], regs: [jumpSaved]uint64(j[jumpRegs : jumpRegs+jumpSaved])}

	stub := nativeFunc(reflect.TypeFor[func(uintptr) uint64](), resumeStub()).Interface().(func(uintptr) uint64)
	r := stub(uintptr(unsafe.Pointer(req)))

	runtime.KeepAlive(f)

	return r
}

func (i *Instance) globalAt(name string) (uint32, bool) {
	for _, e := range i.obj.meta.exports {
		if e.kind == externGlobal && e.name == name {
			return i.obj.meta.globals[e.index].offset, true
		}
	}

	return 0, false
}

func (i *Instance) globalExport(name string) (uint32, bool) {
	at, ok := i.globalAt(name)

	if !ok {
		return 0, false
	}

	return binary.LittleEndian.Uint32(i.block[at:]), true
}

func (i *Instance) specFunctionRef(index uint64) (uintptr, bool) {
	m := i.obj.meta

	if index >= uint64(m.functionCount) {
		return 0, false
	}

	at := uint64(m.functions) + index*descBytes

	if binary.LittleEndian.Uint64(i.block[at:]) == 0 {
		return 0, false
	}

	return uintptr(at), true
}

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
		at := int(i.below) - (k+1)*profileCounterBytes
		count := binary.LittleEndian.Uint64(i.region[at : at+profileCounterBytes])

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
	path := os.Getenv("VM_TAIL_PROFILE_FILE")

	if path == "" || meta == nil || meta.profile == 0 {
		return
	}

	f := throw2(os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644))

	defer f.Close()

	throw(json.NewEncoder(f).Encode(i.tailProfile()))
}
