#!/usr/bin/env python3
"""Keep byte-load narrowing through handlers returning via a one-target table.

    run_single_target_dispatch.py <vm> <native-aot> <wazero> <wat2wasm> <fixture.wat> <rootfs>
"""
import os
from pathlib import Path
import struct
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import objects  # noqa: E402
import system  # noqa: E402


def program(template):
    checks = []
    for seed in (*range(16), 0x80000080, 0xdeadbeef, 0xffffffff):
        for mode in (0, 1, 0xffffffff):
            for rounds in (1, 2, 5, 17):
                expected = seed & 15
                for _ in range(rounds - 1):
                    expected = (expected * 29 + 3) & 15
                checks.append(f"(if (i32.ne (call $route (i32.const {seed}) "
                              f"(i32.const {mode}) (i32.const {rounds})) "
                              f"(i32.const {expected})) (then unreachable))")
    return template.rstrip()[:-1] + '\n(func (export "_start")\n' + "\n".join(checks) + "\n))\n", len(checks)


def byte_reads(obj, env):
    # The reads of f0 at [r15 + disp32], the module's memory: an operand of
    # mod 10 and rm 111, the base r15 by a REX prefix's B, after one opcode
    # byte or after 0f and one. 0F B6 is MOVZX from a byte; a word/dword read
    # here would leave redundant dispatch masks.
    obj = objects.read(obj)
    code = next(obj.text[start:end] for name, start, end in obj.functions if name == "f0")
    reads = {}
    for at in range(3, len(code) - 4):
        address = struct.unpack_from("<i", code, at + 1)[0]
        if code[at] & 0xc7 != 0x87 or not 2048 <= address < 2112 or address % 4:
            continue
        if code[at - 2] == 0x0f and code[at - 3] & 0xf1 == 0x41:
            opcode = code[at - 2:at]
        elif code[at - 2] & 0xf1 == 0x41:
            opcode = code[at - 1:at + 1]
        else:
            continue
        reads.setdefault(address, []).append(opcode.hex())
    expected = set(range(2048, 2112, 4))
    assert set(reads) == expected, ("missing dispatch reads", reads)
    assert all(opcode == "0fb6" for values in reads.values() for opcode in values), ("dispatch reads were not narrowed to bytes", reads)


def main():
    vm, native, wazero, wat2wasm, fixture, rootfs = sys.argv[1:]
    text, count = program(Path(fixture).read_text())
    with tempfile.TemporaryDirectory(prefix="pizdos-wat-single-target-dispatch-") as directory:
        work = Path(directory)
        source, module, compiled, embedded = (work / name for name in ("input.wat", "input.wasm", "native.o", "embedded.o"))
        source.write_text(text)
        result = subprocess.run([wat2wasm, str(source), "-o", str(module)], capture_output=True, timeout=30)
        assert result.returncode == 0, result.stderr.decode()
        result = subprocess.run([wazero, "-interpreter", str(module)], capture_output=True, timeout=30)
        assert result.returncode == 0 and result.stdout == b"", result
        for strict in (False, True):
            env = dict(os.environ, VM_MEMORY_TAIL="strict" if strict else "optimized", VM_TAIL_PROFILE="")
            result = subprocess.run([native, *(["--strict"] if strict else [])], input=module.read_bytes(), capture_output=True, env=env, timeout=60)
            assert result.returncode == 0, (strict, result.stderr)
            compiled.write_bytes(result.stdout)
            result = subprocess.run([vm, "-o", str(embedded), str(module)], capture_output=True, env=env, timeout=60)
            assert result.returncode == 0 and embedded.read_bytes() == compiled.read_bytes(), (strict, result)
            # A mode is a boot of the system of its own.
            result = system.run(vm, rootfs, [("input", module.read_bytes(), [], b"")], env=env)[0]
            assert result == (0, b"", b""), (strict, result)
            byte_reads(compiled, env)
    print(f"WAT single-target dispatch: {count} modeled routes agree with wazero and both AOTs; all 16 masked word reads become byte loads in strict/optimized modes")


if __name__ == "__main__":
    main()
