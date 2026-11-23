#!/usr/bin/env python3
"""The kernel's link of libc.wasm into a program before it compiles it
(bin/vm/wasm_linker.go), by `vm link`: tst/link/check.c, 127 functions
of its own, which libc.wasm's make a count a byte longer. The linked
module imports none of the kernel functions libc.wasm defines, holds only
what the program reaches of them, exports libc.wasm's functions the kernel
calls the program's through, and says how far its code moved, so that its
trap names the program's line.

    test_libc_link.py <vm> <check.wasm>
"""
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "dev"))

import exe  # noqa: E402

SOURCE = Path(__file__).resolve().parent / "link" / "check.c"
REACHED = {"__kernel_dbm_open", "__kernel_dbm_close"}
CALLS = {"__call_signal_handler", "__call_signal_action", "__call_glob_error"}
UNREACHED = {"__kernel_dbm_store", "__kernel_dbm_delete", "__kernel_dbm_fetch", "__kernel_dbm_firstkey", "__kernel_dbm_nextkey"}


def leb(data, at):
    value = shift = 0

    while True:
        byte = data[at]
        at += 1
        value |= (byte & 0x7f) << shift
        shift += 7

        if byte < 0x80:
            return value, at


def text(data, at):
    size, at = leb(data, at)

    return data[at:at + size].decode(), at + size


def sections(data):
    """A module's sections, a custom one by its name."""
    out, at = {}, 8

    while at < len(data):
        kind = data[at]
        size, at = leb(data, at + 1)
        body = data[at:at + size]
        at += size

        if kind == 0:
            name, start = text(body, 0)
            out[name] = body[start:]
        else:
            out[kind] = body

    return out


def imports(body):
    count, at = leb(body, 0)
    out = set()

    for _ in range(count):
        module, at = text(body, at)
        name, at = text(body, at)
        assert body[at] == 0, (module, name)
        _, at = leb(body, at + 1)
        out.add((module, name))

    return out


def exports(body):
    count, at = leb(body, 0)
    out = {}

    for _ in range(count):
        name, at = text(body, at)
        kind = body[at]
        index, at = leb(body, at + 1)
        out[name] = (kind, index)

    return out


def function_names(body):
    at = 0

    while at < len(body):
        kind = body[at]
        size, at = leb(body, at + 1)

        if kind == 1:
            count, start = leb(body, at)
            names = set()

            for _ in range(count):
                _, start = leb(body, start)
                name, start = text(body, start)
                names.add(name)

            return names

        at += size

    return set()


def main():
    vm, module = sys.argv[1:]
    original = sections(Path(module).read_bytes())

    with tempfile.TemporaryDirectory(prefix="pizdos-link-") as directory:
        linked, program = Path(directory) / "linked.wasm", Path(directory) / "check"
        subprocess.run([vm, "link", "-o", str(linked), module], check=True, timeout=60)
        merged = sections(linked.read_bytes())
        program.write_bytes(exe.pack(Path(module).read_bytes()))
        result = subprocess.run([vm, "run", "--strace=faults", "--", str(program)], capture_output=True, timeout=120)

    own, count = leb(original[10], 0)[0], leb(merged[10], 0)[0]
    assert own == 127, own
    assert ("kernel", "dbm_open") in imports(original[2]), imports(original[2])
    kernel = {name for module, name in imports(merged[2]) if module == "kernel"}
    assert not kernel & {"dbm_open", "dbm_close"}, kernel
    names = function_names(merged["name"])
    assert REACHED <= names and not names & UNREACHED, names & (REACHED | UNREACHED)
    assert not CALLS & set(exports(original[7])), exports(original[7])
    calls = {name: entry for name, entry in exports(merged[7]).items() if name in CALLS}
    assert set(calls) == CALLS and all(kind == 0 and index >= count - own for kind, index in calls.values()), calls
    shift = leb(merged["w2o.code_shift"], 0)[0]
    assert shift == 1 and count > own, (shift, count)
    trap = next(i for i, line in enumerate(SOURCE.read_text().splitlines(), 1) if "__builtin_trap" in line)
    assert result.returncode == 132 and b"wasm trap: unreachable" in result.stderr, result
    assert f"check.c:{trap}\n".encode() in result.stderr, result.stderr.decode(errors="replace")
    print(f"libc link: {count - own} functions of libc.wasm in the program, its trap at check.c:{trap} through a code shift of {shift}")


if __name__ == "__main__":
    main()
