#!/usr/bin/env python3
"""The compiler, a program of the system, matches the kernel's.

    test_aot.py <aot.wasm> <vm> <fixture.wasm> <rootfs>

vm -o compiles with the compiler the kernel runs as a process of its own.
The same aot.wasm, an executable of the batch, compiles the same inputs in
the system, all of them one batch of a boot (tst/system.py), and refuses
what is no module, as vm -o does.
"""
import os
import subprocess
import sys
import tempfile
from pathlib import Path

import system


def main():
    aot, vm, fixture, rootfs = sys.argv[1:]
    env = dict(os.environ, W2X_MEMORY_TAIL="optimized", W2X_TAIL_PROFILE="")
    empty = b"\0asm\1\0\0\0"
    sources = {
        "empty": empty,
        # A malformed optional name subsection must not reject the module.
        "optional-names": empty + b"\0\x0b\x04name\x01\x04\x01\0\x01\xff",
        "fixture": Path(fixture).read_bytes(),
        "aot": Path(aot).read_bytes(),
    }
    malformed = [b"", b"bad!\1\0\0\0", b"\0asm\1\0\0\0\1\1\x80"]

    with tempfile.TemporaryDirectory(prefix="aot-") as directory:
        directory = Path(directory)
        source, output = directory / "input.wasm", directory / "output.o"
        references = {}

        for name, data in sources.items():
            source.write_bytes(data)
            subprocess.run([vm, "-o", str(output), str(source)], env=env, capture_output=True, check=True)
            references[name] = output.read_bytes()

        for data in malformed:
            source.write_bytes(data)
            result = subprocess.run([vm, "-o", str(output), str(source)], env=env, capture_output=True)
            assert result.returncode == 1 and b"aot exited with code 134: " in result.stderr, (data, result)

    program = Path(aot).read_bytes()
    inputs = [*sources.values(), *malformed]
    results = system.run(vm, rootfs, [("aot", program, [], data) for data in inputs], env=env)

    for name, (status, out, err) in zip(sources, results):
        assert status == 0 and out == references[name], f"aot in the system differs for {name}: {status} {err[-500:]!r}"
        print(f"aot in the system: {name} -> {len(out)} bytes, identical")

    for data, (status, out, err) in zip(malformed, results[len(sources):]):
        assert status == 134 and out == b"" and err, (data, status, out, err)

    print(f"aot in the system and vm -o: {len(malformed)} malformed modules refused")


if __name__ == "__main__":
    main()
