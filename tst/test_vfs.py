#!/usr/bin/env python3
"""Paths resolve as Linux resolves them. One probe of the walk, run by
python in rootfs, gives what Linux gives:
slashes at the end, symbolic links (followed, not, created through, too
many), "." and ".." (at a mount's root, past the root, after a link),
names that are mounts, the working directory and directory descriptors
as directories through renames and removals, the length of names and
paths, the names a call refuses, open's flags, calls on a link itself,
and what the VFS keeps, which holds no longer than its name, its node
and its directory do. Every case starts in a directory of its own under
/run.

    test_vfs.py <vm> <rootfs>
    test_vfs.py --linux

LINUX is what Linux answers: `test_vfs.py --linux` runs the probe in a
chroot of a user namespace whose root, read-only, is laid out as the
image's (bin/rootfs/image.py): /etc and /bin links into the realm,
/pzd/realm/system, a link to its directory of the store, whose etc has
links to etc/base's files and mc's directory, /usr -> /, and a tmpfs at
/run, with the host's python. rootfs must answer it line for line, each
directory of the store's uid as <uid>.
"""
import os
import re
from pathlib import Path
import subprocess
import sys
import tempfile

PROBE = r'''import errno, os, stat, sys

CASES = []

def case(name):
    def deco(f):
        CASES.append((name, f))
        return f
    return deco

def kind(p, follow=True, **kw):
    return stat.filemode(os.stat(p, follow_symlinks=follow, **kw).st_mode)[0]

def ident(p, **kw):
    st = os.stat(p, **kw)
    return (st.st_dev, st.st_ino)

def same(a, b):
    return ident(a) == ident(b)

def create(p, flags=0, **kw):
    os.close(os.open(p, os.O_WRONLY | os.O_CREAT | flags, 0o644, **kw))

def exists(p):
    return os.path.lexists(p)

def err(f):
    try:
        f()
        return "ok"
    except OSError as e:
        return errno.errorcode.get(e.errno, str(e.errno))

def fixture(t):
    os.mkdir(t)
    os.chdir(t)
    with open("f", "w") as out:
        out.write("f")
    os.mkdir("d")
    create("d/x")
    os.symlink("d", "ld")
    os.symlink("f", "lf")
    os.symlink("missing", "dl")
    os.symlink("/etc", "abs")
    os.symlink("..", "up")
    os.symlink("../../..", "up3")
    os.symlink("loop", "loop")
    os.symlink(".", "dot")
    os.symlink(t + "/d", "absself")
    os.symlink("d/", "lslash")
    os.symlink("f/", "lfslash")

# A: a trailing slash
case("A01 stat f/")(lambda: kind("f/"))
case("A02 open f/")(lambda: os.close(os.open("f/", os.O_RDONLY)))
case("A03 lstat ld/")(lambda: kind("ld/", False))
case("A04 lstat lf/")(lambda: kind("lf/", False))
case("A05 open(O_CREAT) n/ ; n made")(lambda: (err(lambda: create("n/")), exists("n")))
case("A06 mkdir d2/")(lambda: os.mkdir("d2/"))
case("A07 unlink f/ ; f kept")(lambda: (err(lambda: os.unlink("f/")), exists("f")))
case("A08 unlink ld/ ; ld kept")(lambda: (err(lambda: os.unlink("ld/")), exists("ld")))
case("A09 rmdir ld/ ; ld kept")(lambda: (err(lambda: os.rmdir("ld/")), exists("ld")))
case("A10 rename f/ g ; f kept")(lambda: (err(lambda: os.rename("f/", "g")), exists("f")))
case("A11 rename f g/ ; f kept")(lambda: (err(lambda: os.rename("f", "g/")), exists("f")))
case("A12 symlink x s/ ; s made")(lambda: (err(lambda: os.symlink("x", "s/")), exists("s")))
case("A13 link f h/ ; h made")(lambda: (err(lambda: os.link("f", "h/")), exists("h")))
case("A14 mkfifo q/ ; q made")(lambda: (err(lambda: os.mkfifo("q/")), exists("q")))
case("A15 readlink ld/")(lambda: os.readlink("ld/"))
case("A16 rmdir d/ after emptying")(lambda: (os.unlink("d/x"), os.rmdir("d/"))[1])
case("A17 stat d/")(lambda: kind("d/"))
case("A18 chdir f/")(lambda: os.chdir("f/"))
case("A19 stat lslash (target d/)")(lambda: kind("lslash"))
case("A20 stat lfslash (target f/)")(lambda: kind("lfslash"))
case("A21 open(O_CREAT) dl/ ; missing made")(lambda: (err(lambda: create("dl/")), exists("missing")))
case("A22 stat d/x/ ")(lambda: kind("d/x/"))
case("A23 stat d/.")(lambda: kind("d/."))
case("A24 stat f/.")(lambda: kind("f/."))

# B: symbolic links
case("B01 open(O_CREAT) dangling dl ; dl kind, missing made")(lambda: (err(lambda: create("dl")), kind("dl", False), exists("missing")))
case("B02 open(O_CREAT|O_EXCL) dangling dl ; dl kind, missing made")(lambda: (err(lambda: create("dl", os.O_EXCL)), kind("dl", False), exists("missing")))
case("B03 open(O_NOFOLLOW) lf")(lambda: os.close(os.open("lf", os.O_RDONLY | os.O_NOFOLLOW)))
case("B04 open(O_NOFOLLOW) ld/x")(lambda: os.close(os.open("ld/x", os.O_RDONLY | os.O_NOFOLLOW)))
case("B05 open abs/motd (absolute link in /run)")(lambda: open("abs/motd").read().strip())
case("B06 stat up3/etc/motd (relative link past /run's root)")(lambda: kind("up3/etc/motd"))
case("B07 stat absself/x (absolute link back into /run)")(lambda: kind("absself/x"))
case("B08 stat loop")(lambda: kind("loop"))
def chain(n):
    os.symlink("f", "c0")
    for i in range(1, n):
        os.symlink(f"c{i - 1}", f"c{i}")
    return kind(f"c{n - 1}")
case("B09 stat a chain of 40 links")(lambda: chain(40))
case("B10 stat a chain of 41 links")(lambda: chain(41))
case("B11 stat dot*40/f (40 links in a row)")(lambda: kind("dot/" * 40 + "f"))
case("B12 stat dot*41/f (41 links in a row)")(lambda: kind("dot/" * 41 + "f"))
case("B13 mkdir over dangling dl")(lambda: os.mkdir("dl"))
case("B14 symlink over existing f")(lambda: os.symlink("x", "f"))
case("B15 open(O_DIRECTORY) ld")(lambda: os.close(os.open("ld", os.O_RDONLY | os.O_DIRECTORY)))
case("B16 open(O_DIRECTORY|O_NOFOLLOW) ld")(lambda: os.close(os.open("ld", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)))
case("B17 chdir ld ; getcwd")(lambda: (os.chdir("ld"), os.getcwd())[1])
case("B18 chdir up3 ; getcwd")(lambda: (os.chdir("up3"), os.getcwd())[1])
case("B19 chdir abs ; getcwd")(lambda: (os.chdir("abs"), os.getcwd())[1])
case("B20 readlink f")(lambda: os.readlink("f"))
case("B21 listdir up3/usr/usr/etc")(lambda: sorted(os.listdir("up3/usr/usr/etc")))
case("B22 open(O_CREAT) through dangling into missing dir")(lambda: (os.symlink("nodir/file", "dl4"), create("dl4"))[1])
case("B23 open(O_CREAT) through abs link into /etc")(lambda: create("abs/new"))
case("B24 rmdir ld")(lambda: os.rmdir("ld"))
case("B25 unlink ld ; d kept")(lambda: (os.unlink("ld"), exists("d"))[1])

# C: ".."
case("C01 /run/.. is /")(lambda: same("/run/..", "/"))
case("C02 stat /run/../etc/motd")(lambda: kind("/run/../etc/motd"))
case("C03 chdir /run ; stat ../etc/motd")(lambda: (os.chdir("/run"), kind("../etc/motd"))[1])
case("C04 /.. is /")(lambda: same("/..", "/"))
case("C05 /usr/.. is /")(lambda: same("/usr/..", "/"))
case("C06 stat /etc/motd/..")(lambda: kind("/etc/motd/.."))
case("C07 stat /nonexistent/..")(lambda: kind("/nonexistent/.."))
case("C08 ld/.. is .")(lambda: same("ld/..", "."))
case("C09 abs/.. is /")(lambda: same("abs/..", "/"))
def c10():
    fd = os.open("abs/..", os.O_RDONLY | os.O_DIRECTORY)
    try:
        return kind("etc/motd", dir_fd=fd)
    finally:
        os.close(fd)
case("C10 openat(open(abs/..), etc/motd)")(c10)
def c11():
    fd = os.open("/run", os.O_RDONLY | os.O_DIRECTORY)
    try:
        return kind("../etc/motd", dir_fd=fd)
    finally:
        os.close(fd)
case("C11 openat(open(/run), ../etc/motd)")(c11)
case("C12 st_dev of /run/.. is /'s")(lambda: os.stat("/run/..").st_dev == os.stat("/").st_dev)
case("C13 st_dev of /run is not /'s")(lambda: os.stat("/run").st_dev != os.stat("/").st_dev)
case("C14 chdir /run ; chdir .. ; getcwd")(lambda: (os.chdir("/run"), os.chdir(".."), os.getcwd())[2])
case("C15 //etc//motd")(lambda: kind("//etc//motd"))
case("C16 stat /etc/.")(lambda: kind("/etc/."))

# D: the working directory and directory descriptors are objects
def d01():
    os.mkdir("w")
    os.chdir("w")
    os.rename("../w", "../w2")
    return (err(lambda: create("x")), exists("../w2/x"), os.getcwd().rsplit("/", 1)[-1])
case("D01 cwd renamed: create x ; made in w2 ; getcwd's last name")(d01)
def d02():
    os.mkdir("v")
    fd = os.open("v", os.O_RDONLY | os.O_DIRECTORY)
    os.rename("v", "v2")
    try:
        return (err(lambda: create("y", dir_fd=fd)), exists("v2/y"))
    finally:
        os.close(fd)
case("D02 dirfd renamed: openat y ; made in v2")(d02)
def d03():
    here = os.getcwd()
    os.mkdir("z")
    os.chdir("z")
    os.rmdir(here + "/z")
    return (err(os.getcwd), err(lambda: create("a")))
case("D03 cwd removed: getcwd ; create")(d03)
def d04():
    here = os.getcwd()
    os.mkdir("k")
    os.chdir("k")
    os.rename(here + "/k", here + "/k_old")
    os.mkdir(here + "/k")
    return (err(lambda: create("m")), exists(here + "/k_old/m"), exists(here + "/k/m"))
case("D04 cwd replaced by a new dir of its name: create m ; in k_old ; in new k")(d04)
def d05():
    fd = os.open("/run", os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fchdir(fd)
        return os.getcwd()
    finally:
        os.close(fd)
case("D05 fchdir /run ; getcwd")(d05)
def d06():
    fd = os.open("ld", os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fchdir(fd)
        return os.getcwd().rsplit("/", 1)[-1]
    finally:
        os.close(fd)
case("D06 fchdir open(ld) ; getcwd's last name")(d06)

# E: names
case("E01 create a name of 256")(lambda: create("a" * 256))
case("E02 create a name of 255")(lambda: create("a" * 255))
case("E03 stat empty")(lambda: kind(""))
case("E04 stat a path of 4096")(lambda: kind("/" + "./" * 2047 + "."))
case("E05 stat a path of 4095")(lambda: kind("/" + "./" * 2047))
def e06():
    for i in range(22):
        name = "%03d" % i + "n" * 197
        os.mkdir(name)
        os.chdir(name)
    return (err(lambda: create("leaf")), err(os.getcwd))
case("E06 below a cwd longer than 4096: create ; getcwd")(e06)

# F: special names, mount points
case("F01 rmdir d/.")(lambda: os.rmdir("d/."))
case("F02 rmdir d/..")(lambda: os.rmdir("d/.."))
case("F03 unlink d/.")(lambda: os.unlink("d/."))
case("F04 mkdir d/.")(lambda: os.mkdir("d/."))
case("F05 rename d d/sub")(lambda: os.rename("d", "d/sub"))
case("F06 rename d/. x")(lambda: os.rename("d/.", "x"))
case("F07 rmdir /run")(lambda: os.rmdir("/run"))
case("F08 rename f /f")(lambda: os.rename("f", "/f"))
case("F09 link f /f2")(lambda: os.link("f", "/f2"))
case("F10 rename /run /run2")(lambda: os.rename("/run", "/run2"))
case("F11 link d d3")(lambda: os.link("d", "d3"))
case("F12 link lf hl, following ; hl is f")(lambda: (os.link("lf", "hl", follow_symlinks=True), same("hl", "f"))[1])
case("F13 link lf hl2, not following ; hl2 kind")(lambda: (os.link("lf", "hl2", follow_symlinks=False), kind("hl2", False))[1])
case("F14 rmdir /")(lambda: os.rmdir("/"))
case("F15 unlink d/..")(lambda: os.unlink("d/.."))
case("F16 rename f lf (onto a link) ; lf kind")(lambda: (os.rename("f", "lf"), kind("lf", False))[1])
case("F17 open(O_CREAT|O_WRONLY) .")(lambda: create("."))

# G: open flags
case("G01 open(O_DIRECTORY) /etc/motd")(lambda: os.close(os.open("/etc/motd", os.O_RDONLY | os.O_DIRECTORY)))
case("G02 open(O_DIRECTORY|O_NOFOLLOW) /usr")(lambda: os.close(os.open("/usr", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)))
case("G03 open(O_WRONLY) d")(lambda: os.close(os.open("d", os.O_WRONLY)))
case("G04 listdir ld")(lambda: sorted(os.listdir("ld")))
case("G05 listdir /usr/usr/etc")(lambda: sorted(os.listdir("/usr/usr/etc")))

# H: the link itself
case("H01 access dl F_OK")(lambda: os.access("dl", os.F_OK))
case("H02 access dl F_OK, not following")(lambda: os.access("dl", os.F_OK, follow_symlinks=False))
case("H03 chmod lf 600, not following ; f's mode")(lambda: (err(lambda: os.chmod("lf", 0o600, follow_symlinks=False)), oct(os.stat("f").st_mode & 0o777)))
case("H04 utime lf, not following ; lf's mtime, f's is 1")(lambda: (os.utime("lf", (1, 1), follow_symlinks=False), int(os.lstat("lf").st_mtime), int(os.stat("f").st_mtime) == 1)[1:])
case("H05 lchown lf")(lambda: os.chown("lf", -1, -1, follow_symlinks=False))

# I: what the VFS keeps holds no longer than its name, its node or its directory
case("I01 stat d/x ; unlink d/x ; stat d/x")(lambda: (kind("d/x"), os.unlink("d/x"), err(lambda: kind("d/x")))[2])
case("I02 stat n ; create n ; stat n")(lambda: (err(lambda: kind("n")), create("n"), kind("n"))[0::2])
case("I03 stat d/x ; empty d, rmdir d, mkdir d ; stat d/x")(lambda: (kind("d/x"), os.unlink("d/x"), os.rmdir("d"), os.mkdir("d"), err(lambda: kind("d/x")))[4])
case("I04 stat d/x ; rename d e ; stat d/x, e/x")(lambda: (kind("d/x"), os.rename("d", "e"), err(lambda: kind("d/x")), kind("e/x"))[2:])
case("I05 stat d ; rename e over an emptied d ; stat d/y, e")(lambda: (kind("d/x"), os.unlink("d/x"), os.mkdir("e"), create("e/y"), kind("d"), os.rename("e", "d"), kind("d/y"), err(lambda: kind("e")))[6:])
case("I06 readlink ld ; ld made a link to f ; stat ld, readlink ld")(lambda: (kind("ld"), os.readlink("ld"), os.unlink("ld"), os.symlink("f", "ld"), kind("ld"), os.readlink("ld"))[4:])
def i07():
    t = os.getcwd()
    os.chdir("d")
    kind("x")
    os.rename(t + "/d", t + "/e")
    return (kind("x"), os.getcwd() == t + "/e")
case("I07 chdir d, stat x ; rename d e ; stat x, getcwd")(i07)
def i08():
    t = os.getcwd()
    os.mkdir("d/s")
    os.chdir("d/s")
    kind("..")
    os.rename(t + "/d/s", t + "/s")
    return same("..", t)
case("I08 chdir d/s, stat .. ; rename d/s s ; .. is the case's")(i08)
case("I09 link lf lf2, not following ; lstat lf ; unlink lf2 ; lstat lf")(lambda: (os.link("lf", "lf2", follow_symlinks=False), os.lstat("lf").st_nlink, os.unlink("lf2"), os.lstat("lf").st_nlink)[1::2])
case("I10 stat d ; chmod d 700 ; stat d")(lambda: (os.stat("d"), os.chmod("d", 0o700), oct(os.stat("d").st_mode & 0o777))[2])
def i11():
    fd = os.open("d", os.O_RDONLY)
    os.stat("d")
    os.fchmod(fd, 0o711)
    mode = oct(os.stat("d").st_mode & 0o777)
    os.close(fd)
    return mode
case("I11 stat d ; fchmod of d's descriptor 711 ; stat d")(i11)
case("I12 stat d ; utime d ; stat d")(lambda: (os.stat("d"), os.utime("d", (1, 2)), int(os.stat("d").st_mtime))[2])
case("I13 utime d 1 ; stat d ; mkdir d/s ; d's mtime moved")(lambda: (os.utime("d", (1, 1)), int(os.stat("d").st_mtime), os.mkdir("d/s"), int(os.stat("d").st_mtime) != 1)[1::2])
case("I14 stat m ; create m excl ; again")(lambda: (err(lambda: kind("m")), create("m", os.O_EXCL), err(lambda: create("m", os.O_EXCL)))[0::2])
case("I15 stat d ; mkdir d")(lambda: (kind("d"), os.mkdir("d"))[1])
def i16():
    size = os.stat("f").st_size
    fd = os.open("f", os.O_WRONLY | os.O_APPEND)
    os.write(fd, b"gg")
    os.close(fd)
    return (size, os.stat("f").st_size)
case("I16 stat f ; append to f ; stat f")(i16)

for i, (name, f) in enumerate(CASES):
    t = "/run/p%03d" % i
    try:
        fixture(t)
        v = f()
        print(f"{name}: ok" + ("" if v is None else f" {v}"))
    except OSError as e:
        print(f"{name}: {errno.errorcode.get(e.errno, e.errno)}")
    except NotImplementedError:
        print(f"{name}: n/a")
    except Exception as e:
        print(f"{name}: {type(e).__name__} {e}")
    finally:
        os.chdir("/")
'''

LINUX = """\
A01 stat f/: ENOTDIR
A02 open f/: ENOTDIR
A03 lstat ld/: ok d
A04 lstat lf/: ENOTDIR
A05 open(O_CREAT) n/ ; n made: ok ('EISDIR', False)
A06 mkdir d2/: ok
A07 unlink f/ ; f kept: ok ('ENOTDIR', True)
A08 unlink ld/ ; ld kept: ok ('ENOTDIR', True)
A09 rmdir ld/ ; ld kept: ok ('ENOTDIR', True)
A10 rename f/ g ; f kept: ok ('ENOTDIR', True)
A11 rename f g/ ; f kept: ok ('ENOTDIR', True)
A12 symlink x s/ ; s made: ok ('ENOENT', False)
A13 link f h/ ; h made: ok ('ENOENT', False)
A14 mkfifo q/ ; q made: ok ('ENOENT', False)
A15 readlink ld/: EINVAL
A16 rmdir d/ after emptying: ok
A17 stat d/: ok d
A18 chdir f/: ENOTDIR
A19 stat lslash (target d/): ok d
A20 stat lfslash (target f/): ENOTDIR
A21 open(O_CREAT) dl/ ; missing made: ok ('EISDIR', False)
A22 stat d/x/ : ENOTDIR
A23 stat d/.: ok d
A24 stat f/.: ENOTDIR
B01 open(O_CREAT) dangling dl ; dl kind, missing made: ok ('ok', 'l', True)
B02 open(O_CREAT|O_EXCL) dangling dl ; dl kind, missing made: ok ('EEXIST', 'l', False)
B03 open(O_NOFOLLOW) lf: ELOOP
B04 open(O_NOFOLLOW) ld/x: ok
B05 open abs/motd (absolute link in /run): ok pizdos
B06 stat up3/etc/motd (relative link past /run's root): ok -
B07 stat absself/x (absolute link back into /run): ok -
B08 stat loop: ELOOP
B09 stat a chain of 40 links: ok -
B10 stat a chain of 41 links: ELOOP
B11 stat dot*40/f (40 links in a row): ok -
B12 stat dot*41/f (41 links in a row): ELOOP
B13 mkdir over dangling dl: EEXIST
B14 symlink over existing f: EEXIST
B15 open(O_DIRECTORY) ld: ok
B16 open(O_DIRECTORY|O_NOFOLLOW) ld: ENOTDIR
B17 chdir ld ; getcwd: ok /run/p040/d
B18 chdir up3 ; getcwd: ok /
B19 chdir abs ; getcwd: ok /pzd/store/<uid>-rlm-system/etc
B20 readlink f: EINVAL
B21 listdir up3/usr/usr/etc: ok ['group', 'hosts', 'mc', 'motd', 'passwd']
B22 open(O_CREAT) through dangling into missing dir: ENOENT
B23 open(O_CREAT) through abs link into /etc: EROFS
B24 rmdir ld: ENOTDIR
B25 unlink ld ; d kept: ok True
C01 /run/.. is /: ok True
C02 stat /run/../etc/motd: ok -
C03 chdir /run ; stat ../etc/motd: ok -
C04 /.. is /: ok True
C05 /usr/.. is /: ok True
C06 stat /etc/motd/..: ENOTDIR
C07 stat /nonexistent/..: ENOENT
C08 ld/.. is .: ok True
C09 abs/.. is /: ok False
C10 openat(open(abs/..), etc/motd): ok -
C11 openat(open(/run), ../etc/motd): ok -
C12 st_dev of /run/.. is /'s: ok True
C13 st_dev of /run is not /'s: ok True
C14 chdir /run ; chdir .. ; getcwd: ok /
C15 //etc//motd: ok -
C16 stat /etc/.: ok d
D01 cwd renamed: create x ; made in w2 ; getcwd's last name: ok ('ok', True, 'w2')
D02 dirfd renamed: openat y ; made in v2: ok ('ok', True)
D03 cwd removed: getcwd ; create: ok ('ENOENT', 'ENOENT')
D04 cwd replaced by a new dir of its name: create m ; in k_old ; in new k: ok ('ok', True, False)
D05 fchdir /run ; getcwd: ok /run
D06 fchdir open(ld) ; getcwd's last name: ok d
E01 create a name of 256: ENAMETOOLONG
E02 create a name of 255: ok
E03 stat empty: ENOENT
E04 stat a path of 4096: ENAMETOOLONG
E05 stat a path of 4095: ok d
E06 below a cwd longer than 4096: create ; getcwd: ok ('ok', 'ENAMETOOLONG')
F01 rmdir d/.: EINVAL
F02 rmdir d/..: ENOTEMPTY
F03 unlink d/.: EISDIR
F04 mkdir d/.: EEXIST
F05 rename d d/sub: EINVAL
F06 rename d/. x: EBUSY
F07 rmdir /run: EROFS
F08 rename f /f: EXDEV
F09 link f /f2: EROFS
F10 rename /run /run2: EROFS
F11 link d d3: EPERM
F12 link lf hl, following ; hl is f: ok True
F13 link lf hl2, not following ; hl2 kind: ok l
F14 rmdir /: EBUSY
F15 unlink d/..: EISDIR
F16 rename f lf (onto a link) ; lf kind: ok -
F17 open(O_CREAT|O_WRONLY) .: EISDIR
G01 open(O_DIRECTORY) /etc/motd: ENOTDIR
G02 open(O_DIRECTORY|O_NOFOLLOW) /usr: ENOTDIR
G03 open(O_WRONLY) d: EISDIR
G04 listdir ld: ok ['x']
G05 listdir /usr/usr/etc: ok ['group', 'hosts', 'mc', 'motd', 'passwd']
H01 access dl F_OK: ok False
H02 access dl F_OK, not following: ok True
H03 chmod lf 600, not following ; f's mode: n/a
H04 utime lf, not following ; lf's mtime, f's is 1: ok (1, False)
H05 lchown lf: ok
I01 stat d/x ; unlink d/x ; stat d/x: ok ENOENT
I02 stat n ; create n ; stat n: ok ('ENOENT', '-')
I03 stat d/x ; empty d, rmdir d, mkdir d ; stat d/x: ok ENOENT
I04 stat d/x ; rename d e ; stat d/x, e/x: ok ('ENOENT', '-')
I05 stat d ; rename e over an emptied d ; stat d/y, e: ok ('-', 'ENOENT')
I06 readlink ld ; ld made a link to f ; stat ld, readlink ld: ok ('-', 'f')
I07 chdir d, stat x ; rename d e ; stat x, getcwd: ok ('-', True)
I08 chdir d/s, stat .. ; rename d/s s ; .. is the case's: ok True
I09 link lf lf2, not following ; lstat lf ; unlink lf2 ; lstat lf: ok (2, 1)
I10 stat d ; chmod d 700 ; stat d: ok 0o700
I11 stat d ; fchmod of d's descriptor 711 ; stat d: ok 0o711
I12 stat d ; utime d ; stat d: ok 2
I13 utime d 1 ; stat d ; mkdir d/s ; d's mtime moved: ok (1, True)
I14 stat m ; create m excl ; again: ok ('ENOENT', 'EEXIST')
I15 stat d ; mkdir d: EEXIST
I16 stat f ; append to f ; stat f: ok (1, 3)
"""

FILES = {"etc/motd": "pizdos\n", "etc/passwd": "root:x:0:0:root:/:/bin/sh\n", "etc/group": "root:x:0:\n",
         "etc/hosts": "127.0.0.1 localhost\n::1 localhost\n"}
REALM = "pzd/store/" + "r" * 22 + "-rlm-system"
BASE = "pzd/store/" + "b" * 22 + "-etc-base"


def stored(output):
    """The output with every directory of the store's uid as <uid>."""
    return re.sub(r"/pzd/store/[0-9A-Za-z]{22}-", "/pzd/store/<uid>-", output)


def compare(got, want, where):
    got, want = got.splitlines(), want.splitlines()
    differ = [f"{w}\n    {where}: {g.partition(': ')[2]}" for g, w in zip(got, want) if g != w]
    assert len(got) == len(want) and not differ, (where, len(got), len(want), "\n".join(differ))


def linux():
    """The probe's answers on this host's Linux, as LINUX holds them."""
    python = os.path.realpath(sys.executable)
    store = "/" + Path(python).parts[1]
    with tempfile.TemporaryDirectory(prefix="pizdos-vfs-linux-") as work:
        root = Path(work)
        for d in ("run", "dev", store.lstrip("/"), f"{REALM}/bin", f"{REALM}/etc/mc", f"{BASE}/etc", "pzd/realm"):
            (root / d).mkdir(parents=True, exist_ok=True)
        for name, text in FILES.items():
            (root / BASE / name).write_text(text)
            (root / REALM / name).symlink_to(f"/{BASE}/{name}")
        (root / "pzd/realm/system").symlink_to("/" + REALM)
        (root / "bin").symlink_to("pzd/realm/system/bin")
        (root / "etc").symlink_to("pzd/realm/system/etc")
        (root / "usr").symlink_to("/")
        (root / "dev/null").touch()
        (root / "probe.py").write_text(PROBE)
        script = (f"mount --bind {root} {root} && mount -o remount,bind,ro {root} && mount -t tmpfs none {root}/run && "
                  f"mount --rbind {store} {root}{store} && mount --bind /dev/null {root}/dev/null && "
                  f"chroot {root} {python} -I /probe.py")
        result = subprocess.run(["unshare", "--user", "--map-root-user", "--mount", "--fork", "sh", "-c", script],
                                capture_output=True, text=True, timeout=600)
        assert result.returncode == 0 and result.stderr == "", result
        return stored(result.stdout)


def main():
    if sys.argv[1:] == ["--linux"]:
        print(linux(), end="")
        return
    vm, rootfs = sys.argv[1:3]
    env = {"W2X_MEMORY_TAIL": "strict", "W2X_TAIL_PROFILE": ""}
    script = "python3 - <<'PROBE_EOF'\n" + PROBE + "PROBE_EOF\nexit 0\n"
    result = subprocess.run([vm, "run", "--", rootfs], input=script.encode(), capture_output=True, env=env, timeout=600)
    assert result.returncode == 0 and result.stderr == b"", result
    compare(stored(result.stdout.decode()), LINUX, "rootfs")
    print(f"vfs: {len(LINUX.splitlines())} cases of the walk answer as Linux's in rootfs")


if __name__ == "__main__":
    main()
