#!/usr/bin/env python3
"""AF_UNIX's SOCK_SEQPACKET sockets of the system are the VFS's, rootfs's,
bound to paths (bin/rootfs, the sockets): bind makes a socket node of
the path in its file system, MKNOD's, and rootfs keeps the listener by
the node; connect walks the path to the node, and rootfs queues a
connection, a pair of the kernel's, which accept takes. A name bound to
is EADDRINUSE, a socket that does not listen or is closed refuses, a name
unlinked is gone, what is no socket refuses, and the read-only image
holds none. The sockets are a program the test brings
(tst/unix_sockets.cpp), which runs in /run and in /dev: there, as every
name of /dev, the socket's name is the program's, registered before it
binds it and gone with it, and devfs unlinks none and makes none for a
bind.

    test_unix_sockets.py <vm> <rootfs> <unix_sockets>
"""
import base64
import sys
import tempfile

import system

SCRIPT = """\
python3 -c 'import base64, sys; open("/run/sockets", "wb").write(base64.b64decode(sys.stdin.read()))' <<'EOF'
{program}
EOF
chmod +x /run/sockets
/run/sockets /run/s
ls -l /run | grep -c '^s'
/run/sockets /dev/s s
ls /dev/s 2>&1
"""

RUN = """\
bind ok
stat ok
socket 1
bind again Address already in use
connect before listen Connection refused
listen ok
accept ok, 10 bytes: a message
child: connect ok, back back
connect after close Connection refused
unlink ok
connect after unlink No such file or directory
connect to a file Connection refused
bind in the image Read-only file system
"""

DEV = RUN.replace("unlink ok", "unlink Read-only file system").replace(
    "connect after unlink No such file or directory", "connect after unlink Connection refused")

WANT = (RUN + "0\n" + DEV + "bind of no name in /dev Read-only file system\n" +
        "ls: /dev/s: No such file or directory\n")


def main():
    vm, rootfs, program = sys.argv[1:4]
    script = SCRIPT.replace("{program}", base64.encodebytes(open(program, "rb").read()).decode().strip())
    with tempfile.TemporaryDirectory(prefix="pizdos-sockets-") as empty:
        env = {"VM_MEMORY_TAIL": "strict", "VM_TAIL_PROFILE": ""}
        result = system.shell(vm, rootfs, script + "exit 3\n", cwd=empty, env=env, timeout=180)
        assert (result.returncode, result.stdout.decode(), result.stderr.decode()) == (3, WANT, ""), result
    print("unix sockets: the system's are rootfs's, bound to socket nodes of the VFS's, in /run and registered ones of /dev, "
          "connected to and accepted through them")


if __name__ == "__main__":
    main()
