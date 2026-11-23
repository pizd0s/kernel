#!/usr/bin/env python3
"""Build lib/sqlite3: SQLite of its release tarball, the amalgamation, for
lib/c.

    make.py <cc> <ar> <tarball> <libc-include> <libsqlite3.a> <include.tar>

libsqlite3.a is sqlite3.c of one thread, as the system's programs have,
with no extensions loaded, as there is no dynamic loading, and pread and
pwrite of POSIX's. include.tar is include/, sqlite3.h and sqlite3ext.h.
"""
import os
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "dev"))

import wasm  # noqa: E402

FLAGS = ["-O2", "-w", "-DSQLITE_THREADSAFE=0", "-DSQLITE_OMIT_LOAD_EXTENSION=1", "-DHAVE_PREAD=1", "-DHAVE_PWRITE=1",
         "-DHAVE_USLEEP=1", "-DHAVE_LOCALTIME_R=1", "-DHAVE_GMTIME_R=1", "-DHAVE_FDATASYNC=1", "-DHAVE_STRERROR_R=1"]


def main():
    cc, ar, tarball, libc_include, library, include_tar = sys.argv[1:7]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libsqlite3"
    shutil.rmtree(work, ignore_errors=True)
    tree = wasm.unpack(tarball, work / "src")
    wasm.library(cc, ar, tree, ["sqlite3.c"], FLAGS + ["-isystem", str(Path(libc_include).resolve())], library, work)
    wasm.headers(include_tar, work / "out" / "include", [(tree / name, name) for name in ("sqlite3.h", "sqlite3ext.h")])


if __name__ == "__main__":
    main()
