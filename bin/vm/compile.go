//go:build linux && amd64

package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

var (
	//go:embed aot.aot
	compilerBlob []byte
	wasmHeader   = []byte("\x00asm\x01\x00\x00\x00")
	exeMagic     = []byte("PZD1")
)

var compilerCode = sync.OnceValue(func() *Code {
	return link("aot", readObject(compilerBlob), CompilerImports{})
})

var unpacker = sync.OnceValue(func() *zstd.Decoder {
	return throw2(zstd.NewReader(nil, zstd.WithDecoderMaxMemory(unpackLimit)))
})

const (
	maxObjectHint = 16 << 30
	exeHeader     = 8
	unpackLimit   = 1 << 30
)

type CompileOptions struct {
	memoryTail  bool
	tailRanges  bool
	tailProfile bool

	asan bool
}

func compileOptions() CompileOptions {
	options := CompileOptions{memoryTail: true, tailRanges: true}

	switch mode := os.Getenv("W2X_MEMORY_TAIL"); mode {
	case "", "1", "optimized":
	case "plain":
		options.tailRanges = false
	case "strict":
		options.memoryTail, options.tailRanges = false, false
	default:
		throwFmt("unknown memory tail mode %q", mode)
	}

	options.tailProfile = options.tailRanges && os.Getenv("W2X_TAIL_PROFILE") != ""

	return options
}

type CompilerImports struct{}

type CompilerIO struct {
	input      io.Reader
	output     func(p []byte) (int, error)
	diagnostic bytes.Buffer
}

func (CompilerImports) function(imp *ImportMeta) any {
	switch imp.name {
	case "fd_read":
		return compilerRead
	case "fd_write":
		return compilerWrite
	}

	return compilerExit
}

func compilerTransfer(fd, iovs, done uint32, write bool) uint32 {
	m := guest()
	host := current().host.(*CompilerIO)
	table := span(m, iovs, 8)
	at := binary.LittleEndian.Uint32(table)
	size := binary.LittleEndian.Uint32(table[4:])
	b := span(m, at, uint64(size))

	var n int
	var err error

	if write {
		out := host.output

		if fd == 2 {
			out = host.diagnostic.Write
		}

		if chaosFail("compile-write") {
			err = io.ErrShortWrite
		} else {
			n, err = out(b)
		}
	} else {
		if chaosFail("compile-read") {
			err = io.ErrUnexpectedEOF
		} else {
			n, err = host.input.Read(b)
		}
	}

	if err != nil && err != io.EOF {
		throw(err)
	}

	put32(m, done, uint32(n))

	return wasiOK
}

func compilerRead(fd, iovs, count, done uint32) uint32 {
	return compilerTransfer(fd, iovs, done, false)
}

func compilerWrite(fd, iovs, count, done uint32) uint32 {
	return compilerTransfer(fd, iovs, done, true)
}

func compilerExit(code uint32) {
	diagnostic := strings.TrimSpace(current().host.(*CompilerIO).diagnostic.String())

	throwFmt("aot exited with code %d: %s", code, diagnostic)
}

func compileStreams(input io.Reader, output func(p []byte) (int, error), options CompileOptions) {
	i := newInstance(compilerCode(), runStack)

	defer i.release()

	i.attach(&CompilerIO{input: input, output: output})
	i.start()
	setupCompilerChaos(i)

	flags := uint32(0)

	if options.memoryTail {
		flags |= 1
	}

	if options.tailRanges {
		flags |= 2
	}

	if options.tailProfile {
		flags |= 4
	}

	if options.asan {
		flags |= 8
	}

	if flags == 3 {
		entry, _ := i.export("compile")

		entry.Interface().(func(uintptr))(i.handle())
	} else {
		entry, _ := i.export("compileOptions")

		entry.Interface().(func(uintptr, uint32))(i.handle(), flags)
	}
}

type ObjectWriter struct {
	b []byte
}

func (w *ObjectWriter) write(p []byte) (int, error) {
	if w.b == nil {
		w.b = make([]byte, 0, objectSize(p))
	}

	w.b = append(w.b, p...)

	return len(p), nil
}

func objectSize(header []byte) int {
	if len(header) < 64 || !bytes.HasPrefix(header, []byte("\x7fELF\x02\x01")) {
		return 0
	}

	le := binary.LittleEndian
	size := le.Uint64(header[40:]) + uint64(le.Uint16(header[58:]))*uint64(le.Uint16(header[60:]))

	if size > maxObjectHint {
		return 0
	}

	return int(size)
}

func compileModuleWithOptions(data []byte, options CompileOptions) []byte {
	var out ObjectWriter
	compileStreams(bytes.NewReader(data), out.write, options)

	if len(out.b) != cap(out.b) {
		return bytes.Clone(out.b)
	}

	return out.b
}

func programOptions(asan bool) CompileOptions {
	if asan {
		return CompileOptions{asan: true}
	}

	return compileOptions()
}

func exeLength(header []byte, size int64) (int, error) {
	if len(header) < exeHeader || !bytes.HasPrefix(header, exeMagic) {
		return 0, nil
	}

	length := int64(binary.LittleEndian.Uint32(header[len(exeMagic):]))

	if length <= exeHeader || length > size {
		return 0, fmt.Errorf("PZD1: a length of %d in a file of %d bytes: %w", length, size, ENOEXEC)
	}

	return int(length), nil
}

func unpack(exe []byte) ([]byte, error) {
	module, err := unpacker().DecodeAll(exe[exeHeader:], nil)

	if err != nil {
		return nil, fmt.Errorf("zstd: %v: %w", err, ENOEXEC)
	}

	return module, nil
}

func compileExecutable(exe []byte, options CompileOptions) (*ElfObject, error) {
	module, err := unpack(exe)

	if err != nil {
		return nil, err
	}

	return compileModule(module, options)
}

func compileModule(data []byte, options CompileOptions) (obj *ElfObject, err error) {
	if !bytes.HasPrefix(data, wasmHeader) {
		return nil, ENOEXEC
	}

	if t := tryAs[*Trap](func() {
		if e := try(func() { obj = readObject(compileModuleWithOptions(data, options)) }); e != nil {
			err = fmt.Errorf("%s: %w", e, ENOEXEC)
		}
	}); t != nil {
		return nil, fmt.Errorf("the compiler: %s: %w", t.Error(), ENOEXEC)
	}

	return obj, err
}
