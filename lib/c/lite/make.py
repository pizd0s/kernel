#!/usr/bin/env python3
"""Build lib/c/lite's archive, the C library of a program the kernel runs
with its Go alone and no libc.wasm, the compiler: its entry, errno's
place, environ and getenv (crt.c), and the forwarders of the kernel's
functions it calls (kernel.c). It takes lib/c's headers and the start's
import of lib/c's src/kernel.h.

    make.py <cc> <ar> <libc.a>
"""
import os
import shutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
LIBC = ROOT.parent
sys.path.insert(0, str(LIBC.parent.parent / "dev"))

import wasm  # noqa: E402

FLAGS = ["-O2", "-std=c99", "-D_XOPEN_SOURCE=700", "-w", "-I", str(LIBC / "src"), "-isystem", str(LIBC / "include")]


def main():
    cc, ar, output = sys.argv[1:4]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libc-lite"
    shutil.rmtree(work, ignore_errors=True)
    units = [(source, work / source.with_suffix(".o").name, FLAGS) for source in sorted(ROOT.glob("*.c"))]

    wasm.compile_all(cc, units)
    wasm.archive(ar, output, [obj for source, obj, flags in units])


if __name__ == "__main__":
    main()
