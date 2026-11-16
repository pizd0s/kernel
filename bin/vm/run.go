//go:build linux && amd64

package main

import (
	"syscall"
)

// runModule compiles a module and runs it as the first process, and
// returns the exit code of vm: a program of the kernel, from its
// executable (PZD1), or a WASI command, from an executable or a module,
// which WASI's personality of the kernel runs (kernel_wasi.go). A module
// is all vm runs: no object of vm's. --asan checks a program of the
// kernel's heap, which a WASI command has none of.
func runModule(path string, argv []string, asan bool, disks []disk, user cred) int {
	data, exe, err := hostProgram(path)

	if err != nil {
		throwFmt("%s: %v", path, err)
	}

	options := programOptions(asan)
	var obj *ElfObject

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

	kernel := importsKernel(obj)

	if kernel && !exe {
		throwFmt("%s: a program of the kernel, not in an executable of PZD1", path)
	}

	if asan && !kernel {
		throwFmt("--asan runs programs of the kernel")
	}

	return runKernel(path, argv, options, data, obj, disks, user)
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
