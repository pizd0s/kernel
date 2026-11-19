#!/usr/bin/env python3
"""The constants the kernel shares with lib/c (bin/vm/libc_consts.go) are
lib/c's: clang evaluates what each one is of with lib/c's headers for
wasm32. A constant is of the macro or the enumerator of its name, case and
underscores apart (eInval of EINVAL, fGetlk of F_GETLK); a sizeof is of
the type TYPES gives it; another is of the expression EXPRESSIONS gives
it, which is the constant's value, or with {v} in it is true of the
value. A constant that is of nothing of lib/c's has no place in the file.
Every type whose size lib/c passes the kernel has its size there.

    test_libc_consts.py <clang> <source root>
"""
import collections
import re
import subprocess
import sys
import tempfile
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

# What the constants that are no macro of their name are of.
EXPRESSIONS = {
    "rlimitCount": "RLIM_NLIMITS",
    "pAll": "P_ALL",
    "pPid": "P_PID",
    "pPgid": "P_PGID",
    "cldCont": "CLD_CONTINUED",
    "epollOneshot": "EPOLLONESHOT",
    "epollEt": "EPOLLET",
    "clockProcessCputime": "CLOCK_PROCESS_CPUTIME_ID",
    "clockThreadCputime": "CLOCK_THREAD_CPUTIME_ID",
    "globTildeChk": "GLOB_TILDE_CHECK",
    "localeNameItem": "_NL_LOCALE_NAME(0)",
    "sunPathMax": "sizeof(((struct sockaddr_un *)0)->sun_path)",
    "sockaddrMax": "sizeof(struct sockaddr_storage)",
    "guestCmsgHeader": "sizeof(struct cmsghdr)",
    "ucredSize": "sizeof(struct ucred)",
    "utsField": "sizeof(((struct utsname *)0)->sysname)",
    "epollEventSize": "sizeof(struct epoll_event)",
    "lconvStrings": "offsetof(struct lconv, int_frac_digits) / sizeof(char *)",
    "lconvChars": "offsetof(struct lconv, int_n_sign_posn) - offsetof(struct lconv, int_frac_digits) + 1",
    "stoppedBy": "WIFSTOPPED(({v}) | 5 << 8) && WSTOPSIG(({v}) | 5 << 8) == 5",
    "continued": "WIFCONTINUED({v})",
}

# A typedef the kernel knows by the type it names.
ALIASES = {"sigjmp_buf": "jmp_buf"}


def same(kernel, libc):
    """A number of the kernel's is lib/c's, which clang gives as 64 bits: a pointer's or an unsigned one's of 32 by its low half."""
    return kernel == libc or -2**32 < kernel < 2**32 and -2**32 < libc < 2**32 and kernel & 0xffffffff == libc & 0xffffffff


def main():
    clang, root = sys.argv[1], Path(sys.argv[2])
    go = (root / "bin/vm/libc_consts.go").read_text()
    block = re.search(r"^const \(\n(.*?)^\)", go, re.M | re.S).group(1)
    consts = dict((name, int(value, 0)) for name, value in re.findall(r"^\t(\w+)(?:[ \t]+\w+)?[ \t]+= (-?(?:0x[0-9a-fA-F]+|0o[0-7]+|\d+))$", block, re.M))
    assert len(consts) == block.count("\n"), "a constant of lib/c's is a number, one a line"

    include = root / "lib/c/include"
    headers = sorted(h for d in (".", "sys", "netinet", "arpa", "net") for h in (include / d).glob("*.h") if not h.name.startswith("__"))
    names = collections.defaultdict(list)
    for h in headers:
        text = h.read_text()
        found = re.findall(r"^[ \t]*#[ \t]*define[ \t]+([A-Za-z_]\w*)\b(?!\()", text, re.M)
        for body in re.findall(r"\benum\b[^{;]*\{([^}]*)\}", text):
            found += re.findall(r"\b([A-Za-z_]\w*)\b\s*(?:=[^,]*)?(?:,|$)", body)
        for name in found:
            names[name.replace("_", "").lower()].append(name)

    sizes = {name: value for name, value in consts.items() if name.startswith("sizeof")}
    assert set(sizes) == set(TYPES), f"sizes with no C type here, or C types with no size: {sorted(set(sizes) ^ set(TYPES))}"
    known = {TYPES[name]: (name, size) for name, size in sizes.items()}
    sources = [root / "lib/c/src/kernel.c", include / "setjmp.h", include / "pty.h"]
    passed = set()
    for source in sources:
        passed.update(re.findall(r"sizeof\(((?:struct )?\w+)\)", source.read_text()))
    passed = {ALIASES.get(t, t) for t in passed}
    unknown = sorted(passed - set(known))
    assert not unknown, f"lib/c passes sizes the kernel does not know: {unknown}"

    of, nothing = {}, []
    for name, value in consts.items():
        if name in TYPES:
            of[name] = f"sizeof({TYPES[name]})"
        elif name in EXPRESSIONS:
            of[name] = EXPRESSIONS[name].replace("{v}", str(value))
        elif names.get(name.lower()):
            of[name] = sorted(names[name.lower()], key=lambda n: (n.startswith("_"), n))[0]
        else:
            nothing.append(name)
    assert not nothing, f"constants that are of nothing of lib/c's: {nothing}"

    order = sorted(of)
    program = "".join(f"#include <{h.relative_to(include)}>\n" for h in headers)
    program += "".join(f"const long long v_{k} = (long long)({of[name]});\n" for k, name in enumerate(order))
    with tempfile.TemporaryDirectory(prefix="pizdos-libc-consts-") as directory:
        source = Path(directory) / "consts.c"
        source.write_text(program)
        result = subprocess.run([clang, "--target=wasm32-unknown-unknown", "-Xclang", "-mlong-double-64", "-nostdinc",
                                 "-isystem", str(include), "-S", "-emit-llvm", "-o", "-", str(source)],
                                capture_output=True, text=True, timeout=120)
    assert result.returncode == 0, result.stderr
    got = {int(k): int(n) for k, n in re.findall(r"^@v_(\d+) = .*\bi64 (-?\d+)", result.stdout, re.M)}
    assert len(got) == len(order), (len(got), len(order))
    wrong = []
    for k, name in enumerate(order):
        want = 1 if "{v}" in EXPRESSIONS.get(name, "") else consts[name]
        if not same(want, got[k]):
            wrong.append((name, of[name], consts[name], got[k]))
    assert not wrong, f"the kernel's and lib/c's differ (constant, what it is of, the kernel's, clang's): {wrong}"
    print(f"libc consts: the kernel's {len(order)} constants are lib/c's, {len(sizes)} of them sizes, and it knows all {len(passed)} sizes that lib/c passes")


if __name__ == "__main__":
    main()
