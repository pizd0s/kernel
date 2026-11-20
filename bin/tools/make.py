#!/usr/bin/env python3
"""Build a program of the system for the kernel from its sources: clang
compiles each for wasm32 against lib/c's headers, with no C++ library and
the source root on the include path (<lib/protocol/service.h>), and
wasm-ld links them with libc.a. bin/tools is one such program, the tools'
multicall executable.

    make.py <clang> <wasm-ld> <libc-include> <libc.a> <output.wasm> <source.cpp>...
"""
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent.parent
sys.path.insert(0, str(ROOT / "dev"))

import wasm  # noqa: E402

CXXFLAGS = ["-O2", "-std=c++23", "-fno-exceptions", "-fno-rtti", "-nostdinc++", "-Wall", "-Wextra", "-Werror"]

LINK = ["--stack-first", "--max-memory=4294967296", "--export=__heap_base",
        "--export=__stack_pointer", "--gc-sections"]


def main():
    if len(sys.argv) < 7:
        raise SystemExit(__doc__)
    cc, ld, include, libc, output = sys.argv[1:6]
    sources = [Path(source) for source in sys.argv[6:]]
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory(dir=output.parent, prefix=output.stem + "-") as work:
        work = Path(work)
        flags = [*CXXFLAGS, "-I", str(ROOT), "-isystem", str(Path(include).resolve())]
        units = [(source, work / f"{i}-{source.stem}.o", flags) for i, source in enumerate(sources)]
        wasm.compile_all(cc, units)
        subprocess.run([ld, *LINK, "-o", str(output), *(str(o) for _, o, _ in units), libc], check=True)


if __name__ == "__main__":
    main()
