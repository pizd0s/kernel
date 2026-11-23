#!/usr/bin/env python3
"""The kernel's address sanitizer under `vm run --asan`: tst/asan/check.c,
built with `-g`, the image of the tests' /tests/asan/check, which the
kernel compiles with the shadow map's checks, as it does the whole
system then, runs each of its memory errors, and the report names the
error, where the shadow map puts it and the source line of the fault,
through the kernel's own frames when the kernel caught it; its clean case
runs through, and runs without the flag too, compiled without the checks.

    test_asan.py <vm> <rootfs> <tests.img>
"""
import re
import subprocess
import sys

import system

SEGV, ABRT = 128 + 11, 128 + 6
HEX = r"0x[0-9a-f]+"
CASES = {
    "overflow": (SEGV, [rf"asan: 1-byte store at {HEX}: past the end of the 20-byte block at {HEX}",
                        r"overflow\+0x[0-9a-f]+ +check\.c:51", r"main\+0x[0-9a-f]+ +check\.c:\d+"]),
    "after_free": (SEGV, [rf"asan: 1-byte load at {HEX}: in the freed 32-byte block at {HEX}", r"after_free\+0x[0-9a-f]+ +check\.c:60"]),
    "syscall_read": (ABRT, [rf"asan: access of a range at {HEX}: in the freed 16-byte block at {HEX}",
                            r"\(\*Process\)\.span +process\.go:\d+", r"libcRead +libc\.go:\d+", r"read\+0x", r"syscall_read\+0x[0-9a-f]+ +check\.c:70"]),
    # strlen and memcpy are lib/c's functions, which the code compiles
    # where they are called: the fault's line is the call's.
    "string": (SEGV, [rf"asan: access of a range at {HEX}: past the end of the 4-byte block at {HEX}", r"string\+0x[0-9a-f]+ +check\.c:77"]),
    "copy": (SEGV, [rf"asan: 8-byte load at {HEX}: in free memory of the heap", r"copy\+0x[0-9a-f]+ +check\.c:85"]),
    "null": (SEGV, [r"asan: 4-byte store at 0x0: in the first KiB", r"null\+0x[0-9a-f]+ +check\.c:91"]),
    "shrink": (SEGV, [rf"asan: 1-byte store at {HEX}: past the end of the 4-byte block at {HEX}", r"shrink\+0x[0-9a-f]+ +check\.c:98"]),
    "mapped": (SEGV, [rf"asan: 1-byte store at {HEX}: in the freed 8192-byte block at {HEX}", r"mapped\+0x[0-9a-f]+ +check\.c:105"]),
    "run": (SEGV, [rf"asan: 1-byte store at {HEX}: past the end of the 100000-byte block at {HEX}", r"run\+0x[0-9a-f]+ +check\.c:111"]),
    "run_shrink": (SEGV, [rf"asan: 1-byte store at {HEX}: in freed memory", r"run_shrink\+0x[0-9a-f]+ +check\.c:131"]),
    "wide": (SEGV, [rf"asan: 4-byte load at {HEX}, whose byte {HEX} is past the end of the 6-byte block at {HEX}", r"wide\+0x[0-9a-f]+ +check\.c:117"]),
}


def run(vm, rootfs, image, args, asan=True):
    flags = [*(["--asan"] if asan else []), "--disk", f"image={image}"]
    r = system.shell(vm, rootfs, "cd /run && exec /tests/asan/check " + " ".join(args), flags, timeout=120)

    return subprocess.CompletedProcess(r.args, r.returncode, r.stdout.decode(), r.stderr.decode())


def main():
    vm, rootfs, image = sys.argv[1:4]
    failed = 0
    r = run(vm, rootfs, image, ["ok"])

    if r.returncode != 0 or r.stdout != "ok 14 data\n":
        print(f"FAIL ok: exit {r.returncode}\n{r.stdout}{r.stderr}")
        failed += 1

    for name, (code, patterns) in CASES.items():
        r = run(vm, rootfs, image, [name])
        missing = [p for p in patterns if not re.search(p, r.stderr)]

        if r.returncode != code or missing:
            print(f"FAIL {name}: exit {r.returncode}, want {code}; missing {missing}\n{r.stderr}")
            failed += 1

    r = run(vm, rootfs, image, ["ok"], asan=False)

    if r.returncode != 0 or r.stdout != "ok 14 data\n":
        print(f"FAIL ok without --asan: exit {r.returncode}\n{r.stdout}{r.stderr}")
        failed += 1

    if failed:
        sys.exit(1)

    print(f"asan: {len(CASES)} errors reported with their lines, the clean case runs with and without the checks")


if __name__ == "__main__":
    main()
