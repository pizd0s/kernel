#!/usr/bin/env python3
"""vm's network: with --net the system has a network device, a
SOCK_SEQPACKET end whose packets are Ethernet frames, which NETS names,
after the disks; behind it is gvisor-tap-vsock's network in vm, whose
gateway, 192.168.127.1, answers ARP, offers an address by DHCP and
answers an echo. tst/net_probe.cpp asks it, by frames alone; without
--net there is no device.

    test_net.py <vm> <net_probe>
"""
import subprocess
import sys

WANT = """\
arp: 192.168.127.1 is at 5a:94:ef:e4:0c:dd
dhcp: offered 192.168.127.2 by 192.168.127.1
icmp: echo reply from 192.168.127.1, pizdos
"""


def main():
    vm, probe = sys.argv[1:3]
    r = subprocess.run([vm, "run", "--net", "--", probe], capture_output=True, text=True, timeout=120)

    if r.returncode != 0 or r.stdout != WANT:
        print(f"FAIL net: exit {r.returncode}, stdout {r.stdout!r}, want {WANT!r}\n{r.stderr[-2000:]}")
        sys.exit(1)

    r = subprocess.run([vm, "run", "--", probe], capture_output=True, text=True, timeout=120)

    if r.returncode != 1 or r.stdout != "NETS: none\n":
        print(f"FAIL net: without --net: exit {r.returncode}, stdout {r.stdout!r}\n{r.stderr[-2000:]}")
        sys.exit(1)

    print("net: vm's network device carries Ethernet frames to gvisor-tap-vsock's gateway, which answers ARP, DHCP and ICMP")


if __name__ == "__main__":
    main()
