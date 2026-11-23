#!/usr/bin/env python3
"""watchdir of devfs's directories (lib/c's <sys/watchdir.h>): a watch's
records of the names of a directory as it starts, then of what changes,
which tst/watch_dir.cpp, a program the test hands the system, makes by
registering names at /dev/init/ctl and letting them go; and the directories
of /dev, which come with the first name registered in them, at any
depth, and go with the last. Then watch_dir's names on /run, tmpfs's, as
tst/test_mounts.py has them on fuse2fs's.

    test_watchdir.py <vm> <rootfs> <watch_dir>
"""
import base64
import sys
import tempfile

import system

SCRIPT = """\
python3 -c 'import base64, sys; open("/run/watch_dir", "wb").write(base64.b64decode(sys.stdin.read()))' <<'PROGRAM'
{program}
PROGRAM
chmod +x /run/watch_dir
/run/watch_dir
ls /dev/w 2>&1
/run/watch_dir names /run
"""

WANT = """\
w: existing a
w: idle
w: added b
w: removed a
w: added a
quick: existing b
quick: existing a
quick: idle
quick: Resource temporarily unavailable
quick: poll 0
quick: poll 1 POLLIN
quick: added d
w: added d
b: existing c
b: idle
b: removed c
b: end
w: removed b
short: -1 Invalid argument
w: removed d
a device: -1 Not a directory
no descriptor: -1 Bad file descriptor
flags: -1 Invalid argument
ls: /dev/w: No such file or directory
names: existing a
names: idle
quick: existing a
quick: idle
names: added b
names: removed a
names: removed b
names: added c
quick: removed a
quick: added c
quick: Resource temporarily unavailable
names: added d
names: Interrupted system call
names: added e
sub: idle
sub: end
names: removed e
names: removed c
names: removed d
names: added f
names: added g
names: poll 0
names: added h
names: added i
h: idle
h: end
names: removed i
names: removed h
names: added h
lone: idle
lone: end
names: removed f
names: removed g
names: removed h
"""


def main():
    vm, rootfs, program = sys.argv[1:4]
    script = SCRIPT.replace("{program}", base64.encodebytes(open(program, "rb").read()).decode().strip())

    with tempfile.TemporaryDirectory(prefix="pizdos-watchdir-") as empty:
        r = system.shell(vm, rootfs, script + "exit 3\n", cwd=empty, timeout=180)

    if (r.returncode, r.stdout.decode(), r.stderr) != (3, WANT, b""):
        print(f"FAIL watchdir: exit {r.returncode}\n{r.stdout.decode()}\nwant\n{WANT}\n{r.stderr.decode()[-2000:]}")
        sys.exit(1)

    print("watchdir: a watch of a directory of /dev tells its names, then what came and went, and ends with it")


if __name__ == "__main__":
    main()
