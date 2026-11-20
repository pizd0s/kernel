#!/usr/bin/env python3
"""The embedded compiler and the same compiler run in the system agree with
C++."""
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

import system


def main():
    vm, native, compiler, fixture, rootfs = sys.argv[1:]
    # The self-compiled AOT, the program the kernel compiles as it starts,
    # compiles its inputs in the system, all of them in one boot.
    self_compiled = []

    with tempfile.TemporaryDirectory(prefix="vm-") as work:
        out = Path(work) / "out.o"
        isolated = Path(work) / "vm"
        shutil.copyfile(vm, isolated)
        isolated.chmod(0o755)
        env = dict(os.environ, PATH="")

        for source in [compiler, fixture]:
            data = Path(source).read_bytes()
            want = subprocess.run([native], input=data, capture_output=True, check=True).stdout
            subprocess.run([str(isolated), "-o", str(out), source], cwd=work, env=env, check=True)
            assert out.read_bytes() == want, f"embedded AOT: {source} differs from native C++"
            print(f"embedded AOT: {Path(source).name} -> {len(want)} bytes, identical")
            self_compiled.append((Path(source).name, data, want))

        # An ignored custom section makes input end exactly on a page boundary.
        # Supplying it through a pipe also exercises partial stdin reads.
        empty = b"\0asm\1\0\0\0"
        want = subprocess.run([native], input=empty, capture_output=True, check=True).stdout
        payload_size = 2 * 65536 - len(empty) - 4
        length = bytearray()
        remaining = payload_size
        while remaining >= 128:
            length.append((remaining & 127) | 128)
            remaining >>= 7
        length.append(remaining)
        padded = empty + b"\0" + length + bytes(payload_size)
        assert len(padded) == 2 * 65536
        got = subprocess.run([native], input=padded, capture_output=True, check=True, pipesize=4096).stdout
        assert got == want, "native: page-aligned stdin changed the module"
        aligned = Path(work) / "aligned.wasm"
        aligned.write_bytes(padded)
        subprocess.run([str(isolated), "-o", str(out), str(aligned)], cwd=work, env=env, check=True)
        assert out.read_bytes() == want, "embedded AOT: page-aligned stdin changed the module"
        self_compiled.append(("page-aligned stdin", padded, want))
        program = Path(compiler).read_bytes()
        results = system.run(vm, rootfs, [("aot", program, [], data) for _, data, _ in self_compiled])
        for (label, _, want), (status, got, err) in zip(self_compiled, results):
            assert status == 0 and got == want, f"self-compiled AOT: {label} differs from native C++: {status} {err[-500:]!r}"
            print(f"self-compiled AOT: {label} -> {len(want)} bytes, identical")
        print("page-aligned stdin: native/embedded/self-compiled identical")

        bad = Path(work) / "bad.wasm"
        bad.write_bytes(b"not wasm")
        out.write_bytes(b"previous output")
        result = subprocess.run([str(isolated), "-o", str(out), str(bad)], cwd=work, env=env, capture_output=True)
        assert result.returncode != 0 and b"aot exited" in result.stderr
        assert out.read_bytes() == b"previous output", "failed compilation replaced the output"
        assert not list(Path(work).glob(".vm-*")), "failed compilation retained temporary output"


if __name__ == "__main__":
    main()
