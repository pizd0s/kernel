#!/usr/bin/env python3
"""Build lib/c's in-process archive: our own sources, the musl subset in
musl/files.txt, FreeBSD's sources in freebsd/files.txt, LLVM libc's
functions in llvm/files.txt, Arm's optimized-routines' in arm/files.txt
and Bionic's sources in bionic/files.txt.

    make.py <cc> <ar> <musl-tarball> <freebsd-slice> <llvm-slice> <arm-tarball> <bionic-slice> <libc.a>
    make.py headers <include.tar>

The second packs include/, the headers as a program built inside the system
finds them.

A .h line of musl/files.txt names a musl internal header to keep; every
other header of src/internal is deleted, so a quoted include of it falls
through to our replacement in musl/. musl's public headers are never on the
path.

The FreeBSD and LLVM slices are what freebsd/slice.txt and llvm/slice.txt
cut out of their release tarballs (dev/slice.py). FreeBSD's sources build
over our headers with freebsd/'s for what they take of FreeBSD's own,
sys/cdefs.h included first in each; FreeBSD's math.h is deleted, so their
quoted include of it falls through to ours. A line of freebsd/files.txt is
a path from the tree's root and the file's own flags.

A line of llvm/files.txt is a C prototype, then the names of the same
function: each becomes a C++ source of its own, the function calling LLVM
libc's of its name in libc/shared/math, header-only, and weak aliases of
it; or a source of LLVM libc's alone, which gives its C name itself.
llvm/patches/ are fixes of LLVM libc's sources for wasm32, each saying
why, applied to the slice first. A line of arm/files.txt is a source of optimized-routines' math/ and
such a prototype and names, which a C source of ours includes it with and
makes the aliases of; or a source alone. They build for the one rounding
mode wasm has, to nearest, and set no errno, as math_errhandling says,
with none of the names glibc's ABI adds.

Bionic's sources build over our headers with bionic/'s for what they take
of Bionic's own, sys/cdefs.h included first in each, its private headers,
its stdio's and its OpenBSD compatibility on the path; those it carries of
OpenBSD with openbsd-compat.h first, of FreeBSD with freebsd-compat.h, as
Bionic builds them. A line of
bionic/files.txt is a path from the tree's root and the file's own flags.
bionic/patches/ are changes of Bionic's sources for lib/c, each saying
why, applied to the slice first; bionic/*.cpp are lib/c's own, built as
Bionic's are, over its FILE.
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
sys.path.insert(0, str(ROOT.parent.parent / "dev"))

import wasm  # noqa: E402

FLAGS = ["-O2", "-std=c99", "-D_XOPEN_SOURCE=700", "-w"]
FREEBSD_FLAGS = ["-O2", "-std=c11", "-w", "-include", str(ROOT / "freebsd" / "sys" / "cdefs.h"), "-I", str(ROOT / "freebsd")]
LLVM_FLAGS = ["-O2", "-std=c++17", "-fno-exceptions", "-fno-rtti", "-nostdinc++", "-w", "-DLIBC_NAMESPACE=__llvm_libc"]
ARM_FLAGS = ["-O2", "-std=c11", "-w", "-DWANT_ROUNDING=0", "-DWANT_ERRNO=0", "-DUSE_GLIBC_ABI=0"]
BIONIC_C_FLAGS = ["-O2", "-std=gnu11", "-w"]
BIONIC_CXX_FLAGS = ["-O2", "-std=gnu++20", "-fno-exceptions", "-fno-rtti", "-nostdinc++", "-w"]


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


def main():
    if sys.argv[1] == "headers":
        wasm.write_tar(sys.argv[2], ROOT, ROOT / "include")

        return

    cc, ar, tarball, freebsd_slice, llvm_slice, arm_tarball, bionic_slice, output = sys.argv[1:9]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libc"
    shutil.rmtree(work, ignore_errors=True)
    work.mkdir(parents=True)

    with tarfile.open(tarball) as archive:
        archive.extractall(work, filter="data")

    tree = next(work.glob("musl-*"))
    musl = tree / "src"
    lines = list(listed(ROOT / "musl" / "files.txt"))
    kept = {musl / line for line in lines if line.endswith(".h")}

    for header in (musl / "internal").glob("*.h"):
        if header not in kept:
            header.unlink()

    freebsd = unpack(freebsd_slice, work / "freebsd")
    (freebsd / "lib" / "msun" / "src" / "math.h").unlink()
    llvm = unpack(llvm_slice, work / "llvm")

    for fix in sorted((ROOT / "llvm" / "patches").glob("*.patch")):
        subprocess.run(["patch", "-p1", "--batch", "--forward", "-s", "-d", str(llvm), "-i", str(fix)], check=True)
    optimized = next(unpack(arm_tarball, work / "arm").glob("optimized-routines-*"))
    bionic = unpack(bionic_slice, work / "bionic")

    for change in sorted((ROOT / "bionic" / "patches").glob("*.patch")):
        subprocess.run(["patch", "-p1", "--batch", "--forward", "-s", "-d", str(bionic), "-i", str(change)], check=True)

    includes = [
        "-include", str(ROOT / "musl" / "features.h"),
        "-I", str(ROOT / "src"),
        "-I", str(ROOT / "musl"),
        "-I", str(musl / "internal"),
        "-I", str(tree / "arch" / "generic"),
        "-isystem", str(ROOT / "include"),
    ]
    units = []

    for line in lines:
        if not line.endswith(".h"):
            units.append((musl / line, work / "obj" / Path(line).with_suffix(".o"), FLAGS + includes))

    for source in sorted((ROOT / "src").glob("*.c")):
        units.append((source, work / "obj" / "pizdos" / source.with_suffix(".o").name, FLAGS + includes))

    msun = ["-I", str(freebsd / "lib" / "msun" / "src"), "-isystem", str(ROOT / "include")]

    for line in listed(ROOT / "freebsd" / "files.txt"):
        path, *flags = shlex.split(line)
        units.append((freebsd / path, work / "obj" / "freebsd" / Path(path).with_suffix(".o").name,
                      FREEBSD_FLAGS + msun + flags))

    (work / "llvm-wrappers").mkdir()

    for source in wrappers(work / "llvm-wrappers", llvm / "libc"):
        units.append((source, work / "obj" / "llvm" / source.with_suffix(".o").name,
                      LLVM_FLAGS + ["-DLIBC_COPT_PUBLIC_PACKAGING", "-I", str(llvm / "libc"), "-isystem", str(ROOT / "include")]))

    compat = bionic / "libc" / "upstream-openbsd" / "android" / "include"
    freebsd_compat = bionic / "libc" / "upstream-freebsd" / "android" / "include"
    openbsd_libc = bionic / "libc" / "upstream-openbsd" / "lib" / "libc"
    paths = ["-include", str(ROOT / "bionic" / "sys" / "cdefs.h"), "-I", str(ROOT / "bionic"), "-I", str(compat),
             "-I", str(bionic / "libc"), "-I", str(bionic / "libc" / "private"), "-I", str(bionic / "libc" / "stdio"),
             "-I", str(openbsd_libc / "include"), "-I", str(openbsd_libc / "stdio"), "-isystem", str(ROOT / "include")]

    for line in listed(ROOT / "bionic" / "files.txt"):
        path, *flags = shlex.split(line)
        source = bionic / path
        language = BIONIC_CXX_FLAGS if source.suffix == ".cpp" else BIONIC_C_FLAGS
        upstream = []

        if path.startswith("libc/upstream-openbsd/"):
            # Bionic's switch for its OpenBSD sources over musl's headers,
            # which lib/c's are of: explicit_bzero is the libc's.
            upstream = ["-DANDROID_HOST_MUSL", "-include", str(compat / "openbsd-compat.h")]
        elif path.startswith("libc/upstream-freebsd/"):
            upstream = ["-include", str(freebsd_compat / "freebsd-compat.h"), "-I", str(freebsd_compat)]

        # Named by the path, as Bionic's files of one name are of more than one place.
        obj = "-".join(Path(path).with_suffix(".o").parts[1:])
        units.append((source, work / "obj" / "bionic" / obj, language + paths + upstream + flags))

    for source in sorted((ROOT / "bionic").glob("*.cpp")):
        units.append((source, work / "obj" / "pizdos" / source.with_suffix(".o").name, BIONIC_CXX_FLAGS + paths))

    (work / "arm-wrappers").mkdir()
    math = optimized / "math"
    # Its internal names, which its headers map from reserved ones to
    # arm_math_, a program's to name: reserved again.
    internal = set()

    for header in math.glob("*.h"):
        internal.update(re.findall(r"#\s*define\s+__\w+\s+(arm_math_\w+)", header.read_text()))

    renames = [f"-D{name}=__{name}" for name in sorted(internal)]

    for source in arm(optimized, work / "arm-wrappers"):
        units.append((source, work / "obj" / "arm" / source.with_suffix(".o").name,
                      ARM_FLAGS + renames + ["-I", str(math / "include"), "-I", str(math), "-isystem", str(ROOT / "include")]))

    # An archive's members are named by their files' names: one name twice
    # would be one member.
    seen = {}

    for source, obj, flags in units:
        if obj.name in seen:
            raise SystemExit(f"{source} and {seen[obj.name]} make objects of one name")

        seen[obj.name] = source

    wasm.compile_all(cc, units)
    wasm.archive(ar, output, [obj for source, obj, flags in units])


if __name__ == "__main__":
    main()
