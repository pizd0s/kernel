package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

func usage() {
	throwFmt("usage: wazero [-interpreter] [-compile] [-reserve] [-aot] module.wasm [args...]")
}

func compileAot(ctx context.Context, runtime wazero.Runtime, compiled wazero.CompiledModule, args []string, repeat int) {
	input := throw2(io.ReadAll(os.Stdin))
	reader := bytes.NewReader(input)

	var output bytes.Buffer

	config := moduleConfig(args).WithStartFunctions().WithStdin(reader).WithStdout(&output)
	module := throw2(runtime.InstantiateModule(ctx, compiled, config))

	var expected []byte

	for i := 0; i < repeat; i++ {
		throw2(reader.Seek(0, io.SeekStart))
		output.Reset()
		throw2(module.ExportedFunction("compile").Call(ctx))

		if i == 0 {
			expected = append([]byte(nil), output.Bytes()...)
		} else if !bytes.Equal(expected, output.Bytes()) {
			throwFmt("repeated compilation changed the object")
		}
	}

	pages, _ := module.Memory().Grow(0)

	fmt.Fprintf(os.Stderr, "wazero: aot memory %.3f MiB\n", float64(pages)/16)
	throw2(os.Stdout.Write(expected))
}

func report(compile time.Duration, run time.Duration) {
	fmt.Fprintf(os.Stderr, "wazero: compile %.3f run %.3f\n", compile.Seconds(), run.Seconds())
}

func exitCode(err error) int {
	var exit *sys.ExitError

	if errors.As(err, &exit) {
		return int(exit.ExitCode())
	}

	throw(err)

	return 0
}

func moduleConfig(args []string) wazero.ModuleConfig {
	config := wazero.NewModuleConfig().
		WithArgs(args...).
		WithStdin(os.Stdin).
		WithStdout(os.Stdout).
		WithStderr(os.Stderr).
		WithFSConfig(wazero.NewFSConfig().WithDirMount("/", "/")).
		WithSysNanotime().
		WithSysWalltime().
		WithSysNanosleep().
		WithRandSource(rand.Reader).
		WithEnv("PWD", throw2(os.Getwd()))

	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")

		if name != "PWD" {
			config = config.WithEnv(name, value)
		}
	}

	return config
}

func run(args []string) int {
	interpreter, compileOnly, reserve, aot := false, false, false, false

	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "-interpreter":
			interpreter = true
		case "-compile":
			compileOnly = true
		case "-reserve":
			reserve = true
		case "-aot":
			aot = true
		default:
			usage()
		}

		args = args[1:]
	}

	if len(args) == 0 {
		usage()
	}

	ctx := context.Background()
	config := wazero.NewRuntimeConfigCompiler()

	if interpreter {
		config = wazero.NewRuntimeConfigInterpreter()
	}

	runtime := wazero.NewRuntimeWithConfig(ctx, config.WithMemoryCapacityFromMax(reserve))
	wasm := throw2(os.ReadFile(args[0]))
	start := time.Now()
	compiled := throw2(runtime.CompileModule(ctx, wasm))
	compile := time.Since(start)

	if compileOnly {
		report(compile, 0)

		return 0
	}

	wasi_snapshot_preview1.MustInstantiate(ctx, runtime)
	start = time.Now()
	var err error

	if aot {
		repeat := 1

		if len(args) > 1 {
			repeat = throw2(strconv.Atoi(args[1]))
		}

		if repeat < 1 {
			throwFmt("aot repeat must be positive")
		}

		compileAot(ctx, runtime, compiled, args, repeat)
	} else {
		_, err = runtime.InstantiateModule(ctx, compiled, moduleConfig(args))
	}

	elapsed := time.Since(start)
	code := 0

	if err != nil {
		code = exitCode(err)
	}

	report(compile, elapsed)

	return code
}

func main() {
	code := 0

	if e := try(func() {
		code = run(os.Args[1:])
	}); e != nil {
		fmt.Fprintln(os.Stderr, "wazero:", e)
		os.Exit(1)
	}

	os.Exit(code)
}
