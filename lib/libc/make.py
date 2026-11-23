#!/usr/bin/env python3
"""Build libc.wasm: kernel functions written in C. A source of this
directory defines __kernel_X, the kernel's function X as a program imports
it, by lib/c's declaration of it in lib/c/src/kernel.c; the kernel links
those a program imports, and what they reach, into the program before it
compiles it (bin/vm/wasm_linker.go), and the rest of its functions are its
Go. libc.wasm is a relocatable object (wasm-ld -r) of these sources and of
lib/c's forwarders to the kernel, kernel.c, which they call the kernel
through as a program does; libc.h is what they include first.

    make.py <cc> <wasm-ld> <libc.wasm>
"""
import os
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
LIBC = ROOT.parent / "c"
sys.path.insert(0, str(ROOT.parent.parent / "dev"))

import wasm  # noqa: E402

FORWARDER_FLAGS = ["-O2", "-std=c99", "-D_XOPEN_SOURCE=700", "-w", "-I", str(LIBC / "src"), "-isystem", str(LIBC / "include")]
FLAGS = ["-O2", "-std=c11", "-D_GNU_SOURCE", "-Wall", "-Werror", "-isystem", str(LIBC / "include")]


def main():
    cc, ld, output = sys.argv[1:4]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libc.wasm"
    shutil.rmtree(work, ignore_errors=True)
    units = [(LIBC / "src" / "kernel.c", work / "kernel.o", FORWARDER_FLAGS)]

    for source in sorted(ROOT.glob("*.c")):
        units.append((source, work / source.with_suffix(".o").name, FLAGS))

    wasm.compile_all(cc, units)
    Path(output).parent.mkdir(parents=True, exist_ok=True)
    subprocess.run([ld, "-r", "-o", output, *(str(obj) for source, obj, flags in units)], check=True)


if __name__ == "__main__":
    main()
