#!/usr/bin/env python3
"""`vm run -- rootfs`: a system from one executable and nothing of the
host's file system. The image follows rootfs in its file, and vm gives
rootfs what follows as its disk named image, as it gives a file of the
host by --disk: the executable alone starts no system, and starts this
one with its image for the disk. erofs serves the image at the root, the
packages of the system in /pzd/store, the executables of busybox and
python among them, whose modules the kernel compiles as they are
executed, its realm, whose bin and etc are /bin and /etc, a /dev and a
/run, and starts
busybox's shell on the terminal; the shell's status is vm's. The shell's
HOME and TMPDIR are /run, and its TERM the host's name of the console's type, which
the terminfo database describes, under a terminal's other name by a
link: none where the host has none. Each
server's files are on a device of their own and belong to who made them,
a directory keeps its mode, a directory removed while it is read is
removed whole, mkdir -p finds what is there, and rename keeps a directory
out of itself and off a directory that is not empty. Appends from two
shells lose no line, flock's and fcntl's locks are an open file's, a
FIFO's readers and writers meet, a file has hard links, ctime is its own,
statvfs tells the capacity of /run and a root that is read-only, and who
is not root gets no more than the modes give.

    test_rootfs.py <vm> <rootfs>
"""
import os
import re
import struct
import subprocess
import sys
import tempfile
from pathlib import Path

SCRIPT = """\
echo hello from $0
echo "$PATH $HOME $TMPDIR ${TERM-of no type}"; cd && pwd && cd /
for i in 1 2 3; do ls /run > /dev/null; done; echo fresh > /run/fresh && cat /run/fresh && rm /run/fresh
cat /etc/motd
ls /bin | grep -x -e ash -e cat -e sh
ls -d /dev/null /dev/tty /dev/zero
echo dropped > /dev/null
head -c 4 /dev/zero | wc -c
readlink /usr && ls /usr && ls /usr/usr/bin/sh && ls -d /sbin /lib 2>&1
readlink -f /usr/bin && readlink -f /usr/bin/../etc/motd && cd /usr/bin && pwd -P && cd /run && ln -s .. up && readlink -f up/usr/bin && rm up && cd /
printf '#!/usr/bin/env sh\\necho through env\\n' > /run/e.sh && chmod +x /run/e.sh && /run/e.sh && rm /run/e.sh
readlink /bin/sh
test -x /bin/busybox && echo executable
head -c 4 /bin/busybox | od -An -tx1
(echo nothing > /etc/x) 2>/dev/null || echo read-only
echo hi > /run/a && echo more >> /run/a && cat /run/a | wc -l
chmod 600 /run/a && stat -c %a /run/a && truncate -s 2 /run/a && wc -c < /run/a
mkdir /run/d && mv /run/a /run/d/b && ls /run/d && rm -r /run/d && ls /run | wc -l
cd /run && echo x > y && touch -d @1000000000 y && stat -c %Y y && cd /
sh -c 'exit 5'; echo inner=$?
seq 1 20000 | sort -rn | head -1
head -c 8 /dev/urandom | wc -c
printf ab | od -An -tx1
python3 -c 'import json, os, sys; print(2 ** 100, sys.version_info[:2], len(os.urandom(5)), json.__file__)'
python3 -c 'import json, os; print(json.dumps(sorted(n for n in os.listdir("/usr/bin") if n.startswith("python"))))'
python3 -c 'import tempfile; f = tempfile.NamedTemporaryFile("w+"); f.write("x" * 5000); f.seek(0); print(len(f.read()), f.name.startswith("/run/"))'
python3 -c 'import os; d = os.urandom(3 << 20); f = os.open("/run/big", os.O_CREAT | os.O_RDWR); print(os.write(f, d), os.pread(f, 4 << 20, 0) == d, len(os.read(os.open("/dev/zero", os.O_RDONLY), 1 << 20)), len(os.read(os.open("/dev/urandom", os.O_RDONLY), 300000)))'; rm /run/big
ln -s target /run/sl && readlink /run/sl
ls -ld /run | cut -c1-10
mkdir -m 700 /run/m && ls -ld /run/m | cut -c1-10 && chmod 750 /run/m && stat -c %a /run/m
touch /run/o && test "$(stat -c %u /run/o)" = "$(id -u)" && echo owned
stat -c %d / /dev /run | sort -u | wc -l
mkdir -p /run/p/q /usr/bin && ls -d /run/p/q
mkdir /usr /run 2>&1
mkdir -p /run/r/s && mv /run/r /run/r/s/t 2>&1; ls -d /run/r/s
mkdir /run/many && for i in $(seq 1 300); do : > /run/many/f$i; done && rm -r /run/many && ls /run/many 2>&1
mkdir /run/u && touch /run/u/x /run/v && python3 -c 'import os; os.rename("/run/v", "/run/u")' 2>&1 | tail -1; ls /run/u
(for i in $(seq 1 200); do echo a$i >> /run/log; done) & (for i in $(seq 1 200); do echo b$i >> /run/log; done) & wait
sort -u /run/log | wc -l
touch /run/lk && flock -n /run/lk -c 'flock -n /run/lk -c true || echo held' && flock -n /run/lk -c 'echo free'
python3 -c '
import fcntl, os, sys
f = open("/run/rl", "w")
fcntl.lockf(f, fcntl.LOCK_EX, 10, 0)
if os.fork() == 0:
    g = open("/run/rl", "a")
    for start in (0, 10):
        try:
            fcntl.lockf(g, fcntl.LOCK_EX | fcntl.LOCK_NB, 10, start); print("taken", start)
        except OSError as e: print("refused", start)
    sys.stdout.flush()
    os._exit(0)
os.wait()
fcntl.lockf(f, fcntl.LOCK_UN, 10, 0)
fcntl.lockf(open("/run/rl", "a"), fcntl.LOCK_EX | fcntl.LOCK_NB, 10, 0); print("taken after")
'
mkfifo /run/ff && ls -l /run/ff | cut -c1
(echo through > /run/ff &); cat /run/ff
python3 -c '
import os
try:
    os.open("/run/ff", os.O_WRONLY | os.O_NONBLOCK)
except OSError as e: print(e.strerror)
r = os.open("/run/ff", os.O_RDONLY | os.O_NONBLOCK)
w = os.open("/run/ff", os.O_WRONLY); os.write(w, b"1"); os.close(w)
print(os.read(r, 10), os.read(r, 10))
w = os.open("/run/ff", os.O_WRONLY); os.write(w, b"2"); os.close(w)
print(os.read(r, 10))
w = os.open("/run/ro2", os.O_WRONLY | os.O_CREAT, 0o600)
try: os.read(w, 1)
except OSError as e: print(e.strerror)
'
echo hl > /run/h1 && ln /run/h1 /run/h2 && stat -c %h /run/h1 && [ "$(stat -c %i /run/h1)" = "$(stat -c %i /run/h2)" ] && rm /run/h1 && cat /run/h2 && stat -c %h /run/h2
mkdir /run/hd && ln /run/hd /run/hd2 2>&1; ln /bin/busybox /run/bb 2>&1
mkdir -p /run/nl/a /run/nl/b && stat -c %h /run/nl
touch -d @1000000000 /run/c && [ "$(stat -c %Z /run/c)" != 1000000000 ] && echo changed
python3 -c 'import os; t, r = os.statvfs("/run"), os.statvfs("/"); print(t.f_blocks, t.f_bsize, t.f_bfree < t.f_blocks, t.f_ffree < t.f_files, t.f_flag & 1, r.f_flag & 1, r.f_bfree, r.f_blocks > 1000)'
"""

# Who is not root may do no more than the modes say, as they are now: a
# directory's mode changed holds for what was looked up in it before.
PERMISSIONS = """\
echo a > /run/ro && chmod 400 /run/ro && (echo b >> /run/ro) 2>&1; cat /run/ro
mkdir /run/ns && touch /run/ns/f && chmod 600 /run/ns && ls /run/ns/f 2>&1; chmod 700 /run/ns
chmod 777 /run 2>&1
python3 -c 'import os; os.chown("/run/ro", 0, 0)' 2>&1 | tail -1
printf '#!/bin/sh\\necho ran\\n' > /run/s.sh && /run/s.sh 2>&1; chmod +x /run/s.sh && /run/s.sh
mkdir /run/cw && touch /run/cw/y && cd /run/cw && ls y && chmod 0 . && ls y 2>&1; chmod 700 . && ls y && cd /
mkdir /run/sd && touch /run/sd/z && ls /run/sd/z && chmod 600 /run/sd && ls /run/sd/z 2>&1; chmod 700 /run/sd && ls /run/sd/z
"""

PERMITTED = """\
sh: can't create /run/ro: Permission denied
a
ls: /run/ns/f: Permission denied
chmod: /run: Operation not permitted
PermissionError: [Errno 1] Operation not permitted: '/run/ro'
sh: /run/s.sh: Permission denied
ran
y
ls: y: Permission denied
y
/run/sd/z
ls: /run/sd/z: Permission denied
/run/sd/z
"""

WANT = """\
hello from sh
/bin /run /run of no type
/run
fresh
pizdos
ash
cat
sh
/dev/null
/dev/tty
/dev/zero
4
/
bin
dev
etc
pzd
run
usr
/usr/usr/bin/sh
ls: /sbin: No such file or directory
ls: /lib: No such file or directory
/pzd/store/<uid>-rlm-system/bin
/pzd/store/<uid>-etc-base/etc/motd
/pzd/store/<uid>-rlm-system/bin
/pzd/store/<uid>-rlm-system/bin
through env
/pzd/store/<uid>-bin-busybox/bin/sh
executable
 50 5a 44 31
read-only
2
600
2
b
0
1000000000
inner=5
20000
8
 61 62
1267650600228229401496703205376 (3, 14) 5 /pzd/store/<uid>-bin-python/bin/python3/json/__init__.py
["python", "python3"]
5000 True
3145728 True 1048576 300000
target
drwxrwxrwt
drwx------
750
owned
3
/run/p/q
mkdir: can't create directory '/usr': File exists
mkdir: can't create directory '/run': File exists
mv: can't rename '/run/r': Invalid argument
/run/r/s
ls: /run/many: No such file or directory
IsADirectoryError: [Errno 21] Is a directory: '/run/v' -> '/run/u'
x
400
held
free
refused 0
taken 10
taken after
p
through
No such device or address
b'1' b''
b'2'
Bad file descriptor
2
hl
1
ln: /run/hd2: Operation not permitted
ln: /run/bb: Invalid cross-device link
4
changed
262144 4096 True True 0 1 0 True
"""


def stored(output):
    """The output with every directory of the store's uid as <uid>: a uid
    is its recipe's, 22 digits of base 62."""
    return re.sub(rb"/pzd/store/[0-9A-Za-z]{22}-", b"/pzd/store/<uid>-", output)


def main():
    vm, rootfs = sys.argv[1:3]
    # No root, no cwd of the host's: the environment is empty but for what
    # vm needs, and the working directory is one nothing is in.
    # The system's user is root; PERMISSIONS run as one who is not.
    with tempfile.TemporaryDirectory(prefix="pizdos-rootfs-") as empty:
        env = {"W2X_MEMORY_TAIL": "strict", "W2X_TAIL_PROFILE": ""}
        for user, script, want in ((), SCRIPT, WANT), (("--user", "1000:1000"), PERMISSIONS, PERMITTED):
            result = subprocess.run([vm, "run", *user, "--", rootfs], input=(script + "exit 3\n").encode(), capture_output=True, cwd=empty,
                                    env=env, timeout=180)
            assert (result.returncode, stored(result.stdout).decode(), result.stderr) == (3, want, b""), result
        assert not os.listdir(empty), os.listdir(empty)
        result = subprocess.run([vm, "run", "--", rootfs], input=b"ls /missing\n", capture_output=True, cwd=empty, env=env, timeout=120)
        assert result.returncode == 1 and b"No such file" in result.stderr and result.stdout == b"", result
        # The console's type is the host's to name: here another name of
        # FreeBSD's console, whose description is under its first.
        script = b"cd /pzd/store/*-aux-terminfo/share/terminfo && echo $TERM && readlink a/$TERM && cmp a/$TERM c/cons25 && head -c 2 a/$TERM | od -An -tx1\n"
        result = subprocess.run([vm, "run", "--", rootfs], input=script, capture_output=True, cwd=empty, env=dict(env, TERM="ansis"),
                                timeout=120)
        assert (result.returncode, result.stdout, result.stderr) == (0, b"ansis\n../c/cons25\n 1a 01\n", b""), result
    # The file is rootfs's executable, to the length its header tells, and
    # the image: the two apart are the program and a disk.
    with tempfile.TemporaryDirectory(prefix="pizdos-rootfs-") as work:
        whole = Path(rootfs).read_bytes()
        (length,) = struct.unpack_from("<I", whole, 4)
        assert whole[:4] == b"PZD1" and whole[length:length + 4] == b"PZIM", "rootfs's executable and the image after it"
        program, image = Path(work) / "rootfs", Path(work) / "image"
        program.write_bytes(whole[:length])
        image.write_bytes(whole[length:])
        result = subprocess.run([vm, "run", "--", program], input=b"cat /etc/motd\n", capture_output=True, env=env, timeout=120)
        assert result.returncode != 0 and b"rootfs: no image" in result.stderr and result.stdout == b"", result
        result = subprocess.run([vm, "run", "--disk", f"image={image}", "--", program], input=b"cat /etc/motd\n", capture_output=True,
                                env=env, timeout=120)
        assert (result.returncode, result.stdout, result.stderr) == (0, b"pizdos\n", b""), result
    print("rootfs: busybox's shell runs from the image erofs serves, without the host's files")


if __name__ == "__main__":
    main()
