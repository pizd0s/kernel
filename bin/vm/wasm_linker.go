package main

import (
	"bytes"
	"encoding/binary"
	"maps"
	"slices"
	"strings"
)

const wasmPage = 1 << 16

type WasmLinker struct {
	o        *WasmObject
	sections []WasmSection

	types   [][]byte
	typeAt  map[string]uint32
	imports []WasmImport
	funcs   []uint32
	bound   map[int]uint32

	stack      int
	heapGlobal int
	heap       uint32
	newHeap    uint32

	merged  []WasmImport
	funcMap []uint32
	objMap  []uint32
	live    []bool
	used    []bool
	segLive []bool
	segAt   []uint32

	hasTable  bool
	needTable bool
	tableBase uint32
	slots     map[uint32]uint32
	slotFuncs []uint32
}

func linkLibc(module []byte, functions map[string]any) []byte {
	if !bytes.HasPrefix(module, wasmHeader) {
		return module
	}

	l := &WasmLinker{o: libcObject(), bound: map[int]uint32{}, slots: map[uint32]uint32{}}

	for name := range l.o.kernel {
		if _, ok := functions[name]; ok {
			throwFmt("kernel function %s is both Go's and libc.wasm's", name)
		}
	}

	if e := try(func() { l.bind(module) }); e != nil || len(l.bound) == 0 {
		return module
	}

	return l.link()
}

func (l *WasmLinker) bind(module []byte) {
	l.sections = wasmSections(module)

	if s := l.section(2); s != nil {
		r := WasmReader{data: s.body}

		l.imports = r.imports()
	}

	for i, imp := range l.imports {
		if f, ok := l.o.kernel[imp.name]; ok && imp.module == "kernel" && imp.kind == 0 {
			l.bound[i] = f
		}
	}
}

func (l *WasmLinker) section(id byte) *WasmSection {
	for i := range l.sections {
		if l.sections[i].id == id && id != 0 {
			return &l.sections[i]
		}
	}

	return nil
}

func (l *WasmLinker) link() []byte {
	l.read()
	l.mark()
	l.layout()
	l.place()

	var bodies, segments [][]byte

	for f, live := range l.live {
		if live {
			bodies = append(bodies, l.patch(l.o.bodies[f]))
		}
	}

	for s, live := range l.segLive {
		piece := l.o.segments[s]

		if !live || len(piece.relocs) == 0 && !slices.ContainsFunc(piece.data, func(c byte) bool { return c != 0 }) {
			continue
		}

		data := l.patch(piece)
		segment := append(appendSleb([]byte{0, 0x41}, int64(int32(l.segAt[s]))), 0x0b)

		segments = append(segments, append(appendLeb(segment, uint64(len(data))), data...))
	}

	code, shift, moved := l.code(bodies)
	built := map[byte][]byte{10: code}

	built[3] = l.functionSection()
	built[2] = l.importSection()
	built[4] = l.tableSection()
	built[5] = l.memorySection()
	built[6] = l.globalSection()
	built[7] = l.exportSection()
	built[8] = l.startSection()
	built[9] = l.elementSection()
	built[11] = l.dataSection(segments)
	built[12] = l.dataCountSection(len(segments))
	built[1] = l.typeSection()

	out := bytes.Clone(wasmHeader)

	if shift > 0 && !moved {
		out = appendWasmSection(out, 0, appendLeb(appendWasmName(nil, "w2o.code_shift"), uint64(shift)))
	}

	for _, id := range []byte{1, 2, 3, 4, 5, 13, 6, 7, 8, 9, 12, 10, 11} {
		if b := built[id]; b != nil {
			out = appendWasmSection(out, id, b)
		} else if s := l.section(id); s != nil {
			out = appendWasmSection(out, id, s.body)
		}
	}

	named := false

	for _, s := range l.sections {
		switch {
		case s.id != 0, s.name == "linking", strings.HasPrefix(s.name, "reloc."), moved && strings.HasPrefix(s.name, ".debug_"):
		case s.name == "name":
			out = appendWasmSection(out, 0, l.names(s.body))
			named = true
		default:
			out = appendWasmSection(out, 0, s.body)
		}
	}

	if !named {
		out = appendWasmSection(out, 0, l.names(appendWasmSection(appendWasmName(nil, "name"), 1, appendLeb(nil, 0))))
	}

	return out
}

func (l *WasmLinker) read() {
	for _, imp := range l.imports {
		if imp.kind != 0 {
			throwFmt("wasm: an import of kind %d", imp.kind)
		}
	}

	if s := l.section(1); s != nil {
		r := WasmReader{data: s.body}

		l.types = r.types()
	}

	l.typeAt = map[string]uint32{}

	for i, t := range l.types {
		if _, ok := l.typeAt[string(t)]; !ok {
			l.typeAt[string(t)] = uint32(i)
		}
	}

	if s := l.section(3); s != nil {
		r := WasmReader{data: s.body}

		for n := r.count(1); n > 0; n-- {
			l.funcs = append(l.funcs, r.u32())
		}
	}

	for i, f := range l.bound {
		have, want := l.imports[i].typ, l.o.funcs[int(f)-len(l.o.funcImports)]

		if int(have) >= len(l.types) || int(want) >= len(l.o.types) || !bytes.Equal(l.types[have], l.o.types[want]) {
			throwFmt("kernel function %s: the module's type is not libc.wasm's", l.imports[i].name)
		}
	}

	l.stack, l.heapGlobal = -1, -1

	if s := l.section(7); s != nil {
		r := WasmReader{data: s.body}

		for n := r.count(3); n > 0; n-- {
			name, kind, index := r.name(), r.byte(), r.u32()

			switch {
			case kind == 3 && name == "__stack_pointer":
				l.stack = int(index)
			case kind == 3 && name == "__heap_base":
				l.heapGlobal = int(index)
			}
		}
	}

	l.heap = l.globalValue()

	if s := l.section(4); s != nil {
		r := WasmReader{data: s.body}

		if r.count(3) > 0 {
			r.byte()
			r.u32()

			l.hasTable, l.tableBase = true, r.u32()
		}
	}

	if !l.hasTable {
		l.tableBase = 1
	}
}

func (l *WasmLinker) globalValue() uint32 {
	s := l.section(6)

	if l.heapGlobal < 0 || s == nil {
		throwFmt("wasm: no __heap_base")
	}

	r := WasmReader{data: s.body}

	for i, n := 0, r.count(3); i < n; i++ {
		r.bytes(2)

		if i < l.heapGlobal {
			remapExpr(&r, nil, nil)

			continue
		}

		if r.byte() != 0x41 {
			break
		}

		v := r.s32()

		if r.byte() != 0x0b {
			break
		}

		return uint32(v)
	}

	throwFmt("wasm: no constant __heap_base")

	return 0
}

func (l *WasmLinker) symbol(i uint32, kind byte) WasmSymbol {
	o := l.o

	if int(i) >= len(o.symbols) || o.symbols[i].kind != kind {
		throwFmt("libc.wasm: symbol %d of no kind %d", i, kind)
	}

	sym := o.symbols[i]

	if kind == wasmSymbolFunction && int(sym.index) >= len(o.funcImports)+len(o.funcs) {
		throwFmt("libc.wasm: function %d of %d", sym.index, len(o.funcImports)+len(o.funcs))
	}

	return sym
}

func (l *WasmLinker) mark() {
	o := l.o
	imported := len(o.funcImports)

	l.live, l.used, l.segLive = make([]bool, len(o.funcs)), make([]bool, imported), make([]bool, len(o.segments))

	var funcs, segs []int

	visit := func(fi int) {
		if fi < imported {
			l.used[fi] = true
		} else if f := fi - imported; !l.live[f] {
			l.live[f] = true
			funcs = append(funcs, f)
		}
	}

	refer := func(x WasmReloc) {
		switch x.kind {
		case wasmFunctionIndexLeb, wasmFunctionIndexI32, wasmTableIndexSleb, wasmTableIndexI32:
			visit(int(l.symbol(x.index, wasmSymbolFunction).index))
		case wasmMemoryAddrLeb, wasmMemoryAddrSleb, wasmMemoryAddrI32:
			sym := l.symbol(x.index, wasmSymbolData)

			if sym.flags&wasmSymbolUndefined != 0 || int(sym.index) >= len(o.segments) {
				throwFmt("libc.wasm: %s is no data of its own", sym.name)
			}

			if !l.segLive[sym.index] {
				l.segLive[sym.index] = true
				segs = append(segs, int(sym.index))
			}
		case wasmTypeIndexLeb, wasmGlobalIndexLeb, wasmGlobalIndexI32, wasmTableNumberLeb:
		default:
			throwFmt("libc.wasm: a relocation of kind %d", x.kind)
		}
	}

	for _, fi := range l.bound {
		visit(int(fi))
	}

	for _, fi := range o.exported {
		visit(int(fi))
	}

	for len(funcs) > 0 || len(segs) > 0 {
		var relocs []WasmReloc

		if n := len(funcs); n > 0 {
			relocs, funcs = o.bodies[funcs[n-1]].relocs, funcs[:n-1]
		} else {
			n = len(segs)
			relocs, segs = o.segments[segs[n-1]].relocs, segs[:n-1]
		}

		for _, x := range relocs {
			refer(x)
		}
	}
}

func (l *WasmLinker) layout() {
	o := l.o
	imported := len(o.funcImports)
	have := map[string]uint32{}

	l.funcMap = make([]uint32, len(l.imports)+len(l.funcs))
	l.objMap = make([]uint32, imported+len(o.funcs))

	for i := range l.objMap {
		l.objMap[i] = ^uint32(0)
	}

	for i, imp := range l.imports {
		if _, ok := l.bound[i]; !ok {
			l.funcMap[i] = uint32(len(l.merged))
			have[imp.module+"\x00"+imp.name] = l.funcMap[i]
			l.merged = append(l.merged, imp)
		}
	}

	for i, imp := range o.funcImports {
		if !l.used[i] {
			continue
		}

		if _, ok := o.kernel[imp.name]; imp.module != "kernel" || ok {
			throwFmt("libc.wasm: %s.%s is undefined", imp.module, imp.name)
		}

		if int(imp.typ) >= len(o.types) {
			throwFmt("libc.wasm: type %d of %d", imp.typ, len(o.types))
		}

		key := imp.module + "\x00" + imp.name

		if at, ok := have[key]; ok {
			if t := l.merged[at].typ; int(t) >= len(l.types) || !bytes.Equal(l.types[t], o.types[imp.typ]) {
				throwFmt("kernel function %s: the module's type is not libc.wasm's", imp.name)
			}

			l.objMap[i] = at

			continue
		}

		l.objMap[i] = uint32(len(l.merged))
		have[key] = l.objMap[i]
		l.merged = append(l.merged, WasmImport{module: imp.module, name: imp.name, typ: l.typeOf(imp.typ)})
	}

	next := uint32(len(l.merged))

	for j := range l.funcs {
		l.funcMap[len(l.imports)+j] = next
		next++
	}

	for f, live := range l.live {
		if live {
			l.objMap[imported+f] = next
			next++
		}
	}

	for i, fi := range l.bound {
		l.funcMap[i] = l.objMap[fi]
	}
}

func (l *WasmLinker) place() {
	at := uint64(l.heap)

	l.segAt = make([]uint32, len(l.o.segments))

	for s, live := range l.segLive {
		if live {
			align := uint64(1) << l.o.segments[s].align

			at = (at + align - 1) &^ (align - 1)
			l.segAt[s] = uint32(at)
			at += uint64(len(l.o.segments[s].data))
		}
	}

	at = (at + 15) &^ 15

	if at > 1<<32-wasmPage {
		throwFmt("wasm: libc.wasm's data past the memory")
	}

	l.newHeap = uint32(at)
}

func (l *WasmLinker) typeOf(t uint32) uint32 {
	if int(t) >= len(l.o.types) {
		throwFmt("libc.wasm: type %d of %d", t, len(l.o.types))
	}

	key := string(l.o.types[t])

	if at, ok := l.typeAt[key]; ok {
		return at
	}

	at := uint32(len(l.types))

	l.types = append(l.types, l.o.types[t])
	l.typeAt[key] = at

	return at
}

func (l *WasmLinker) function(i uint32) uint32 {
	f := l.objMap[l.symbol(i, wasmSymbolFunction).index]

	if f == ^uint32(0) {
		throwFmt("libc.wasm: function %d unlinked", l.symbol(i, wasmSymbolFunction).index)
	}

	return f
}

func (l *WasmLinker) slot(i uint32) uint32 {
	f := l.function(i)

	if at, ok := l.slots[f]; ok {
		return at
	}

	at := l.tableBase + uint32(len(l.slotFuncs))

	l.slots[f] = at
	l.slotFuncs = append(l.slotFuncs, f)

	return at
}

func (l *WasmLinker) address(x WasmReloc) uint32 {
	sym := l.symbol(x.index, wasmSymbolData)

	return l.segAt[sym.index] + sym.offset + uint32(x.addend)
}

func (l *WasmLinker) global(i uint32) uint32 {
	o := l.o
	sym := l.symbol(i, wasmSymbolGlobal)

	if sym.flags&wasmSymbolUndefined == 0 || int(sym.index) >= len(o.globalImports) || o.globalImports[sym.index].name != "__stack_pointer" {
		throwFmt("libc.wasm: global %s", sym.name)
	}

	if l.stack < 0 {
		throwFmt("wasm: no __stack_pointer")
	}

	return uint32(l.stack)
}

func (l *WasmLinker) table(i uint32) uint32 {
	o := l.o
	sym := l.symbol(i, wasmSymbolTable)

	if sym.flags&wasmSymbolUndefined == 0 || int(sym.index) >= len(o.tableImports) || o.tableImports[sym.index].name != "__indirect_function_table" {
		throwFmt("libc.wasm: table %s", sym.name)
	}

	l.needTable = true

	return 0
}

func (l *WasmLinker) patch(piece WasmPiece) []byte {
	b := bytes.Clone(piece.data)

	for _, x := range piece.relocs {
		at := b[x.offset:]

		switch x.kind {
		case wasmFunctionIndexLeb:
			putLeb5(at, l.function(x.index))
		case wasmFunctionIndexI32:
			binary.LittleEndian.PutUint32(at, l.function(x.index))
		case wasmTableIndexSleb:
			putSleb5(at, int32(l.slot(x.index)))
		case wasmTableIndexI32:
			binary.LittleEndian.PutUint32(at, l.slot(x.index))
		case wasmMemoryAddrLeb:
			putLeb5(at, l.address(x))
		case wasmMemoryAddrSleb:
			putSleb5(at, int32(l.address(x)))
		case wasmMemoryAddrI32:
			binary.LittleEndian.PutUint32(at, l.address(x))
		case wasmTypeIndexLeb:
			putLeb5(at, l.typeOf(x.index))
		case wasmGlobalIndexLeb:
			putLeb5(at, l.global(x.index))
		case wasmGlobalIndexI32:
			binary.LittleEndian.PutUint32(at, l.global(x.index))
		case wasmTableNumberLeb:
			putLeb5(at, l.table(x.index))
		}
	}

	return b
}

func (l *WasmLinker) code(bodies [][]byte) ([]byte, int, bool) {
	var count uint64

	width := 1
	r := WasmReader{}

	if s := l.section(10); s != nil {
		r.data = s.body
		count, width = r.leb(5)
	}

	if count != uint64(len(l.funcs)) {
		throwFmt("wasm: %d bodies of %d functions", count, len(l.funcs))
	}

	total := count + uint64(len(bodies))
	w := max(width, lebWidth(total))
	out := appendLebWide(make([]byte, 0, len(r.data)+64), total, w)
	moved := false

	for ; count > 0; count-- {
		size, sw := r.leb(5)

		if size > uint64(r.left()) {
			throwFmt("wasm: the module ends early")
		}

		body := r.bytes(uint32(size))
		b := remapBody(body, l.funcMap)

		moved = moved || len(b) != len(body)
		out = append(appendLebWide(out, uint64(len(b)), max(sw, lebWidth(uint64(len(b))))), b...)
	}

	if r.more() {
		throwFmt("wasm: a code section longer than its bodies")
	}

	for _, b := range bodies {
		out = append(appendLeb(out, uint64(len(b))), b...)
	}

	return out, w - width, moved
}

func (l *WasmLinker) typeSection() []byte {
	out := appendLeb(nil, uint64(len(l.types)))

	for _, t := range l.types {
		out = append(out, t...)
	}

	return out
}

func (l *WasmLinker) importSection() []byte {
	out := appendLeb(nil, uint64(len(l.merged)))

	for _, imp := range l.merged {
		out = appendLeb(append(appendWasmName(appendWasmName(out, imp.module), imp.name), 0), uint64(imp.typ))
	}

	return out
}

func (l *WasmLinker) functionSection() []byte {
	var types []uint32

	for f, live := range l.live {
		if live {
			types = append(types, l.typeOf(l.o.funcs[f]))
		}
	}

	out := appendLeb(nil, uint64(len(l.funcs)+len(types)))

	for _, t := range append(slices.Clone(l.funcs), types...) {
		out = appendLeb(out, uint64(t))
	}

	return out
}

func (l *WasmLinker) tableSection() []byte {
	grow := uint64(len(l.slotFuncs))

	if !l.hasTable {
		if grow == 0 && !l.needTable {
			return nil
		}

		size := uint64(l.tableBase) + grow

		return appendLeb(appendLeb([]byte{1, 0x70, 1}, size), size)
	}

	s := l.section(4)

	if grow == 0 {
		return s.body
	}

	r := WasmReader{data: s.body}
	n := r.count(3)

	if r.byte() != 0x70 {
		throwFmt("wasm: a table of no functions")
	}

	flags := r.u32()

	if flags > 1 {
		throwFmt("wasm: a table of flags %d", flags)
	}

	size := uint64(r.u32()) + grow
	out := appendLeb(append(appendLeb(nil, uint64(n)), 0x70, byte(flags)), size)

	if flags == 1 {
		out = appendLeb(out, max(uint64(r.u32()), size))
	}

	return append(out, r.data[r.pos:]...)
}

func (l *WasmLinker) memorySection() []byte {
	s := l.section(5)

	if s == nil {
		throwFmt("wasm: no memory")
	}

	r := WasmReader{data: s.body}

	if r.count(2) != 1 {
		throwFmt("wasm: no one memory")
	}

	flags := r.u32()

	if flags > 1 {
		throwFmt("wasm: a memory of flags %d", flags)
	}

	pages := max(uint64(r.u32()), (uint64(l.newHeap)+wasmPage-1)/wasmPage)
	out := appendLeb(append([]byte{1}, byte(flags)), pages)

	if flags == 1 {
		most := uint64(r.u32())

		if most < pages {
			throwFmt("wasm: libc.wasm's data past the memory's %d pages", most)
		}

		out = appendLeb(out, most)
	}

	return out
}

func (l *WasmLinker) globalSection() []byte {
	r := WasmReader{data: l.section(6).body}
	n := r.count(3)
	out := appendLeb(nil, uint64(n))

	for i := 0; i < n; i++ {
		out = append(out, r.bytes(2)...)

		if i != l.heapGlobal {
			out = remapExpr(&r, out, l.funcMap)

			continue
		}

		remapExpr(&r, nil, nil)

		out = append(appendSleb(append(out, 0x41), int64(int32(l.newHeap))), 0x0b)
	}

	return out
}

func (l *WasmLinker) exportSection() []byte {
	var out []byte

	count, have := 0, map[string]bool{}

	if s := l.section(7); s != nil {
		r := WasmReader{data: s.body}

		for n := r.count(3); n > 0; n-- {
			name, kind, index := r.name(), r.byte(), r.u32()

			if kind == 0 {
				index = wasmFunction(l.funcMap, uint64(index))
			}

			out = appendLeb(append(appendWasmName(out, name), kind), uint64(index))
			have[name] = true
			count++
		}
	}

	for _, name := range slices.Sorted(maps.Keys(l.o.exported)) {
		if have[name] {
			throwFmt("wasm: %s is the module's export and libc.wasm's", name)
		}

		out = appendLeb(append(appendWasmName(out, name), 0), uint64(l.objMap[l.o.exported[name]]))
		count++
	}

	if count == 0 {
		return nil
	}

	return append(appendLeb(nil, uint64(count)), out...)
}

func (l *WasmLinker) startSection() []byte {
	s := l.section(8)

	if s == nil {
		return nil
	}

	r := WasmReader{data: s.body}

	return appendLeb(nil, uint64(wasmFunction(l.funcMap, uint64(r.u32()))))
}

func (l *WasmLinker) elementSection() []byte {
	s := l.section(9)
	n := 0

	var body []byte

	if s != nil {
		r := WasmReader{data: s.body}

		n = r.count(2)

		for i := 0; i < n; i++ {
			body = l.element(&r, body)
		}
	}

	if len(l.slotFuncs) > 0 {
		body = append(appendSleb(append(body, 0, 0x41), int64(int32(l.tableBase))), 0x0b)
		body = appendLeb(body, uint64(len(l.slotFuncs)))

		for _, f := range l.slotFuncs {
			body = appendLeb(body, uint64(f))
		}

		n++
	}

	if n == 0 {
		return nil
	}

	return append(appendLeb(nil, uint64(n)), body...)
}

func (l *WasmLinker) element(r *WasmReader, out []byte) []byte {
	flags := r.u32()

	if flags > 7 {
		throwFmt("wasm: an element segment of flags %d", flags)
	}

	out = appendLeb(out, uint64(flags))

	if flags&3 == 2 {
		out = appendLeb(out, uint64(r.u32()))
	}

	if flags&1 == 0 {
		out = remapExpr(r, out, l.funcMap)
	}

	if flags&3 != 0 {
		out = append(out, r.byte())
	}

	m := r.count(1)

	out = appendLeb(out, uint64(m))

	for ; m > 0; m-- {
		if flags&4 != 0 {
			out = remapExpr(r, out, l.funcMap)
		} else {
			out = appendLeb(out, uint64(wasmFunction(l.funcMap, uint64(r.u32()))))
		}
	}

	return out
}

func (l *WasmLinker) dataSection(segments [][]byte) []byte {
	s := l.section(11)

	if s == nil && len(segments) == 0 {
		return nil
	}

	n := 0

	var rest []byte

	if s != nil {
		r := WasmReader{data: s.body}

		n = r.count(1)
		rest = s.body[r.pos:]
	}

	out := append(appendLeb(nil, uint64(n+len(segments))), rest...)

	for _, segment := range segments {
		out = append(out, segment...)
	}

	return out
}

func (l *WasmLinker) dataCountSection(added int) []byte {
	s := l.section(12)

	if s == nil {
		return nil
	}

	r := WasmReader{data: s.body}

	return appendLeb(nil, uint64(r.u32())+uint64(added))
}

func (l *WasmLinker) names(body []byte) []byte {
	r := WasmReader{data: body}
	out := appendWasmName(nil, r.name())

	for r.more() {
		id := r.byte()
		sub := r.bytes(r.u32())

		switch id {
		case 1:
			sub = l.functionNames(sub)
		case 2, 3:
			sub = l.nestedNames(sub)
		}

		out = appendWasmSection(out, id, sub)
	}

	return out
}

func (l *WasmLinker) kept(index uint32) bool {
	_, bound := l.bound[int(index)]

	return !bound && int(index) < len(l.funcMap)
}

func (l *WasmLinker) functionNames(sub []byte) []byte {
	o := l.o
	r := WasmReader{data: sub}
	named := map[uint32]string{}

	for n := r.count(2); n > 0; n-- {
		index, name := r.u32(), r.name()

		if l.kept(index) {
			named[l.funcMap[index]] = name
		}
	}

	for i, f := range l.objMap {
		name := o.names[uint32(i)]

		if name == "" && i < len(o.funcImports) {
			name = o.funcImports[i].name
		}

		if _, ok := named[f]; !ok && f != ^uint32(0) && name != "" {
			named[f] = name
		}
	}

	out := appendLeb(nil, uint64(len(named)))

	for _, index := range slices.Sorted(maps.Keys(named)) {
		out = appendWasmName(appendLeb(out, uint64(index)), named[index])
	}

	return out
}

func (l *WasmLinker) nestedNames(sub []byte) []byte {
	r := WasmReader{data: sub}
	nested := map[uint32][]byte{}

	for n := r.count(2); n > 0; n-- {
		index := r.u32()
		start := r.pos

		for m := r.count(2); m > 0; m-- {
			r.u32()
			r.name()
		}

		if l.kept(index) {
			nested[l.funcMap[index]] = r.data[start:r.pos]
		}
	}

	out := appendLeb(nil, uint64(len(nested)))

	for _, index := range slices.Sorted(maps.Keys(nested)) {
		out = append(appendLeb(out, uint64(index)), nested[index]...)
	}

	return out
}
