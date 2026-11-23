#!/usr/bin/env python3
"""Build lib/slang: S-Lang's terminal and screen of its release tarball,
for lib/c.

    make.py <cc> <ar> <tarball> <libc-include> <terminfo package> <libslang.a> <include.tar>

Of S-Lang only the terminal, the keyboard and the screen are built, and
what of the library they use: the part a program of a screen's, as mc,
calls, with S-Lang's own reader of the terminfo database, which looks in
the terminfo package's share/terminfo first. config.h is ours
(slang-config.h). include.tar is include/, slang.h.
"""
import os
import shutil
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent.parent / "dev"))

import pkg  # noqa: E402
import wasm  # noqa: E402

NAMES = "slcommon sldisply slerr slgetkey sllower slmisc slsignal slsmg slstring sltermin slupper slutf8 slutty slwcwidth".split()
FLAGS = ["-O2", "-w", "-DHAVE_CONFIG_H", "-DSLANG"]


def main():
    cc, ar, tarball, libc_include, terminfo_package, library, include_tar = sys.argv[1:8]
    terminfo = pkg.root(terminfo_package) + "/share/terminfo"
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libslang"
    shutil.rmtree(work, ignore_errors=True)
    tree = wasm.unpack(tarball, work / "src")
    src = tree / "src"
    shutil.copy(HERE / "slang-config.h", src / "config.h")
    shutil.copy(src / "terminfo" / "default.inc", src / "terminfo.inc")
    flags = FLAGS + [f'-DMISC_TERMINFO_DIRS="{terminfo}"', "-I", "src", "-isystem", str(Path(libc_include).resolve())]
    wasm.library(cc, ar, tree, [f"src/{name}.c" for name in NAMES], flags, library, work)
    wasm.headers(include_tar, work / "out" / "include", [(src / "slang.h", "slang.h")])


if __name__ == "__main__":
    main()
