#!/usr/bin/env python3
"""busybox in the system: the image's /bin/busybox, each name of
busybox.links a symbolic link to it in /bin and /usr a link to /, runs
its applets, each case a boot of rootfs from the image of the tests, in
/run with tst/busybox's files, the image's /tests/busybox. The shell's
errors, exit and SIGINT are longjmps, one of them out of a signal handler;
its commands, pipes, substitutions and jobs are forks; xargs, find -exec
and awk's pipes are vforks; a loop without system calls is interrupted for
its signals. On a host's terminal the shell's jobs are the system's
pseudoterminal's (bin/tools/console.cpp): ^Z stops one, a read out of the
foreground stops another, fg continues them and ^C ends them.

    test_busybox.py <vm> <rootfs> <tests.img>
"""
import fcntl
import hashlib
import os
import pty
import select
import shlex
import signal
import struct
import subprocess
import sys
import termios
import time

DATA = "banana\napple\ncherry\napple\n10\n9\n"

# argv, standard input, expected standard output, expected exit code
CASES = [
    (["echo", "hello", "world"], "", "hello world\n", 0),
    (["sort"], DATA, "10\n9\napple\napple\nbanana\ncherry\n", 0),
    # lines that are not numbers are all equal as numbers
    (["sort", "-n", "-u"], DATA, "apple\n9\n10\n", 0),
    (["uniq", "-c"], "a\na\nb\n", "      2 a\n      1 b\n", 0),
    (["grep", "-n", "apple"], DATA, "2:apple\n4:apple\n", 0),
    (["grep", "-E", "^(ch|ba)"], DATA, "banana\ncherry\n", 0),
    (["grep", "-c", "kiwi"], DATA, "0\n", 1),
    (["sed", "-e", "s/a\\(p*\\)/[\\1]/g", "-e", "3d"], DATA, "b[]n[]n[]\n[pp]le\n[pp]le\n10\n9\n", 0),
    (["awk", "{n += length($0)} END {printf \"%d %d %.2f\\n\", NR, n, n / NR}"], DATA, "6 25 4.17\n", 0),
    (["tr", "a-z", "A-Z"], "abc\n", "ABC\n", 0),
    (["cut", "-d", "a", "-f", "2"], "xay\n", "y\n", 0),
    (["wc", "-l"], DATA, "6\n", 0),
    (["md5sum"], DATA, hashlib.md5(DATA.encode()).hexdigest() + "  -\n", 0),
    (["sha256sum"], DATA, hashlib.sha256(DATA.encode()).hexdigest() + "  -\n", 0),
    (["seq", "2", "4"], "", "2\n3\n4\n", 0),
    (["expr", "3", "*", "4", "+", "1"], "", "13\n", 0),
    (["printf", "%s-%03d\\n", "a", "7"], "", "a-007\n", 0),
    (["date", "-u", "-d", "@1700000000", "+%F %T %z %j %a"], "", "2023-11-14 22:13:20 +0000 318 Tue\n", 0),
    (["date", "-d", "@0", "+%H"], "", "03\n", 0, {"TZ": "MSK-3"}),
    (["basename", "/a/b/c.txt", ".txt"], "", "c\n", 0),
    (["dirname", "/a/b/c.txt"], "", "/a/b\n", 0),
    (["false"], "", "", 1),
    # exec inside the root: env runs /bin/env, a link to the executable
    (["env", "-i", "A=1", "env"], "", "A=1\n", 0),
    # a #! script runs its interpreter
    (["env", "/run/script.sh", "x"], "", "from /run/script.sh x\n", 0),
    # a symbolic link to / is the root's
    (["ls", "/run/up/bin/busybox"], "", "/run/up/bin/busybox\n", 0),
    (["readlink", "-f", "/run/up/.."], "", "/\n", 0),
    # the shell raises its errors and exit by longjmp
    (["sh", "-c", "echo ${u?unset}; echo no"], "", "", 2),
    (["sh", "-c", "echo $((1 / 0)); echo no"], "", "", 2),
    (["sh", "-c", "cd /missing || echo failed; echo next"], "", "failed\nnext\n", 0),
    (["sh", "-c", "set -e; false; echo no"], "", "", 1),
    (["sh", "-c", "f() { echo in $1; exit 7; }; f a; echo no"], "", "in a\n", 7),
    (["sh", "-c", "for i in 1 2 3; do [ $i = 2 ] && continue; echo $i; done"], "", "1\n3\n", 0),
    (["sh", "-s"], "x=4\necho $((x * x))\nexit 3\n", "16\n", 3),
    (["env", "/run/shell.sh", "a", "b"], "", "2 a b\n", 2),
    # fork: commands, pipes, substitutions, subshells, jobs
    (["sh", "-c", "echo $(echo sub) $(echo $(echo deep))"], "", "sub deep\n", 0),
    (["sh", "-c", "x=$(seq 3 | wc -l); echo x=$x"], "", "x=3\n", 0),
    (["sh", "-c", "echo a b c | tr a-z A-Z | sed s/B/-/"], "", "A - C\n", 0),
    (["sh", "-c", "seq 1 20000 | sort -rn | head -2; seq 1 100 | md5sum"], "",
     "20000\n19999\n" + hashlib.md5("".join(f"{i}\n" for i in range(1, 101)).encode()).hexdigest() + "  -\n", 0),
    (["sh", "-c", "(cd /bin && pwd); pwd"], "", "/bin\n/run\n", 0),
    (["sh", "-c", "f() { echo $1 | sed s/a/b/; }; for i in a aa; do f $i; done | sort"], "", "b\nba\n", 0),
    (["sh", "-c", "sh -c 'exit 4'; echo $?; (exit 3); echo $?"], "", "4\n3\n", 0),
    (["sh", "-c", "cat /run/script.sh > /run/bg.out & wait $!; echo rc=$?; cat /run/bg.out"], "", "rc=0\n#!/bin/echo from\n", 0),
    (["sh", "-c", "sleep 2 & p=$!; kill -STOP $p; kill -TERM $p; kill -CONT $p; wait $p; echo rc=$?"], "", "rc=143\n", 0),
    (["sh", "-c", "trap 'echo chld' CHLD; true & wait; echo done"], "", "chld\ndone\n", 0),
    # a loop without system calls takes its signals: Go interrupts it
    (["sh", "-c", "while :; do :; done & p=$!; sleep 1; kill $p; wait $p; echo rc=$?"], "", "rc=143\n", 0),
    (["sh", "-c", "(trap 'echo usr1; exit 3' USR1; while :; do :; done) & p=$!; sleep 1; kill -USR1 $p; wait $p; echo rc=$?"], "",
     "usr1\nrc=3\n", 0),
    # vfork
    (["xargs", "-n1", "echo", "one"], "a b\n", "one a\none b\n", 0),
    (["xargs", "missing"], "x\n", "", 127),
    (["find", "/run", "-maxdepth", "1", "-name", "shell.sh", "-exec", "echo", "found", "{}", ";"], "", "found /run/shell.sh\n", 0),
    (["awk", "{ print $0 | \"sort -r\" }"], "a\nc\nb\n", "c\nb\na\n", 0),
    (["awk", "BEGIN { while ((\"echo x; echo y\" | getline l) > 0) print \"l=\" l }"], "", "l=x\nl=y\n", 0),
]

# An interactive shell gets SIGINT while it reads a line, and while a loop
# of builtins blocks writing to a full pipe: there the handler longjmps out
# of the write. Each time it takes the next line after a new prompt.
INTERRUPTS = [
    ("", "$ ^C\n$ after\n"),
    ("while :; do echo 0123456789; done", "0123456789\n$ after\n"),
]


# Each boot of the system has a /run of its own: the files go there first,
# and up, a link to the root.
SETUP = "cp -a /tests/busybox/. /run && cd /run && ln -s / up"


def boot(vm, rootfs, image, script):
    return [vm, "run", "--disk", f"image={image}", "--", rootfs, "-c", f"{SETUP} && {script}"]


def run(vm, rootfs, image, argv, stdin, env=None):
    environment = {"TZ": "UTC", **(env or {})}
    script = shlex.join(["exec", "env", *(f"{k}={v}" for k, v in environment.items()), *argv])

    return subprocess.run(boot(vm, rootfs, image, script), input=stdin.encode(), capture_output=True, timeout=60)


def until(fd, marker, seconds):
    """What fd gives until it has given marker, or seconds pass: the shell
    reads its input once it is compiled and its first prompt is out."""
    out, end = b"", time.monotonic() + seconds

    while marker not in out and time.monotonic() < end:
        if select.select([fd], [], [], 0.05)[0]:
            chunk = os.read(fd, 4096)

            if not chunk:
                break

            out += chunk

    return out


def interrupt(vm, rootfs, image, first):
    """An interactive shell gets the host's SIGINT of vm, which the
    console's foreground group, the shell's, takes: after first, while its
    standard output is a pipe nobody reads, then the line that ends it."""
    p = subprocess.Popen(boot(vm, rootfs, image, "exec env PS1='$ ' sh -i"), stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                         stderr=subprocess.PIPE)
    prompt = until(p.stdout.fileno(), b"$ ", 60)

    if first:
        p.stdin.write(first.encode() + b"\n")
        p.stdin.flush()

    time.sleep(0.5)
    p.send_signal(signal.SIGINT)
    time.sleep(0.3)
    p.stdin.write(b"echo after; exit 5\n")
    p.stdin.flush()

    try:
        out, _ = p.communicate(timeout=60)
    except subprocess.TimeoutExpired:
        p.kill()
        out, _ = p.communicate()

    return (prompt + out).decode(errors="replace"), p.returncode


def job_control(vm, rootfs, image):
    """^Z stops a command on the terminal, jobs lists it, fg continues it
    and ^C ends it; a cat in the background stops for SIGTTIN as it reads,
    and fg takes it to the foreground, where it reads, and ^C ends it."""
    pid, fd = pty.fork()

    if pid == 0:
        fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
        os.execv(vm, boot(vm, rootfs, image, "exec env PS1='$ ' sh -i"))

    out = until(fd, b"$ ", 120)

    def pump(seconds):
        nonlocal out
        end = time.monotonic() + seconds

        while time.monotonic() < end:
            if select.select([fd], [], [], 0.05)[0]:
                try:
                    out += os.read(fd, 4096)
                except OSError:
                    return

    pump(0.3)

    for keys in [b"cat\r", b"\x1a", b"jobs\r", b"fg\r", b"\x03", b"echo after $?\r", b"cat &\r", b"jobs\r", b"fg\r", b"\x03",
                 b"echo again $?\r", b"exit 6\r"]:
        os.write(fd, keys)
        pump(0.6)

    pump(5)
    _, status = os.waitpid(pid, 0)
    os.close(fd)

    return out.decode(errors="replace").replace("\r", ""), os.waitstatus_to_exitcode(status)


# files: a tree made, archived, compressed and read back, in one boot
FILES = """\
mkdir -p /run/d/e && cp /run/script.sh /run/d/e/copy && ln -s e/copy /run/d/link &&
tar cf /run/d.tar -C /run d && gzip -k /run/d.tar && mv /run/d /run/old && tar xf /run/d.tar -C /run &&
gzip -dc /run/d.tar.gz | cmp - /run/d.tar && readlink /run/d/link && cat /run/d/e/copy && find /run/d -type f"""


def main():
    vm, rootfs, image = sys.argv[1:4]
    failed = []

    for case in CASES:
        argv, stdin, want, code = case[:4]
        env = case[4] if len(case) > 4 else None
        r = run(vm, rootfs, image, argv, stdin, env)
        got = r.stdout.decode(errors="replace")

        if got != want or r.returncode != code:
            failed.append(f"{argv}: exit {r.returncode}, stdout {got!r}, stderr {r.stderr.decode(errors='replace')!r}; want exit {code}, stdout {want!r}")

    r = subprocess.run(boot(vm, rootfs, image, FILES), capture_output=True, timeout=60)

    if (r.returncode, r.stdout) != (0, b"e/copy\n#!/bin/echo from\n/run/d/e/copy\n"):
        failed.append(f"files: exit {r.returncode}, stdout {r.stdout!r}, stderr {r.stderr!r}")

    r = run(vm, rootfs, image, ["cat", "/run/missing"], "")

    if r.returncode != 1 or b"No such file or directory" not in r.stderr:
        failed.append(f"cat of a missing file: exit {r.returncode}, stderr {r.stderr!r}")

    for first, tail in INTERRUPTS:
        out, code = interrupt(vm, rootfs, image, first)

        if not out.endswith(tail) or code != 5:
            failed.append(f"SIGINT to sh -i after {first!r}: exit {code}, stdout ends {out[-60:]!r}; want exit 5, {tail!r}")

    out, code = job_control(vm, rootfs, image)
    stops = out.count("Stopped") - out.count("Stopped (tty input)")

    if stops < 2 or "Stopped (tty input)" not in out or "after 130" not in out or "again 130" not in out or code != 6:
        failed.append(f"job control: exit {code}, terminal {out!r}")

    for f in failed:
        print(f)

    assert not failed, f"{len(failed)} of the busybox cases failed"
    print(f"{len(CASES) + 3 + len(INTERRUPTS)} cases passed")


if __name__ == "__main__":
    main()
