#!/usr/bin/env python3
"""vm starts its first process under a host descriptor limit of a few:
the guest's objects are the kernel's and take none of the host's.

    test_host_resources.py <vm> <native-aot>
"""
import os
from pathlib import Path
import resource
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "dev"))

from exe import pack  # noqa: E402


def bootstrap_program():
    # A kernel entry that checks getpid() == 1 without allocating or doing I/O.
    sections = [(1, b"\2\x60\0\1\x7f\x60\0\0"),
                (2, b"\1\6kernel\6getpid\0\0"), (3, b"\1\1"),
                (7, b"\1\6_start\0\1"),
                (10, b"\1\13\0\x10\0\x41\1\x47\x04\x40\0\x0b\x0b")]
    return b"\0asm\1\0\0\0" + b"".join(bytes([kind, len(body)]) + body for kind, body in sections)


def main():
    vm, native = sys.argv[1:]
    env = dict(os.environ, VM_MEMORY_TAIL="strict", VM_TAIL_PROFILE="")
    with tempfile.TemporaryDirectory(prefix="pizdos-host-resources-") as directory:
        work = Path(directory)
        compiled, embedded = work / "native.o", work / "embedded.o"
        source, bootstrap = work / "bootstrap.wasm", work / "bootstrap"
        data = bootstrap_program()
        source.write_bytes(data)
        bootstrap.write_bytes(pack(data))
        result = subprocess.run([native, "--strict"], input=data, capture_output=True, env=env, timeout=60)
        assert result.returncode == 0, result
        compiled.write_bytes(result.stdout)
        result = subprocess.run([vm, "-o", str(embedded), str(source)], capture_output=True, env=env, timeout=60)
        assert result.returncode == 0 and embedded.read_bytes() == compiled.read_bytes(), result
        for limit in (8, 9, 10, 12):
            def restrict():
                resource.setrlimit(resource.RLIMIT_NOFILE, (limit, limit))
            result = subprocess.run([vm, "run", "--", str(bootstrap)], capture_output=True,
                                    env=env, preexec_fn=restrict, timeout=30)
            assert (result.returncode, result.stdout, result.stderr) == (0, b"", b""), (limit, result)
    print("host resources: the first process starts under a limit of a few host descriptors with both AOTs")


if __name__ == "__main__":
    main()
