package main

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

var (
	//go:embed aot.aot
	compilerBlob []byte
	//go:embed libc.wasm
	libcBlob   []byte
	wasmHeader = []byte("\x00asm\x01\x00\x00\x00")
	exeMagic   = []byte("PZD1")
)

var libcObject = sync.OnceValue(func() *WasmObject {
	return parseWasmObject(libcBlob)
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

func (o CompileOptions) arguments() []string {
	switch o {
	case CompileOptions{memoryTail: true, tailRanges: true}:
		return nil
	case CompileOptions{memoryTail: true}:
		return []string{"--plain"}
	case CompileOptions{memoryTail: true, tailRanges: true, tailProfile: true}:
		return []string{"--tail-profile"}
	case CompileOptions{}:
		return []string{"--strict"}
	case CompileOptions{asan: true}:
		return []string{"--asan"}
	}

	throwFmt("compile options %+v are no arguments of the compiler", o)

	return nil
}

func (k *Kernel) compileObject(module []byte, options CompileOptions) ([]byte, error) {
	module = linkLibc(module, k.functions)

	code := k.compiler()
	p := k.process(0, nil)
	out, diagnostic := newCapture(objectSize), newCapture(nil)

	p.files.put(0, newMemfd(&LinearMemory{data: module}, 0, int64(len(module)), O_RDONLY).desc, false)
	p.files.put(1, held(out), false)
	p.files.put(2, held(diagnostic), false)

	status := p.run(&ExecImage{code: code, argv: append([]string{"aot"}, options.arguments()...), envp: compilerEnv()})
	text := strings.TrimSpace(string(diagnostic.bytes()))
	failed := ""

	switch {
	case status&0x7f != 0:
		failed = fmt.Sprintf("aot killed by %s: %s", signalName(status&0x7f), text)
	case status != 0:
		failed = fmt.Sprintf("aot exited with code %d: %s", status>>8&0xff, text)
	}

	if failed != "" && p.starved {
		return nil, fmt.Errorf("%s: %w", failed, ENOMEM)
	}

	if failed != "" {
		return nil, errors.New(failed)
	}

	b := out.bytes()

	if len(b) != cap(b) {
		b = bytes.Clone(b)
	}

	return b, nil
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

func (k *Kernel) compileExecutable(exe []byte, options CompileOptions) (*ElfObject, error) {
	module, err := unpack(exe)

	if err != nil {
		return nil, err
	}

	return k.compileModule(module, options)
}

func (k *Kernel) compileModule(data []byte, options CompileOptions) (obj *ElfObject, err error) {
	if !bytes.HasPrefix(data, wasmHeader) {
		return nil, ENOEXEC
	}

	if e := try(func() {
		b, failed := k.compileObject(data, options)

		if failed != nil && !errors.Is(failed, ENOMEM) {
			failed = fmt.Errorf("%s: %w", failed, ENOEXEC)
		}

		if failed != nil {
			err = failed

			return
		}

		obj = readObject(b)
	}); e != nil {
		return nil, fmt.Errorf("%s: %w", e, ENOEXEC)
	}

	return obj, err
}
