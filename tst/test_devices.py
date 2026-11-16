#!/usr/bin/env python3
"""The devices of /dev are services': rootfs's /dev/init takes a name, a
mode and the end of a pair (REGISTER, bin/tools/protocol.h), and every
open of /dev/<name> goes on to the service that registered it, which
serves the open file from then on. A read that waits there ends for a
signal: the kernel sends CANCEL, and the read restarts if the handler
says so, or dies with the process for SIGKILL. A service's file may tell
its readiness, which select, poll and epoll find; one that does not is
ready whenever asked. copy_file_range between such a file and
a memfd brings the memfd itself in the READ or WRITE, at its offset. The system's own, null, zero, random
and urandom, are /bin/devices', there before the shell starts. A service
here is a program the test brings: it registers a device whose bytes
are a text and serves its opens (tst/devices_service.cpp, which the test
hands to the system); the name goes when the service does, and what was
open of it fails.

    test_devices.py <vm> <rootfs> <devices_service>
"""
import base64
import os
import subprocess
import sys
import tempfile

SCRIPT = """\
ls /dev
stat -c '%F %a' /dev/init /dev/null
echo dropped > /dev/null && cat /dev/null | wc -c
head -c 5 /dev/zero | od -An -tx1
head -c 300 /dev/random | wc -c
python3 -c 'import os; print(os.stat("/dev/null").st_ino == os.fstat(os.open("/dev/null", os.O_RDONLY)).st_ino)'
python3 -c '
import os
try: os.read(os.open("/dev/zero", os.O_WRONLY), 1)
except OSError as e: print(e.strerror)
'
python3 -c 'import base64, sys; open("/tmp/service", "wb").write(base64.b64decode(sys.stdin.read()))' <<'EOF'
{service}
EOF
chmod +x /tmp/service
/tmp/service hello 020644 'hello, world'
cat /dev/hello; echo
stat -c '%F %a' /dev/hello
python3 -c 'import os; f = os.open("/dev/hello", os.O_RDONLY); print(os.pread(f, 5, 7), os.fstat(f).st_ino == os.stat("/dev/hello").st_ino)'
python3 -c '
import os
try: os.read(os.open("/dev/hello", os.O_WRONLY), 1)
except OSError as e: print(e.strerror)
'
/tmp/service copy /dev/hello 7 3 5
/tmp/service put /dev/null hello
/tmp/service hello 020644 again
/tmp/service a/b 020644 bad
/tmp/service fifo 010644 bad
/tmp/service readonly 020644 bad ro
/tmp/service disk 060640 'the blocks'
stat -c '%F %a' /dev/disk && head -c 3 /dev/disk; echo
python3 -c '
import os, signal, time
f = os.open("/dev/hello", os.O_RDONLY)
os.kill(int(open("/tmp/hello.pid").read()), signal.SIGKILL)
for _ in range(1000):
    if not os.path.exists("/dev/hello"): break
    time.sleep(0.01)
print(os.path.exists("/dev/hello"))
try: os.pread(f, 5, 0)
except OSError as e: print(e.strerror)
'
ls /dev/hello 2>&1
/tmp/service hello 020644 'hello again' && cat /dev/hello; echo
/tmp/service mbox 020666 '' wait
/tmp/service wait /dev/mbox 0
(sleep 2; printf ping > /dev/mbox) & /tmp/service wait /dev/mbox 1; wait
/tmp/service wait /dev/mbox 1 & sleep 1.5; kill -9 $!; wait $! 2> /dev/null; echo $?
(sleep 2; printf pong > /dev/mbox) & /tmp/service wait /dev/mbox 1; wait
printf mail > /dev/mbox; /tmp/service wait /dev/mbox 1
python3 -c '
import os, select
f = os.open("/dev/mbox", os.O_RDWR | os.O_NONBLOCK)
r, w, _ = select.select([f], [f], [], 0.2)
print(r == [], w == [f])
try: os.read(f, 4)
except OSError as e: print(e.strerror)
g = os.open("/dev/mbox", os.O_WRONLY)
os.write(g, b"note")
print(select.select([f], [], [], 1)[0] == [f], os.read(f, 16))
p = select.poll()
p.register(f, select.POLLIN)
print(p.poll(200))
os.write(g, b"more")
print(p.poll(1000) == [(f, select.POLLIN)], os.read(f, 16))
'
"""

WANT = """\
full
init
null
random
tty
urandom
zero
character special file 600
character special file 666
0
 00 00 00 00 00
300
True
Bad file descriptor
registered hello
hello, world
character special file 644
b'world' True
Bad file descriptor
5 12 8 ...world........
5
register hello: File exists
register a/b: Invalid argument
register fifo: Invalid argument
register readonly: Bad file descriptor
registered disk
block special file 640
the
False
Input/output error
ls: /dev/hello: No such file or directory
registered hello
hello again
registered mbox
Interrupted system call 1
ping 1
137
pong 1
mail 0
True True
Resource temporarily unavailable
True b'note'
[]
True b'more'
"""


def main():
    vm, rootfs, service = sys.argv[1:4]
    script = SCRIPT.replace("{service}", base64.encodebytes(open(service, "rb").read()).decode().strip())
    with tempfile.TemporaryDirectory(prefix="pizdos-devices-") as empty:
        env = {"W2X_MEMORY_TAIL": "strict", "W2X_TAIL_PROFILE": ""}
        result = subprocess.run([vm, "run", "--", rootfs], input=(script + "exit 3\n").encode(), capture_output=True, cwd=empty,
                                env=env, timeout=180)
        assert (result.returncode, result.stdout.decode(), result.stderr) == (3, WANT, b""), result
    print("devices: services register the devices of /dev at /dev/init, serve their opens, and take them along when they go")


if __name__ == "__main__":
    main()
