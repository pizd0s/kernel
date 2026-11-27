#!/usr/bin/env python3
"""A module's start function cannot copy command frames before _start runs.

    run_startup_fork.py <vm> <native-aot> <wazero> <wat2wasm> <fixture.wat> <rootfs>
"""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import system  # noqa: E402


def main():
    vm, native, wazero, wat2wasm, fixture, rootfs = sys.argv[1:]
    with tempfile.TemporaryDirectory(prefix="pizdos-wat-startup-fork-") as directory:
        work = Path(directory)
        module, compiled, embedded = (work / name for name in ("input.wasm", "native.o", "embedded.o"))
        result = subprocess.run([wat2wasm, fixture, "-o", str(module)], capture_output=True, timeout=30)
        assert result.returncode == 0, result.stderr.decode()
        result = subprocess.run([wazero, "-compile", str(module)], capture_output=True, timeout=30)
        assert result.returncode == 0, result
        for strict in (False, True):
            env = dict(os.environ, VM_MEMORY_TAIL="strict" if strict else "optimized", VM_TAIL_PROFILE="")
            result = subprocess.run([native, *(["--strict"] if strict else [])], input=module.read_bytes(), capture_output=True, env=env, timeout=60)
            assert result.returncode == 0, (strict, result.stderr)
            compiled.write_bytes(result.stdout)
            result = subprocess.run([vm, "-o", str(embedded), str(module)], capture_output=True, env=env, timeout=60)
            assert result.returncode == 0 and embedded.read_bytes() == compiled.read_bytes(), (strict, result)
            (result,) = system.run(vm, rootfs, [("input", module.read_bytes(), [], b"")], env=env)
            assert result == (0, b"", b""), (strict, result)
    print("WAT startup fork: initialization receives ENOSYS and the command then runs, through identical native/Wasm objects")


if __name__ == "__main__":
    main()
