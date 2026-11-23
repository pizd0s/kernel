#!/usr/bin/env python3
"""doom in the system: doomgeneric's frame is ANSI half blocks, its clock
the kernel's monotonic time. Headless it plays the shareware demo from the
wad, which doom finds in its package (bin/doom), and hashes each second of
frames, which are the same in two runs. On a host's terminal it runs on
the system's pseudoterminal joined to the console (bin/tools/console.cpp),
its keys the terminal's in raw mode: it takes the alternate screen, draws
truecolor cells, and ^C, which the raw terminal no longer turns into a
signal, still ends it, the alternate screen given back.

    test_doom.py <vm> <rootfs> <tests.img>
"""
import fcntl
import os
import pty
import select
import struct
import subprocess
import sys
import termios
import time

import system

COMMAND = "cd /run && exec env DG_HEADLESS=1 doom -nosound -timedemo demo1"
TERMINAL = "cd /run && exec env TERM=xterm-256color DG_COLS=80 DG_ROWS=50 doom -nosound"


def timedemo(vm, rootfs, image):
    """The shareware demo plays twice, headless: the frames hash the same."""
    hashes = []

    for _ in range(2):
        r = system.shell(vm, rootfs, COMMAND, ("--disk", f"image={image}"), timeout=300)
        out = r.stdout.decode(errors="replace")
        err = r.stderr.decode(errors="replace")

        if r.returncode != 255 or "timed" not in err or "fps" not in err:
            return None, f"timedemo: exit {r.returncode}, stdout {out[-500:]!r}, stderr {err[-500:]!r}"

        hashes.append([line for line in out.splitlines() if line.startswith("frame ")])

    if not hashes[0] or hashes[0] != hashes[1]:
        return None, f"the demo's frames differ between runs: {hashes[0][-3:]} vs {hashes[1][-3:]}"

    return hashes[0], None


def terminal(vm, rootfs, image):
    """doom on a host's terminal: what it drew until a few seconds after
    it took the alternate screen and then ^C, and vm's status."""
    pid, fd = pty.fork()

    if pid == 0:
        fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 50, 80, 0, 0))
        os.execv(vm, [vm, "run", "--disk", f"image={image}", "--", rootfs, "-c", TERMINAL])

    out = b""

    def pump(seconds, until=None):
        nonlocal out
        end = time.monotonic() + seconds

        while time.monotonic() < end and not (until and until in out):
            if select.select([fd], [], [], 0.05)[0]:
                try:
                    chunk = os.read(fd, 65536)
                except OSError:
                    return

                if not chunk:
                    return

                out += chunk

    try:
        pump(180, b"\x1b[?1049h")
        pump(5)
        os.write(fd, b"\x03")
        pump(30, b"\x1b[?1049l")
        pump(1)
    finally:
        os.close(fd)

    _, status = os.waitpid(pid, 0)

    return out, os.waitstatus_to_exitcode(status)


def main():
    vm, rootfs, image = sys.argv[1:4]
    failed = []
    frames, error = timedemo(vm, rootfs, image)

    if error:
        failed.append(error)
    elif len(frames) < 10:
        failed.append(f"timedemo hashed only {len(frames)} seconds of frames")

    out, code = terminal(vm, rootfs, image)
    colors = out.count(b"\x1b[38;2;")

    if b"\x1b[?1049h" not in out:
        failed.append(f"the alternate screen is not taken: {out[:300]!r}")
    if colors < 10:
        failed.append(f"too few truecolor cells drawn: {colors}")
    if out.count(b"\xe2\x96\x80") < 1000:
        failed.append("too few half blocks drawn")
    if b"\x1b[?1049l" not in out:
        failed.append(f"the alternate screen is not given back: {out[-300:]!r}")
    if code != 0:
        failed.append(f"^C: exit {code}, not 0")

    for f in failed:
        print(f)

    assert not failed, f"{len(failed)} of the doom cases failed"
    print(f"{len(frames or [])} seconds of frames, identical runs, and a terminal's frames passed")


if __name__ == "__main__":
    main()
