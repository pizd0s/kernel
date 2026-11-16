#!/usr/bin/env python3
"""Propagate real Linux syscall failures of vm's own: its read of the
program it starts. The guest's calls, a WASI command's too, are the
kernel's, none of the host's.

    test_host_syscalls.py <vm> <native-aot> <filter>

A seccomp filter restricts only each child VM process. It returns selected error numbers for
selected syscalls, leaving the parent and later commands unaffected.
"""
import errno
import os
from pathlib import Path
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "dev"))

from exe import pack  # noqa: E402


def leb(value, signed=False):
    result = bytearray()
    while True:
        byte = value & 127
        value >>= 7
        if value == 0 and (not signed or byte < 64) or signed and value == -1 and byte >= 64:
            return bytes(result + bytes([byte]))
        result.append(byte | 128)


def section(kind, data):
    return bytes([kind]) + leb(len(data)) + data


def kernel_program():
    """A program of the kernel that ends at once: it imports getpid."""
    module = b"\0asm\1\0\0\0" + section(1, b"\2\x60\0\1\x7f\x60\0\0") + section(2, b"\1\6kernel\6getpid\0\0")
    return module + section(3, b"\1\1") + section(7, b"\1\6_start\0\1") + section(10, b"\1\5\0\x10\0\x1a\x0b")


def main():
    vm, native, wrapper = sys.argv[1:]
    env = dict(os.environ, W2X_MEMORY_TAIL="strict", W2X_TAIL_PROFILE="")
    with tempfile.TemporaryDirectory(prefix="pizdos-host-syscalls-") as directory:
        program = Path(directory) / "program"
        program.write_bytes(pack(kernel_program()))
        # Startup reads the module using fstat and pread, as exec does.
        # Returning zero from a suppressed pread models EOF after its
        # recorded size.
        for number, error, diagnostic in ((5, errno.EIO, b"input/output error"),
                                           (17, errno.EIO, b"input/output error"), (17, 0, b"exec format error")):
            result = subprocess.run([wrapper, f"{error}/{number}", "--", vm, "run", "--", str(program)],
                                    capture_output=True, env=env, timeout=30)
            assert result.returncode == 1 and result.stdout == b"" and diagnostic in result.stderr and b"panic:" not in result.stderr, (number, error, result)
    print("host syscalls: vm's read of its program reports the host's errors")


if __name__ == "__main__":
    main()
