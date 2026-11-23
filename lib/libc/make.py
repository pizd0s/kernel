#!/usr/bin/env python3
"""Build libc.wasm: kernel functions written in C. A source of this
directory defines __kernel_X, the kernel's function X as a program imports
it, by lib/c's declaration of it in lib/c/src/kernel.c; the kernel links
those a program imports, and what they reach, into the program before it
compiles it (bin/vm/wasm_linker.go), and the rest of its functions are its
Go. libc.wasm is a relocatable object (wasm-ld -r) of these sources, of
lib/c's forwarders to the kernel, kernel.c, which they call the kernel
through as a program does, and of FreeBSD's code they stand on: its regex,
built as its libregex is, with the multibyte functions it calls over lib/c's
one locale of them (lib/c/freebsd/xlocale.c). The forwarders are weak, so
FreeBSD's function of a forwarder's name is the one libc.wasm calls.
FreeBSD's sources are lib/c's slice of them with lib/c/freebsd/patches and
then patches/ applied; freebsd.h is what FreeBSD's include first.

    make.py <cc> <wasm-ld> <freebsd-slice> <libc.wasm>
"""
import os
import shutil
import subprocess
import sys
import tarfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent
LIBC = ROOT.parent / "c"
sys.path.insert(0, str(ROOT.parent.parent / "dev"))

import wasm  # noqa: E402

FORWARDER_FLAGS = ["-O2", "-std=c99", "-D_XOPEN_SOURCE=700", "-w", "-I", str(LIBC / "src"), "-isystem", str(LIBC / "include")]
FLAGS = ["-O2", "-std=c11", "-D_GNU_SOURCE", "-Wall", "-Werror", "-isystem", str(LIBC / "include")]
FREEBSD = ["lib/libc/regex/regcomp.c", "lib/libc/regex/regexec.c", "lib/libc/regex/regerror.c", "lib/libc/regex/regfree.c",
           "lib/libc/locale/mbrtowc.c", "lib/libc/locale/wcrtomb.c"]


def main():
    cc, ld, freebsd_slice, output = sys.argv[1:5]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libc.wasm"
    shutil.rmtree(work, ignore_errors=True)
    tree = work / "freebsd"
    tree.mkdir(parents=True)

    with tarfile.open(freebsd_slice) as archive:
        archive.extractall(tree, filter="data")

    for change in [*sorted((LIBC / "freebsd" / "patches").glob("*.patch")), *sorted((ROOT / "patches").glob("*.patch"))]:
        subprocess.run(["patch", "-p1", "--batch", "--forward", "-s", "-d", str(tree), "-i", str(change)], check=True)

    libc = tree / "lib" / "libc"
    bsd = ["-O2", "-std=gnu11", "-w", "-include", str(LIBC / "freebsd" / "sys" / "cdefs.h"), "-include", str(ROOT / "freebsd.h"),
           "-I", str(LIBC / "freebsd"), "-I", str(libc / "locale"), "-I", str(libc / "stdio"), "-I", str(tree / "include"),
           "-isystem", str(LIBC / "include"), "-DLIBREGEX"]
    objects = work / "obj"
    units = [(LIBC / "src" / "kernel.c", objects / "kernel.o", FORWARDER_FLAGS),
             (LIBC / "freebsd" / "xlocale.c", objects / "xlocale.o", bsd)]

    for path in FREEBSD:
        units.append((tree / path, objects / Path(path).with_suffix(".o").name, bsd))

    for source in sorted(ROOT.glob("*.c")):
        units.append((source, objects / ("libc-" + source.with_suffix(".o").name), FLAGS))

    wasm.compile_all(cc, units)
    Path(output).parent.mkdir(parents=True, exist_ok=True)
    subprocess.run([ld, "-r", "-o", output, *(str(obj) for source, obj, flags in units)], check=True)


if __name__ == "__main__":
    main()
