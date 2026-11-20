#!/usr/bin/env python3
"""Build rootfs for the kernel: clang compiles rootfs.cpp and the protocol's
sources for wasm32 against lib/c's headers, with no C++ library and the
source root on the include path, and wasm-ld links them with libc.a.

    make.py <clang> <wasm-ld> <libc-include> <libc.a> <rootfs.wasm> <source.cpp>...

The module is every system's: a system is its executable with the
system's image (bin/rootfs/image.py) after it, in one file.
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
    cc, ld, include, libc, output = sys.argv[1:6]
    sources = [Path(source) for source in sys.argv[6:]]
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory(dir=output.parent, prefix="rootfs-") as work:
        work = Path(work)
        flags = [*CXXFLAGS, "-I", str(ROOT), "-isystem", str(Path(include).resolve())]
        units = [(source, work / f"{i}-{source.stem}.o", flags) for i, source in enumerate(sources)]
        wasm.compile_all(cc, units)
        subprocess.run([ld, *LINK, "-o", str(output), *(str(o) for _, o, _ in units), libc], check=True)


if __name__ == "__main__":
    main()
