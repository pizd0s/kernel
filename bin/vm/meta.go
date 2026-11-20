package main

import "encoding/binary"

var siteKinds = [...]string{"check", "guard", "fallback", "omitted", "loop", "entry", "fast_loop", "fast_entry"}

const (
	siteCheck = iota
	siteGuard
	siteFallback
	siteOmitted
)

type MemoryMeta struct {
	min, max uint64
	tail     uint64
	tailEnd  uint32

	profile uint32
	sites   []ProfileSite

	shadow uint32
}

type ProfileSite struct {
	function, kind, offset uint32
}

func (m *Meta) asan() bool {
	return m.memory != nil && m.memory.shadow != 0
}

type TableMeta struct {
	offset   uint32
	min, max uint64
}

type GlobalMeta struct {
	offset  uint32
	typ     ValType
	mutable bool
}

type ImportMeta struct {
	kind   byte
	module string
	name   string

	index   uint32
	sym     string
	params  []ValType
	results []ValType
}

type ExportMeta struct {
	kind  byte
	name  string
	index uint32

	sym     string
	params  []ValType
	results []ValType
}

type Region struct {
	name       string
	start, end uint32
	sp         []SpEntry
}

type LineEntry struct {
	pc, file, line uint32
}

type Meta struct {
	instanceSize uint32
	memory       *MemoryMeta
	tables       []TableMeta
	globals      []GlobalMeta
	imports      []ImportMeta
	exports      []ExportMeta

	init    string
	regions []Region

	files []string
	lines []LineEntry

	functions, functionCount uint32
}

type MetaReader struct {
	data []byte
}

func (r *MetaReader) u() uint64 {
	x, n := binary.Uvarint(r.data)

	if n <= 0 {
		throwFmt("malformed object metadata")
	}

	r.data = r.data[n:]

	return x
}

func (r *MetaReader) u32() uint32 {
	x := r.u()

	if x > 1<<32-1 {
		throwFmt("malformed object metadata")
	}

	return uint32(x)
}

func (r *MetaReader) flag() bool {
	return r.u() != 0
}

func (r *MetaReader) count() int {
	n := r.u()

	if n > uint64(len(r.data)) {
		throwFmt("malformed object metadata")
	}

	return int(n)
}

func (r *MetaReader) str() string {
	n := r.count()
	s := string(r.data[:n])

	r.data = r.data[n:]

	return s
}

func decodeNote(data []byte) *Meta {
	r := &MetaReader{data: data}
	m := &Meta{instanceSize: r.u32()}

	if m.instanceSize > 1<<30 {
		throwFmt("instance of %d bytes", m.instanceSize)
	}

	if r.flag() {
		m.memory = &MemoryMeta{min: r.u(), max: r.u()}
	}

	m.tables = make([]TableMeta, r.count())

	for i := range m.tables {
		m.tables[i] = TableMeta{offset: r.u32(), min: r.u(), max: r.u()}
	}

	m.globals = make([]GlobalMeta, r.count())

	for i := range m.globals {
		m.globals[i] = GlobalMeta{offset: r.u32(), typ: ValType(r.u()), mutable: r.flag()}
	}

	m.imports = make([]ImportMeta, r.count())

	for i := range m.imports {
		m.imports[i] = ImportMeta{kind: byte(r.u()), module: r.str(), name: r.str(), index: r.u32()}
	}

	m.exports = make([]ExportMeta, r.count())

	for i := range m.exports {
		m.exports[i] = ExportMeta{kind: byte(r.u()), name: r.str(), index: r.u32()}
	}

	flags := r.u()

	if flags&7 != 0 && m.memory == nil {
		throwFmt("memory metadata without a memory")
	}

	if flags&1 != 0 {
		m.memory.tail = r.u()
		m.memory.tailEnd = r.u32()

		if m.memory.tail != boundSlack || m.memory.tailEnd&7 != 0 || uint64(m.memory.tailEnd)+8 > uint64(m.instanceSize) {
			throwFmt("unsupported memory tail %d", m.memory.tail)
		}
	}

	if flags&2 != 0 {
		m.memory.profile = r.u32()
		m.memory.sites = make([]ProfileSite, r.count())

		for k := range m.memory.sites {
			m.memory.sites[k] = ProfileSite{function: r.u32(), kind: r.u32(), offset: r.u32()}

			if m.memory.sites[k].kind >= uint32(len(siteKinds)) {
				throwFmt("invalid memory tail profile")
			}
		}

		if m.memory.tail == 0 || m.memory.profile != 1 {
			throwFmt("invalid memory tail profile")
		}
	}

	if flags&4 != 0 {
		m.memory.shadow = r.u32()

		if m.memory.tail != 0 || m.memory.shadow < headerSize || m.memory.shadow&7 != 0 || uint64(m.memory.shadow)+8 > uint64(m.instanceSize) {
			throwFmt("invalid shadow slot %d", m.memory.shadow)
		}
	}

	if flags&8 != 0 {
		m.files = make([]string, r.count())

		for i := range m.files {
			m.files[i] = r.str()
		}

		m.lines = make([]LineEntry, r.count())

		pc := uint32(0)

		for i := range m.lines {
			pc += r.u32()
			m.lines[i] = LineEntry{pc: pc, file: r.u32(), line: r.u32()}

			if uint64(m.lines[i].file) >= uint64(len(m.files)) || i > 0 && pc == m.lines[i-1].pc {
				throwFmt("invalid line table")
			}
		}
	}

	if flags&16 != 0 {
		m.functions, m.functionCount = r.u32(), r.u32()

		if m.functions < headerSize || m.functions&15 != 0 || m.functionCount == 0 || uint64(m.functions)+uint64(m.functionCount)*descBytes > uint64(m.instanceSize) {
			throwFmt("invalid function descriptors")
		}
	}

	if len(r.data) != 0 {
		throwFmt("trailing object metadata")
	}

	return m
}
