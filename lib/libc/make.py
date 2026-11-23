#!/usr/bin/env python3
"""Build libc.wasm: the kernel's functions written in C, all of the C
library but what a program keeps (lib/c: its forwarders to the kernel, its
entry and the compiler's runtime). The kernel links those a program imports,
and what they reach, into the program before it compiles it
(bin/vm/wasm_linker.go); the rest of its functions are its Go.

    make.py <cc> <wasm-ld> <freebsd-slice> <llvm-slice> <arm-tarball> <libc.wasm>

A program imports the kernel's function X as ("kernel", X), by lib/c's
declaration of it in lib/c/src/kernel.c, and libc.wasm's function
__kernel_X is that one. The sources of this directory, our own, define
__kernel_X themselves, with the import's parameters: a structure's size
after it, as the kernel's functions take one. FreeBSD's sources in
freebsd/files.txt, LLVM libc's in llvm/files.txt and Arm's
optimized-routines' in arm/files.txt define X, the C function: symbols.py
names each __kernel_X beside, where the import is of the function's type.

libc.wasm is a relocatable object (wasm-ld -r) of all of them with lib/c's
forwarders, kernel.c, through which they call the kernel as a program does,
and its compiler's runtime, builtins.c. The forwarders are weak, so the
function of a forwarder's name that libc.wasm has is the one it calls. A
function a source here exports (export_name) the kernel links into every
program and exports from it by its name: the kernel calls the program's
functions through them (start.c).

The FreeBSD and LLVM slices are what freebsd/slice.txt and llvm/slice.txt
cut out of their release tarballs (dev/slice.py). FreeBSD's sources build
over lib/c's headers with freebsd/'s for what they take of FreeBSD's own,
sys/cdefs.h included first in each; FreeBSD's math.h is deleted, so their
quoted include of it falls through to lib/c's. A line of freebsd/files.txt
is a path from the tree's root and the file's own flags. Its libc's sources
have their stdio's and locale's headers on the path and xlocale/'s
declarations, and freebsd/patches/ are changes of them for lib/c, each
saying why, applied to the slice first; freebsd/*.c are our own, over
FreeBSD's FILE and locale. msun's own functions of the names __kernel_X,
the kernels of its sines and cosines, are renamed.

A line of llvm/files.txt is a C prototype, then the names of the same
function: each becomes a C++ source of its own, the function calling LLVM
libc's of its name in libc/shared/math, header-only, and weak aliases of
it; or a source of LLVM libc's alone, which gives its C name itself.
llvm/patches/ are fixes of LLVM libc's sources for wasm32, each saying
why, applied to the slice first. A line of arm/files.txt is a source of
optimized-routines' math/ and such a prototype and names, which a C source
of ours includes it with and makes the aliases of; or a source alone. They
build for the one rounding mode wasm has, to nearest, and set no errno, as
math_errhandling says, with none of the names glibc's ABI adds.
"""
import os
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent
LIBC = ROOT.parent / "c"
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(ROOT.parent.parent / "dev"))

import symbols  # noqa: E402
import wasm  # noqa: E402

INCLUDE = ["-isystem", str(LIBC / "include")]
FORWARDER_FLAGS = ["-O2", "-std=c99", "-D_XOPEN_SOURCE=700", "-w", "-I", str(LIBC / "src"), *INCLUDE]
FLAGS = ["-O2", "-std=c11", "-D_GNU_SOURCE", "-Wall", "-Werror", "-I", str(LIBC / "src"), *INCLUDE]
FREEBSD_FLAGS = ["-O2", "-std=c11", "-w", "-include", str(ROOT / "freebsd" / "sys" / "cdefs.h"), "-I", str(ROOT / "freebsd")]
LLVM_FLAGS = ["-O2", "-std=c++17", "-fno-exceptions", "-fno-rtti", "-nostdinc++", "-w", "-DLIBC_NAMESPACE=__llvm_libc"]
ARM_FLAGS = ["-O2", "-std=c11", "-w", "-DWANT_ROUNDING=0", "-DWANT_ERRNO=0", "-DUSE_GLIBC_ABI=0"]


def listed(path):
    for line in path.read_text().splitlines():
        line = line.split("#", 1)[0].strip()

        if line:
            yield line


def unpack(tarball, work):
    work.mkdir(parents=True)

    with tarfile.open(tarball) as archive:
        archive.extractall(work, filter="data")

    return work


def patched(tree, patches):
    for change in sorted(patches.glob("*.patch")):
        subprocess.run(["patch", "-p1", "--batch", "--forward", "-s", "-d", str(tree), "-i", str(change)], check=True)

    return tree


def function(line, listing):
    """A prototype and the names of the same function: its result, name,
    parameter types and those names."""
    prototype, _, names = line.partition(";")
    match = re.fullmatch(r"(.+?)\s*\b(\w+)\((.*)\)", prototype.strip())

    if not match:
        raise SystemExit(f"{listing}: {line}")

    result, name, parameters = match.groups()

    return result, name, [part.strip() for part in parameters.split(",")], names.split()


def aliases(result, name, types, names, linkage):
    """Weak aliases of a function, each declared as its name says: one
    ending in l is long double's, the prototype with long double for
    double."""
    lines = []

    for alias in names:
        def to(t, wide=alias.endswith("l")):
            return re.sub(r"(?<!long )\bdouble\b", "long double", t) if wide else t

        lines.append(f'{linkage}{to(result)} {alias}({", ".join(to(t) for t in types)}) '
                     f'__attribute__((__weak__, __alias__("{name}")));')

    return lines


def wrappers(work, llvm):
    """llvm/files.txt's functions, a C++ source each, and its sources."""
    sources = []

    for line in listed(ROOT / "llvm" / "files.txt"):
        if "(" not in line:
            sources.append(llvm / line)
            continue

        result, name, types, names = function(line, "llvm/files.txt")
        arguments = [f"a{i}" for i in range(len(types))]
        call = f"LIBC_NAMESPACE::shared::{name}({', '.join(arguments)})"
        text = [f'#include "shared/math/{name}.h"',
                f'extern "C" {result} {name}({", ".join(f"{t} {a}" for t, a in zip(types, arguments))}) {{',
                f"    {'' if result == 'void' else 'return '}{call};",
                "}"]
        text += aliases(result, name, types, names, 'extern "C" ')
        source = work / f"{name}.cpp"
        source.write_text("\n".join(text) + "\n")
        sources.append(source)

    return sources


def arm(tree, work):
    """arm/files.txt's sources: of a function, ours including it and making
    its aliases; the others as they are."""
    sources = []

    for line in listed(ROOT / "arm" / "files.txt"):
        path, _, rest = line.partition(":")
        source = tree / path.strip()

        if rest.strip():
            result, name, types, names = function(rest, "arm/files.txt")
            text = [f'#include "{source}"', *aliases(result, name, types, names, "")]
            source = work / f"{name}.c"
            source.write_text("\n".join(text) + "\n")

        sources.append(source)

    return sources


def relocatable(ld, output, objects):
    """One object of many (wasm-ld -r), their names in a file: there are
    more than a command line holds."""
    listing = output.with_suffix(".rsp")
    listing.write_text("\n".join(str(obj) for obj in objects) + "\n")
    subprocess.run([ld, "-r", "-o", str(output), f"@{listing}"], check=True)


def main():
    cc, ld, freebsd_slice, llvm_slice, arm_tarball, output = sys.argv[1:7]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libc.wasm"
    shutil.rmtree(work, ignore_errors=True)
    work.mkdir(parents=True)
    freebsd = unpack(freebsd_slice, work / "freebsd")
    (freebsd / "lib" / "msun" / "src" / "math.h").unlink()
    patched(freebsd, ROOT / "freebsd" / "patches")
    llvm = patched(unpack(llvm_slice, work / "llvm"), ROOT / "llvm" / "patches")
    optimized = next(unpack(arm_tarball, work / "arm").glob("optimized-routines-*"))
    objects = work / "obj"
    units = []

    for source in sorted(ROOT.glob("*.c")):
        units.append((source, objects / "pizdos" / source.with_suffix(".o").name, FLAGS))

    # msun's kernels, __kernel_sin and its kind, are not the kernel's sin.
    kernels = set()

    for source in (freebsd / "lib" / "msun").rglob("*.[ch]"):
        kernels.update(re.findall(r"\b__kernel_\w+", source.read_text(errors="replace")))

    msun = [*FREEBSD_FLAGS, "-I", str(freebsd / "lib" / "msun" / "src"), *INCLUDE,
            *(f"-D{name}=__freebsd{name[1:]}" for name in sorted(kernels))]
    libc = freebsd / "lib" / "libc"
    bsd = ["-O2", "-std=gnu11", "-w", "-include", str(ROOT / "freebsd" / "sys" / "cdefs.h"), "-I", str(ROOT / "freebsd"),
           "-I", str(libc / "locale"), "-I", str(libc / "stdio"), "-I", str(freebsd / "include"), *INCLUDE]

    for line in listed(ROOT / "freebsd" / "files.txt"):
        path, *flags = shlex.split(line)

        if path.startswith("lib/libc/"):
            # Named by the path, as its files of one name are of more than one place.
            units.append((freebsd / path, objects / "freebsd" / "-".join(Path(path).with_suffix(".o").parts[2:]), bsd + flags))
        else:
            units.append((freebsd / path, objects / "freebsd" / Path(path).with_suffix(".o").name, msun + flags))

    for source in sorted((ROOT / "freebsd").glob("*.c")):
        units.append((source, objects / "pizdos" / ("freebsd-" + source.with_suffix(".o").name), bsd))

    (work / "llvm-wrappers").mkdir()

    for source in wrappers(work / "llvm-wrappers", llvm / "libc"):
        units.append((source, objects / "llvm" / source.with_suffix(".o").name,
                      LLVM_FLAGS + ["-DLIBC_COPT_PUBLIC_PACKAGING", "-I", str(llvm / "libc"), *INCLUDE]))

    (work / "arm-wrappers").mkdir()
    math = optimized / "math"
    # Its internal names, which its headers map from reserved ones to
    # arm_math_, a program's to name: reserved again.
    internal = set()

    for header in math.glob("*.h"):
        internal.update(re.findall(r"#\s*define\s+__\w+\s+(arm_math_\w+)", header.read_text()))

    renames = [f"-D{name}=__{name}" for name in sorted(internal)]

    for source in arm(optimized, work / "arm-wrappers"):
        units.append((source, objects / "arm" / source.with_suffix(".o").name,
                      ARM_FLAGS + renames + ["-I", str(math / "include"), "-I", str(math), *INCLUDE]))

    seen = {}

    for source, obj, flags in units:
        if obj in seen:
            raise SystemExit(f"{source} and {seen[obj]} make one object")

        seen[obj] = source

    forwarders = (LIBC / "src" / "kernel.c", objects / "kernel.o", FORWARDER_FLAGS)
    runtime = (LIBC / "src" / "builtins.c", objects / "builtins.o", FORWARDER_FLAGS)
    wasm.compile_all(cc, [*units, forwarders, runtime])

    functions = work / "functions.o"
    relocatable(ld, functions, [obj for source, obj, flags in units])
    functions.write_bytes(symbols.named(functions.read_bytes(), forwarders[1].read_bytes()))
    Path(output).parent.mkdir(parents=True, exist_ok=True)
    relocatable(ld, Path(output), [functions, forwarders[1], runtime[1]])


if __name__ == "__main__":
    main()
