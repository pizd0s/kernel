#!/usr/bin/env python3
"""CPython in the system: python3 is the image's /bin/python3, of its
package (bin/python), its standard library its own file's. A script, its
standard input, prints what the interpreter computes and what it gets
from the kernel: processes, pipes, a pseudoterminal, signals, timers,
locks, mappings written back, the user database, locales, the
tokenizer's positions, sockets of the network's family on the loopback,
and the modules of the libraries of lib/: compression, SQLite, XML,
decimal, CJK's codecs and ndbm. Then a set of CPython's own tests, from
the release tarball's Lib/test, the image of the tests'
/tests/python/test, which PYTHONPATH names, runs with its regrtest, but
for the cases SKIPPED names and XFAIL's, which run apart and must fail.
Each boots rootfs from the image of the tests and runs in /run.

    test_python.py <vm> <rootfs> <tests.img>
"""
import shlex
import subprocess
import sys

import system

# CPython's tests that need nothing the build leaves out: no threads, no
# test modules of the C API.
TESTS = [
    "test_bisect", "test_bool", "test_dict", "test_errno", "test_exceptions", "test_float", "test_fstring",
    "test_glob", "test_grammar", "test_grp", "test_list", "test_long", "test_pwd", "test_re", "test_resource",
    "test_scope", "test_select", "test_set", "test_str", "test_string", "test_struct", "test_time", "test_tuple",
    "test_zlib", "test_gzip", "test_bz2", "test_lzma", "test_zstd", "test_zipfile", "test_tarfile", "test_sqlite3",
    "test_decimal", "test_xml_etree", "test_xml_etree_c", "test_pyexpat", "test_minidom", "test_sax",
    "test_multibytecodec", "test_codecencodings_cn", "test_codecencodings_hk", "test_codecencodings_iso2022",
    "test_codecencodings_jp", "test_codecencodings_kr", "test_codecencodings_tw", "test_dbm", "test_dbm_ndbm",
]

# Cases the system does not run: those that start threads, which CPython's
# tests take every platform but WASI's and Emscripten's to have; those of a
# recursion in C deeper than the kernel lets a stack grow, which python,
# as it checks the depth of none, dies of; and ZipInfo's of the tests' own
# files, whose times in the image are of 1970, before ZIP's.
SKIPPED = [
    "*test_bz2.BZ2FileTest.testThreading", "*test_zstd.FreeThreadingMethodTests.*", "*test_sqlite3.test_dbapi.ThreadTests.*",
    "*test_decimal.*ThreadingTest.test_threading", "*.test_deeply_nested_deepcopy",
    "*test_pyexpat.ElementDeclHandlerTest.test_deeply_nested_content_model", "*test_zipfile.test_core.ZipInfoTests.test_from_*",
]

# Cases that expect RecursionError of a recursion in C deeper than a C stack
# of a fixed size holds. The system's stack grows in blocks up to the
# kernel's limit, which their recursion stays within, so python checks the
# depth of none: they fail, and the run checks that they still do.
XFAIL = ["*test_dict.DictTest.test_repr_deep", "*test_list.ListTest.test_repr_deep", "*test_tuple.TupleTest.test_hash_deeply_nested"]

SCRIPT = r'''
import bz2, decimal, dbm, dbm.ndbm, errno, fcntl, hashlib, io, json, locale, lzma, math, mmap, os, pty, pwd, random, re
import resource, select, signal, socket, sqlite3, struct, subprocess, sys, time, tokenize, zlib
import xml.etree.ElementTree as ET
from compression import zstd

print(sys.version.split()[0], sys.platform, len(sys.builtin_module_names))
print(hashlib.sha256(b"abc").hexdigest()[:16], random.Random("seed").random())
print((2**200 + 1) // 7 % 10**9, repr(1 / 3), format(2.5e-5, ".17g"), struct.pack("<dq", 1.1, -5).hex())
print(json.dumps({"x": [1.5, 1e300, -0.0]}), re.sub(r"(\w+)", r"<\1>", "hi there"), sorted({"b": 1, "a": 2}))

# the tokenizer's positions, which a misordered spill once lost
print([t.start for t in tokenize.generate_tokens(io.StringIO("a b\n").readline)])
try:
    compile("1 +\n", "<s>", "exec")
except SyntaxError as e:
    print("syntax", e.lineno, e.offset, repr(e.text))

# processes: fork and wait, the shell, exec of python itself
pid = os.fork()
if pid == 0:
    os._exit(7)
print("fork", os.waitpid(pid, 0)[1] >> 8)
print("sh", subprocess.run(["sh", "-c", "echo $((6 * 7))"], capture_output=True, text=True).stdout.strip())
print("python", subprocess.run([sys.executable, "-c", "print(2 ** 10)"], capture_output=True, text=True).stdout.strip())
pid = os.fork()
if pid == 0:
    os._exit(3)
info = os.waitid(os.P_PID, pid, os.WEXITED)
print("waitid", info.si_status, info.si_code == os.CLD_EXITED)

# pipes and select
rd, wr = os.pipe()
print("select", select.select([rd], [], [], 0)[0], end=" ")
os.write(wr, b"x")
print(select.select([rd], [], [], 1)[0] == [rd], os.read(rd, 1))

# a pseudoterminal: forkpty's child writes to its terminal; the master
# reads it, then EIO once the slave is closed
pid, master = pty.fork()
if pid == 0:
    os.write(1, b"from the child\n")
    os._exit(0)
out = b""
while True:
    try:
        chunk = os.read(master, 100)
    except OSError:
        break
    if not chunk:
        break
    out += chunk
os.waitpid(pid, 0)
print("pty", out.replace(b"\r", b"").decode().strip(), os.isatty(master))


# signals and timers
got = []
signal.signal(signal.SIGALRM, lambda s, f: got.append(s))
signal.setitimer(signal.ITIMER_REAL, 0.05)
time.sleep(0.5)
print("alarm", got == [signal.SIGALRM])
signal.signal(signal.SIGUSR1, lambda s, f: got.append(s))
os.kill(os.getpid(), signal.SIGUSR1)
print("kill", got[-1] == signal.SIGUSR1)
signal.pthread_sigmask(signal.SIG_BLOCK, [signal.SIGUSR2])
os.kill(os.getpid(), signal.SIGUSR2)
print("sigwait", signal.sigwait([signal.SIGUSR2]) == signal.SIGUSR2)

# files: locks, a shared mapping written back, times
path = "/run/data"
with open(path, "wb") as f:
    f.write(b"hello world")
fd = os.open(path, os.O_RDWR)
fcntl.flock(fd, fcntl.LOCK_EX)
fcntl.lockf(fd, fcntl.LOCK_EX)
m = mmap.mmap(fd, 0)
m[0:5] = b"HELLO"
m.flush()
print("mmap", os.pread(fd, 11, 0))
m.close()
os.utime(path, (1000000000, 1000000000))
print("utime", os.stat(path).st_mtime, os.path.getsize(path))
os.close(fd)

# the system: limits, users, scheduling, locale, clocks
print("rlimit", resource.getrlimit(resource.RLIMIT_NOFILE)[0] > 0, pwd.getpwnam("root").pw_uid, pwd.getpwuid(0).pw_name)
print("sched", os.sched_getscheduler(0), os.getpriority(os.PRIO_PROCESS, 0))
print("locale", locale.setlocale(locale.LC_ALL, ""), locale.getpreferredencoding())
t = time.process_time()
sum(range(100000))
print("clock", time.process_time() > t, time.monotonic() > 0)

# sockets on the loopback: a TCP server of port 0, whose name has its port
# from the bind on, and a connection to it; a datagram, and a socket of its
# descriptor's; a pair; a new socket's options; the names of the resolver,
# of AI_ADDRCONFIG's family the loopback's too; a sendall of more than a
# packet of the kernel's to the stack, to a reader of its own, and a
# datagram too big, EMSGSIZE, of a socket on no stack and of a bound one
srv = socket.socket()
srv.bind(("127.0.0.1", 0))
port = srv.getsockname()[1]
srv.listen()
c = socket.create_connection(("127.0.0.1", port))
conn, addr = srv.accept()
c.sendall(b"tcp")
print("tcp", port != 0, srv.getsockname()[1] == port, conn.recv(3), addr[0])
u = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
u.bind(("127.0.0.1", 0))
socket.socket(socket.AF_INET, socket.SOCK_DGRAM).sendto(b"udp", u.getsockname())
v = socket.socket(fileno=os.dup(u.fileno()))
print("udp", u.recvfrom(3)[0], v.type == socket.SOCK_DGRAM, v.getsockopt(socket.SOL_SOCKET, socket.SO_RCVTIMEO, 16) == bytes(16))
a, b = socket.socketpair()
a.sendall(b"pair")
n = socket.socket()
print("pair", b.recv(4), n.getsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR), n.getsockopt(socket.SOL_SOCKET, socket.SO_TYPE))
configured = socket.getaddrinfo("127.0.0.1", 80, socket.AF_UNSPEC, socket.SOCK_STREAM, 0, socket.AI_ADDRCONFIG)
print("names", socket.getaddrinfo("localhost", 80, socket.AF_INET, socket.SOCK_STREAM)[0][4], socket.gethostbyname("localhost"), len(configured))
big = bytes(range(256)) * 1024
reader = os.fork()
if reader == 0:
    c.close()
    got = bytearray()
    while len(got) < len(big) and (d := conn.recv(65536)):
        got += d
    os._exit(got != big)
c.sendall(big)
c.close()
refused = []
for s, size in ((socket.socket(socket.AF_INET, socket.SOCK_DGRAM), 65480), (u, 70000)):
    try:
        s.sendto(bytes(size), ("127.0.0.1", 9))
    except OSError as e:
        refused.append(errno.errorcode[e.errno])
print("large", os.waitpid(reader, 0)[1], refused)

# the libraries' modules: compression, SQLite of a journal of WAL's, XML,
# decimal, CJK's codecs, ndbm
data = b"pizdos " * 1000
print("compress", [m.decompress(m.compress(data)) == data for m in (zlib, bz2, lzma, zstd)], zlib.ZLIB_RUNTIME_VERSION)
db = sqlite3.connect("/run/t.db")
db.execute("pragma journal_mode=wal")
db.execute("create table t (v)")
db.executemany("insert into t values (?)", [("a",), ("b",)])
db.commit()
print("sqlite", sqlite3.sqlite_version, db.execute("select group_concat(v) from t").fetchone()[0], db.execute("pragma journal_mode").fetchone()[0])
print("xml", ET.fromstring("<a><b>t</b></a>").find("b").text, decimal.Decimal(1) / decimal.Decimal(7), "日本".encode("shift_jis").hex())
with dbm.ndbm.open("/run/db", "c") as d:
    d[b"k"] = b"v"
print("dbm", dbm.whichdb("/run/db"), dbm.ndbm.open("/run/db")[b"k"])
'''

EXPECTED = '''3.14.10 pizdos 85
ba7816bf8f01cfea 0.1382359792209653
547900196 0.3333333333333333 2.5000000000000001e-05 9a9999999999f13ffbffffffffffffff
{"x": [1.5, 1e+300, -0.0]} <hi> <there> ['a', 'b']
[(1, 0), (1, 2), (1, 3), (2, 0)]
syntax 1 4 '1 +\\n'
fork 7
sh 42
python 1024
waitid 3 True
select [] True b'x'
pty from the child True
alarm True
kill True
sigwait True
mmap b'HELLO world'
utime 1000000000.0 11
rlimit True 0 root
sched 0 0
locale C.UTF-8;C;C;C;C;C UTF-8
clock True True
tcp True True b'tcp' 127.0.0.1
udp b'udp' True True
pair b'pair' 0 1
names ('127.0.0.1', 80) 127.0.0.1 1
large 0 ['EMSGSIZE', 'EMSGSIZE']
compress [True, True, True, True] 1.3.2
sqlite 3.54.1 a,b wal
xml t 0.1428571428571428571428571429 93fa967b
dbm dbm.ndbm b'v'
'''


def python(vm, rootfs, image, args, timeout, stdin=None):
    """python3 of args in /run, its standard input stdin's lines by a
    here-document; its standard output is the terminal, where NO_COLOR
    keeps regrtest's words plain."""
    script = "cd /run && exec env TZ=UTC NO_COLOR=1 PYTHONPATH=/tests/python python3 " + " ".join(args)

    if stdin is not None:
        assert stdin.endswith("\n") and "\nPYTHON_EOF\n" not in stdin
        script += " <<'PYTHON_EOF'\n" + stdin + "PYTHON_EOF"

    r = system.shell(vm, rootfs, script, ("--disk", f"image={image}"), timeout=timeout)

    return subprocess.CompletedProcess(r.args, r.returncode, r.stdout.decode(errors="replace"), r.stderr.decode(errors="replace"))


def main():
    vm, rootfs, image = sys.argv[1:4]
    failures = []
    r = python(vm, rootfs, image, ["-"], 300, SCRIPT)

    if r.returncode != 0 or r.stdout != EXPECTED:
        failures.append(f"check.py: exit {r.returncode}\n--- stdout\n{r.stdout}\n--- expected\n{EXPECTED}\n--- stderr\n{r.stderr[-3000:]}")

    r = python(vm, rootfs, image, ["-m", "test", *TESTS, *(shlex.quote("--ignore=" + x) for x in SKIPPED + XFAIL)], 1800)

    if r.returncode != 0 or "Result: SUCCESS" not in r.stdout:
        failures.append(f"regrtest: exit {r.returncode}\n{r.stdout[-5000:]}\n{r.stderr[-2000:]}")

    r = python(vm, rootfs, image, ["-m", "test", "test_dict", "test_list", "test_tuple", *(shlex.quote("--match=" + x) for x in XFAIL)], 600)

    n = len(XFAIL)

    if "Result: FAILURE" not in r.stdout or f"run={n}/{n}" not in r.stdout or f"failed={n}" not in r.stdout or (r.stdout + r.stderr).count("RecursionError not raised") != n:
        failures.append(f"expected failures: exit {r.returncode}\n{r.stdout[-5000:]}\n{r.stderr[-2000:]}")

    for f in failures:
        print(f"FAIL {f}")

    if failures:
        sys.exit(1)

    print(f"python: the check script and {len(TESTS)} of CPython's tests pass, {len(XFAIL)} of their cases fail as expected")


if __name__ == "__main__":
    main()
