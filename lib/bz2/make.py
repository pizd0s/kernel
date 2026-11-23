#!/usr/bin/env python3
"""Build lib/bz2: bzip2's library of its release tarball, for lib/c.

    make.py <cc> <ar> <tarball> <libc-include> <libbz2.a> <include.tar>

libbz2.a is the library of bzip2's Makefile, OBJS; include.tar is
include/, bzlib.h.
"""
import os
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "dev"))

import wasm  # noqa: E402

SOURCES = [f"{name}.c" for name in ("blocksort", "huffman", "crctable", "randtable", "compress", "decompress", "bzlib")]
FLAGS = ["-O2", "-w", "-D_FILE_OFFSET_BITS=64"]


def main():
    cc, ar, tarball, libc_include, library, include_tar = sys.argv[1:7]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libbz2"
    shutil.rmtree(work, ignore_errors=True)
    tree = wasm.unpack(tarball, work / "src")
    wasm.library(cc, ar, tree, SOURCES, FLAGS + ["-isystem", str(Path(libc_include).resolve())], library, work)
    wasm.headers(include_tar, work / "out" / "include", [(tree / "bzlib.h", "bzlib.h")])


if __name__ == "__main__":
    main()
