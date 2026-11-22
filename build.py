import build
import json
import os
import re
import shlex
import shutil


def tool(name):
    path = shutil.which(name)

    if path is None:
        raise RuntimeError(f"{name} is not on PATH")

    return path


def touch(path):
    return [
        "python3",
        "-c",
        f"from pathlib import Path; p=Path(r'{path}'); p.parent.mkdir(parents=True, exist_ok=True); p.touch()",
    ]


# Tool paths are absolute in every command line, so a toolchain change is a
# node UID change.
CLANG = tool("clang")
CLANGXX = tool("clang++")
LLVM_AR = tool("llvm-ar")
LLVM_RANLIB = tool("llvm-ranlib")
WASM_LD = tool("wasm-ld")
GO = tool("go")

MUSL = "1.2.5"
BUSYBOX = "1.36.1"
E2FSPROGS = "1.47.4"
DOOMGENERIC = "e257a09527062bfb986b6e5e0780839b6cbd5b71"
PYTHON = "3.14.10"
STR = "e96fca3f07464a01a4a0f52deb0f6e69be293bd3"
LLVM = "23.1.5"
MC = "4.8.33"
GLIB = "2.90.1"
SLANG = "2.3.3"
PCRE2 = "10.49"
NCURSES = "6.6"
FSTACK = "2.0"


def fetch(name, url, sha256, output):
    return command(
        name=name,
        local=True,
        inputs=["$(S)/dev/fetch.py"],
        outputs=[output],
        cmd=["python3", "$(S)/dev/fetch.py", url, sha256, output],
        descr="FE",
        color="blue",
    )


musl_tarball = fetch(
    "musl_tarball",
    f"https://musl.libc.org/releases/musl-{MUSL}.tar.gz",
    "a9a118bbe84d8764da0ea0d28b3ab3fae8477fc7e4085d90102b8596fc7c75e4",
    f"$(B)/ext/musl-{MUSL}.tar.gz",
)

busybox_tarball = fetch(
    "busybox_tarball",
    f"https://busybox.net/downloads/busybox-{BUSYBOX}.tar.bz2",
    "b8cc24c9574d809e7279c3be349795c5d5ceb6fdf19ca709f80cde50e47de314",
    f"$(B)/ext/busybox-{BUSYBOX}.tar.bz2",
)

fstack_tarball = fetch(
    "fstack_tarball",
    f"https://github.com/F-Stack/f-stack/archive/refs/tags/v{FSTACK}.tar.gz",
    "edc80768b31f1582044e9e4183b97160ce2abb887265b3a98db59f1522b6a2ad",
    f"$(B)/ext/f-stack-{FSTACK}.tar.gz",
)

doomgeneric_tarball = fetch(
    "doomgeneric_tarball",
    f"https://github.com/ozkl/doomgeneric/archive/{DOOMGENERIC}.tar.gz",
    "26f8f60daa6c6e4d3c01e5eac9fb700d592dede803d13ac848154ff356b2a2e7",
    f"$(B)/ext/doomgeneric-{DOOMGENERIC}.tar.gz",
)

e2fsprogs_tarball = fetch(
    "e2fsprogs_tarball",
    f"https://mirrors.edge.kernel.org/pub/linux/kernel/people/tytso/e2fsprogs/v{E2FSPROGS}/e2fsprogs-{E2FSPROGS}.tar.xz",
    "fd5bf388cbdbe006a3d3b318d983b2948382440acc85a87f1e7d108653e8db0b",
    f"$(B)/ext/e2fsprogs-{E2FSPROGS}.tar.xz",
)

python_tarball = fetch(
    "python_tarball",
    f"https://www.python.org/ftp/python/{PYTHON}/Python-{PYTHON}.tgz",
    "9e4996c19ae5a3df31f10c37723661f71d3455d73bd005f79df7098e5cc13192",
    f"$(B)/ext/Python-{PYTHON}.tgz",
)

str_tarball = fetch(
    "str_tarball",
    f"https://github.com/pg83/str/archive/{STR}.tar.gz",
    "0ecc2afb9b111cf0890c0482c81bdbfdb30d1b83cb3f9ea738c82b64b3f2bbec",
    f"$(B)/ext/str-{STR}.tar.gz",
)

llvm_tarball = fetch(
    "llvm_tarball",
    f"https://github.com/llvm/llvm-project/releases/download/llvmorg-{LLVM}/llvm-project-{LLVM}.src.tar.xz",
    "fb50b1eac93ddeb3aa8dab4cfe324dff74f4a9c2bdee6160c9e36188f8d06a6e",
    f"$(B)/ext/llvm-project-{LLVM}.src.tar.xz",
)

# Midnight Commander and what it stands on (bin/mc); ncurses for its
# source of the terminals' descriptions (dev/terminfo.py).
mc_tarball = fetch(
    "mc_tarball",
    f"http://ftp.midnight-commander.org/mc-{MC}.tar.xz",
    "cae149d42f844e5185d8c81d7db3913a8fa214c65f852200a9d896b468af164c",
    f"$(B)/ext/mc-{MC}.tar.xz",
)

glib_tarball = fetch(
    "glib_tarball",
    f"https://download.gnome.org/sources/glib/{GLIB.rsplit('.', 1)[0]}/glib-{GLIB}.tar.xz",
    "93c941aa17d5eb1d53fe838365f29a8b4e539c222a256d974ec8f30fc413e396",
    f"$(B)/ext/glib-{GLIB}.tar.xz",
)

slang_tarball = fetch(
    "slang_tarball",
    f"https://www.jedsoft.org/releases/slang/slang-{SLANG}.tar.bz2",
    "f9145054ae131973c61208ea82486d5dd10e3c5cdad23b7c4a0617743c8f5a18",
    f"$(B)/ext/slang-{SLANG}.tar.bz2",
)

pcre2_tarball = fetch(
    "pcre2_tarball",
    f"https://github.com/PCRE2Project/pcre2/releases/download/pcre2-{PCRE2}/pcre2-{PCRE2}.tar.bz2",
    "53c156e1ba416a20da8e65395daa132da0d80e76910424caca3fcdae7831d384",
    f"$(B)/ext/pcre2-{PCRE2}.tar.bz2",
)

ncurses_tarball = fetch(
    "ncurses_tarball",
    f"https://ftp.gnu.org/gnu/ncurses/ncurses-{NCURSES}.tar.gz",
    "355b4cbbed880b0381a04c46617b7656e362585d52e9cf84a67e2009b749ff11",
    f"$(B)/ext/ncurses-{NCURSES}.tar.gz",
)

# doom1.wad of the shareware 1.9, the last one id released
doom_wad = fetch(
    "doom_wad",
    "https://silicon-pirates.com/doom/doom1.wad",
    "1d7d43be501e67d927e415e0b8f3e29c3bf33075e859721816f652a526cac771",
    "$(B)/ext/doom/doom1.wad",
)

LIBC_HEADERS = build.glob("$(S)/lib/c/include/**/*.h")

# lib/c: the in-process part of the C library, our sources plus the musl
# subset named in lib/c/musl/files.txt and FreeBSD's complex functions in
# lib/c/freebsd; every other libc function is a kernel import declared by its
# header.
libc = command(
    name="libc",
    inputs=[
        *LIBC_HEADERS,
        *build.glob("$(S)/lib/c/musl/*"),
        *build.glob("$(S)/lib/c/src/*"),
        *build.glob("$(S)/lib/c/freebsd/*"),
        "$(S)/lib/c/make.py",
        "$(S)/dev/wasm.py",
    ],
    outputs=["$(B)/lib/c/libc.a"],
    deps=[musl_tarball],
    cmd=["python3", "$(S)/lib/c/make.py", CLANG, LLVM_AR, musl_tarball.outputs[0], "$(B)/lib/c/libc.a"],
    descr="CC",
    color="green",
)

# lib/c's headers as a program compiled inside the system finds them.
libc_headers = command(
    name="libc_headers",
    inputs=[*LIBC_HEADERS, "$(S)/lib/c/make.py", "$(S)/dev/wasm.py"],
    outputs=["$(B)/lib/c/include.tar"],
    cmd=["python3", "$(S)/lib/c/make.py", "headers", "$(B)/lib/c/include.tar"],
    descr="HD",
    color="green",
)

# lib/cxx: libc++ and libc++abi of the LLVM release, for lib/c: the library
# and include/c++/v1, the headers a C++ program includes before lib/c's.
libcxx = command(
    name="libcxx",
    inputs=[*LIBC_HEADERS, "$(S)/lib/cxx/make.py", "$(S)/lib/cxx/__config_site", "$(S)/dev/wasm.py"],
    outputs=["$(B)/lib/cxx/libc++.a", "$(B)/lib/cxx/include.tar"],
    deps=[llvm_tarball],
    cmd=["python3", "$(S)/lib/cxx/make.py", CLANG, LLVM_AR, llvm_tarball.outputs[0], "$(S)/lib/c/include",
         "$(B)/lib/cxx/libc++.a", "$(B)/lib/cxx/include.tar"],
    descr="CC",
    color="green",
)

# lib/str: pg83/str's string and memory functions, compiled by str's own
# compiler in the convention of the code vm compiles, for its loader.
libstr = command(
    name="libstr",
    inputs=["$(S)/lib/str/make.py"],
    outputs=["$(B)/lib/str/libstr.o"],
    deps=[str_tarball],
    cmd=["python3", "$(S)/lib/str/make.py", str_tarball.outputs[0], "$(B)/lib/str/libstr.o"],
    descr="CC",
    color="green",
)

# The executable of a program of the kernel (dev/exe.py): the PZD1 header,
# its module compressed by zstd, and the tail of the program's own; the
# kernel runs nothing else. The module, the .wasm, stays what the compilers
# and the ABI checks read.
def packing(module, output, tail=None, level=None):
    """The command that packs a module into an executable (dev/exe.py)."""
    return ["python3", "$(S)/dev/exe.py", module, output, *(["--tail", tail] if tail else []), *(["--level", str(level)] if level else [])]


def executable(name, program, output, tail=None, level=None, deps=()):
    return command(
        name=name,
        inputs=["$(S)/dev/exe.py"],
        outputs=[output],
        deps=[program, *deps],
        cmd=packing(program.outputs[0], output, tail, level),
        descr="PK",
        color="cyan",
    )


# Packages as ix makes them (dev/pkg.py): a package is its directory of the
# store, /pzd/store/<uid>-<name>, the uid its recipe's ($(UID)), and its
# file the tar.zst of that directory, $(B)/pkg/<name>.tar.zst, its last
# output. A recipe whose program names where it is, as mc its data, python
# its prefix and doom its IWAD, builds in the package's own node: the
# directory is its own uid's.
STORE = "/pzd/store"


def store_name(name):
    return name.replace("/", "-")


def store(name):
    """The package's directory of the store, as its own recipe names it."""
    return f"{STORE}/$(UID)-{store_name(name)}"


def package(name, contents, deps=(), inputs=(), recipe=(), outputs=(), descr="PG", color="cyan"):
    """A package: the recipe's commands, then its directory laid out of
    contents, pkg.py pack's arguments."""
    tarball = f"$(B)/pkg/{store_name(name)}.tar.zst"
    return command(
        name="pkg_" + store_name(name).replace("-", "_"),
        inputs=["$(S)/dev/pkg.py", *inputs],
        outputs=[*outputs, tarball],
        deps=deps,
        cmd=[*recipe, ["python3", "$(S)/dev/pkg.py", "pack", tarball, store(name), *contents]],
        descr=descr,
        color=color,
    )


busybox = command(
    name="busybox",
    inputs=[
        *LIBC_HEADERS,
        "$(S)/bin/busybox/config",
        "$(S)/bin/busybox/gen.py",
        "$(S)/bin/busybox/make.py",
        "$(S)/dev/wasm.py",
    ],
    outputs=["$(B)/bin/busybox/busybox.wasm", "$(B)/bin/busybox/busybox.links"],
    deps=[busybox_tarball, libc],
    cmd=[
        "python3", "$(S)/bin/busybox/make.py",
        CLANG, LLVM_AR, WASM_LD,
        busybox_tarball.outputs[0],
        "$(S)/bin/busybox/config",
        "$(S)/lib/c/include",
        libc.outputs[0],
        "$(B)/bin/busybox/busybox.wasm",
        "$(B)/bin/busybox/busybox.links",
    ],
    descr="BB",
    color="cyan",
)
busybox_exe = executable("busybox_exe", busybox, "$(B)/bin/busybox/busybox")
busybox_pkg = package("bin/busybox", ["--file", "bin/busybox", busybox_exe.outputs[0], "--links", "bin", "busybox", busybox.outputs[1]],
                      deps=[busybox, busybox_exe])

# The kernel ABI busybox needs: every import and export of the module.
busybox_abi = command(
    name="busybox_abi",
    inputs=["$(S)/dev/wasm_info.py"],
    outputs=["$(B)/bin/busybox/abi.txt"],
    deps=[busybox],
    cmd=["python3", "$(S)/dev/wasm_info.py", busybox.outputs[0], "$(B)/bin/busybox/abi.txt"],
    descr="AB",
    color="magenta",
)

# doom: doomgeneric with our terminal backend, drawn as ANSI half blocks on
# the tty the kernel passes through, and the shareware IWAD, which it
# finds in its own share/doom.
doom = package(
    "bin/doom",
    ["--file", "bin/doom", "$(B)/bin/doom/doom", "--file", "share/doom/doom1.wad", doom_wad.outputs[0]],
    inputs=[
        *LIBC_HEADERS,
        "$(S)/bin/doom/doomgeneric_pizdos.c",
        "$(S)/bin/doom/make.py",
        "$(S)/dev/wasm.py",
        "$(S)/dev/exe.py",
    ],
    outputs=["$(B)/bin/doom/doom.wasm", "$(B)/bin/doom/doom"],
    deps=[doomgeneric_tarball, libc, doom_wad],
    recipe=[
        [
            "python3", "$(S)/bin/doom/make.py",
            CLANG, WASM_LD,
            doomgeneric_tarball.outputs[0],
            "$(S)/bin/doom/doomgeneric_pizdos.c",
            "$(S)/lib/c/include",
            libc.outputs[0],
            "$(B)/bin/doom/doom.wasm",
            store("bin/doom") + "/share/doom",
        ],
        packing("$(B)/bin/doom/doom.wasm", "$(B)/bin/doom/doom"),
    ],
    descr="DM",
    color="light-magenta",
)

# CPython's interpreter and built-in modules, and its freezer: nothing of
# vm, so a change of the compiler rebuilds none of it.
python_core = command(
    name="python_core",
    inputs=[*LIBC_HEADERS, "$(S)/bin/python/pyconfig.h", "$(S)/bin/python/make.py", "$(S)/dev/wasm.py"],
    outputs=["$(B)/bin/python/libpython.a", "$(B)/bin/python/freeze.wasm"],
    deps=[python_tarball, libc],
    cmd=[
        "python3", "$(S)/bin/python/make.py", "core",
        CLANG, LLVM_AR, WASM_LD,
        python_tarball.outputs[0],
        "$(S)/bin/python/pyconfig.h",
        "$(S)/lib/c/include",
        libc.outputs[0],
        "$(B)/bin/python/libpython.a",
        "$(B)/bin/python/freeze.wasm",
    ],
    descr="PY",
    color="yellow",
)

# bin/aot is a single C++ translation unit, a command: the module comes on
# fd 0, the object goes to fd 1, the options are its arguments. Its wasm
# build is a program of the system on lib/c, the one the kernel runs as its
# compiler; the native build is the same program on the host's libc.
AOT_FLAGS = ["-x", "c++", "-std=c++20", "-O2", "-fno-exceptions", "-fno-rtti", "-fno-builtin", "-fwrapv", "-ffp-contract=off", "-Wno-parentheses-equality"]
# A program's flags and link (dev/wasm.py, bin/tools/make.py). The
# multivalue ABI passes and returns small structs, the compiler's slices and
# views among them, as values instead of through the shadow stack; what it
# calls of lib/c takes and returns scalars only.
AOT_WASM_FLAGS = [*AOT_FLAGS, "--target=wasm32-unknown-unknown", "-Xclang", "-mlong-double-64", "-mbulk-memory", "-nostdinc", "-nostdinc++",
                  "-nostdlib", "-isystem", "$(S)/lib/c/include", "-ffunction-sections", "-fdata-sections",
                  "-Xclang", "-target-abi", "-Xclang", "experimental-mv", "-Wl,--stack-first", "-Wl,--max-memory=4294967296",
                  "-Wl,--export=__heap_base", "-Wl,--export=__stack_pointer", "-Wl,--gc-sections"]

aot = command(
    name="aot",
    inputs=["$(S)/bin/aot/aot.cpp", *LIBC_HEADERS],
    outputs=["$(B)/bin/aot/aot.wasm"],
    deps=[libc],
    cmd=[CLANG, *AOT_WASM_FLAGS, "$(S)/bin/aot/aot.cpp", "-x", "none", libc.outputs[0], "-o", "$(B)/bin/aot/aot.wasm"],
    descr="CC", color="green",
)

# A native build needs host libc headers/libraries, supplied by the usual
# CPPFLAGS/LDFLAGS. The C driver links libc without a C++ runtime.
aot_native = command(
    name="aot_native",
    inputs=["$(S)/bin/aot/aot.cpp"],
    outputs=["$(B)/bin/aot/aot"],
    cmd=[CLANG, *build.cppflags, *build.cxxflags, *AOT_FLAGS, "-O3", "$(S)/bin/aot/aot.cpp", "-o", "$(B)/bin/aot/aot", *build.ldflags],
    descr="CC", color="green",
)

coverage_profile_runtime = os.environ.get("COVERAGE_PROFILE_RUNTIME", "")
coverage_native_inputs = ["$(S)/bin/aot/aot.cpp", "$(S)/dev/coverage_build.py", "$(S)/dev/coverage_native.c", *([coverage_profile_runtime] if coverage_profile_runtime else [])]
coverage_cppflags = shlex.split(os.environ["COVERAGE_CPPFLAGS"]) if "COVERAGE_CPPFLAGS" in os.environ else build.cppflags
coverage_ldflags = shlex.split(os.environ["COVERAGE_LDFLAGS"]) if "COVERAGE_LDFLAGS" in os.environ else build.ldflags
coverage_native_tools = {"cc": CLANG, "cppflags": coverage_cppflags, "cxxflags": build.cxxflags, "ldflags": coverage_ldflags, "flags": AOT_FLAGS, "profile_runtime": coverage_profile_runtime}
aot_coverage = command(
    name="aot_coverage",
    inputs=coverage_native_inputs,
    outputs=["$(B)/coverage/bin/aot"],
    cmd=["python3", "$(S)/dev/coverage_build.py", "$(S)/bin/aot/aot.cpp", "$(S)/dev/coverage_native.c",
         "$(B)/coverage/bin/aot", json.dumps(coverage_native_tools)],
    descr="CC", color="green",
)

aot_chaos_coverage = command(
    name="aot_chaos_coverage",
    inputs=coverage_native_inputs,
    outputs=["$(B)/coverage/chaos/aot"],
    cmd=["python3", "$(S)/dev/coverage_build.py", "$(S)/bin/aot/aot.cpp", "$(S)/dev/coverage_native.c",
         "$(B)/coverage/chaos/aot", json.dumps({**coverage_native_tools, "chaos": True})],
    descr="CC", color="green",
)

aot_measure = command(
    name="aot_measure",
    inputs=["$(S)/dev/aot_measure.c"],
    outputs=["$(B)/dev/aot_measure"],
    cmd=[CLANG, *build.cppflags, "-O2", "$(S)/dev/aot_measure.c", "-o", "$(B)/dev/aot_measure", *build.ldflags],
    descr="CC", color="green",
)

# Bootstrap without an existing vm: the native C++ compiler compiles the
# wasm compiler to the machine-code object embedded in the Go loader.
aot_aot = command(
    name="aot_aot",
    inputs=["$(S)/dev/aot.py"],
    outputs=["$(B)/bin/aot/aot.aot"],
    deps=[aot_native, aot],
    cmd=["python3", "$(S)/dev/aot.py", aot_native.outputs[0], aot.outputs[0], "$(B)/bin/aot/aot.aot"],
    descr="AO", color="light-cyan",
)

aot_native_chaos = command(
    name="aot_native_chaos",
    inputs=["$(S)/bin/aot/aot.cpp"],
    outputs=["$(B)/chaos/bin/aot"],
    cmd=[CLANG, *build.cppflags, *build.cxxflags, *AOT_FLAGS, "-O3", "-DAOT_CHAOS", "$(S)/bin/aot/aot.cpp",
         "-o", "$(B)/chaos/bin/aot", *build.ldflags],
    descr="CC", color="green",
)

aot_wasm_chaos = command(
    name="aot_wasm_chaos",
    inputs=["$(S)/bin/aot/aot.cpp", *LIBC_HEADERS],
    outputs=["$(B)/chaos/bin/aot.wasm"],
    deps=[libc],
    cmd=[CLANG, *AOT_WASM_FLAGS, "-DAOT_CHAOS", "$(S)/bin/aot/aot.cpp", "-x", "none", libc.outputs[0], "-o", "$(B)/chaos/bin/aot.wasm"],
    descr="CC", color="green",
)

aot_chaos_aot = command(
    name="aot_chaos_aot",
    inputs=["$(S)/dev/aot.py"],
    outputs=["$(B)/chaos/compiler/aot.aot"],
    deps=[aot_native, aot_wasm_chaos],
    cmd=["python3", "$(S)/dev/aot.py", aot_native.outputs[0], aot_wasm_chaos.outputs[0], "$(B)/chaos/compiler/aot.aot"],
    descr="AO", color="light-cyan",
)

# Keep LLVM's coverage mapping in the Wasm module. Only coverage VMs embed
# these compilers; the production and chaos executables use the usual ones.
wasm_coverage = []
for chaos in (False, True):
    variant = "chaos/" if chaos else ""
    prefix = f"$(B)/coverage/{variant}compiler"
    compiler = command(
        name="aot_wasm_chaos_coverage" if chaos else "aot_wasm_coverage",
        inputs=["$(S)/bin/aot/aot.cpp", "$(S)/dev/coverage_wasm.c", "$(S)/dev/coverage_wasm_build.py", *LIBC_HEADERS],
        outputs=[prefix + "/aot.wasm"],
        deps=[libc],
        cmd=["python3", "$(S)/dev/coverage_wasm_build.py", CLANG, WASM_LD,
             "$(S)/bin/aot/aot.cpp", "$(S)/dev/coverage_wasm.c", prefix + "/aot.wasm",
             json.dumps({"flags": AOT_WASM_FLAGS, "chaos": chaos, "libc": libc.outputs[0]})],
        descr="CC", color="green",
    )
    embedded = command(
        name="aot_chaos_coverage_aot" if chaos else "aot_coverage_aot",
        inputs=["$(S)/dev/aot.py"],
        outputs=[prefix + "/aot.aot"],
        deps=[aot_native, compiler],
        cmd=["python3", "$(S)/dev/aot.py", aot_native.outputs[0], compiler.outputs[0], prefix + "/aot.aot"],
        descr="AO", color="light-cyan",
    )
    wasm_coverage.append((compiler, embedded))

WASM2O_SOURCES = [*build.glob("$(S)/bin/vm/*.go"), *build.glob("$(S)/bin/vm/*.s"), "$(S)/bin/vm/go.mod", "$(S)/bin/vm/go.sum", "$(S)/dev/go.py"]
GO_RUNTIME = [*build.glob("$(S)/lib/goruntime/*.go"), "$(S)/lib/goruntime/edits.py"]

# vm's modules (the zstd decoder of compressed modules), checked against
# go.sum, are fetched once; its builds have no network.
vm_modules = command(
    name="vm_modules",
    local=True,
    inputs=["$(S)/dev/go.py", "$(S)/bin/vm/go.mod", "$(S)/bin/vm/go.sum"],
    outputs=["$(B)/bin/vm/modules.tar"],
    cmd=["python3", "$(S)/dev/go.py", "download", GO, "$(S)/bin/vm", "$(B)/bin/vm/modules.tar"],
    descr="FE",
    color="blue",
)
VM_MODULES = ["--modules", vm_modules.outputs[0]]

vm = command(
    name="vm",
    inputs=[*WASM2O_SOURCES, *GO_RUNTIME],
    outputs=["$(B)/bin/vm/vm"],
    deps=[aot_aot, libstr, vm_modules],
    cmd=["python3", "$(S)/dev/go.py", "build", GO, "$(S)/bin/vm", "$(B)/bin/vm/vm", "--runtime", "$(S)/lib/goruntime", "--embed", aot_aot.outputs[0],
         "--embed", libstr.outputs[0], *VM_MODULES],
    descr="GO", color="light-blue",
)

runtime_abi_vms = []
for mode in ("version", "layout"):
    output = f"$(B)/tst/runtime_abi/{mode}"
    runtime_abi_vms.append(command(
        name=f"vm_runtime_{mode}",
        inputs=[*WASM2O_SOURCES, *GO_RUNTIME, "$(S)/tst/runtime_abi/make.py"],
        outputs=[output],
        deps=[aot_aot, libstr, vm_modules],
        cmd=["python3", "$(S)/tst/runtime_abi/make.py", GO, "$(S)", mode,
             aot_aot.outputs[0], libstr.outputs[0], vm_modules.outputs[0], output],
        descr="GO", color="light-blue",
    ))

vm_coverage = command(
    name="vm_coverage",
    inputs=[*WASM2O_SOURCES, *GO_RUNTIME, "$(S)/dev/coverage_collect.go"],
    outputs=["$(B)/coverage/bin/vm"],
    deps=[wasm_coverage[0][1], libstr, vm_modules],
    cmd=["python3", "$(S)/dev/go.py", "build", GO, "$(S)/bin/vm", "$(B)/coverage/bin/vm",
         "--cover", "--tags", "pizdoscoverage", "--runtime", "$(S)/lib/goruntime",
         "--embed", wasm_coverage[0][1].outputs[0], "--embed", libstr.outputs[0], "--embed", "$(S)/dev/coverage_collect.go", *VM_MODULES],
    descr="GO", color="light-blue",
)

vm_chaos = command(
    name="vm_chaos",
    inputs=[*WASM2O_SOURCES, *GO_RUNTIME],
    outputs=["$(B)/chaos/bin/vm"],
    deps=[aot_chaos_aot, libstr, vm_modules],
    cmd=["python3", "$(S)/dev/go.py", "build", GO, "$(S)/bin/vm", "$(B)/chaos/bin/vm", "--tags", "pizdoschaos",
         "--runtime", "$(S)/lib/goruntime", "--embed", aot_chaos_aot.outputs[0], "--embed", libstr.outputs[0], *VM_MODULES],
    descr="GO", color="light-blue",
)

vm_chaos_coverage = command(
    name="vm_chaos_coverage",
    inputs=[*WASM2O_SOURCES, *GO_RUNTIME, "$(S)/dev/coverage_collect.go"],
    outputs=["$(B)/coverage/chaos/vm"],
    deps=[wasm_coverage[1][1], libstr, vm_modules],
    cmd=["python3", "$(S)/dev/go.py", "build", GO, "$(S)/bin/vm", "$(B)/coverage/chaos/vm", "--cover", "--tags", "pizdoschaos,pizdoscoverage",
         "--runtime", "$(S)/lib/goruntime", "--embed", wasm_coverage[1][1].outputs[0], "--embed", libstr.outputs[0],
         "--embed", "$(S)/dev/coverage_collect.go", *VM_MODULES],
    descr="GO", color="light-blue",
)

# The tools of the system, its services and the commands that speak to
# them, in one multicall executable, /bin/tools, each a link to it in the
# image (bin/tools).
TOOLS = ["blk", "console", "devfs", "devices", "dns", "erofs", "mount", "pty", "sockmgr", "spawn", "tmpfs", "umount"]
PROTOCOL_SOURCES = build.glob("$(S)/lib/protocol/*.cpp")
PROTOCOL_HEADERS = build.glob("$(S)/lib/protocol/*.h")
TOOL_SOURCES = [*build.glob("$(S)/bin/tools/*.cpp"), *PROTOCOL_SOURCES]
TOOL_INPUTS = [*build.glob("$(S)/bin/tools/*.h"), *PROTOCOL_HEADERS, "$(S)/bin/tools/make.py", "$(S)/dev/wasm.py", *LIBC_HEADERS]
tools_module = command(
    name="tools_module",
    inputs=[*TOOL_SOURCES, *TOOL_INPUTS],
    outputs=["$(B)/bin/tools/tools.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/bin/tools/make.py", CLANG, WASM_LD, "$(S)/lib/c/include", libc.outputs[0], "$(B)/bin/tools/tools.wasm",
         *TOOL_SOURCES],
    descr="CC",
    color="cyan",
)
tools = executable("tools_exe", tools_module, "$(B)/bin/tools/tools")
tools_pkg = package("bin/tools", ["--file", "bin/tools", tools.outputs[0], *(arg for tool in TOOLS for arg in ("--link", "bin/" + tool, "tools"))],
                    deps=[tools])
# A service of tst/test_devices.py, which hands it to the system it runs.
devices_service = executable("devices_service_exe", command(
    name="devices_service_module",
    inputs=["$(S)/tst/devices_service.cpp", *PROTOCOL_SOURCES, *TOOL_INPUTS],
    outputs=["$(B)/tst/devices_service.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/bin/tools/make.py", CLANG, WASM_LD, "$(S)/lib/c/include", libc.outputs[0], "$(B)/tst/devices_service.wasm",
         "$(S)/tst/devices_service.cpp", *PROTOCOL_SOURCES],
    descr="CC",
    color="cyan",
), "$(B)/tst/devices_service")

# The sockets of tst/test_net.py's system, which it hands to the system.
inet_sockets = executable("inet_sockets_exe", command(
    name="inet_sockets_module",
    inputs=["$(S)/tst/inet_sockets.cpp", *TOOL_INPUTS],
    outputs=["$(B)/tst/inet_sockets.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/bin/tools/make.py", CLANG, WASM_LD, "$(S)/lib/c/include", libc.outputs[0], "$(B)/tst/inet_sockets.wasm",
         "$(S)/tst/inet_sockets.cpp"],
    descr="CC",
    color="cyan",
), "$(B)/tst/inet_sockets")

# The program of tst/test_watchdir.py, which hands it to the system.
watch_dir = executable("watch_dir_exe", command(
    name="watch_dir_module",
    inputs=["$(S)/tst/watch_dir.cpp", *PROTOCOL_SOURCES, *TOOL_INPUTS],
    outputs=["$(B)/tst/watch_dir.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/bin/tools/make.py", CLANG, WASM_LD, "$(S)/lib/c/include", libc.outputs[0], "$(B)/tst/watch_dir.wasm",
         "$(S)/tst/watch_dir.cpp", *PROTOCOL_SOURCES],
    descr="CC",
    color="cyan",
), "$(B)/tst/watch_dir")

# The probe of vm's network device of tst/test_net.py, which vm runs.
net_probe = executable("net_probe_exe", command(
    name="net_probe_module",
    inputs=["$(S)/tst/net_probe.cpp", *TOOL_INPUTS],
    outputs=["$(B)/tst/net_probe.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/bin/tools/make.py", CLANG, WASM_LD, "$(S)/lib/c/include", libc.outputs[0], "$(B)/tst/net_probe.wasm",
         "$(S)/tst/net_probe.cpp"],
    descr="CC",
    color="cyan",
), "$(B)/tst/net_probe")

# The sockets of tst/test_unix_sockets.py, which hands them to the system.
unix_sockets = executable("unix_sockets_exe", command(
    name="unix_sockets_module",
    inputs=["$(S)/tst/unix_sockets.cpp", *PROTOCOL_SOURCES, *TOOL_INPUTS],
    outputs=["$(B)/tst/unix_sockets.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/bin/tools/make.py", CLANG, WASM_LD, "$(S)/lib/c/include", libc.outputs[0], "$(B)/tst/unix_sockets.wasm",
         "$(S)/tst/unix_sockets.cpp", *PROTOCOL_SOURCES],
    descr="CC",
    color="cyan",
), "$(B)/tst/unix_sockets")

# fuse2fs, e2fsprogs' FUSE server of ext2, ext3 and ext4, on the system's
# libfuse (lib/fuse), which blk starts for such a partition (bin/fuse2fs).
FUSE_SOURCES = [*build.glob("$(S)/lib/fuse/*.cpp"), *build.glob("$(S)/lib/fuse/*.h")]
fuse2fs = command(
    name="fuse2fs",
    inputs=["$(S)/bin/fuse2fs/make.py", "$(S)/bin/fuse2fs/config.h", "$(S)/bin/fuse2fs/blkid.c", *FUSE_SOURCES, *PROTOCOL_SOURCES,
            *PROTOCOL_HEADERS, "$(S)/dev/wasm.py", *LIBC_HEADERS],
    outputs=["$(B)/bin/fuse2fs/fuse2fs.wasm"],
    deps=[libc, e2fsprogs_tarball],
    cmd=["python3", "$(S)/bin/fuse2fs/make.py", CLANG, WASM_LD, "$(S)/lib/c/include", libc.outputs[0], e2fsprogs_tarball.outputs[0],
         "$(B)/bin/fuse2fs/fuse2fs.wasm"],
    descr="CC",
    color="cyan",
)
fuse2fs_exe = executable("fuse2fs_exe", fuse2fs, "$(B)/bin/fuse2fs/fuse2fs")

# inet, the system's TCP/IP: FreeBSD's stack as F-Stack runs it in a
# program, on the network device of vm run --net (bin/inet).
inet = command(
    name="inet",
    inputs=["$(S)/bin/inet/make.py", "$(S)/bin/inet/fstack-wasm32.patch", "$(S)/bin/inet/names.h", "$(S)/bin/inet/glue.c",
            "$(S)/bin/inet/address.c", "$(S)/bin/inet/host.c", *build.glob("$(S)/bin/inet/*.h"), *build.glob("$(S)/bin/inet/*.cpp"),
            *build.glob("$(S)/bin/inet/machine/*.h"), *PROTOCOL_SOURCES, *PROTOCOL_HEADERS, "$(S)/dev/wasm.py", *LIBC_HEADERS],
    outputs=["$(B)/bin/inet/inet.wasm"],
    deps=[libc, fstack_tarball],
    cmd=["python3", "$(S)/bin/inet/make.py", CLANG, WASM_LD, "$(S)/lib/c/include", libc.outputs[0], fstack_tarball.outputs[0],
         "$(B)/bin/inet/inet.wasm"],
    descr="CC",
    color="cyan",
)
inet_exe = executable("inet_exe", inet, "$(B)/bin/inet/inet")
inet_pkg = package("bin/inet", ["--file", "bin/inet", inet_exe.outputs[0]], deps=[inet_exe])
fuse2fs_pkg = package("bin/fuse2fs", ["--file", "bin/fuse2fs", fuse2fs_exe.outputs[0]], deps=[fuse2fs_exe])

# The system's own /etc: its message of the day and its users, root alone.
base_etc = package("etc/base", ["--tree", "etc", "$(S)/bin/rootfs/etc"], inputs=build.glob("$(S)/bin/rootfs/etc/*"))

# rootfs, the first process and the VFS, has its image after it in its
# file: a system of one executable (bin/rootfs), which vm gives the image
# of as a disk. erofs of the tools serves the image at the root, with /dev
# and /run of its own, to the shell rootfs starts, and the kernel compiles
# each module as it is executed. An image is packages and their realm
# (bin/rootfs/image.py). The base system has busybox, the tools and
# fuse2fs: its rootfs runs python's freezer as python builds.
BASE = [busybox_pkg, tools_pkg, fuse2fs_pkg, inet_pkg, base_etc]
rootfs_module = command(
    name="rootfs_module",
    inputs=["$(S)/bin/rootfs/rootfs.cpp", "$(S)/lib/protocol/protocol.cpp", "$(S)/lib/protocol/protocol.h", "$(S)/bin/rootfs/make.py",
            "$(S)/dev/wasm.py", *LIBC_HEADERS],
    outputs=["$(B)/bin/rootfs/rootfs.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/bin/rootfs/make.py", CLANG, WASM_LD, "$(S)/lib/c/include", libc.outputs[0], "$(B)/bin/rootfs/rootfs.wasm",
         "$(S)/bin/rootfs/rootfs.cpp", "$(S)/lib/protocol/protocol.cpp"],
    descr="CC",
    color="cyan",
)


def image(name, output, packages, arguments=(), deps=()):
    """The image of packages: their realm, the system, as ix links one
    (dev/pkg.py), and the root that names it, with the files of
    arguments, image.py's."""
    realm = f"$(B)/realm/{name}.tar.zst"
    linked = command(
        name=name + "_realm",
        inputs=["$(S)/dev/pkg.py"],
        outputs=[realm],
        deps=packages,
        cmd=["python3", "$(S)/dev/pkg.py", "realm", realm, store("rlm/system"), *(node.outputs[-1] for node in packages)],
        descr="RL",
        color="cyan",
    )
    return command(
        name=name,
        inputs=["$(S)/bin/rootfs/image.py", "$(S)/dev/pkg.py"],
        outputs=[output],
        deps=[linked, *packages, *deps],
        cmd=["python3", "$(S)/bin/rootfs/image.py", output, "--realm", realm, *(arg for node in packages for arg in ("--package", node.outputs[-1])),
             *arguments, "--server", "/bin/tools"],
        descr="IM",
        color="cyan",
    )


def system(name, packages, directory):
    """The image of a system and the rootfs of it: rootfs's executable with
    the image after it."""
    built = image(name + "_image", f"$(B)/{directory}/rootfs.img", packages)
    return built, executable(name, rootfs_module, f"$(B)/{directory}/rootfs", tail=built.outputs[0], deps=[built])


base_image, rootfs_base = system("rootfs_base", BASE, "bin/rootfs/base")

# python.wasm, its modules frozen by the freezer running in the base system, and
# the standard library as a zip; python3, the executable, ends with that
# zip, which python imports from its own file.
python = package(
    "bin/python",
    ["--file", "bin/python3", "$(B)/bin/python/python3", "--link", "bin/python", "python3"],
    inputs=[*LIBC_HEADERS, "$(S)/bin/python/pyconfig.h", "$(S)/bin/python/make.py", "$(S)/bin/python/main.c", "$(S)/dev/wasm.py",
            "$(S)/dev/exe.py"],
    outputs=["$(B)/bin/python/python.wasm", "$(B)/bin/python/stdlib.zip", "$(B)/bin/python/python3"],
    deps=[python_tarball, libc, python_core, vm, rootfs_base],
    recipe=[
        [
            "python3", "$(S)/bin/python/make.py", "python",
            CLANG, WASM_LD, vm.outputs[0], rootfs_base.outputs[0],
            python_tarball.outputs[0],
            "$(S)/bin/python/pyconfig.h",
            "$(S)/lib/c/include",
            libc.outputs[0],
            *python_core.outputs,
            "$(B)/bin/python/python.wasm",
            "$(B)/bin/python/stdlib.zip",
            store("bin/python"),
        ],
        packing("$(B)/bin/python/python.wasm", "$(B)/bin/python/python3", tail="$(B)/bin/python/stdlib.zip"),
    ],
    descr="PY",
    color="yellow",
)

# The terminfo database, aux/terminfo's share/terminfo: every terminal
# ncurses describes, as the console's type is the host's to name (TERM).
terminfo = command(
    name="terminfo",
    inputs=["$(S)/dev/terminfo.py"],
    outputs=["$(B)/share/terminfo.tar"],
    deps=[ncurses_tarball],
    cmd=["python3", "$(S)/dev/terminfo.py", ncurses_tarball.outputs[0], "$(B)/share/terminfo.tar"],
    descr="TI",
    color="cyan",
)
terminfo_pkg = package("aux/terminfo", ["--tar", "share/terminfo", terminfo.outputs[0], "terminfo"], deps=[terminfo])

# mc: Midnight Commander on GLib and S-Lang, both with bin/mc's patches,
# mc.wasm, and mc.tar, what it reads as it runs: its configuration, skins,
# syntax rules and scripts, in its package, and the terminals'
# descriptions in terminfo's; mcedit, mcview and mcdiff are mc by another
# name.
mc = package(
    "bin/mc",
    ["--file", "bin/mc", "$(B)/bin/mc/mc", *(arg for name in ("mcedit", "mcview", "mcdiff") for arg in ("--link", "bin/" + name, "mc")),
     *(arg for member in ("etc/mc", "share/mc", "libexec/mc") for arg in ("--tar", member, "$(B)/bin/mc/mc.tar", member))],
    inputs=[*LIBC_HEADERS, "$(S)/bin/mc/make.py", "$(S)/bin/mc/mc-config.h", "$(S)/bin/mc/glib-config.h", "$(S)/bin/mc/slang-config.h",
            *build.glob("$(S)/bin/mc/*.patch"), "$(S)/dev/wasm.py", "$(S)/dev/exe.py"],
    outputs=["$(B)/bin/mc/mc.wasm", "$(B)/bin/mc/mc.tar", "$(B)/bin/mc/mc"],
    deps=[mc_tarball, glib_tarball, slang_tarball, pcre2_tarball, libc, terminfo_pkg],
    recipe=[
        ["python3", "$(S)/bin/mc/make.py", CLANG, WASM_LD, "$(S)/lib/c/include", libc.outputs[0], mc_tarball.outputs[0],
         glib_tarball.outputs[0], slang_tarball.outputs[0], pcre2_tarball.outputs[0], "$(B)/bin/mc/mc.wasm", "$(B)/bin/mc/mc.tar",
         store("bin/mc"), terminfo_pkg.outputs[-1]],
        packing("$(B)/bin/mc/mc.wasm", "$(B)/bin/mc/mc"),
    ],
    descr="CC",
    color="cyan",
)

# The system: the base system, python, mc, the terminals' descriptions and
# doom.
SYSTEM = [*BASE, python, mc, terminfo_pkg, doom]
rootfs_image, rootfs = system("rootfs", SYSTEM, "bin/rootfs")

# clang for this system: LLVM's multicall program of the release lib/cxx
# comes from, compiled against lib/c and lib/cxx, and the headers clang
# keeps beside it. The host gives cmake, ninja and the release's
# llvm-tblgen and clang-tblgen on PATH.
LLVM_TBLGEN = shutil.which("llvm-tblgen") or ""
CLANG_TBLGEN = shutil.which("clang-tblgen") or ""

clang = command(
    name="clang",
    inputs=[*LIBC_HEADERS, "$(S)/bin/clang/make.py", "$(S)/dev/wasm.py"],
    outputs=["$(B)/bin/clang/llvm.wasm", "$(B)/bin/clang/resource.tar"],
    deps=[llvm_tarball, libc, libcxx],
    cmd=[
        "python3", "$(S)/bin/clang/make.py",
        CLANG, CLANGXX, LLVM_AR, LLVM_RANLIB,
        shutil.which("cmake") or "cmake", shutil.which("ninja") or "ninja",
        llvm_tarball.outputs[0], LLVM_TBLGEN, CLANG_TBLGEN,
        "$(S)/lib/c/include", libc.outputs[0], libcxx.outputs[1], libcxx.outputs[0],
        "$(B)/bin/clang/llvm.wasm", "$(B)/bin/clang/resource.tar",
    ],
    descr="CL",
    color="green",
)
# llvm's module is 230 MB: zstd's level 3 packs it in a second.
llvm_exe = executable("llvm_exe", clang, "$(B)/bin/clang/llvm", level=3)

# pzd packs a module into an executable inside the system, as dev/exe.py
# does outside: test_clang runs what clang links there.
pzd_module = command(
    name="pzd_module",
    inputs=[*LIBC_HEADERS, "$(S)/bin/pzd/pzd.c", "$(S)/tst/libc/make.py", "$(S)/dev/wasm.py"],
    outputs=["$(B)/bin/pzd/pzd.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/tst/libc/make.py", CLANG, WASM_LD, "$(S)/bin/pzd/pzd.c", "$(S)/lib/c/include", libc.outputs[0], "$(B)/bin/pzd/pzd.wasm"],
    descr="CC",
    color="green",
)
pzd = executable("pzd", pzd_module, "$(B)/bin/pzd/pzd")

# test_libc's program: lib/c's functions with deterministic results.
libc_check = command(
    name="libc_check",
    inputs=[*LIBC_HEADERS, "$(S)/tst/libc/check.c", "$(S)/tst/libc/make.py", "$(S)/dev/wasm.py"],
    outputs=["$(B)/tst/libc/check.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/tst/libc/make.py", CLANG, WASM_LD, "$(S)/tst/libc/check.c", "$(S)/lib/c/include", libc.outputs[0], "$(B)/tst/libc/check.wasm"],
    descr="CC",
    color="green",
)
libc_check_exe = executable("libc_check_exe", libc_check, "$(B)/tst/libc/check")

# test_asan's program: lib/c's allocator, string functions and calls misused,
# one memory error a case, built as test_libc's is but with -g, so the
# traces name the lines; `vm run --asan` compiles it with the shadow map.
asan_check = command(
    name="asan_check",
    inputs=[*LIBC_HEADERS, "$(S)/tst/asan/check.c", "$(S)/tst/libc/make.py", "$(S)/dev/wasm.py"],
    outputs=["$(B)/tst/asan/check.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/tst/libc/make.py", CLANG, WASM_LD, "$(S)/tst/asan/check.c", "$(S)/lib/c/include", libc.outputs[0], "$(B)/tst/asan/check.wasm", "-g"],
    descr="CC",
    color="green",
)
asan_check_exe = executable("asan_check_exe", asan_check, "$(B)/tst/asan/check")

# test_cxx's program: lib/cxx's library over lib/c with deterministic results.
cxx_check = command(
    name="cxx_check",
    inputs=[*LIBC_HEADERS, "$(S)/tst/cxx/check.cpp", "$(S)/tst/cxx/make.py", "$(S)/dev/wasm.py"],
    outputs=["$(B)/tst/cxx/check.wasm"],
    deps=[libc, libcxx],
    cmd=["python3", "$(S)/tst/cxx/make.py", CLANG, WASM_LD, "$(S)/tst/cxx/check.cpp", "$(S)/lib/c/include", libc.outputs[0],
         libcxx.outputs[1], libcxx.outputs[0], "$(B)/tst/cxx/check.wasm"],
    descr="CC",
    color="green",
)
cxx_check_exe = executable("cxx_check_exe", cxx_check, "$(B)/tst/cxx/check")


# The WebAssembly spec testsuite through `vm spec`; wast2json comes from
# wabt, under ix run bld/wabt.
TESTSUITE = "9b9fbf8260507169066bf3e8de7b01fb8c145e55"
WAST2JSON = shutil.which("wast2json") or ""
WAT2WASM = shutil.which("wat2wasm") or ""

testsuite_tarball = fetch(
    "testsuite_tarball",
    f"https://github.com/WebAssembly/testsuite/archive/{TESTSUITE}.tar.gz",
    "0d0ed50f55f17f25b5db9e28d3d84d4a0a9feb5e97fd9a718f6d7017b16eb2e4",
    f"$(B)/ext/testsuite-{TESTSUITE}.tar.gz",
)

vm_spec_test = command(
    name="test_vm_spec",
    inputs=["$(S)/tst/vm_spec.py"],
    outputs=["$(B)/tst/vm_spec.stamp"],
    deps=[vm, aot_native, testsuite_tarball],
    cmd=["python3", "$(S)/tst/vm_spec.py", vm.outputs[0], WAST2JSON, testsuite_tarball.outputs[0], "$(B)/tst/vm_spec.stamp", aot_native.outputs[0]],
    descr="TS",
    color="green",
)

# tst/wazero: a WASI runner on wazero, what vm is measured against. Its
# modules, checked against go.sum, are fetched once; the build has no network.
wazero_modules = command(
    name="wazero_modules",
    local=True,
    inputs=["$(S)/dev/go.py", "$(S)/tst/wazero/go.mod", "$(S)/tst/wazero/go.sum"],
    outputs=["$(B)/tst/wazero/modules.tar"],
    cmd=["python3", "$(S)/dev/go.py", "download", GO, "$(S)/tst/wazero", "$(B)/tst/wazero/modules.tar"],
    descr="FE",
    color="blue",
)

wazero = command(
    name="wazero",
    inputs=[*build.glob("$(S)/tst/wazero/*.go"), "$(S)/tst/wazero/go.mod", "$(S)/tst/wazero/go.sum", "$(S)/dev/go.py"],
    outputs=["$(B)/tst/wazero/wazero"],
    deps=[wazero_modules],
    cmd=[
        "python3", "$(S)/dev/go.py", "build", GO, "$(S)/tst/wazero", "$(B)/tst/wazero/wazero",
        "--modules", wazero_modules.outputs[0],
    ],
    descr="GO",
    color="light-blue",
)

# The stand compares vm's code with native code of the same programs:
# the C++ compiler, and decode release 16, pure C/C++ wasm whose native
# column uses the same wrappers and the libraries of the ix revision that
# built that release. dev/stand/build.sh supplies those libraries and the
# host toolchain.
DECODE_IX = "53f12e265c480e7849bfa58eedf608db63020b1b"
DECODE = {
    "im": ("im", "0a168cb80eb61c6540a4e445a509f0d57b086565e99da0605671fb8d61b99783", "lib/image/magick/wasm/decode.c", "06a48395995911dc4f6ddeaf5fd07b13232de1872eb5d5f340fc01cad735a8c1"),
    "pdf": ("pdf", "ab2c0067403b605b2bdc2009e4bb0a48df80511a19c5ccca9785f27dd9d4df0d", "lib/pdf/ium/wasm/pdf.c", "e6ae5484e10deb16e338a4dbc7dc9d9f0d8834b51f9bff397eba6b4eb6282e65"),
    "djvu": ("djvu", "82468c78dcc7d9a3a5f2cd2d9a7cc961b215e485f09104cdefa3da11e32137a3", "lib/djvulibre/wasm/djvu.cpp", "68cecb4c5947fdb340e56a9d8627f7a16ce1f286279e43826aeff8f253ae5e5c"),
    "svg": ("svg_simple", "377cdcf753d46b07c749a2a593edbd7bc3c6539537779591f6ba9af93ee48ebb", "lib/svg/wasm/simple/svg.c", "054cb55d74726ef04be7c03af517b016d5207ab2854f0f6b5dffbec785af33b8"),
    "magic": ("magic", "3d5a2a4a148126b587836004d1e09e0c90d2c062b6b94179c4f40c57b092d41e", "lib/magic/wasm/magic.c", "9caceafefa48fed7ef7ca358f249c0b4347f595f79b5976d133ca90738e9a658"),
}
NATIVE_CONFIG = json.dumps({
    "cc": CLANG,
    "ar": LLVM_AR,
    "cxx": shutil.which("clang++") or "clang++",
    "pkg_config": shutil.which("pkg-config") or "pkg-config",
    "cppflags": build.cppflags,
    "cflags": build.cflags,
    "cxxflags": build.cxxflags,
    "ldflags": build.ldflags,
    "magic_data": os.environ.get("MAGIC_DATA", ""),
    "ix": os.environ.get("STAND_IX_REV", ""),
    "scalar": os.environ.get("STAND_NATIVE_SCALAR", ""),
}, sort_keys=True)
decode_stand = []

for kind, (module, digest, source, source_digest) in DECODE.items():
    folder = f"$(B)/stand/decode/{kind}"
    packed = fetch(f"stand_{kind}_download", f"https://github.com/impulse-desktop/decode/releases/download/16/{module}.wasm.zstd", digest, f"{folder}/input.wasm.zstd")
    wasm = command(
        name=f"stand_{kind}_wasm",
        deps=[packed],
        outputs=[f"{folder}/input.wasm"],
        cmd=[shutil.which("zstd") or "zstd", "-d", "-q", "-f", packed.outputs[0], "-o", f"{folder}/input.wasm"],
        descr="UZ", color="blue",
    )
    obj = command(
        name=f"stand_{kind}_aot",
        deps=[vm, wasm],
        outputs=[f"{folder}/module.o"],
        cmd=[vm.outputs[0], "-o", f"{folder}/module.o", wasm.outputs[0]],
        descr="AO", color="light-cyan",
    )
    wrapper = fetch(f"stand_{kind}_source", f"https://raw.githubusercontent.com/pg83/ix/{DECODE_IX}/pkgs/{source}", source_digest, f"{folder}/wrapper" + os.path.splitext(source)[1])
    native = command(
        name=f"stand_{kind}_native",
        inputs=["$(S)/dev/stand/native.py", "$(S)/dev/stand/native.h", "$(S)/dev/stand/decode.c"],
        deps=[wrapper],
        outputs=[f"{folder}/native"],
        cmd=["python3", "$(S)/dev/stand/native.py", kind, wrapper.outputs[0], f"{folder}/native", NATIVE_CONFIG],
        env={"PKG_CONFIG_PATH": os.environ.get("PKG_CONFIG_PATH", "")},
        descr="CC", color="green",
    )
    decode_stand.extend([obj, native])

# CPython built natively from the tarball python.wasm comes from, with its
# built-in modules, against python.wasm on dev/stand/pybench.py.
stand_python_native = command(
    name="stand_python_native",
    inputs=["$(S)/dev/stand/python.py", "$(S)/bin/python/make.py", "$(S)/dev/wasm.py"],
    deps=[python_tarball],
    outputs=["$(B)/stand/python/native"],
    cmd=["python3", "$(S)/dev/stand/python.py", python_tarball.outputs[0], "$(B)/stand/python/native", NATIVE_CONFIG],
    descr="CC", color="green",
)

stand_corpus = fetch(
    "stand_corpus",
    "https://raw.githubusercontent.com/pg83/store/1a4411b2ee6fdf6c3a05b7ddafd4d71ab79dbb54/decode-corpus-1.tgz",
    "0d6a8975159dd12a0a8d6bee270659791780bef806151790054205326c6d2685",
    "$(B)/stand/decode-corpus-1.tgz",
)

# A reactor of no imports the stand links (test_stand.py): it exports what
# the stand calls, and its stack is the one it links, for the stand gives
# no stack blocks.
stand_fixture = command(
    name="stand_fixture",
    inputs=["$(S)/tst/stand/fixture.cpp"],
    outputs=["$(B)/tst/stand/fixture.wasm"],
    cmd=[CLANG, *AOT_FLAGS, "--target=wasm32-unknown-unknown", "-ffreestanding", "-nostdlib", "-nostdinc", "-nostdinc++", "-mbulk-memory",
         "-Xclang", "-target-abi", "-Xclang", "experimental-mv", "-Wl,--no-entry", "-Wl,--export-memory", "-Wl,--stack-first",
         "-Wl,-z,stack-size=4195328", "$(S)/tst/stand/fixture.cpp", "-o", "$(B)/tst/stand/fixture.wasm"],
    descr="CC", color="green",
)

# What each test runs against: its arguments are the outputs of these nodes.
chaos_check = command(
    name="chaos_check",
    inputs=[*LIBC_HEADERS, "$(S)/tst/chaos/check.c", "$(S)/tst/libc/make.py", "$(S)/dev/wasm.py"],
    outputs=["$(B)/tst/chaos/check.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/tst/libc/make.py", CLANG, WASM_LD, "$(S)/tst/chaos/check.c", "$(S)/lib/c/include", libc.outputs[0], "$(B)/tst/chaos/check.wasm", "-g"],
    descr="CC", color="green",
)
chaos_check_exe = executable("chaos_check_exe", chaos_check, "$(B)/tst/chaos/check")
# The first process of test_memory_pool.py's vm, whose guests' memory it
# makes small.
memory_pool = executable("memory_pool_exe", command(
    name="memory_pool_module",
    inputs=[*LIBC_HEADERS, "$(S)/tst/memory_pool.c", "$(S)/tst/libc/make.py", "$(S)/dev/wasm.py"],
    outputs=["$(B)/tst/memory_pool.wasm"],
    deps=[libc],
    cmd=["python3", "$(S)/tst/libc/make.py", CLANG, WASM_LD, "$(S)/tst/memory_pool.c", "$(S)/lib/c/include", libc.outputs[0], "$(B)/tst/memory_pool.wasm"],
    descr="CC", color="green",
), "$(B)/tst/memory_pool")
host_syscalls_filter = command(
    name="host_syscalls_filter",
    inputs=["$(S)/tst/host_syscalls/filter.c"],
    outputs=["$(B)/tst/host_syscalls/filter"],
    cmd=[CLANG, *build.cppflags, *build.cflags, "-O2", "$(S)/tst/host_syscalls/filter.c", "-o", "$(B)/tst/host_syscalls/filter", *build.ldflags],
    descr="CC", color="green",
)

UNIT_TOOLS = json.dumps({
    "cc": CLANG,
    "ld": WASM_LD,
    "aot": aot_native.outputs[0],
    "vm": vm.outputs[0],
    "c_headers": "$(S)/lib/c/include",
    "libc": libc.outputs[0],
    "cxx_headers": libcxx.outputs[1],
    "libcxx": libcxx.outputs[0],
    "host_cflags": [*build.cppflags, *build.cxxflags],
    "host_ldflags": build.ldflags,
}, sort_keys=True)
unit_tests = []
coverage_cases = []
coverage_dependencies = []
coverage_inputs = ["$(S)/dev/coverage.py", "$(S)/dev/coverage_wasm.py", "$(S)/tst/unit/run.py", "$(S)/dev/wasm.py", "$(S)/dev/wasm_info.py"]
coverage_filter = os.environ.get("COVERAGE_TESTS", "")


def coverage_case(name, command_line, inputs, dependencies):
    if coverage_filter and not re.search(coverage_filter, name):
        return
    coverage_cases.append({"name": name, "command": command_line})
    coverage_inputs.extend(inputs)
    coverage_dependencies.extend(dependencies)

# A unit test is built first, its executable /tests/unit/<name> of the
# image of the tests, which every test of the system boots rootfs from:
# the system's programs and the tests' own (tst/unit/run.py).
unit_cases = []
for source in build.glob("$(S)/tst/unit/**/*.cpp"):
    name = source[len("$(S)/tst/unit/"):-len(".cpp")]
    prefix = f"$(B)/tst/unit/{name}"
    unit_cases.append((name, source, prefix, command(
        name="unit_" + name.replace("/", "_") + "_exe",
        inputs=[source, "$(S)/tst/unit/run.py", "$(S)/dev/wasm.py", "$(S)/dev/exe.py", *LIBC_HEADERS],
        outputs=[prefix + suffix for suffix in (".exe", ".wasm", ".native.o", ".embedded.o")],
        deps=[aot_native, vm, libc, libcxx],
        cmd=["python3", "$(S)/tst/unit/run.py", "build", source, prefix, UNIT_TOOLS],
        env={"PYTHONDONTWRITEBYTECODE": "1"},
        descr="EE", color="green",
    )))

# The programs and data of the tests of the system, each test's under
# /tests/<test> of the image of the tests: files from outputs, trees of
# the source.
TEST_FILES = [
    ("/tests/libc/check", libc_check_exe),
    ("/tests/cxx/check", cxx_check_exe),
    ("/tests/asan/check", asan_check_exe),
    ("/tests/chaos/check", chaos_check_exe),
]
TEST_TREES = [("/tests/libc/etc", "$(S)/tst/libc/etc"), ("/tests/busybox", "$(S)/tst/busybox")]
# CPython's own tests, the package test, which test_python puts on
# PYTHONPATH.
TEST_TARS = [("/tests/python/test", python_tarball, f"Python-{PYTHON}/Lib/test")]

tests_image = image(
    "tests_image", "$(B)/tst/tests.img", SYSTEM,
    [*(arg for name, _, prefix, _ in unit_cases for arg in ("--file", "/tests/unit/" + name, prefix + ".exe")),
     *(arg for path, node in TEST_FILES for arg in ("--file", path, node.outputs[0])),
     *(arg for path, tree in TEST_TREES for arg in ("--tree", path, tree)),
     *(arg for path, node, member in TEST_TARS for arg in ("--tar", path, node.outputs[0], member))],
    deps=[*(built for _, _, _, built in unit_cases), *dict.fromkeys(node for _, node in TEST_FILES), *(node for _, node, _ in TEST_TARS)],
)
SYSTEM_TOOLS = {"vm": vm.outputs[0], "rootfs": rootfs.outputs[0], "image": tests_image.outputs[0]}

# The image test_clang boots: the system's packages and clang's. bin/clang
# is LLVM's multicall executable, bin/llvm, with clang, clang++ and
# wasm-ld links, and its resource headers in lib/clang beside it, where
# clang finds them from its file; lib/c and lib/cxx are lib/c's and
# lib/cxx's headers and libraries, which the realm has in its include and
# lib; bin/pzd is pzd.
clang_pkg = package("bin/clang", ["--file", "bin/llvm", llvm_exe.outputs[0],
                                  *(arg for name in ("clang", "clang++", "wasm-ld") for arg in ("--link", "bin/" + name, "llvm")),
                                  "--tar", "lib/clang", clang.outputs[1], "lib/clang"], deps=[llvm_exe, clang])
libc_pkg = package("lib/c", ["--tar", "include", libc_headers.outputs[0], "include", "--file", "lib/libc.a", libc.outputs[0]],
                   deps=[libc_headers, libc])
libcxx_pkg = package("lib/cxx", ["--tar", "include/c++", libcxx.outputs[1], "include/c++", "--file", "lib/libc++.a", libcxx.outputs[0]],
                     deps=[libcxx])
pzd_pkg = package("bin/pzd", ["--file", "bin/pzd", pzd.outputs[0]], deps=[pzd])
clang_image = image("clang_image", "$(B)/tst/clang.img", [*SYSTEM, clang_pkg, libc_pkg, libcxx_pkg, pzd_pkg])

E2E_TESTED = {
    "aot": [aot, vm, chaos_check, rootfs],
    "asan": [vm, rootfs, tests_image],
    "asan_bounds": [vm, aot_native, wazero, rootfs],
    "bounded_copy_loops": [vm, aot_native, wazero, rootfs],
    "chaos": [vm, vm_chaos, aot_native, aot_native_chaos, chaos_check, rootfs, tests_image],
    "memory_pool": [vm, memory_pool, busybox_exe],
    "cli_errors": [vm, aot_native],
    "compiled_calls": [vm, rootfs, busybox],
    "compiler_modes": [vm, aot_native, rootfs],
    "compiler_rejections": [vm, aot_native, wazero, rootfs],
    "conditional_floats": [vm, aot_native, wazero, rootfs],
    "constant_arithmetic": [vm, aot_native, wazero, rootfs],
    "copy_loops": [vm, aot_native, rootfs],
    "cxx": [vm, rootfs, tests_image],
    "data_segments": [vm, aot_native, rootfs],
    "dead_instructions": [vm, aot_native, wazero, rootfs],
    "deep_control": [vm, aot_native, wazero, rootfs],
    "dispatch_loops": [vm, aot_native, wazero, rootfs],
    "fork_modes": [vm, aot_native, wazero],
    "float_boundaries": [vm, aot_native, wazero, rootfs],
    "float_integer_phis": [vm, aot_native, wazero, rootfs],
    "float_permutations": [vm, aot_native, wazero, rootfs],
    "debug_lines": [vm, aot_native, rootfs],
    "debug_names": [vm, aot_native, rootfs],
    "import_errors": [vm, aot_native, wazero],
    "initializers": [vm, aot_native, wazero, rootfs],
    "interleaved_float_selects": [vm, aot_native, wazero, rootfs],
    "integer_identities": [vm, aot_native, wazero, rootfs],
    "kernel_exhaustion": [vm, aot_native, wazero],
    "invariant_loops": [vm, aot_native, wazero, rootfs],
    "invariant_results": [vm, aot_native, wazero, rootfs],
    "host_io": [vm, aot_native, rootfs],
    "host_resources": [vm, aot_native],
    "host_syscalls": [vm, aot_native, host_syscalls_filter],
    "runtime_abi": [vm, aot_native, *runtime_abi_vms],
    "host_signals": [vm, aot_native, wazero],
    "libc": [vm, rootfs, tests_image],
    "large_functions": [vm, aot_native, rootfs],
    "large_switches": [vm, aot_native, wazero, rootfs],
    "loop_limits": [vm, aot_native, wazero, rootfs],
    "profile_artifacts": [vm, aot_native],
    "runtime_errors": [vm, aot_native, rootfs],
    "runtime_safety": [vm, aot_native, wazero, rootfs],
    "spec_cli": [vm, aot_native],
    "sparse_values": [vm, aot_native, wazero, rootfs],
    "stand": [vm, aot_native, stand_fixture],
    "table_defaults": [vm, aot_native, wazero, rootfs],
    "tail_profile": [vm, aot_native],
    "unary_loops": [vm, aot_native, wazero, rootfs],
    "vfork_traps": [vm, aot_native, wazero, rootfs],
    "wide_results": [vm, aot_native, wazero, rootfs],
    "vm": [vm, aot_native, aot, chaos_check, rootfs],
}
APPLICATION_TESTED = {
    "rootfs": [vm, rootfs],
    "console": [vm, rootfs],
    "devices": [vm, rootfs, devices_service],
    "disks": [vm, rootfs],
    "mounts": [vm, rootfs, watch_dir],
    "net": [vm, net_probe, inet_exe, rootfs, inet_sockets],
    "watchdir": [vm, rootfs, watch_dir],
    "unix_sockets": [vm, rootfs, unix_sockets],
    "vfs": [vm, rootfs],
    "busybox_abi": [busybox],
    "busybox": [vm, rootfs, tests_image],
    "clang": [vm, rootfs, clang_image],
    "doom": [vm, rootfs, tests_image],
    "mc": [vm, rootfs, tests_image],
    "python": [vm, rootfs, tests_image],
    "spawn": [vm, rootfs, tests_image],
    "terminfo": [ncurses_tarball, terminfo],
}
TESTED = {**E2E_TESTED, **APPLICATION_TESTED}
tests = []
e2e_tests = []

for name, source, prefix, built in unit_cases:
    unit_tests.append(command(
        name="unit_" + name.replace("/", "_"),
        inputs=[source, "$(S)/tst/unit/run.py"],
        outputs=[prefix + ".stamp"],
        deps=[vm, rootfs, tests_image, built],
        cmd=["python3", "$(S)/tst/unit/run.py", "run", source, prefix, name, json.dumps(SYSTEM_TOOLS, sort_keys=True)],
        env={"PYTHONDONTWRITEBYTECODE": "1"},
        descr="EE", color="green",
    ))
    coverage_tools = {**json.loads(UNIT_TOOLS), **SYSTEM_TOOLS}
    coverage_tools.update(vm="$VM", aot="$AOT")
    coverage_case("unit_" + name.replace("/", "_"),
                  ["python3", "$(S)/tst/unit/run.py", "all", source, "$OUT", name, json.dumps(coverage_tools)],
                  [source, *LIBC_HEADERS], [libc, libcxx, rootfs, tests_image])

for test in build.glob("$(S)/tst/test_*.py"):
    name = test.rsplit("/", 1)[-1][len("test_"):-len(".py")]
    stamp = f"$(B)/tst/{name}.stamp"

    if name not in TESTED:
        continue

    tested = TESTED[name]
    case_test = command(
        name=f"test_{name}",
        inputs=[test, "$(S)/dev/wasm_info.py", "$(S)/dev/exe.py", "$(S)/tst/system.py"],
        outputs=[stamp],
        deps=tested,
        cmd=[
            ["python3", test, *(output for node in tested for output in node.outputs)],
            touch(stamp),
        ],
        env={"PYTHONDONTWRITEBYTECODE": "1"},
        descr="TS",
        color="green",
    )
    tests.append(case_test)
    if name not in E2E_TESTED:
        continue
    e2e_tests.append(case_test)
    coverage_arguments = []
    for node in tested:
        if node is vm:
            coverage_arguments.append("$VM")
        elif node is aot_native:
            coverage_arguments.append("$AOT")
        elif node is vm_chaos:
            coverage_arguments.append("$VM_CHAOS")
        elif node is aot_native_chaos:
            coverage_arguments.append("$AOT_CHAOS")
        else:
            coverage_arguments.extend(node.outputs)
    coverage_case("test_" + name, ["python3", test, *coverage_arguments], [test],
                  [node for node in tested if node not in (vm, aot_native, vm_chaos, aot_native_chaos)])

# The constants the kernel shares with lib/c, the sizes of the structures
# lib/c hands it among them, against lib/c's headers as clang has them.
libc_consts_test = command(
    name="test_libc_consts",
    inputs=["$(S)/tst/test_libc_consts.py", "$(S)/bin/vm/libc_consts.go", "$(S)/lib/c/src/kernel.c", *LIBC_HEADERS],
    outputs=["$(B)/tst/libc_consts.stamp"],
    deps=[],
    cmd=[["python3", "$(S)/tst/test_libc_consts.py", CLANG, "$(S)"], touch("$(B)/tst/libc_consts.stamp")],
    env={"PYTHONDONTWRITEBYTECODE": "1"},
    descr="TS", color="green",
)
tests.append(libc_consts_test)
e2e_tests.append(libc_consts_test)

# How the kernel writes the arguments of a traced call, against lib/c's
# declarations of the kernel's functions.
call_formats_test = command(
    name="test_call_formats",
    inputs=["$(S)/tst/test_call_formats.py", "$(S)/bin/vm/traced_imports.go", "$(S)/lib/c/src/kernel.c", "$(S)/lib/c/src/kernel.h", *LIBC_HEADERS],
    outputs=["$(B)/tst/call_formats.stamp"],
    deps=[],
    cmd=[["python3", "$(S)/tst/test_call_formats.py", CLANG, "$(S)"], touch("$(B)/tst/call_formats.stamp")],
    env={"PYTHONDONTWRITEBYTECODE": "1"},
    descr="TS", color="green",
)
tests.append(call_formats_test)
e2e_tests.append(call_formats_test)

for name, runner, fixture in (("wat_updates", "run_updates.py", "update_branches.wat"),
                              ("wat_float_selects", "run_float_selects.py", "float_selects.wat"),
                              ("wat_memory_arithmetic", "run_memory_arithmetic.py", "memory_arithmetic.wat"),
                              ("wat_copy_addresses", "run_copy_addresses.py", "copy_addresses.wat"),
                              ("wat_dispatch_values", "run_dispatch_values.py", "dispatch_values.wat"),
                              ("wat_go_float_results", "run_go_float_results.py", "go_float_results.wat"),
                              ("wat_deep_addresses", "run_deep_addresses.py", "deep_addresses.wat"),
                              ("wat_result_masks", "run_result_masks.py", "result_masks.wat"),
                              ("wat_exit_values", "run_exit_values.py", "exit_values.wat"),
                              ("wat_startup_fork", "run_startup_fork.py", "startup_fork.wat"),
                              ("wat_branch_threads", "run_branch_threads.py", "branch_threads.wat"),
                              ("wat_branch_pairs", "run_branch_pairs.py", "branch_pairs.wat"),
                              ("wat_branch_pair_addresses", "run_branch_pair_addresses.py", "branch_pair_addresses.wat"),
                              ("wat_branch_pair_constants", "run_branch_pair_constants.py", "branch_pair_constants.wat"),
                              ("wat_diamond_lifetimes", "run_diamond_lifetimes.py", "diamond_lifetimes.wat"),
                              ("wat_conversion_diamonds", "run_conversion_diamonds.py", "conversion_diamonds.wat"),
                              ("wat_threaded_parameters", "run_threaded_parameters.py", "threaded_parameters.wat"),
                              ("wat_single_target_dispatch", "run_single_target_dispatch.py", "single_target_dispatch.wat"),
                              ("wat_unrolled_results", "run_unrolled_results.py", "unrolled_results.wat"),
                              ("wat_spec_funcrefs", "run_spec_funcrefs.py", "spec_funcrefs.wat"),
                              ("wat_late_constants", "run_late_constants.py", "late_constants.wat"),
                              ("wat_shared_addresses", "run_shared_addresses.py", "shared_addresses.wat"),
                              ("wat_address_rotation", "run_address_rotation.py", "address_rotation.wat"),
                              ("wat_adjacent_loops", "run_adjacent_loops.py", "adjacent_loops.wat"),
                              ("wat_wide_allocation_limit", "run_wide_allocation_limit.py", "wide_allocation_limit.wat"),
                              ("wat_loop_escapes", "run_loop_escapes.py", "loop_escapes.wat")):
    runner, fixture = "$(S)/tst/wat/" + runner, "$(S)/tst/wat/" + fixture
    stamp = f"$(B)/tst/{name}.stamp"
    case_test = command(
        name="test_" + name,
        inputs=[runner, fixture, "$(S)/dev/exe.py", "$(S)/tst/system.py"],
        outputs=[stamp],
        deps=[vm, aot_native, wazero, rootfs],
        cmd=[["python3", runner, vm.outputs[0], aot_native.outputs[0], wazero.outputs[0], WAT2WASM, fixture, rootfs.outputs[0]], touch(stamp)],
        descr="EE", color="green",
    )
    tests.append(case_test)
    e2e_tests.append(case_test)
    coverage_case("test_" + name, ["python3", runner, "$VM", "$AOT", wazero.outputs[0], WAT2WASM, fixture, rootfs.outputs[0]],
                  [runner, fixture], [wazero, rootfs])

coverage_case("test_vm_spec",
              ["python3", "$(S)/tst/vm_spec.py", "$VM", WAST2JSON, testsuite_tarball.outputs[0], "$OUT.stamp", "$AOT"],
              ["$(S)/tst/vm_spec.py"], [testsuite_tarball])

coverage = command(
    name="coverage",
    inputs=[*dict.fromkeys(coverage_inputs), *WASM2O_SOURCES, "$(S)/bin/aot/aot.cpp"],
    outputs=["$(B)/coverage/" + name for name in ("summary.json", "summary.txt", "uncovered.txt", "aot.json", "aot-chaos.json", "vm.out", "vm-codecov.out", "aot-native.info", "aot-wasm.info")]
            + ["$(B)/coverage/" + stem + suffix for stem in ("aot-native", "aot-native-chaos") for suffix in (".profdata", ".lcov")]
            + ["$(B)/coverage/" + stem + suffix for stem in ("aot-wasm", "aot-wasm-chaos") for suffix in (".proftext", ".profdata", ".lcov")]
            + ["$(B)/coverage/" + case["name"] + ".log" for case in coverage_cases],
    deps=[aot_coverage, vm_coverage, aot_chaos_coverage, vm_chaos_coverage,
          *[compiler for compiler, embedded in wasm_coverage], *dict.fromkeys(coverage_dependencies)],
    cmd=["python3", "$(S)/dev/coverage.py", "$(B)/coverage", json.dumps({
        "jobs": int(os.environ.get("COVERAGE_JOBS") or os.environ.get("BUILD_JOBS") or "4"),
        "wrapper_tools": {"cc": CLANG, "cppflags": build.cppflags, "ldflags": build.ldflags},
        "source_root": "$(S)", "aot": aot_coverage.outputs[0], "vm": vm_coverage.outputs[0],
        "aot_chaos": aot_chaos_coverage.outputs[0], "vm_chaos": vm_chaos_coverage.outputs[0],
        "go": GO,
        "aot_wasm": wasm_coverage[0][0].outputs[0], "aot_wasm_chaos": wasm_coverage[1][0].outputs[0],
        "llvm_cov": os.environ.get("LLVM_COV") or shutil.which("llvm-cov") or "llvm-cov",
        "llvm_profdata": os.environ.get("LLVM_PROFDATA") or shutil.which("llvm-profdata") or "llvm-profdata",
        "cases": coverage_cases,
    })],
    descr="CV", color="cyan",
)

coverage_check = command(
    name="coverage_check",
    inputs=["$(S)/dev/coverage.py"],
    outputs=["$(B)/coverage/check.stamp"],
    deps=[coverage],
    cmd=[["python3", "$(S)/dev/coverage.py", "--check", coverage.outputs[0], "100"], touch("$(B)/coverage/check.stamp")],
    descr="CV", color="cyan",
)

group("install", libcxx, libc_headers, clang, llvm_exe, busybox, busybox_exe, busybox_abi, doom, python, pzd,
      vm, aot_native, aot, aot_aot, wazero, rootfs)
group("unit", *unit_tests)
group("e2e", *e2e_tests, *unit_tests, vm_spec_test)
group("test", *tests, *unit_tests, vm_spec_test)
group("stand", vm, aot_native, aot, aot_aot, busybox, stand_corpus, *decode_stand, python, rootfs, stand_python_native)
