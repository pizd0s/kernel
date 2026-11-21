#!/usr/bin/env python3
"""Build inet for the system: FreeBSD's TCP/IP stack as F-Stack runs it in
one thread of a program, from F-Stack's release tarball, on the network
device vm gives the system. Of the tarball, its FreeBSD tree and its
lib: the stack's sources, FreeBSD's kernel files F-Stack's Makefile
compiles without netgraph, ipfw, IPsec, the NAT of ipfw's (libalias) and
the extra TCP stacks, and its
glue of the kernel's, compiled as F-Stack compiles them, for wasm32: the
machine is arm's 32-bit one (FreeBSD 15 keeps arm's headers, not i386's),
but for the headers of bin/inet/machine (the atomics plain operations of
one thread, no assembly, the basic types of a machine of 32-bit words)
and arm64's endian.h and in_cksum.h, which are the compiler's builtins and
C; the kernel's names lib/c has too are renamed (names.h). Its host's
side is ours, host.c for DPDK's and Linux's (memory, clocks, randomness,
the card on the device), inet.cpp, the card's files of /dev, dhcp.cpp
and stack.cpp, with the protocol's sources, against lib/c's headers;
wasm-ld links them with libc.a. fstack-wasm32.patch is what F-Stack's own files
take for granted of amd64 and glibc.

    make.py <clang> <wasm-ld> <libc-include> <libc.a> <f-stack.tar.gz> <inet.wasm>
"""
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
sys.path.insert(0, str(ROOT / "dev"))

import wasm  # noqa: E402

VERSION = "2.0"

KERNEL_FLAGS = ["-O2", "-fno-strict-aliasing", "-std=gnu99", "-w", "-D__FreeBSD__", "-D_KERNEL", "-DHAVE_KERNEL_OPTION_HEADERS",
                "-DFSTACK", "-DSMP", "-DINET", "-DINET6", "-DTCPHPTS", "-DRATELIMIT", "-DFF_LOOPBACK_SUPPORT"]
HOST_FLAGS = ["-DINET", "-DINET6", "-DFF_LOOPBACK_SUPPORT"]
CFLAGS = ["-O2", "-std=gnu11", "-Wall", "-Wextra", "-Werror"]
CXXFLAGS = ["-O2", "-std=c++23", "-fno-exceptions", "-fno-rtti", "-nostdinc++", "-Wall", "-Wextra", "-Werror"]

LINK = ["--stack-first", "--max-memory=4294967296", "--export=__heap_base", "--export=__stack_pointer"]

# F-Stack's Makefile's lists, found as its VPATH finds them.
SOURCES = """
    ff_compat ff_glue ff_freebsd_init ff_init_main ff_kern_condvar ff_kern_environment ff_kern_intr ff_kern_subr
    ff_kern_synch ff_kern_timeout ff_subr_epoch ff_lock ff_syscall_wrapper ff_subr_prf ff_vfs_ops ff_veth ff_route
    ff_stub_14_extra ff_vimage_glue
    sha1 siphash
    kern_descrip kern_event kern_fail kern_khelp kern_hhook kern_linker kern_mbuf kern_module kern_mtxpool
    kern_ntptime kern_osd kern_sysctl kern_tc kern_uuid link_elf md5c subr_capability subr_counter
    subr_eventhandler subr_kobj subr_lock subr_module subr_param subr_pcpu subr_sbuf subr_taskqueue subr_unit
    subr_smr sys_capability sys_generic sys_socket uipc_accf uipc_mbuf uipc_mbuf2 uipc_domain uipc_sockbuf
    uipc_socket uipc_syscalls
    bcd gsb_crc32 inet_ntoa jenkins_hash strlcpy strnlen arc4random_uniform strtoq
    in_cksum
    bpf bridgestp if if_bridge if_clone if_dead if_ethersubr if_loop if_llatbl if_media if_vlan if_vxlan in_fib
    in_gif ip_reass netisr pfil radix ifq route route_ctl route_rtentry route_tables route_helpers route_ifaddrs
    route_temporal nhop_utils nhop nhop_ctl rtsock slcompress vnet
    if_ether if_gif igmp in in_mcast in_pcb in_proto in_rmx ip_carp ip_divert ip_ecn ip_encap ip_fastfwd ip_icmp
    ip_id ip_input ip_mroute ip_options ip_output raw_ip tcp_hostcache tcp_input tcp_lro tcp_offload tcp_output
    tcp_reass tcp_sack tcp_subr tcp_syncache tcp_timer tcp_timewait tcp_usrreq tcp_ecn udp_usrreq cc cc_newreno
    cc_htcp cc_cubic
    dest6 frag6 icmp6 in6 in6_ifattach in6_mcast in6_pcb in6_proto in6_rmx in6_src ip6_forward ip6_id ip6_input
    ip6_fastfwd ip6_mroute ip6_output mld6 nd6 nd6_nbr nd6_rtr raw_ip6 route6 scope6 send udp6_usrreq in6_cksum
    in6_fib in6_gif
    uma_core
""".split()
VPATH = ["lib", "freebsd/kern", "freebsd/crypto", "freebsd/crypto/siphash", "freebsd/net", "freebsd/net/route",
         "freebsd/netinet", "freebsd/netinet/cc", "freebsd/netinet6", "freebsd/vm", "freebsd/libkern"]


def found(tree, name):
    for directory in VPATH:
        path = tree / directory / f"{name}.c"
        if path.exists():
            return path
    raise SystemExit(f"inet: {name}.c is in none of VPATH")


# The predefined macros a kernel's compile keeps, as kern.pre.mk filters
# them: no host's.
FILTERED = re.compile(r"__STDC__|__STDC_HOSTED__|__STDC_VERSION__|__APPLE__|__MACH__|__CYGWIN|__FreeBSD__|__linux|"
                      r"__gnu__linux__|\blinux\b|_WIN32|_WIN64|__STDC_EMBED")


def patched(tree, patch):
    """Applies a unified diff to a tree: every hunk's old lines are the
    file's at the place the hunk names, or it is an error. What comes
    before the first file's header is the patch's explanation."""
    name = None
    files = {}

    for line in patch.read_text().split("\n"):
        if line.startswith("--- a/"):
            name = None
        elif line.startswith("+++ b/"):
            name = line[len("+++ b/"):]
            files[name] = []
        elif name is not None and line.startswith("@@"):
            start = int(re.match(r"@@ -(\d+)", line)[1])
            files[name].append((start, [], []))
        elif name is not None and files[name] and line[:1] in (" ", "-", "+"):
            _, old, new = files[name][-1]

            if line[0] != "+":
                old.append(line[1:])

            if line[0] != "-":
                new.append(line[1:])

    for name, hunks in files.items():
        path = tree / name
        lines = path.read_text().split("\n")

        for start, old, new in reversed(hunks):
            if lines[start - 1:start - 1 + len(old)] != old:
                raise SystemExit(f"{patch.name}: {name}: the lines at {start} are not the patch's")

            lines[start - 1:start - 1 + len(old)] = new

        path.write_text("\n".join(lines))


def unpack(tarball, work):
    """The tarball's lib and freebsd, without DPDK and the rest."""
    top = f"f-stack-{VERSION}/"
    with tarfile.open(tarball) as archive:
        members = [m for m in archive.getmembers() if m.name.startswith((top + "lib/", top + "freebsd/"))]
        archive.extractall(work, members=members, filter="data")
    return work / top


def generated(cc, tree, gen):
    """The headers a kernel's build makes: the predefined macros it keeps,
    the kobj interfaces' and the vnode interface's, as F-Stack's Makefile
    runs FreeBSD's awk scripts; the machine's include directories."""
    gen.mkdir(parents=True)
    macros = subprocess.run([cc, "--target=wasm32-unknown-unknown", "-dM", "-E", "-"], input="", capture_output=True, text=True,
                            check=True).stdout
    (gen / "filtered_predefined_macros.h").write_text("".join(line + "\n" for line in macros.splitlines() if not FILTERED.search(line)))
    tools = tree / "freebsd/tools"
    for m in ("bus_if", "device_if", "linker_if"):
        subprocess.run(["awk", "-f", str(tools / "makeobjops.awk"), str(tree / "freebsd/kern" / f"{m}.m"), "-h"], cwd=gen, check=True)
    subprocess.run(["awk", "-f", str(tools / "makeobjops.awk"), str(tree / "freebsd/kern/linker_if.m"), "-c"], cwd=gen, check=True)
    for flag in ("-h", "-p", "-q"):
        subprocess.run(["awk", "-f", str(tools / "vnode_if.awk"), str(tree / "freebsd/kern/vnode_if.src"), flag], cwd=gen, check=True)
    arm = gen / "arm/machine"
    shutil.copytree(tree / "freebsd/arm/include", arm)
    ours = gen / "wasm/machine"
    ours.mkdir(parents=True)
    for name in ("endian.h", "in_cksum.h"):
        shutil.copy(tree / "freebsd/arm64/include" / name, ours / name)
    for header in sorted((HERE / "machine").glob("*.h")):
        shutil.copy(header, ours / header.name)


def main():
    if len(sys.argv) != 7:
        raise SystemExit(__doc__)
    cc, ld, include, libc, tarball, output = sys.argv[1:7]
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory(dir=output.parent, prefix="inet-") as work:
        work = Path(work)
        tree = unpack(tarball, work)
        patched(tree, HERE / "fstack-wasm32.patch")
        gen = work / "gen"
        generated(cc, tree, gen)

        lib = tree / "lib"
        kernel = [*KERNEL_FLAGS, "-include", "opt_global.h", "-include", str(HERE / "names.h"), "-undef", "-imacros",
                  str(gen / "filtered_predefined_macros.h"), "-I", str(lib / "include"), "-I", str(lib), "-I", str(gen / "wasm"),
                  "-I", str(gen / "arm"), "-I", str(lib / "opt"), "-I", str(gen), "-I", str(tree / "freebsd"),
                  "-I", str(tree / "freebsd/contrib/ck/include")]
        sources = [found(tree, n) for n in SOURCES] + [gen / "linker_if.c", HERE / "glue.c"]
        units = [(s, work / "o" / f"k{i}-{s.stem}.o", kernel) for i, s in enumerate(sources)]

        system = ["-isystem", str(Path(include).resolve())]
        host = [*HOST_FLAGS, "-I", str(lib), "-I", str(ROOT), *system]
        units += [(HERE / "host.c", work / "o/host.o", [*CFLAGS, *host])]
        ours = [HERE / "inet.cpp", HERE / "dhcp.cpp", HERE / "stack.cpp", HERE / "sockets.cpp", *sorted((ROOT / "lib/protocol").glob("*.cpp"))]
        units += [(s, work / "o" / f"h{i}-{s.stem}.o", [*CXXFLAGS, *host]) for i, s in enumerate(ours)]
        wasm.compile_all(cc, units)
        subprocess.run([ld, *LINK, "-o", str(output), *(str(o) for _, o, _ in units), libc], check=True)


if __name__ == "__main__":
    main()
