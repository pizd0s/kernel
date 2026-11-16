#!/usr/bin/env python3
"""The console on a terminal of the host's: vm makes the terminal raw
while it runs, and rootfs runs the shell on a pseudoterminal of the
system's joined to the console (bin/tools/console.cpp), so the shell is
interactive and edits its lines; the terminal's window is the host's,
again as it changes; ^C interrupts the command in the foreground, not
the shell; the shell's status is vm's, and the host's terminal is as it
was once vm ends.

    test_console.py <vm> <rootfs>
"""
import fcntl
import os
import pty
import select
import signal
import struct
import sys
import termios
import time


def read_for(fd, seconds, until=None):
    out = b""
    end = time.time() + seconds
    while time.time() < end and not (until and out.endswith(until)):
        ready, _, _ = select.select([fd], [], [], 0.1)
        if not ready:
            continue
        try:
            data = os.read(fd, 4096)
        except OSError:
            break
        if not data:
            break
        out += data
    return out


def main():
    vm, rootfs = sys.argv[1:]
    env = dict(os.environ, W2X_MEMORY_TAIL="strict", W2X_TAIL_PROFILE="")
    pid, fd = pty.fork()
    if pid == 0:
        fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        os.execve("/bin/sh", ["sh", "-c", f"stty -g; exec 3>&1; '{vm}' run -- '{rootfs}'; echo status $?; stty -g"], env)
    prompt = b"~ # "
    start = read_for(fd, 120, prompt)
    assert start.endswith(prompt), start
    before = start.split(b"\r\n")[0]

    def typed(line, seconds=10):
        os.write(fd, line)
        return read_for(fd, seconds, prompt).replace(b"\x1b[J", b"")

    assert typed(b"echo hel\x7flo\r") == b"echo hel\blo\r\nhelo\r\n" + prompt
    assert typed(b"tty; stty size\r") == b"tty; stty size\r\n/dev/pts/0\r\n30 100\r\n" + prompt
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    os.kill(pid, signal.SIGWINCH)
    time.sleep(1)
    read_for(fd, 2, prompt)
    assert typed(b"stty size\r").endswith(b"stty size\r\n40 120\r\n" + prompt)
    os.write(fd, b"sleep 30\r")
    time.sleep(1)
    assert typed(b"\x03") == b"sleep 30\r\n^C\r\n" + prompt
    os.write(fd, b"exit 7\r")
    end = read_for(fd, 30)
    lines = end.replace(b"\r", b"").split(b"\n")
    assert b"status 7" in lines and lines[-2] == before, end
    assert os.waitpid(pid, 0)[1] == 0
    print("console: on a host's terminal the shell runs on a pseudoterminal of the system's, edits its lines, follows the window, "
          "and the terminal is given back")


if __name__ == "__main__":
    main()
