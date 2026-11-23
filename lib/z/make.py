#!/usr/bin/env python3
"""Build lib/z: zlib of its release tarball, for lib/c.

    make.py <cc> <ar> <tarball> <libc-include> <libz.a> <include.tar>

libz.a is the library of zlib's Makefile, OBJC; include.tar is include/,
zlib.h and zconf.h, which says, as zlib's configure makes it say, that
lib/c has unistd.h and stdarg.h: z_off_t is off_t.
"""
import os
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "dev"))

import wasm  # noqa: E402

SOURCES = [f"{name}.c" for name in ("adler32", "crc32", "deflate", "infback", "inffast", "inflate", "inftrees", "trees",
                                     "zutil", "compress", "uncompr", "gzclose", "gzlib", "gzread", "gzwrite")]
FLAGS = ["-O2", "-w"]


def main():
    cc, ar, tarball, libc_include, library, include_tar = sys.argv[1:7]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libz"
    shutil.rmtree(work, ignore_errors=True)
    tree = wasm.unpack(tarball, work / "src")
    zconf = tree / "zconf.h"
    text = zconf.read_text()

    for name in ("HAVE_UNISTD_H", "HAVE_STDARG_H"):
        old = f"#if {name}-0"

        if text.count(old) != 1:
            raise SystemExit(f"zconf.h has no {old}")

        text = text.replace(old, "#if 1")

    zconf.write_text(text)
    wasm.library(cc, ar, tree, SOURCES, FLAGS + ["-isystem", str(Path(libc_include).resolve())], library, work)
    wasm.headers(include_tar, work / "out" / "include", [(tree / name, name) for name in ("zlib.h", "zconf.h")])


if __name__ == "__main__":
    main()
