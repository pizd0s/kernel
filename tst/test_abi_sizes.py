#!/usr/bin/env python3
"""The sizes the kernel knows of the structures lib/c hands it
(bin/vm/sizes.go) are lib/c's: clang lays each type out for wasm32
with lib/c's headers. Every type whose size lib/c passes has one.

    test_abi_sizes.py <clang> <source root>
"""
import re
import subprocess
import sys
import tempfile
from pathlib import Path

# The C type each size of the kernel's is of (bin/vm/sizes.go).
TYPES = {
    "sizeofAddrinfo": "struct addrinfo",
    "sizeofDirent": "struct dirent",
    "sizeofEpollEvent": "struct epoll_event",
    "sizeofFdSet": "fd_set",
    "sizeofFlock": "struct flock",
    "sizeofGlob": "glob_t",
    "sizeofGroup": "struct group",
    "sizeofHostent": "struct hostent",
    "sizeofIfNameindex": "struct if_nameindex",
    "sizeofInAddr": "struct in_addr",
    "sizeofIovec": "struct iovec",
    "sizeofItimerval": "struct itimerval",
    "sizeofJmpBuf": "jmp_buf",
    "sizeofLconv": "struct lconv",
    "sizeofMsghdr": "struct msghdr",
    "sizeofNetent": "struct netent",
    "sizeofPasswd": "struct passwd",
    "sizeofPollfd": "struct pollfd",
    "sizeofProtoent": "struct protoent",
    "sizeofRegex": "regex_t",
    "sizeofRegmatch": "regmatch_t",
    "sizeofRlimit": "struct rlimit",
    "sizeofRusage": "struct rusage",
    "sizeofSchedParam": "struct sched_param",
    "sizeofServent": "struct servent",
    "sizeofSigaction": "struct sigaction",
    "sizeofSiginfo": "siginfo_t",
    "sizeofSigset": "sigset_t",
    "sizeofStack": "stack_t",
    "sizeofStat": "struct stat",
    "sizeofStatfs": "struct statfs",
    "sizeofStatvfs": "struct statvfs",
    "sizeofSysinfo": "struct sysinfo",
    "sizeofTermios": "struct termios",
    "sizeofTimespec": "struct timespec",
    "sizeofTimeval": "struct timeval",
    "sizeofTimezone": "struct timezone",
    "sizeofTm": "struct tm",
    "sizeofTms": "struct tms",
    "sizeofUtimbuf": "struct utimbuf",
    "sizeofUtsname": "struct utsname",
    "sizeofWinsize": "struct winsize",
}

# A typedef the kernel knows by the type it names.
ALIASES = {"sigjmp_buf": "jmp_buf"}


def main():
    clang, root = sys.argv[1], Path(sys.argv[2])
    go = (root / "bin/vm/sizes.go").read_text()
    sizes = dict(re.findall(r"^\t(sizeof\w+)\s*=\s*(\d+)\s*$", go, re.M))
    assert set(sizes) == set(TYPES), f"sizes with no C type here, or C types with no size: {sorted(set(sizes) ^ set(TYPES))}"
    known = {TYPES[name]: (name, int(size)) for name, size in sizes.items()}
    sources = [root / "lib/c/src/kernel.c", root / "lib/c/include/setjmp.h", root / "lib/c/include/pty.h"]
    passed = set()
    for source in sources:
        passed.update(re.findall(r"sizeof\(((?:struct )?\w+)\)", source.read_text()))
    passed = {ALIASES.get(t, t) for t in passed}
    unknown = sorted(passed - set(known))
    assert not unknown, f"lib/c passes sizes the kernel does not know: {unknown}"

    includes = re.findall(r"^#include <[^>]+>$", sources[0].read_text(), re.M) + ["#include <pty.h>", "#include <setjmp.h>"]
    types = sorted(known)
    program = "\n".join(includes + [f"char size_{k}[sizeof({t})];" for k, t in enumerate(types)]) + "\n"
    with tempfile.TemporaryDirectory(prefix="pizdos-abi-sizes-") as directory:
        source = Path(directory) / "sizes.c"
        source.write_text(program)
        result = subprocess.run([clang, "--target=wasm32-unknown-unknown", "-Xclang", "-mlong-double-64", "-nostdinc",
                                 "-isystem", str(root / "lib/c/include"), "-S", "-emit-llvm", "-o", "-", str(source)],
                                capture_output=True, text=True, timeout=120)
    assert result.returncode == 0, result.stderr
    laid = {int(k): int(n) for k, n in re.findall(r"^@size_(\d+) = .*\[(\d+) x i8\]", result.stdout, re.M)}
    assert len(laid) == len(types), (len(laid), len(types))
    wrong = [(t, known[t][1], laid[k]) for k, t in enumerate(types) if known[t][1] != laid[k]]
    assert not wrong, f"kernel and lib/c sizes differ (type, kernel, lib/c): {wrong}"
    print(f"abi sizes: the kernel's {len(types)} structure sizes are lib/c's, and it knows all {len(passed)} that lib/c passes")


if __name__ == "__main__":
    main()
