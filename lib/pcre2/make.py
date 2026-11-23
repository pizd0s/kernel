#!/usr/bin/env python3
"""Build lib/pcre2: PCRE2's 8-bit library of its release tarball, for lib/c.

    make.py <cc> <ar> <tarball> <libc-include> <libpcre2.a> <include.tar>

As PCRE2's NON-AUTOTOOLS-BUILD has it: its generic config.h and pcre2.h,
its default character tables, Unicode's support, no JIT. libpcre2.a is
the library of 8-bit code units, pcre2-8's; include.tar is include/,
pcre2.h, of which a program says the width it takes,
PCRE2_CODE_UNIT_WIDTH 8.
"""
import os
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "dev"))

import wasm  # noqa: E402

NAMES = """
    auto_possess chkdint compile compile_cgroup compile_class config context convert dfa_match error
    extuni find_bracket jit_compile maketables match match_data match_next newline ord2utf
    pattern_info script_run serialize string_utils study substitute substring tables ucd valid_utf
    xclass
""".split()
FLAGS = ["-O2", "-w", "-DHAVE_CONFIG_H", "-DPCRE2_CODE_UNIT_WIDTH=8", "-DSUPPORT_UNICODE", "-DPCRE2_STATIC", "-DHAVE_MEMMOVE=1",
         "-DHAVE_STDINT_H=1", "-DHAVE_INTTYPES_H=1", "-DHAVE_LIMITS_H=1", "-DHAVE_STRING_H=1", "-DHAVE_STDLIB_H=1"]


def main():
    cc, ar, tarball, libc_include, library, include_tar = sys.argv[1:7]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libpcre2"
    shutil.rmtree(work, ignore_errors=True)
    tree = wasm.unpack(tarball, work / "src")
    src = tree / "src"
    shutil.copy(src / "pcre2.h.generic", src / "pcre2.h")
    shutil.copy(src / "config.h.generic", src / "config.h")
    shutil.copy(src / "pcre2_chartables.c.dist", src / "pcre2_chartables.c")
    sources = ["src/pcre2_chartables.c", *(f"src/pcre2_{name}.c" for name in NAMES)]
    wasm.library(cc, ar, tree, sources, FLAGS + ["-I", "src", "-isystem", str(Path(libc_include).resolve())], library, work)
    wasm.headers(include_tar, work / "out" / "include", [(src / "pcre2.h", "pcre2.h")])


if __name__ == "__main__":
    main()
