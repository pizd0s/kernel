#!/usr/bin/env python3
"""AF_UNIX's SOCK_SEQPACKET sockets of the system are the kernel's, bound to
paths of the VFS's (bin/vm/unix_socket.go): bind makes a socket node of
the path in its file system, which holds the socket's listener; connect
walks the path to it and queues a connection, which accept takes. A name
that is there is EADDRINUSE, a socket that does not listen or is closed
refuses, a name unlinked is gone, what is no socket refuses, and the
read-only image holds none. The sockets are a program the test brings
(tst/unix_sockets.cpp), which runs in /run and in /dev.

    test_unix_sockets.py <vm> <rootfs> <unix_sockets>
"""
import base64
import subprocess
import sys
import tempfile

SCRIPT = """\
python3 -c 'import base64, sys; open("/run/sockets", "wb").write(base64.b64decode(sys.stdin.read()))' <<'EOF'
{program}
EOF
chmod +x /run/sockets
/run/sockets /run/s
ls -l /run | grep -c '^s'
/run/sockets /dev/s
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
name {path}
connect after close Connection refused
unlink ok
connect after unlink No such file or directory
connect to a file Connection refused
bind in the image Read-only file system
"""

WANT = RUN.replace("{path}", "/run/s") + "0\n" + RUN.replace("{path}", "/dev/s")


def main():
    vm, rootfs, program = sys.argv[1:4]
    script = SCRIPT.replace("{program}", base64.encodebytes(open(program, "rb").read()).decode().strip())
    with tempfile.TemporaryDirectory(prefix="pizdos-sockets-") as empty:
        env = {"W2X_MEMORY_TAIL": "strict", "W2X_TAIL_PROFILE": ""}
        result = subprocess.run([vm, "run", "--", rootfs], input=(script + "exit 3\n").encode(), capture_output=True, cwd=empty, env=env,
                                timeout=180)
        assert (result.returncode, result.stdout.decode(), result.stderr.decode()) == (3, WANT, ""), result
    print("unix sockets: the system's are bound to paths of the VFS's, in /run and /dev, connected to and accepted through them")


if __name__ == "__main__":
    main()
