//go:build linux && amd64

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
)

func usage() {
	throwFmt("usage: vm [-v] [--asan] -o output.o input.wasm\n       vm run [-a argv0] [--asan] [--user uid[:gid]] [--disk name=file]... -- module.wasm [arg...]\n       vm spec test.json\n       vm stand [--repeat N] im|pdf|djvu|svg|magic object.o file...")
}

func main() {
	code := 0

	if e := try(func() {
		setupVmChaos()

		args := os.Args[1:]

		if len(args) > 0 && args[0] == "run" {
			code = runCommand(args[1:])

			return
		}

		if len(args) > 0 && args[0] == "spec" {
			code = specCommand(args[1:])

			return
		}

		if len(args) > 0 && args[0] == "stand" {
			code = standCommand(args[1:])

			return
		}

		compileCommand(args)
	}); e != nil {
		restoreHostTerminal()
		fmt.Fprintln(os.Stderr, "vm:", e)
		os.Exit(1)
	}

	restoreHostTerminal()
	os.Exit(code)
}

func compileCommand(args []string) {
	var input, output string

	verbose, asan := false, false

	for len(args) > 0 {
		arg := args[0]

		args = args[1:]

		switch arg {
		case "-o":
			if len(args) == 0 {
				usage()
			}

			output = args[0]
			args = args[1:]
		case "-v":
			verbose = true
		case "--asan":
			asan = true
		default:
			if input != "" {
				usage()
			}

			input = arg
		}
	}

	if input == "" || output == "" {
		usage()
	}

	source := throw2(os.ReadFile(input))
	destination := throw2(os.CreateTemp(filepath.Dir(output), ".vm-*"))

	defer destination.Close()

	defer os.Remove(destination.Name())

	if path := os.Getenv("W2X_PROFILE"); path != "" {
		f := throw2(os.Create(path))

		throw(pprof.StartCPUProfile(f))

		defer pprof.StopCPUProfile()
	}

	options := programOptions(asan)
	start := time.Now()
	object := throw2(newKernel(Cred{}).compileObject(source, options))
	elapsed := time.Since(start)

	throw2(destination.Write(object))
	throw(destination.Chmod(0o644))
	throw(destination.Close())
	throw(os.Rename(destination.Name(), output))

	if verbose {
		fmt.Fprintf(os.Stderr, "vm: %d bytes -> %d bytes in %s\n", len(source), len(object), elapsed)
	}
}

func runCommand(args []string) int {
	argv0, asan := "", false

	var disks []Disk
	var user Cred

	for len(args) > 0 && args[0] != "--" {
		switch {
		case args[0] == "--asan":
			asan = true
			args = args[1:]
		case len(args) > 1 && args[0] == "--user":
			uid, gid, ok := parseUser(args[1])

			if !ok {
				usage()
			}

			user = Cred{uids: [3]uint32{uid, uid, uid}, gids: [3]uint32{gid, gid, gid}}
			args = args[2:]
		case len(args) > 1 && args[0] == "-a":
			argv0 = args[1]
			args = args[2:]
		case len(args) > 1 && args[0] == "--disk":
			name, path, ok := strings.Cut(args[1], "=")

			if !ok || name == "" || path == "" || strings.ContainsAny(name, "/ ") {
				usage()
			}

			disks = append(disks, Disk{name: name, path: path})
			args = args[2:]
		default:
			usage()
		}
	}

	if len(args) < 2 {
		usage()
	}

	args = args[1:]

	if argv0 == "" {
		argv0 = args[0]
	}

	argv := append([]string{argv0}, args[1:]...)

	return runModule(args[0], argv, asan, disks, user)
}

func parseUser(text string) (uid, gid uint32, ok bool) {
	u, g, both := strings.Cut(text, ":")

	if !both {
		g = u
	}

	a, errA := strconv.ParseUint(u, 10, 32)
	b, errB := strconv.ParseUint(g, 10, 32)

	if errA != nil || errB != nil || a == uint64(keep) || b == uint64(keep) {
		return 0, 0, false
	}

	return uint32(a), uint32(b), true
}
