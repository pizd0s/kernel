//go:build linux && amd64

package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
)

// runModule compiles a module and runs it, and returns the exit code of
// vm: a program of the kernel, from its executable (PZD1), as its first
// process, or a WASI command, an executable or a module, on this
// goroutine, whose code is proc_exit's, or 128 and the trap code after a
// trap. A module is all vm runs: no object of vm's.
func runModule(path string, argv []string, asan bool, disks []disk, user cred) (code int) {
	data, exe, err := hostProgram(path)

	if err != nil {
		throwFmt("%s: %v", path, err)
	}

	options := programOptions(asan)
	var obj *Object

	if exe {
		obj, err = compileExecutable(data, options)
	} else {
		obj, err = compileModule(data, options)
	}

	if err == syscall.ENOEXEC {
		throwFmt("%s: not a WebAssembly module", path)
	}

	if err != nil {
		throwFmt("%s: %v", path, err)
	}

	if importsKernel(obj) {
		if !exe {
			throwFmt("%s: a program of the kernel, not in an executable of PZD1", path)
		}

		return runKernel(path, argv, options, data, obj, disks, user)
	}

	if asan {
		throwFmt("--asan runs programs of the kernel")
	}

	if len(disks) > 0 {
		throwFmt("--disk runs programs of the kernel")
	}

	w := newWASI(argv)
	i := instantiate(argv[0], obj, w, runStack)
	i.attach(w)

	defer i.writeTailProfile()

	defer func() {
		switch e := recover().(type) {
		case nil:
		case *wasiExit:
			code = int(e.code)
		case *Trap:
			fmt.Fprintln(os.Stderr, e.Error())

			for _, line := range traceLines(e.stack) {
				fmt.Fprintln(os.Stderr, "    "+line)
			}

			code = 128 + int(e.code)
		default:
			panic(e)
		}

		runtime.KeepAlive(i)
	}()

	i.start()
	i.startCommand()

	return 0
}

// hostProgram reads the program vm runs, a file of the host, as exec reads
// one (readProgram): fstat and pread are all vm's calls on it.
func hostProgram(path string) ([]byte, bool, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)

	if err != nil {
		return nil, false, err
	}

	defer syscall.Close(fd)

	var st syscall.Stat_t

	if err := syscall.Fstat(fd, &st); err != nil {
		return nil, false, err
	}

	return readProgram(st.Size, func(b []byte, off int64) error {
		for at := 0; at < len(b); {
			n, err := syscall.Pread(fd, b[at:], off+int64(at))

			if err == syscall.EINTR {
				continue
			}

			if err != nil {
				return err
			}

			if n == 0 {
				return syscall.ENOEXEC
			}

			at += n
		}

		return nil
	}, true)
}
