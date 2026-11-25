#!/usr/bin/env python3
"""Preserve floating results of a call when an internal tuple returns them
elsewhere: difftime's, which was the kernel's Go and is libc.wasm's, linked
into the program, which has its stack pointer and heap base for it.

    run_go_float_results.py <vm> <native-aot> <wazero> <wat2wasm> <fixture.wat> <rootfs>
"""
import os
from pathlib import Path
import struct
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import system  # noqa: E402


def signed(value):
    return value if value < 1 << 63 else value - (1 << 64)


def bits(value):
    return int.from_bytes(struct.pack("<d", value), "little")


def program(template, reference=False):
    implementation = ('(func $difference (param $later i64) (param $earlier i64) (result f64) '
                      '(f64.sub (f64.convert_i64_s (local.get $later)) (f64.convert_i64_s (local.get $earlier))))') if reference else (
                      '(import "kernel" "difftime" (func $difference (param i64 i64) (result f64))) (memory 2) '
                      '(global (export "__stack_pointer") (mut i32) (i32.const 65536)) (global (export "__heap_base") i32 (i32.const 65536))')
    template = template.replace(";; DIFFERENCE", implementation)
    checks, count = [], 0
    for first in (0, 1 << 63, bits(13.25), 0x7ff8000000000123):
        for later, earlier in ((0, 0), (17, -3), (-17, 3), ((1 << 63) - 1, -(1 << 63)),
                               (1 << 53, (1 << 53) + 2), (-(1 << 63), -(1 << 63) + 4096)):
            arguments = f"(f64.reinterpret_i64 (i64.const {signed(first)})) (i64.const {later}) (i64.const {earlier})"
            # tzcode's difftime is the exact difference, rounded once; the
            # model's is of the times rounded, the same of these times.
            difference = bits(float(later - earlier))
            assert difference == bits(float(later) - float(earlier))
            calls = [(f"(call $relay {arguments})", difference)]
            for condition in (0, 1):
                other = 0xfff8000000000456
                call = (f"(call $choose (i32.const {condition}) (f64.reinterpret_i64 (i64.const {signed(first)})) "
                        f"(f64.reinterpret_i64 (i64.const {signed(other)})) (i64.const {later}) (i64.const {earlier}))")
                calls.append((call, other if condition else difference))
            for call, expected in calls:
                checks += [call, "(local.set $second)",
                           f"(if (i64.ne (i64.reinterpret_f64) (i64.const {signed(first)})) (then unreachable))",
                           f"(if (i64.ne (i64.reinterpret_f64 (local.get $second)) (i64.const {signed(expected)})) (then unreachable))"]
                count += 1
    return template.rstrip()[:-1] + '\n(func (export "_start") (local $second f64)\n' + "\n".join(checks) + "\n))\n", count


def main():
    vm, native, wazero, wat2wasm, fixture, rootfs = sys.argv[1:]
    template = Path(fixture).read_text()
    with tempfile.TemporaryDirectory(prefix="pizdos-wat-go-float-results-") as directory:
        work = Path(directory)
        source, module, linked, compiled, embedded = (work / name for name in ("input.wat", "input.wasm", "linked.wasm", "native.o", "embedded.o"))
        batches = {False: [], True: []}
        for reference in (True, False):
            wat, checks = program(template, reference)
            source.write_text(wat)
            result = subprocess.run([wat2wasm, str(source), "-o", str(module)], capture_output=True, timeout=30)
            assert result.returncode == 0, result.stderr.decode()
            if reference:
                result = subprocess.run([wazero, "-interpreter", str(module)], capture_output=True, timeout=30)
                assert result.returncode == 0 and result.stdout == b"", result
            # What vm compiles is the module with libc.wasm's functions it
            # imports linked into it, the native compiler's input.
            subprocess.run([vm, "link", "-o", str(linked), str(module)], capture_output=True, check=True, timeout=60)
            for strict in (False, True):
                env = dict(os.environ, W2X_MEMORY_TAIL="strict" if strict else "optimized", W2X_TAIL_PROFILE="")
                result = subprocess.run([native, *(["--strict"] if strict else [])], input=linked.read_bytes(), capture_output=True, env=env, timeout=60)
                assert result.returncode == 0, result.stderr
                compiled.write_bytes(result.stdout)
                result = subprocess.run([vm, "-o", str(embedded), str(module)], capture_output=True, env=env, timeout=60)
                assert result.returncode == 0 and embedded.read_bytes() == compiled.read_bytes(), result
                batches[strict].append((reference, module.read_bytes()))
        # Each mode's programs run in one boot of the system.
        for strict, runs in batches.items():
            env = dict(os.environ, W2X_MEMORY_TAIL="strict" if strict else "optimized", W2X_TAIL_PROFILE="")
            results = system.run(vm, rootfs, [("input", data, [], b"") for _, data in runs], env=env)
            for (reference, _), result in zip(runs, results):
                assert result == (0, b"", b""), (reference, strict, result)
    print(f"WAT float results: {checks} multi-result relays and branch merges preserve result positions, NaN payloads and signed zero with a model, wazero reference and both AOTs")


if __name__ == "__main__":
    main()
