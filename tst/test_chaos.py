#!/usr/bin/env python3
"""Faults exercise real AOT/VM commands; production executables ignore them.

    test_chaos.py <vm> <vm-chaos> <native-aot> <native-aot-chaos> <guest.wasm> <rootfs> <tests.img>

The guest, tst/chaos/check.c, the image of the tests' /tests/chaos/check,
runs in the system, its rule of the kernel's points of failure
PIZDOS_CHAOS in its environment: it does an operation sixteen times and
counts what failed, which the chaos build's rule fails once and the
production build's never. memory-move is a rule that holds: the chaos
build moves a process's memory at every grow and as every signal handler
starts, under waits a handler interrupts, and nothing they bring is
lost. memory-compact holds too: every growth packs the guests' memory,
moving the regions of all processes that can move.
"""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

EMPTY = b"\0asm\1\0\0\0"


def call(command, *, data=None, **variables):
    environment = dict(os.environ)
    environment.pop("PIZDOS_AOT_CHAOS", None)
    environment.pop("PIZDOS_VM_CHAOS", None)
    environment.update(variables)
    return subprocess.run(command, input=data, capture_output=True, env=environment, timeout=60)


def main():
    vm, vm_chaos, native, native_chaos, guest, rootfs, image = sys.argv[1:]
    reference = call([native], data=EMPTY)
    assert reference.returncode == 0, reference.stderr
    assert call([native_chaos], data=EMPTY).stdout == reference.stdout
    with tempfile.TemporaryDirectory(prefix="pizdos-chaos-") as directory:
        work = Path(directory)
        source, destination = work / "input.wasm", work / "out.o"
        source.write_bytes(EMPTY)

        def compile_with(binary, **variables):
            destination.write_bytes(b"previous object")
            result = call([binary, "-o", str(destination), str(source)], **variables)
            if result.returncode:
                assert destination.read_bytes() == b"previous object", "failed compiler replaced the output"
                assert not list(work.glob(".vm-*")), "failed compiler leaked temporary output"
            else:
                assert destination.read_bytes() == reference.stdout, "compiler output changed"
            return result

        assert compile_with(vm, PIZDOS_AOT_CHAOS="read@0", PIZDOS_VM_CHAOS="invalid").returncode == 0
        plain = call([native], data=EMPTY, PIZDOS_AOT_CHAOS="read@0")
        assert plain.returncode == 0 and plain.stdout == reference.stdout
        assert compile_with(vm_chaos).returncode == 0

        for rule, native_text, wasm_text in (
            ("read@0", b"stdin read failed", b"stdin read failed"),
            ("read@1", b"stdin read failed", b"stdin read failed"),
            ("write@0", b"", b"aot exited with code 1"),
            ("write@1", b"", b"aot exited with code 1"),
            ("malloc@0", b"out of memory", b"out of memory"),
        ):
            result = call([native_chaos], data=EMPTY, PIZDOS_AOT_CHAOS=rule)
            assert result.returncode != 0 and native_text in result.stderr, (rule, result)
            result = compile_with(vm_chaos, PIZDOS_AOT_CHAOS=rule)
            assert result.returncode != 0 and wasm_text in result.stderr, (rule, result)
            assert compile_with(vm_chaos).returncode == 0, "a later compilation did not recover"

        result = compile_with(vm_chaos, PIZDOS_VM_CHAOS="mmap@0")
        assert result.returncode != 0 and b"cannot allocate memory" in result.stderr, result
        assert compile_with(vm_chaos).returncode == 0

        for script in ("missing@0", "read@-1", "read@", "read@1,", "read@0,read@1", "write@4294967296"):
            result = call([native_chaos], data=EMPTY, PIZDOS_AOT_CHAOS=script)
            assert result.returncode != 0 and b"invalid PIZDOS_AOT_CHAOS" in result.stderr, (script, result)
            result = compile_with(vm_chaos, PIZDOS_AOT_CHAOS=script)
            assert result.returncode != 0 and b"invalid PIZDOS_AOT_CHAOS" in result.stderr, (script, result)
        assert compile_with(vm_chaos, PIZDOS_AOT_CHAOS="read@4294967295,write@4294967295,malloc@4294967295").returncode == 0
        result = compile_with(vm_chaos, PIZDOS_VM_CHAOS="missing@0")
        assert result.returncode != 0 and b"invalid chaos rule" in result.stderr
        assert compile_with(vm_chaos, PIZDOS_VM_CHAOS="mmap@4294967295").returncode == 0

        compiled = call([native], data=Path(guest).read_bytes())
        assert compiled.returncode == 0, compiled.stderr
        program = work / "guest.o"
        embedded = work / "guest-embedded.o"
        program.write_bytes(compiled.stdout)
        result = call([vm, "-o", str(embedded), guest])
        assert result.returncode == 0 and embedded.read_bytes() == compiled.stdout, result.stderr

        # The check's output goes through cat, which waits for its end: what
        # daemon detaches writes it too.
        def run_guest(binary, operation, failures, rule=""):
            script = f"cd /tmp && env PIZDOS_CHAOS={rule} /tests/chaos/check {operation} | cat"
            result = call([binary, "run", "--disk", f"image={image}", "--", rootfs, "-c", script], data=b"x" * 32)
            expected = f"{operation}: failures={failures}\n".encode()
            assert (result.returncode, result.stdout, result.stderr) == (0, expected, b""), (operation, rule, result)

        for operation, point, skip in (("read", "read", 2), ("write", "write", 2), ("openat", "openat", 2), ("fork", "fork", 2),
                                       ("memory-grow", "memory-grow", 2), ("fstat", "file-stat", 2), ("fstatat", "file-stat", 2),
                                       ("stat", "file-stat", 2), ("lstat", "file-stat", 2), ("fdopendir", "file-stat", 2),
                                       ("msync", "file-stat", 2), ("chdir", "file-stat", 2), ("relative-open", "openat", 1),
                                       ("fexecve", "file-stat", 1), ("system", "openat", 1), ("daemon", "openat", 2)):
            rule = f"{point}@{skip}"
            run_guest(vm, operation, 0, rule)
            run_guest(vm_chaos, operation, 0)
            run_guest(vm_chaos, operation, 1, rule)
            run_guest(vm_chaos, operation, 0, f"{point}@4294967295")

        # A memory moves at every grow and as every handler starts with
        # memory-move, under waits a handler interrupts, in a loop it
        # interrupts anywhere and as the stack grows into blocks of the
        # heap: nothing is lost.
        for binary, rule in ((vm, "memory-move@0"), (vm_chaos, ""), (vm_chaos, "memory-move@0")):
            run_guest(binary, "memory-move", 0, rule)

        # With memory-compact every growth packs the guests' memory: every
        # process of the system, the check's children waiting in a read and
        # running a loop among them, moves, and nothing is lost.
        for binary, rule in ((vm, "memory-compact@0"), (vm_chaos, ""), (vm_chaos, "memory-compact@0")):
            run_guest(binary, "memory-compact", 0, rule)

    print("chaos: native/Wasm AOT and VM faults, the kernel's faults a guest recovers from, atomic output and production no-ops passed")


if __name__ == "__main__":
    main()
