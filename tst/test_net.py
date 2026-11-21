#!/usr/bin/env python3
"""vm's network: with --net the system has a network device, a
SOCK_SEQPACKET end whose packets are Ethernet frames, which NETS names,
after the disks; behind it is gvisor-tap-vsock's network in vm, whose
gateway, 192.168.127.1, answers ARP, offers an address by DHCP and
answers an echo. tst/net_probe.cpp asks it, by frames alone; without
--net there is no device. On the device runs FreeBSD's stack, inet's:
its probe connects to a TCP server of the host's, at 192.168.127.254,
the host's 127.0.0.1 behind vm's network, sends a text and reads the
server's answer up to its close. In the system, devfs starts inet for
the card, which is /dev/net/0: its ctl, stats and addr, and, once "stack"
in its ctl has the stack's lease by DHCP, iface/status. sockmgr, which
devfs starts too, writes it of each card it finds, and the stack, up,
registers with sockmgr, whose ctl lists it. tst/inet_sockets.cpp, which
the test hands the system, finds the host's server by name, host.internal
of the gateway's name server, which dns asks for getaddrinfo, connects
through sockmgr to it, listens on the card's address and accepts a
connection of its own, whose epoll's watch, made of sockmgr's socket
before connect, follows it to the stack's, and finds the errors of a
socket on no stack and of a name the gateway has not; the shell asks
/dev/dns/query itself.

    test_net.py <vm> <net_probe> <inet> <rootfs> <inet_sockets>
"""
import base64
import socket
import subprocess
import sys
import threading

WANT = """\
arp: 192.168.127.1 is at 5a:94:ef:e4:0c:dd
dhcp: offered 192.168.127.2 by 192.168.127.1
icmp: echo reply from 192.168.127.1, pizdos
"""


def echo(server):
    """Answers one connection: what came, after "echo: ", and closes."""
    connection, _ = server.accept()

    with connection:
        connection.settimeout(30)
        connection.sendall(b"echo: " + connection.recv(100))


SYSTEM = """\
for i in $(seq 100); do grep -q '^stack 0 ' /dev/sockmgr/ctl && break; sleep 0.1; done
ls /dev/net /dev/net/0
cat /dev/net/0/ctl /dev/net/0/addr /dev/net/0/iface/status /dev/sockmgr/ctl
grep -c frames /dev/net/0/stats
echo nothing > /dev/net/0/ctl
echo stack > /dev/net/0/ctl
:
"""

SYSTEM_WANT = """\
/dev/net:
0

/dev/net/0:
addr
ctl
iface
stats
stack up
mtu 1500
02:70:7a:64:a8:af
inet 192.168.127.2/24
gateway 192.168.127.1
dns 192.168.127.1
lease 3600
auto *
stack 0 inet 192.168.127.2/24 gateway 192.168.127.1
2
"""

SYSTEM_ERRORS = "sh: write error: Invalid argument\nsh: write error: Device or resource busy\n"

SOCKETS = """\
base64 -d > /run/inet_sockets <<'PROGRAM'
{program}
PROGRAM
chmod +x /run/inet_sockets
for i in $(seq 100); do grep -q '^stack 0 ' /dev/sockmgr/ctl && break; sleep 0.1; done
/run/inet_sockets {port}
exec 3<>/dev/dns/query
echo HOST.internal a >&3
cat <&3
echo nope.internal a >&3
exec 3>&-
"""

SOCKETS_WANT = """\
host.internal is 192.168.127.254, host.internal
connected from 192.168.127.2
read echo: hello
read at the end 0
connect that does not wait: Operation now in progress
poll: 1 POLLOUT
accepted from 192.168.127.2
recv ping
epoll before the answer: 0
epoll: 1 EPOLLIN
read pong
read of a socket on no stack: -1 Transport endpoint is not connected
bind to no card's address: -1 Cannot assign requested address
socket of AF_INET6: -1 Address family not supported by protocol
nope.internal: Name does not resolve
gateway.internal: 192.168.127.1
host.internal a 192.168.127.254
"""

SOCKETS_ERRORS = "sh: write error: No such file or directory\n"


def main():
    vm, probe, inet, rootfs, sockets = sys.argv[1:6]
    r = subprocess.run([vm, "run", "--net", "--", probe], capture_output=True, text=True, timeout=120)

    if r.returncode != 0 or r.stdout != WANT:
        print(f"FAIL net: exit {r.returncode}, stdout {r.stdout!r}, want {WANT!r}\n{r.stderr[-2000:]}")
        sys.exit(1)

    r = subprocess.run([vm, "run", "--", probe], capture_output=True, text=True, timeout=120)

    if r.returncode != 1 or r.stdout != "NETS: none\n":
        print(f"FAIL net: without --net: exit {r.returncode}, stdout {r.stdout!r}\n{r.stderr[-2000:]}")
        sys.exit(1)

    server = socket.create_server(("127.0.0.1", 0))
    answering = threading.Thread(target=echo, args=(server,), daemon=True)
    answering.start()
    port = server.getsockname()[1]
    r = subprocess.run([vm, "run", "--net", "--", inet, "probe", "192.168.127.254", str(port), "hello"], capture_output=True,
                       text=True, timeout=120)
    want = f"connected to 192.168.127.254:{port}\nreceived: echo: hello\nclosed\n"

    if r.returncode != 0 or r.stdout != want:
        print(f"FAIL net: inet probe: exit {r.returncode}, stdout {r.stdout!r}, want {want!r}\n{r.stderr[-2000:]}")
        sys.exit(1)

    r = subprocess.run([vm, "run", "--net", "--", rootfs, "-c", SYSTEM], capture_output=True, text=True, timeout=120)

    if r.returncode != 0 or r.stdout != SYSTEM_WANT or r.stderr != SYSTEM_ERRORS:
        print(f"FAIL net: the card in the system: exit {r.returncode}\n{r.stdout}\nwant\n{SYSTEM_WANT}\n{r.stderr[-2000:]}")
        sys.exit(1)

    server = socket.create_server(("127.0.0.1", 0))
    answering = threading.Thread(target=echo, args=(server,), daemon=True)
    answering.start()

    with open(sockets, "rb") as f:
        program = base64.b64encode(f.read()).decode()

    script = SOCKETS.format(program=program, port=server.getsockname()[1])
    r = subprocess.run([vm, "run", "--net", "--", rootfs, "-c", script], capture_output=True, text=True, timeout=120)

    if r.returncode != 0 or r.stdout != SOCKETS_WANT or r.stderr != SOCKETS_ERRORS:
        print(f"FAIL net: sockets: exit {r.returncode}\n{r.stdout}\nwant\n{SOCKETS_WANT}\n{r.stderr[-2000:]}")
        sys.exit(1)

    print("net: vm's network device carries Ethernet frames to gvisor-tap-vsock's gateway, which answers ARP, DHCP and ICMP; "
          "inet's FreeBSD stack talks TCP on it to a server of the host's; the card is /dev/net/0, its stack up by sockmgr through its ctl; "
          "a program's sockets go through sockmgr to the card's stack")


if __name__ == "__main__":
    main()
