#!/usr/bin/env python3
"""Build lib/c's in-process archive: our own sources, the musl subset in musl/files.txt
and the FreeBSD msun sources in freebsd/.

    make.py <cc> <ar> <musl-tarball> <libc.a>
    make.py headers <include.tar>

The second packs include/, the headers as a program built inside the system
finds them.

A .h line of files.txt names a musl internal header to keep; every other
header of src/internal is deleted, so a quoted include of it falls through to
our replacement in musl/. musl's public headers are never on the path.

freebsd/ holds copies of FreeBSD 14.3's lib/msun/src/catrig.c, catrigf.c,
s_clog.c and s_clogf.c, unchanged: complex.h's inverse functions and clog,
which musl's follow neither Annex G at its special values nor accuracy near
|z| = 1. Its math_private.h and fpmath.h are ours, what those take of
FreeBSD's.
"""
import os
import shutil
import sys
import tarfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT.parent.parent / "dev"))

import wasm  # noqa: E402

FLAGS = ["-O2", "-std=c99", "-D_XOPEN_SOURCE=700", "-w"]


def main():
    if sys.argv[1] == "headers":
        wasm.write_tar(sys.argv[2], ROOT, ROOT / "include")

        return

    cc, ar, tarball, output = sys.argv[1:5]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libc"
    shutil.rmtree(work, ignore_errors=True)
    work.mkdir(parents=True)

    with tarfile.open(tarball) as archive:
        archive.extractall(work, filter="data")

    tree = next(work.glob("musl-*"))
    musl = tree / "src"
    lines = (ROOT / "musl" / "files.txt").read_text().split()
    kept = {musl / line for line in lines if line.endswith(".h")}

    for header in (musl / "internal").glob("*.h"):
        if header not in kept:
            header.unlink()

    includes = [
        "-include", str(ROOT / "musl" / "features.h"),
        "-I", str(ROOT / "src"),
        "-I", str(ROOT / "musl"),
        "-I", str(musl / "internal"),
        "-I", str(tree / "arch" / "generic"),
        "-isystem", str(ROOT / "include"),
    ]
    units = []

    for line in lines:
        if not line.endswith(".h"):
            units.append((musl / line, work / "obj" / Path(line).with_suffix(".o"), FLAGS + includes))

    for source in sorted((ROOT / "src").glob("*.c")):
        units.append((source, work / "obj" / "pizdos" / source.with_suffix(".o").name, FLAGS + includes))

    for source in sorted((ROOT / "freebsd").glob("*.c")):
        units.append((source, work / "obj" / "freebsd" / source.with_suffix(".o").name,
                      FLAGS + ["-I", str(ROOT / "freebsd")] + includes))

    wasm.compile_all(cc, units)
    wasm.archive(ar, output, [obj for source, obj, flags in units])


if __name__ == "__main__":
    main()
