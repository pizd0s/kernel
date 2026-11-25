package main

import (
	"encoding/binary"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"unsafe"
)

var (
	//go:linkname lastModule runtime.lastmoduledatap
	lastModule    *GoModule
	hostModule    = lastModule
	registrations = make(chan Registration)
)

const funcFlagAsm = 4

type GoPCHeader struct {
	magic                                                            uint32
	pad1, pad2, minLC, ptrSize                                       uint8
	nfunc                                                            int
	nfiles                                                           uint
	textStart                                                        uintptr
	funcnameOffset, cuOffset, filetabOffset, pctabOffset, pclnOffset uintptr
}

type GoFuncTab struct {
	entry, offset uint32
}

type GoBitVector struct {
	n    int32
	data *byte
}

type GoModule struct {
	header                                                     *GoPCHeader
	names                                                      []byte
	cutab                                                      []uint32
	files, pctab, pcln                                         []byte
	ftab                                                       []GoFuncTab
	findfunctab, minpc, maxpc                                  uintptr
	text, etext, noptrdata, enoptrdata, data, edata, bss, ebss uintptr
	noptrbss, enoptrbss, covctrs, ecovctrs, end, gcdata, gcbss uintptr
	types, etypes, rodata, gofunc                              uintptr
	textsectmap                                                []byte
	typelinks                                                  []int32
	itablinks, ptab                                            []byte
	pluginpath                                                 string
	pkghashes, inittasks                                       []byte
	modulename                                                 string
	modulehashes                                               []byte
	hasmain                                                    uint8
	bad                                                        bool
	gcdatamask, gcbssmask                                      GoBitVector
	typemap                                                    map[int32]unsafe.Pointer
	next                                                       *GoModule
}

type GoImage struct {
	module   GoModule
	header   GoPCHeader
	stackMap []byte
	buckets  []byte
}

type Registration struct {
	image *GoImage
	done  chan struct{}
}

func init() {
	go func() {
		var images []*GoImage

		for r := range registrations {
			images = append(images, r.image)

			m := &r.image.module

			atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&lastModule.next)), unsafe.Pointer(m))
			lastModule = m
			close(r.done)
		}
	}()
}

func checkGo() {
	if !strings.HasPrefix(runtime.Version(), "go1.25.") {
		throwFmt("the loader is written for Go 1.25, not %s", runtime.Version())
	}

	if runtime.InstanceOffset() != gInstance {
		throwFmt("g.instance is at %d, the code reads %d", runtime.InstanceOffset(), gInstance)
	}

	if unsafe.Offsetof(Instance{}.memBase) != 0 || unsafe.Offsetof(Instance{}.memBound) != 8 || unsafe.Offsetof(Instance{}.blockAt) != 16 {
		throwFmt("the instance's base, bound and block are not where the code reads them")
	}
}

func resumeReturnRange() (lo, hi uintptr) {
	checkGo()

	m := hostModule
	pc := resumeReturnStub() - m.text
	k := sort.Search(len(m.ftab), func(k int) bool { return uintptr(m.ftab[k].entry) > pc })

	return m.text + uintptr(m.ftab[k-1].entry), m.text + uintptr(m.ftab[k].entry)
}

func append32(dst []byte, values ...uint32) []byte {
	for _, v := range values {
		dst = binary.LittleEndian.AppendUint32(dst, v)
	}

	return dst
}

func appendValue(values []SpEntry, pc, value int32) []SpEntry {
	if values[len(values)-1].delta == value {
		return values
	}

	return append(values, SpEntry{pc, value})
}

func (m *GoModule) pcTable(values []SpEntry, size uint32) uint32 {
	offset := uint32(len(m.pctab))
	previous := int32(-1)

	for k, v := range values {
		end := int32(size)

		if k+1 < len(values) {
			end = values[k+1].pc
		}

		if end <= v.pc || k > 0 && v.delta == previous {
			throwFmt("internal error: bad pc table")
		}

		delta := v.delta - previous

		m.pctab = binary.AppendUvarint(m.pctab, uint64(uint32(delta<<1)^uint32(delta>>31)))
		m.pctab = binary.AppendUvarint(m.pctab, uint64(end-v.pc))

		previous = v.delta
	}

	m.pctab = append(m.pctab, 0)

	return offset
}

func registerCode(name string, base uintptr, size uintptr, regions []Region, assembly bool, files []string, lines []LineEntry) {
	checkGo()

	if len(regions) == 0 {
		return
	}

	img := &GoImage{stackMap: append32(nil, 1, 0)}
	m := &img.module
	end := regions[len(regions)-1].end

	img.header = GoPCHeader{magic: 0xfffffff1, minLC: 1, ptrSize: 8, nfunc: len(regions), nfiles: uint(1 + len(files)), textStart: base}

	m.header = &img.header
	m.text, m.minpc, m.maxpc, m.etext = base, base, base+uintptr(end), base+size
	m.modulename = name
	m.gofunc = uintptr(unsafe.Pointer(unsafe.SliceData(img.stackMap)))
	m.names = []byte{0}
	m.pctab = []byte{0}
	m.files, m.cutab = append([]byte(name), 0), []uint32{0}

	for _, f := range files {
		m.cutab = append(m.cutab, uint32(len(m.files)))
		m.files = append(append(m.files, f...), 0)
	}

	for index, r := range regions {
		size := r.end - r.start

		if index > 0 && r.start < regions[index-1].end || len(r.sp) == 0 || r.sp[0].pc != 0 {
			throwFmt("invalid code region %s", r.name)
		}

		fileValues, lineValues := []SpEntry{{0, 0}}, []SpEntry{{0, int32(index + 1)}}

		for k := sort.Search(len(lines), func(k int) bool { return lines[k].pc >= r.start }); k < len(lines) && lines[k].pc < r.end; k++ {
			e := lines[k]
			pc := int32(e.pc - r.start)

			if k > 0 && lines[k-1].pc >= r.start {
				fileValues = appendValue(fileValues, pc, int32(e.file+1))
				lineValues = appendValue(lineValues, pc, int32(e.line))
			} else {
				fileValues, lineValues = []SpEntry{{0, int32(e.file + 1)}}, []SpEntry{{0, int32(e.line)}}
			}
		}

		line := uint32(lineValues[0].delta)
		flag := uint32(0)

		if assembly {
			flag = funcFlagAsm
		}

		pcsp := m.pcTable(r.sp, size)
		pcfile := m.pcTable(fileValues, size)
		pcln := m.pcTable(lineValues, size)

		m.ftab = append(m.ftab, GoFuncTab{r.start, uint32(len(m.pcln))})
		m.pcln = append32(m.pcln, r.start, uint32(len(m.names)), 0, 0, pcsp, pcfile, pcln, 0, 0, line, 2<<24|flag<<8, 0, 0)
		m.names = append(m.names, r.name...)
		m.names = append(m.names, 0)
	}

	m.ftab = append(m.ftab, GoFuncTab{end, 0})
	img.buckets = make([]byte, (uintptr(end)+4095)/4096*20)

	function := 0

	for bucket := 0; bucket < len(img.buckets)/20; bucket++ {
		for function+1 < len(m.ftab)-1 && m.ftab[function+1].entry <= uint32(bucket*4096) {
			function++
		}

		binary.LittleEndian.PutUint32(img.buckets[bucket*20:], uint32(function))
	}

	m.findfunctab = uintptr(unsafe.Pointer(unsafe.SliceData(img.buckets)))
	perfOut.code(name, base, regions, files, lines)
	noteCode(name, base, base+size)

	r := Registration{img, make(chan struct{})}

	registrations <- r
	<-r.done
}
