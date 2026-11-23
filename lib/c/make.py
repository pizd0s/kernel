#!/usr/bin/env python3
"""Build lib/c's archive, what of the C library a program keeps: its
forwarders to the kernel (src/kernel.c), which is where every other function
of the headers is, the kernel's Go or its libc.wasm (lib/libc); its entry and
the variables POSIX has it define (src/crt.c, src/main.c); and the compiler's
runtime (src/builtins.c).

    make.py <cc> <ar> <libc.a>
    make.py headers <include.tar>

The second packs include/, the headers as a program built inside the system
finds them.
"""
import os
import shutil
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT.parent.parent / "dev"))

import wasm  # noqa: E402

FLAGS = ["-O2", "-std=c99", "-D_XOPEN_SOURCE=700", "-w", "-I", str(ROOT / "src"), "-isystem", str(ROOT / "include")]


def main():
    if sys.argv[1] == "headers":
        wasm.write_tar(sys.argv[2], ROOT, ROOT / "include")

        return

    cc, ar, output = sys.argv[1:4]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libc"
    shutil.rmtree(work, ignore_errors=True)
    units = [(source, work / source.with_suffix(".o").name, FLAGS) for source in sorted((ROOT / "src").glob("*.c"))]

    wasm.compile_all(cc, units)
    wasm.archive(ar, output, [obj for source, obj, flags in units])


if __name__ == "__main__":
    main()
