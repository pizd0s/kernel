#!/usr/bin/env python3
"""Build doom.wasm from a doomgeneric release tarball and our terminal backend.

    make.py <cc> <wasm-ld> <doomgeneric-tarball> <platform.c> <libc-include> <libc.a> <doom.wasm> <wads>

The source list is the Makefile's own; its xlib platform object is replaced
by ours, which lives outside the tarball. The frame is 320x200 palette
indices (CMAP256): the backend takes the palette through i_video.c. Nothing
defines FEATURE_SOUND, so i_sound.c links empty. An undefined symbol is a
link error: every libc function is either defined in libc.a or declared as
a kernel import by its header. doom looks for its IWAD in wads, the
directory of its package (dev/pkg.py), where config.h says the files are:
FILES_DIR, "." in the tarball, which a configure of doomgeneric's would set.
"""
import os
import re
import shutil
import subprocess
import sys
import tarfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent.parent / "dev"))

import wasm  # noqa: E402

FLAGS = [
    "-O2",
    "-std=gnu99",
    "-DNORMALUNIX",
    "-DLINUX",
    "-DCMAP256",
    "-DDOOMGENERIC_RESX=320",
    "-DDOOMGENERIC_RESY=200",
    "-w",
]

LINK = [
    "--stack-first",
    "--max-memory=4294967296",
    "--export=__heap_base",
    "--export=__stack_pointer",
    "--gc-sections",
    "--error-limit=0",
]

SRC_DOOM = re.compile(r"^SRC_DOOM\s*=\s*(.*)$")


def sources(tree, platform):
    """The Makefile's objects, its platform file swapped for ours."""
    objects = None

    for line in (tree / "Makefile").read_text().splitlines():
        found = SRC_DOOM.match(line)

        if found:
            objects = found.group(1).split()

    if not objects:
        raise SystemExit("no SRC_DOOM in the Makefile")

    names = [o[:-2] for o in objects if o.endswith(".o")]
    names = [n for n in names if not n.startswith("doomgeneric_") or n == "doomgeneric"]
    return [(tree / f"{n}.c") for n in names] + [platform]


def patch(tree):
    """One shim, where d_main.c calls a boolean function through a
    void(*)(void): the cast is C's, but wasm tables type each entry, and a
    descriptor returning i32 does not match a call wanting nothing."""
    path = tree / "d_main.c"
    cast = "I_AtExit((atexit_func_t) G_CheckDemoStatus, true);"

    src = path.read_text()

    if cast not in src:
        raise SystemExit("d_main.c does not call G_CheckDemoStatus through a cast")

    shim = "static void G_CheckDemoStatusShim(void) { G_CheckDemoStatus(); }\n\n"
    src = src.replace(cast, "I_AtExit(G_CheckDemoStatusShim, true);")
    src = src.replace("void D_DoomMain (void)", shim + "void D_DoomMain (void)", 1)
    path.write_text(src)


def files_dir(tree, wads):
    path = tree / "config.h"
    line = '#define FILES_DIR "."'
    src = path.read_text()

    if src.count(line) != 1:
        raise SystemExit("config.h does not define FILES_DIR as .")

    path.write_text(src.replace(line, f'#define FILES_DIR "{wads}"'))


def main():
    cc, ld, tarball, platform, libc_include, libc, output, wads = sys.argv[1:9]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "doom"
    shutil.rmtree(work, ignore_errors=True)
    work.mkdir(parents=True)

    with tarfile.open(tarball) as archive:
        archive.extractall(work, filter="data")

    tree = next(work.glob("doomgeneric-*")) / "doomgeneric"
    patch(tree)
    files_dir(tree, wads)
    flags = FLAGS + ["-I", str(tree), "-isystem", str(Path(libc_include).resolve())]
    units = [(source, work / "obj" / source.with_suffix(".o").name, flags) for source in sources(tree, Path(platform))]

    wasm.compile_all(cc, units)
    Path(output).parent.mkdir(parents=True, exist_ok=True)
    subprocess.run([ld, *LINK, "-o", output, *(str(obj) for _, obj, _ in units), libc], check=True)


if __name__ == "__main__":
    main()
