#!/usr/bin/env python3
"""Build the instrumented wasm compiler, a program on lib/c, without compiler-rt.

    coverage_wasm_build.py <clang> <wasm-ld> <aot.cpp> <shim.c> <output> <flags-json>

The flags carry the compiler's flags, whether it is the chaos build and
lib/c's libc.a. The VM extracts counters before unmapping each compiler
instance. Keep the coverage mapping custom sections; production --strip-all
would erase them.
"""
import json
from pathlib import Path
import subprocess
import sys
import tempfile


def build(clang, linker, source, shim, output, options):
    output = Path(output).resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    flags = [flag for flag in options["flags"] if not flag.startswith("-Wl,")]
    variant = ["-DAOT_CHAOS"] if options.get("chaos") else []
    with tempfile.TemporaryDirectory(prefix="wasm-coverage-") as directory:
        obj, runtime = [str(Path(directory, name + ".o")) for name in ("aot", "profile")]
        subprocess.run([clang, *flags, *variant, "-fprofile-instr-generate", "-fcoverage-mapping",
                        "-mllvm", "-enable-value-profiling=false",
                        "-mllvm", "-enable-name-compression=false", "-c", source, "-o", obj], check=True)
        subprocess.run([clang, "--target=wasm32-unknown-unknown", "-ffreestanding", "-nostdinc",
                        "-O2", *variant, "-c", shim, "-o", runtime], check=True)
        subprocess.run([linker, "--stack-first", "--max-memory=4294967296", "--export=__heap_base",
                        "--export=__stack_pointer", obj, runtime, options["libc"], "-o", str(output)], check=True)


if __name__ == "__main__":
    build(*sys.argv[1:6], json.loads(sys.argv[6]))
