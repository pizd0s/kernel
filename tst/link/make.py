#!/usr/bin/env python3
"""Build test_libc_link's program: check.c, with -g, and functions of its
own besides, in a source of their own, as many as make the module's count
of functions 127, one short of the count a byte longer.

    make.py <cc> <wasm-ld> <check.c> <libc-include> <libc.a> <check.wasm>
"""
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "dev"))

import wasm  # noqa: E402

LINK = ["--stack-first", "--max-memory=4294967296", "--export=__heap_base", "--export=__stack_pointer", "--gc-sections"]
COUNT = 127


def leb(data, at):
    value = shift = 0

    while True:
        byte = data[at]
        at += 1
        value |= (byte & 0x7f) << shift
        shift += 7

        if byte < 0x80:
            return value, at


def functions(data):
    """The count of a module's functions of its own, its code section's."""
    at = 8

    while at < len(data):
        kind = data[at]
        size, at = leb(data, at + 1)

        if kind == 10:
            return leb(data, at)[0]

        at += size

    return 0


def build(cc, ld, source, include, libc, output, work, extra):
    table = work / "functions.c"
    lines = [f"static int f{i}(int x) {{ return x * {i + 3} + 1; }}" for i in range(extra)]
    lines.append("int (*const functions[])(int) = {" + ", ".join([f"f{i}" for i in range(extra)] or ["0"]) + "};")
    lines.append(f"const int count = {extra};")
    table.write_text("\n".join(lines) + "\n")
    flags = ["-O2", "-g", "-std=c11", "-isystem", str(Path(include).resolve())]
    wasm.compile_all(cc, [(Path(source), work / "check.o", flags), (table, work / "functions.o", flags)])
    subprocess.run([ld, *LINK, "-o", output, str(work / "check.o"), str(work / "functions.o"), libc], check=True)

    return functions(Path(output).read_bytes())


def main():
    cc, ld, source, include, libc, output = sys.argv[1:7]
    Path(output).parent.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory() as tmp:
        own = build(cc, ld, source, include, libc, output, Path(tmp), 0)

        if own >= COUNT:
            raise SystemExit(f"check.c alone makes {own} functions")

        made = build(cc, ld, source, include, libc, output, Path(tmp), COUNT - own)

        if made != COUNT:
            raise SystemExit(f"{COUNT - own} functions besides made {made}, not {COUNT}")


if __name__ == "__main__":
    main()
