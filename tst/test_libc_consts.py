#!/usr/bin/env python3
"""The constants the kernel shares with lib/c (bin/vm/libc_consts.go)
against lib/c's headers, all of them, as clang has them for wasm32.

A constant of the kernel's has lib/c's name, a macro's or an enumerator's,
and must have its value. One with a name of the kernel's own is the size
of a type (TYPES), or what an expression of lib/c's says (EXPRESSIONS):
its value, or with {v} in it something true of the value. A constant
that is none of these has no place in the file. Every type whose size
lib/c passes the kernel has its size there.

The test tells what it found: how many of the kernel's constants are
lib/c's, which differ, which lib/c knows nothing of, and, by header, the
integer constants lib/c has that the kernel has none for. A size lib/c
passes a function libc.wasm defines (lib/libc) is libc.wasm's to check.

    test_libc_consts.py <clang> <source root>
"""
import collections
import re
import subprocess
import sys
import tempfile
import textwrap
from pathlib import Path

# The C type each size of the kernel's is of.
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
    "sizeofItimerspec": "struct itimerspec",
    "sizeofSigevent": "struct sigevent",
    "sizeofJmpBuf": "jmp_buf",
    "sizeofLconv": "struct lconv",
    "sizeofMsghdr": "struct msghdr",
    "sizeofMmsghdr": "struct mmsghdr",
    "sizeofIfaddrs": "struct ifaddrs",
    "sizeofSockaddrLl": "struct sockaddr_ll",
    "sizeofSockaddrIn": "struct sockaddr_in",
    "sizeofResState": "struct __res_state",
    "sizeofNsMsg": "ns_msg",
    "sizeofNsRr": "ns_rr",
    "sizeofNetent": "struct netent",
    "sizeofPasswd": "struct passwd",
    "sizeofPollfd": "struct pollfd",
    "sizeofProtoent": "struct protoent",
    "sizeofRlimit": "struct rlimit",
    "sizeofRusage": "struct rusage",
    "sizeofSchedParam": "struct sched_param",
    "sizeofPosixSpawnattr": "posix_spawnattr_t",
    "sizeofMqAttr": "struct mq_attr",
    "sizeofMsqidDs": "struct msqid_ds",
    "sizeofSemidDs": "struct semid_ds",
    "sizeofSembuf": "struct sembuf",
    "sizeofAiocb": "struct aiocb",
    "sizeofPosixSpawnFileActions": "posix_spawn_file_actions_t",
    "sizeofPthreadAttr": "pthread_attr_t",
    "sizeofPthreadMutex": "pthread_mutex_t",
    "sizeofPthreadMutexattr": "pthread_mutexattr_t",
    "sizeofPthreadCond": "pthread_cond_t",
    "sizeofPthreadCondattr": "pthread_condattr_t",
    "sizeofPthreadRwlock": "pthread_rwlock_t",
    "sizeofPthreadRwlockattr": "pthread_rwlockattr_t",
    "sizeofPthreadBarrier": "pthread_barrier_t",
    "sizeofPthreadBarrierattr": "pthread_barrierattr_t",
    "sizeofMtx": "mtx_t",
    "sizeofCnd": "cnd_t",
    "sizeofSem": "sem_t",
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
    "sizeofUcred": "struct ucred",
    "sizeofSockaddrStorage": "struct sockaddr_storage",
    "sizeofCmsghdr": "struct cmsghdr",
}

# What the other constants with a name of the kernel's own are of.
EXPRESSIONS = {
    "localeNameItem": "_NL_LOCALE_NAME(0)",
    "sunPathMax": "sizeof(((struct sockaddr_un *)0)->sun_path)",
    "utsField": "sizeof(((struct utsname *)0)->sysname)",
    "lconvStrings": "offsetof(struct lconv, int_frac_digits) / sizeof(char *)",
    "lconvChars": "offsetof(struct lconv, int_n_sign_posn) - offsetof(struct lconv, int_frac_digits) + 1",
    "stoppedBy": "WIFSTOPPED(({v}) | 5 << 8) && WSTOPSIG(({v}) | 5 << 8) == 5",
    "continued": "WIFCONTINUED({v})",
}

# A typedef the kernel knows by the type it names.
ALIASES = {"sigjmp_buf": "jmp_buf"}

# What may stand in a macro that is a number: no braces, strings or statements.
NUMBER = re.compile(r"[\w\s()|&~^+\-*/<>',.?:!=]+")


def same(kernel, libc):
    """A number of the kernel's is lib/c's, which clang gives as 64 bits: a pointer's or an unsigned one's of 32 by its low half."""
    return kernel == libc or -2**32 < kernel < 2**32 and -2**32 < libc < 2**32 and kernel & 0xffffffff == libc & 0xffffffff


def identifiers(include):
    """lib/c's macros that may be numbers and its enumerators: name -> header."""
    found = {}
    for h in sorted(include.rglob("*.h")):
        text = h.read_text().replace("\\\n", " ")
        text = re.sub(r"/\*.*?\*/", " ", text, flags=re.S)
        for name, body in re.findall(r"^[ \t]*#[ \t]*define[ \t]+([A-Za-z_]\w*)(?!\()[ \t]+(.+?)[ \t]*(?://.*)?$", text, re.M):
            if NUMBER.fullmatch(body):
                found.setdefault(name, str(h.relative_to(include)))
        for body in re.findall(r"\benum\b[^{;]*\{([^}]*)\}", text):
            for name in re.findall(r"\b([A-Za-z_]\w*)\b\s*(?:=[^,]*)?(?:,|$)", body):
                found.setdefault(name, str(h.relative_to(include)))
    return found


def evaluate(clang, include, expressions):
    """What clang makes of each expression with all of lib/c's headers: an integer, or nothing for what is none."""
    headers = sorted(h for h in include.rglob("*.h") if not h.name.startswith("__") and h.parent.name not in ("bits", "machine"))
    head = "".join(f"#include <{h.relative_to(include)}>\n" for h in headers)
    keys = list(expressions)
    lines = [f"const long long v_{k} = (long long)({expressions[key]}); const int f_{k} = _Generic(({expressions[key]}), float: 1, double: 1, long double: 1, default: 0);" for k, key in enumerate(keys)]
    first = head.count("\n") + 1
    with tempfile.TemporaryDirectory(prefix="pizdos-libc-consts-") as directory:
        source = Path(directory) / "consts.c"
        for _ in range(8):
            source.write_text(head + "\n".join(lines) + "\n")
            result = subprocess.run([clang, "--target=wasm32-unknown-unknown", "-Xclang", "-mlong-double-64", "-nostdinc", "-ferror-limit=0", "-w",
                                     "-isystem", str(include), "-S", "-emit-llvm", "-o", "-", str(source)],
                                    capture_output=True, text=True, timeout=300)
            if result.returncode == 0:
                break
            bad = {int(n) - first for n in re.findall(r"consts\.c:(\d+):\d+: error", result.stderr)}
            assert bad and min(bad) >= 0, result.stderr[:2000]
            for k in bad:
                lines[k] = ""
        assert result.returncode == 0, result.stderr[:2000]
    values = {int(k): int(n) for k, n in re.findall(r"^@v_(\d+) = .*\bi64 (-?\d+)", result.stdout, re.M)}
    floats = {int(k) for k, n in re.findall(r"^@f_(\d+) = .*\bi32 (\d+)", result.stdout, re.M) if n != "0"}
    return {key: values[k] for k, key in enumerate(keys) if k in values and k not in floats}


def go_functions(root):
    """The kernel's functions of its Go; another's sizes are libc.wasm's own."""
    text = (root / "bin/vm/kernel.go").read_text()
    table = text[text.index("func (k *Kernel) functionTable()"):]

    return set(re.findall(r'^\t\t"(\w+)":', table[:table.index("\n}\n")], re.M))


def main():
    clang, root = sys.argv[1], Path(sys.argv[2])
    go = (root / "bin/vm/libc_consts.go").read_text()
    block = re.search(r"^const \(\n(.*?)^\)", go, re.M | re.S).group(1)
    consts = dict((name, int(value, 0)) for name, value in re.findall(r"^\t(\w+)(?:[ \t]+\w+)?[ \t]+= (-?(?:0x[0-9a-fA-F]+|0o[0-7]+|\d+))$", block, re.M))
    assert len(consts) == block.count("\n"), "a constant of lib/c's is a number, one a line"

    include = root / "lib/c/include"
    libc = identifiers(include)

    sizes = {name: value for name, value in consts.items() if name.startswith("sizeof")}
    assert set(sizes) == set(TYPES), f"sizes with no C type here, or C types with no size: {sorted(set(sizes) ^ set(TYPES))}"
    known = {TYPES[name] for name in sizes}
    passed = set()
    go = go_functions(root)
    for source in (root / "lib/c/src/kernel.c", include / "setjmp.h", include / "pty.h"):
        for line in source.read_text().splitlines():
            called = [a or b for a, b in re.findall(r"^KERNEL\w*\(.*?(\w+), \(|\b__kernel_(\w+)\(", line)]
            if not called or any(name in go for name in called):
                passed.update(re.findall(r"sizeof\(((?:struct )?\w+)\)", line))
    passed = {ALIASES.get(t, t) for t in passed}
    assert not passed - known, f"lib/c passes sizes the kernel does not know: {sorted(passed - known)}"

    expressions = {name: name for name in libc}
    nothing = []
    for name, value in consts.items():
        if name in TYPES:
            expressions[name] = f"sizeof({TYPES[name]})"
        elif name in EXPRESSIONS:
            expressions[name] = EXPRESSIONS[name].replace("{v}", str(value))
        elif name not in libc:
            # no macro the headers' text shows may still be a name clang knows
            expressions[name] = name
    values = evaluate(clang, include, expressions)

    differ = []
    for name, value in consts.items():
        if name not in values:
            nothing.append(name)
        elif not same(1 if "{v}" in EXPRESSIONS.get(name, "") else value, values[name]):
            differ.append(f"{name}: the kernel's {value}, lib/c's {values[name]}")
    theirs = collections.defaultdict(list)
    for name, header in libc.items():
        if name in values and name not in consts:
            theirs[header].append(name)

    own = len(TYPES) + len(EXPRESSIONS)
    print(f"libc consts: {len(consts)} constants of the kernel's: {len(consts) - own} have lib/c's name, {len(TYPES)} are sizes and {len(EXPRESSIONS)} what an expression says; "
          f"{len(differ)} differ from lib/c's, {len(nothing)} are of nothing of lib/c's; the kernel knows all {len(passed)} sizes that lib/c passes")
    print(f"lib/c has {sum(map(len, theirs.values()))} integer constants more, which the kernel has no constant for:")
    for header in sorted(theirs):
        print(textwrap.fill(" ".join(sorted(theirs[header])), 150, initial_indent=f"  {header}: ", subsequent_indent="    "))
    assert not differ, f"the kernel's and lib/c's differ: {differ}"
    assert not nothing, f"constants that are of nothing of lib/c's: {nothing}"


if __name__ == "__main__":
    main()
