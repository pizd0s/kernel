#!/usr/bin/env python3
"""Calls through function pointers of other types (dev/fpcast.py), in the
system: tst/fpcast/check.c calls functions as C programs do, with a
parameter too many or too few, for a result there is none of, with an
integer for a float. As the linker leaves it, the image of the tests'
/tests/fpcast/plain, the first such call is a trap, WebAssembly's check of
the type; rewritten, /tests/fpcast/check, every call does what it does on
a native machine, and a call of the right type and a function pointer's
value are as they were.

    test_fpcast.py <vm> <rootfs> <tests.img>
"""
import subprocess
import sys

EXPECTED = """extra 41
fewer 500
absent 0 12
ignored 7
widened 4294967294
narrowed 3
double 3
float 3.5
own -5 1
qsort 9 7 3 1
"""


def run(vm, rootfs, image, program):
    return subprocess.run([vm, "run", "--disk", f"image={image}", "--", rootfs, "-c", f"cd /tmp && /tests/fpcast/{program}"],
                          capture_output=True, text=True, timeout=120)


def main():
    vm, rootfs, image = sys.argv[1:4]
    plain = run(vm, rootfs, image, "plain")

    if plain.returncode == 0 or "indirect call type mismatch" not in plain.stderr or plain.stdout != "":
        raise SystemExit(f"fpcast: the program as linked: exit {plain.returncode}, stdout {plain.stdout!r}, stderr {plain.stderr[-300:]!r}")

    check = run(vm, rootfs, image, "check")

    if check.returncode != 0 or check.stdout != EXPECTED or check.stderr != "":
        raise SystemExit(f"fpcast: the program rewritten: exit {check.returncode}, stdout {check.stdout!r}, stderr {check.stderr[-300:]!r}")

    print("fpcast: a call through a pointer of another type traps as linked and works rewritten: 10 lines")


if __name__ == "__main__":
    main()
