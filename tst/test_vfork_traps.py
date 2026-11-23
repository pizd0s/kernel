#!/usr/bin/env python3
"""A trap in nested vfork children must unwind the whole borrowed stack.

    test_vfork_traps.py <vm> <native-aot> <wazero> <rootfs>
"""
import os
import re
from pathlib import Path
import subprocess
import sys
import tempfile

import system

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "dev"))

from exe import pack  # noqa: E402


def leb(value):
    result = bytearray()
    while value >= 128:
        result.append(value & 127 | 128)
        value >>= 7
    return bytes(result + bytes([value]))


def section(kind, data):
    return bytes([kind]) + leb(len(data)) + data


def text(value):
    data = value.encode()
    return leb(len(data)) + data


def program(children, fault):
    module = b"\0asm\1\0\0\0" + section(1, b"\2\x60\0\1\x7f\x60\0\0")
    module += section(2, b"\1" + text("kernel") + text("vfork") + b"\0\0") + section(3, b"\1\1")
    module += section(5, b"\1\1\1\1")
    module += section(6, b"\1\x7f\1\x41\x80\x80\2\x0b")
    module += section(7, b"\2" + text("_start") + b"\0\1" + text("__stack_pointer") + b"\3\0")
    body = b"\0" + b"\x10\0\x1a" * children + fault + b"\x0b"
    return module + section(10, b"\1" + leb(len(body)) + body)


def main():
    vm, native, wazero, rootfs = sys.argv[1:]
    faults = [(b"\0", 132, b"wasm trap: unreachable"),
              (b"\x41\x81\x80\4\x2d\0\0\x1a", 139, b"out of bounds memory access"),
              (b"\x41\1\x41\0\x6d\x1a", 136, b"integer divide by zero")]
    env = dict(os.environ, W2X_MEMORY_TAIL="strict", W2X_TAIL_PROFILE="")
    with tempfile.TemporaryDirectory(prefix="pizdos-vfork-traps-") as directory:
        work = Path(directory)
        source, compiled, embedded = [work / name for name in ("input.wasm", "native.o", "embedded.o")]
        runs = []
        for children in (1, 2, 4):
            for fault, status, diagnostic in faults:
                data = program(children, fault)
                source.write_bytes(data)
                result = subprocess.run([wazero, "-compile", str(source)], capture_output=True, timeout=30)
                assert result.returncode == 0, result
                result = subprocess.run([native, "--strict"], input=data, capture_output=True, env=env, timeout=60)
                assert result.returncode == 0, result.stderr
                compiled.write_bytes(result.stdout)
                result = subprocess.run([vm, "-o", str(embedded), str(source)], capture_output=True, env=env, timeout=60)
                assert result.returncode == 0 and embedded.read_bytes() == compiled.read_bytes(), result
                runs.append(((children, status, diagnostic), pack(data)))
        # The programs run in one boot of the system; the trap is the
        # innermost borrower's, whose trace its pid marks.
        results = system.run(vm, rootfs, [("vfork-trap", program, [], b"") for _, program in runs], env=env, faults=True)
        for ((children, status, diagnostic), _), (code, out, err) in zip(runs, results):
            assert code == status and out == b"" and diagnostic in err, (children, status, code, out, err)
            assert re.match(rb"\[\d+\] ", err) and b"panic:" not in err, (children, err)
    print("vfork traps: unreachable, memory and arithmetic faults in nested borrowers unwind safely with both AOTs")


if __name__ == "__main__":
    main()
