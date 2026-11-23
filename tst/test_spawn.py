#!/usr/bin/env python3
"""spawn, the system's command that runs a command in a mode of the
kernel's (bin/tools/spawn.cpp, lib/c's sys/execmode.h), from the shell of a
system started plainly: a traced command's lines on spawn's standard
error and in a file, the command's own status spawn's, its children
traced with it; a command under the address sanitizer, its report with
the source's lines, where the same command runs through unchecked without
spawn; and spawn's usage.

    test_spawn.py <vm> <rootfs> <tests.img>
"""
import re
import sys

import system

HEX = r"0x[0-9a-f]+"
# A case: the shell's script, its exit status, and the patterns its
# standard output and standard error have, a line each, in their order.
CASES = {
    "a trace on the standard error": (
        "spawn --strace --wait -- cat /run/none", 1, [],
        [r'\[(\d+)\] exec "/bin/cat"', rf'\[\d+\] open\("/run/none", 0, {HEX}\) = -1 ENOENT \(no such file or directory\)',
         r"cat: can't open '/run/none': No such file or directory", r"\[\d+\] write\(2, 0x[0-9a-f]+, \d+\) = \d+", r"\[\d+\] exit 1"]),
    "spawn becomes the command": (
        "spawn --strace=process --exec -- sh -c 'echo $$; exit 4'", 4, [r"(\d+)"], [r'\[(\d+)\] exec "/bin/sh"', r"\[\d+\] exit 4"]),
    "children, a signal and a file": (
        "spawn --strace=process,signals --out /run/t --wait -- sh -c '/bin/true; kill -TERM $$'; echo $?; cat /run/t", 0,
        [r"143", r'\[(\d+)\] exec "/bin/sh"', r"\[\d+\] child of \d+", r'\[\d+\] exec "/bin/true"', r"\[\d+\] exit 0", r"\[\d+\] signal SIGCHLD",
         r"\[\d+\] signal SIGTERM", r"\[\d+\] killed SIGTERM"], []),
    "library calls": (
        "spawn --strace=library --wait -- /tests/asan/check ok", 0, [r"ok 14 data"], [rf"\[\d+\] malloc\(\d+\) = {HEX}", rf"\[\d+\] free\({HEX}\)"]),
    "the sanitizer": (
        "cd /run && spawn --asan --wait -- /tests/asan/check overflow", 128 + 11, [],
        [rf".*: asan: 1-byte store at {HEX}: past the end of the 20-byte block at {HEX}", r".*: +overflow\+0x[0-9a-f]+ +check\.c:51"]),
    "the sanitizer's report in a trace": (
        "cd /run && spawn --asan --strace=faults --out /run/f --wait -- /tests/asan/check after_free; echo $?; cat /run/f", 0,
        [r"139", rf"\[\d+\] asan: 1-byte load at {HEX}: in the freed 32-byte block at {HEX}", r"\[\d+\] +after_free\+0x[0-9a-f]+ +check\.c:60"], []),
    "unchecked without spawn": ("cd /run && /tests/asan/check overflow > /dev/null; echo $?", 0, [r"\d+"], []),
    "no command": ("spawn --strace --", 2, [], [r"usage: spawn .*"]),
    "an unknown class": ("spawn --strace=calls -- true", 2, [], [r"usage: spawn .*", r"classes: system library process signals faults"]),
    "a file without a trace": ("spawn --out /run/t -- true", 2, [], [r"usage: spawn .*"]),
    "no such command": ("spawn --wait -- /bin/none", 127, [], [r"spawn: /bin/none: No such file or directory"]),
}


def ordered(patterns, text):
    """The pattern that no line of text matches after the ones before it, or None."""
    lines = text.splitlines()
    at = 0

    for pattern in patterns:
        while at < len(lines) and not re.fullmatch(pattern, lines[at]):
            at += 1

        if at == len(lines):
            return pattern

        at += 1

    return None


def main():
    vm, rootfs, image = sys.argv[1:4]
    failed = 0

    for name, (script, status, out, err) in CASES.items():
        r = system.shell(vm, rootfs, script, ("--disk", f"image={image}"), timeout=120)
        stdout, stderr = r.stdout.decode(), r.stderr.decode()
        missing = ordered(out, stdout) or ordered(err, stderr)

        if r.returncode != status or missing:
            print(f"FAIL {name}: exit {r.returncode}, want {status}; missing {missing!r}\n{stdout}{stderr}")
            failed += 1

    if failed:
        sys.exit(1)

    print(f"spawn: {len(CASES)} cases: traces to the standard error and to a file, children traced, the sanitizer from a plain system")


if __name__ == "__main__":
    main()
