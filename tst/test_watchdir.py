#!/usr/bin/env python3
"""watchdir of devfs's directories (lib/c's <sys/watchdir.h>): a watch's
records of the names of a directory as it starts, then of what changes,
which tst/watch_dir.cpp, a program the test hands the system, makes by
registering names at /dev/init/ctl and letting them go; and the directories
of /dev, which come with the first name registered in them, at any
depth, and go with the last.

    test_watchdir.py <vm> <rootfs> <watch_dir>
"""
import base64
import subprocess
import sys
import tempfile

SCRIPT = """\
python3 -c 'import base64, sys; open("/run/watch_dir", "wb").write(base64.b64decode(sys.stdin.read()))' <<'PROGRAM'
{program}
PROGRAM
chmod +x /run/watch_dir
/run/watch_dir
ls /dev/w 2>&1
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
/run: -1 Function not implemented
no descriptor: -1 Bad file descriptor
flags: -1 Invalid argument
ls: /dev/w: No such file or directory
"""


def main():
    vm, rootfs, program = sys.argv[1:4]
    script = SCRIPT.replace("{program}", base64.encodebytes(open(program, "rb").read()).decode().strip())

    with tempfile.TemporaryDirectory(prefix="pizdos-watchdir-") as empty:
        r = subprocess.run([vm, "run", "--", rootfs], input=(script + "exit 3\n").encode(), capture_output=True, cwd=empty,
                           timeout=180)

    if (r.returncode, r.stdout.decode(), r.stderr) != (3, WANT, b""):
        print(f"FAIL watchdir: exit {r.returncode}\n{r.stdout.decode()}\nwant\n{WANT}\n{r.stderr.decode()[-2000:]}")
        sys.exit(1)

    print("watchdir: a watch of a directory of /dev tells its names, then what came and went, and ends with it")


if __name__ == "__main__":
    main()
