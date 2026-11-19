//go:build linux && amd64

package main

import (
	"os"
	"slices"
	"strings"
	"syscall"
)

const imageDisk = "image"

func runModule(path string, argv []string, asan bool, disks []Disk, user Cred) int {
	data, exe, tail, err := hostProgram(path)

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

	if err == ENOEXEC {
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

	given := slices.ContainsFunc(disks, func(d Disk) bool { return d.name == imageDisk })

	if kernel && len(tail) > 0 && !given {
		disks = append([]Disk{{name: imageDisk, mem: tail}}, disks...)
	}

	return runKernel(path, argv, options, data, obj, disks, user)
}

func hostProgram(path string) (data []byte, exe bool, tail []byte, err error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)

	if err != nil {
		return nil, false, nil, err
	}

	defer syscall.Close(fd)

	var st syscall.Stat_t

	if err := syscall.Fstat(fd, &st); err != nil {
		return nil, false, nil, err
	}

	data, exe, err = readProgram(st.Size, func(b []byte, off int64) error {
		for at := 0; at < len(b); {
			n, err := syscall.Pread(fd, b[at:], off+int64(at))

			if err == syscall.EINTR {
				continue
			}

			if err != nil {
				return err
			}

			if n == 0 {
				return ENOEXEC
			}

			at += n
		}

		return nil
	}, true)

	if err != nil || !exe || st.Size == int64(len(data)) {
		return data, exe, nil, err
	}

	from := int64(len(data)) &^ int64(syscall.Getpagesize()-1)
	pages, err := syscall.Mmap(fd, from, int(st.Size-from), syscall.PROT_READ, syscall.MAP_PRIVATE)

	if err != nil {
		return nil, false, nil, err
	}

	return data, exe, pages[int64(len(data))-from:], nil
}

func importsKernel(obj *ElfObject) bool {
	for _, imp := range obj.meta.imports {
		if imp.module == "kernel" {
			return true
		}
	}

	return false
}

func runKernel(path string, argv []string, options CompileOptions, data []byte, obj *ElfObject, disks []Disk, user Cred) int {
	k := newKernel(options, user)
	p := throw2(k.newProcess(nil))
	code, err := k.codeOf(p, data, path, obj)

	throw(err)

	cwd, pwd := p.firstCwd()

	p.setCwd(cwd)

	env := []string{"PWD=" + pwd}

	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "PWD=") {
			env = append(env, e)
		}
	}

	for fd, f := range k.newConsole() {
		if f != nil {
			p.files.put(fd, f, false)
		}
	}

	if len(disks) > 0 {
		names := make([]string, len(disks))

		for i, d := range disks {
			names[i] = d.name

			if d.mem != nil {
				p.files.put(max(p.files.size(), 3), newMemfd(nil, d.mem, O_RDONLY), false)

				continue
			}

			fd, err := syscall.Open(d.path, syscall.O_RDWR|syscall.O_CLOEXEC, 0)

			if err != nil {
				throwFmt("--disk %s: %v", d.path, err)
			}

			p.files.put(max(p.files.size(), 3), newDiskFile(fd), false)
		}

		env = append(env, "DISKS="+strings.Join(names, " "))
	}

	k.first = p
	k.fg = p.pgid

	go p.run(&ExecImage{code: code, argv: argv, envp: env})

	status := <-k.exit

	k.console.flush()

	return status
}
