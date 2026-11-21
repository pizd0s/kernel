#!/usr/bin/env python3
"""vm's network: with --net the system has a network device, a
SOCK_SEQPACKET end whose packets are Ethernet frames, which NETS names,
after the disks; behind it is gvisor-tap-vsock's network in vm, whose
gateway, 192.168.127.1, answers ARP, offers an address by DHCP and
answers an echo. tst/net_probe.cpp asks it, by frames alone; without
--net there is no device. On the device runs FreeBSD's stack, inet's:
its probe connects to a TCP server of the host's, at 192.168.127.254,
the host's 127.0.0.1 behind vm's network, sends a text and reads the
server's answer up to its close.

    test_net.py <vm> <net_probe> <inet>
"""
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


def main():
    vm, probe, inet = sys.argv[1:4]
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

    print("net: vm's network device carries Ethernet frames to gvisor-tap-vsock's gateway, which answers ARP, DHCP and ICMP; "
          "inet's FreeBSD stack talks TCP on it to a server of the host's")


if __name__ == "__main__":
    main()
