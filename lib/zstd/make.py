#!/usr/bin/env python3
"""Build lib/zstd: zstd's library of its release tarball, for lib/c.

    make.py <cc> <ar> <tarball> <libc-include> <libzstd.a> <include.tar>

libzstd.a is lib's common, compress, decompress and dictBuilder, as zstd's
static library is built: one thread, no assembly, no legacy formats, its
xxHash in its own namespace. include.tar is include/, zstd.h, zdict.h and
zstd_errors.h.
"""
import os
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "dev"))

import wasm  # noqa: E402

PARTS = ["common", "compress", "decompress", "dictBuilder"]
FLAGS = ["-O2", "-w", "-DZSTD_DISABLE_ASM", "-DZSTD_LEGACY_SUPPORT=0", "-DXXH_NAMESPACE=ZSTD_", "-DDEBUGLEVEL=0"]


def main():
    cc, ar, tarball, libc_include, library, include_tar = sys.argv[1:7]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libzstd"
    shutil.rmtree(work, ignore_errors=True)
    tree = wasm.unpack(tarball, work / "src")
    sources = [str(path.relative_to(tree)) for part in PARTS for path in sorted((tree / "lib" / part).glob("*.c"))]
    flags = FLAGS + ["-I", "lib", "-I", "lib/common", "-isystem", str(Path(libc_include).resolve())]
    wasm.library(cc, ar, tree, sources, flags, library, work)
    wasm.headers(include_tar, work / "out" / "include", [(tree / "lib" / name, name) for name in ("zstd.h", "zdict.h", "zstd_errors.h")])


if __name__ == "__main__":
    main()
