#!/usr/bin/env python3
"""Mounts: mount <where> <what> (bin/tools/mount.cpp, mount(2)) mounts a
server's directory on a directory of rootfs's: a walk into it, and a call
on its name, go on to that server, which answers the kernel itself.

A directory of /run mounted on another, rootfs its server: the names, the
bytes and what is written are the mounted directory's. A mount point is
busy to rmdir and to another mount; umount lets go of it; what is not
there, not a directory, or a device is refused.

File systems on a disk: blk serves the disk and each partition, and
starts fuse2fs, on the system's libfuse, for one with an ext2 file
system, /dev/blk/<disk>/<n>/fs, a file system to mount, there before the shell
starts. The disk's ext2 and ext4 file systems are made here (Image),
the test's user's: their files' bytes come whole from runs of blocks,
from blocks interleaved with another file's, through indirect blocks and
an extent tree of a leaf, holes and unwritten extents as zeros; links are
followed, an absolute one from the root of the process; ".." out of the
root goes to where it is mounted, and so does the path of a directory.
Names are made, moved, linked and removed, modes, times and bytes
written, and watched (tst/watch_dir.cpp's names, on both file systems:
libfuse's watchers see every change, which comes as a call of theirs),
and all of it is there when the system starts again: the test
syncs each, an fsync of a file of it, which has fuse2fs write what it
holds. The system kills fuse2fs as it goes down, and a file system it
had has no mark of being checked: fuse2fs, mounting it again, warns. flock's and fcntl's locks, which the
kernel keeps by the dev and ino libfuse makes an open file with, are an
open file's: one another holds is busy to try and waited for, a range's
as the file's, and goes with its open. A directory made after one was
removed
gets its inode, and nothing of the removed one's: not its names, not
its "..". A file system of
features fuse2fs does not know is refused.

    test_mounts.py <vm> <rootfs> <watch_dir>
"""
import base64
import os
import struct
import subprocess
import sys
import tempfile
import uuid
import zlib

DIR, REG, LNK = 0o040755, 0o100644, 0o120777


class Image:
    """An ext2 file system, or an ext4 one with extents, in one block group,
    as ext4 (bin/tools/ext4.cpp) reads it and e2fsck finds it whole: its
    files' blocks taken in turn, or one of every two, the other another's,
    holes left, unwritten extents, fast and slow links."""

    def __init__(self, block, blocks, extents, owner=0, inodes=64):
        self.block, self.blocks, self.extents, self.inodes, self.owner = block, blocks, extents, inodes, owner
        self.disk = bytearray(block * blocks)
        self.first_data = 1 if block == 1024 else 0
        self.gdt = self.first_data + 1
        self.block_bitmap, self.inode_bitmap, self.inode_table = self.gdt + 1, self.gdt + 2, self.gdt + 3
        self.next_block = self.inode_table + (inodes * 128 + block - 1) // block
        self.used_blocks = set(range(self.first_data, self.next_block))
        self.next_inode = 11
        self.dirs = 0
        self.root = self.directory(2, 2)
        self.add_dir(self.root, "lost+found")

    def allocate(self):
        while self.next_block in self.used_blocks:
            self.next_block += 1
        if self.next_block >= self.blocks:
            raise ValueError("full")
        self.used_blocks.add(self.next_block)
        return self.next_block

    def directory(self, ino, parent):
        self.dirs += 1
        return {"ino": ino, "parent": parent, "entries": []}

    def inode(self, ino, mode, size, links, blocks_used, flags, i_block):
        raw = struct.pack("<HHIIIIIHHIII", mode, self.owner & 0xFFFF, size & 0xFFFFFFFF, 1000000000, 1000000000, 1000000000, 0,
                          self.owner & 0xFFFF, links, blocks_used * (self.block // 512), flags, 0)
        raw += i_block.ljust(60, b"\0") + struct.pack("<IIII", 0, 0, size >> 32, 0) + struct.pack("<IHH", 0, self.owner >> 16,
                                                                                                   self.owner >> 16) + bytes(4)
        assert len(raw) == 128
        at = self.inode_table * self.block + (ino - 1) * 128
        self.disk[at:at + 128] = raw

    def new_inode(self):
        ino = self.next_inode
        self.next_inode += 1
        return ino

    def add_dir(self, parent, name):
        d = self.directory(self.new_inode(), parent["ino"])
        parent["entries"].append((name, d["ino"], 2, d))
        return d

    def put(self, physical, data):
        at = physical * self.block
        self.disk[at:at + len(data)] = data

    def map_blocks(self, blocks):
        """i_block for a list of (logical block, physical block or 0): a
        map of ext2's, indirect blocks taken as the blocks run past the
        direct ones, or ext4's extents, in the inode or a leaf block."""
        if self.extents:
            runs = []
            for logical, physical, unwritten in blocks:
                if physical and runs and runs[-1][0] + runs[-1][1] == logical and runs[-1][2] + runs[-1][1] == physical \
                        and runs[-1][3] == unwritten:
                    runs[-1][1] += 1
                elif physical:
                    runs.append([logical, 1, physical, unwritten])
            extents = b"".join(struct.pack("<IHHI", l, n + (32768 if u else 0), p >> 32, p & 0xFFFFFFFF) for l, n, p, u in runs)
            if len(runs) <= 4:
                return struct.pack("<HHHHI", 0xF30A, len(runs), 4, 0, 0) + extents, 0
            leaf = self.allocate()
            self.put(leaf, struct.pack("<HHHHI", 0xF30A, len(runs), (self.block - 12) // 12, 0, 0) + extents)
            return struct.pack("<HHHHI", 0xF30A, 1, 4, 1, 0) + struct.pack("<IIHH", 0, leaf, 0, 0), 1
        per = self.block // 4
        direct = [0] * 15
        tables, extra = {}, 0

        def table(key, slot):
            nonlocal extra
            if key not in tables:
                tables[key] = [self.allocate(), [0] * per]
                extra += 1
                if slot is not None:
                    slot()
            return tables[key]

        for logical, physical, _ in blocks:
            if not physical:
                continue
            if logical < 12:
                direct[logical] = physical
                continue
            b = logical - 12
            if b < per:
                t = table(("1",), lambda: direct.__setitem__(12, tables[("1",)][0]))
                t[1][b] = physical
                continue
            b -= per
            top = table(("2",), lambda: direct.__setitem__(13, tables[("2",)][0]))
            mid = table(("2", b // per), lambda: top[1].__setitem__(b // per, tables[("2", b // per)][0]))
            mid[1][b % per] = physical
        for number, entries in tables.values():
            self.put(number, struct.pack(f"<{per}I", *entries))
        return struct.pack("<15I", *direct), extra

    def add_file(self, parent, name, data, holes=(), interleave=None, unwritten=(), mode=REG, given=None):
        """A file of data: its blocks taken one by one, or one of every
        two, the other put in interleave, or the blocks given; the blocks in
        holes not taken, those in unwritten taken but unwritten (ext4)."""
        ino = self.new_inode()
        count = (len(data) + self.block - 1) // self.block
        blocks = []
        for logical in range(count):
            if logical in holes:
                blocks.append((logical, 0, False))
                continue
            physical = given[logical] if given is not None else self.allocate()
            if interleave is not None:
                interleave.append(self.allocate())
            chunk = data[logical * self.block:(logical + 1) * self.block]
            self.put(physical, chunk if logical not in unwritten else b"\xAA" * len(chunk))
            blocks.append((logical, physical, logical in unwritten))
        i_block, extra = self.map_blocks(blocks)
        used = sum(1 for _, p, _ in blocks if p) + extra
        self.inode(ino, mode, len(data), 1, used, 0x80000 if self.extents else 0, i_block)
        parent["entries"].append((name, ino, 7 if mode == LNK else 1, None))
        return ino

    def add_link(self, parent, name, target):
        target = target.encode()
        if len(target) < 60:
            ino = self.new_inode()
            self.inode(ino, LNK, len(target), 1, 0, 0, target)
            parent["entries"].append((name, ino, 7, None))
            return ino
        return self.add_file(parent, name, target, mode=LNK)

    def write_dirs(self, d):
        entries = [(".", d["ino"], 2), ("..", d["parent"], 2)] + [(n, i, t) for n, i, t, _ in d["entries"]]
        blocks, current = [], bytearray()
        records = []
        for name, ino, kind in entries:
            raw = name.encode()
            need = (8 + len(raw) + 3) & ~3
            if len(current) + need > self.block:
                records.append(current)
                current = bytearray()
            current += struct.pack("<IHBB", ino, need, len(raw), kind) + raw + bytes(need - 8 - len(raw))
        records.append(current)
        for record in records:
            # The last entry of a block runs to its end.
            at = 0
            last = 0
            while at < len(record):
                last = at
                at += struct.unpack_from("<H", record, at + 4)[0]
            struct.pack_into("<H", record, last + 4, self.block - last)
            physical = self.allocate()
            self.put(physical, bytes(record).ljust(self.block, b"\0"))
            blocks.append((len(blocks), physical, False))
        i_block, extra = self.map_blocks(blocks)
        subdirs = sum(1 for _, _, t, sub in d["entries"] if sub is not None)
        self.inode(d["ino"], DIR, len(blocks) * self.block, 2 + subdirs, len(blocks) + extra, 0x80000 if self.extents else 0, i_block)
        for _, _, _, sub in d["entries"]:
            if sub is not None:
                self.write_dirs(sub)

    def finish(self):
        self.write_dirs(self.root)
        used_inodes = self.next_inode - 1
        bitmap = bytearray(self.block)
        for b in self.used_blocks:
            bitmap[(b - self.first_data) // 8] |= 1 << ((b - self.first_data) % 8)
        for bit in range(self.blocks - self.first_data, self.block * 8):
            bitmap[bit // 8] |= 1 << (bit % 8)
        self.put(self.block_bitmap, bitmap)
        bitmap = bytearray(self.block)
        for i in range(used_inodes):
            bitmap[i // 8] |= 1 << (i % 8)
        for bit in range(self.inodes, self.block * 8):
            bitmap[bit // 8] |= 1 << (bit % 8)
        self.put(self.inode_bitmap, bitmap)
        free_blocks = self.blocks - self.first_data - len(self.used_blocks)
        free_inodes = self.inodes - used_inodes
        self.put(self.gdt, struct.pack("<IIIHHH", self.block_bitmap, self.inode_bitmap, self.inode_table, free_blocks, free_inodes,
                                       self.dirs).ljust(32, b"\0"))
        log = {1024: 0, 2048: 1, 4096: 2}[self.block]
        incompat = 0x2 | (0x40 if self.extents else 0)
        sb = struct.pack("<IIIIIIIIIIIIIHhHHHHIIIIHHIHHIII", self.inodes, self.blocks, 0, free_blocks, free_inodes, self.first_data, log,
                         log, self.block * 8, self.block * 8, self.inodes, 1000000000, 1000000000, 0, -1, 0xEF53, 1, 1, 0,
                         1000000000, 0, 0, 1, 0, 0, 11, 128, 0, 0, incompat, 0)
        self.disk[1024:1024 + len(sb)] = sb
        return bytes(self.disk)


def gpt_image(path, partitions):
    """A disk of 512-byte blocks with a GUID partition table, each partition
    a file system's bytes, one after another from block 2048."""
    first = 2048
    places = []
    for data in partitions:
        places.append((first, data))
        first += len(data) // 512
    sectors = first + 2048
    disk = bytearray(sectors * 512)
    linux = uuid.UUID("0FC63DAF-8483-4772-8E79-3D69D8477DE4").bytes_le
    entries = bytearray(128 * 128)
    for i, (first, data) in enumerate(places):
        entries[i * 128:(i + 1) * 128] = struct.pack("<16s16sQQQ72s", linux, uuid.uuid4().bytes_le, first, first + len(data) // 512 - 1, 0,
                                                     b"")
        disk[first * 512:first * 512 + len(data)] = data
    disk[1024:1024 + len(entries)] = entries
    header = bytearray(struct.pack("<8sIIIIQQQQ16sQIII", b"EFI PART", 0x10000, 92, 0, 0, 1, sectors - 1, 34, sectors - 34,
                                   uuid.uuid4().bytes_le, 2, 128, 128, zlib.crc32(entries)))
    struct.pack_into("<I", header, 16, zlib.crc32(header))
    disk[512:512 + len(header)] = header
    with open(path, "wb") as f:
        f.write(disk)


def tree(image):
    """The files of the test's file systems."""
    root = image.root
    image.add_file(root, "hello.txt", b"hello from ext\n")
    sub = image.add_dir(root, "sub")
    image.add_file(sub, "deep.txt", b"deep\n")
    image.add_link(root, "link", "hello.txt")
    image.add_link(root, "abs", "/etc/motd")
    image.add_link(root, "long", "sub/" + "./" * 40 + "deep.txt")
    image.add_file(root, "big", bytes(i % 251 for i in range(1 << 20)))
    filler = []
    image.add_file(root, "frag", bytes(i * 7 % 256 for i in range(64 * 1024)), interleave=filler)
    image.add_file(root, "filler", b"\x55" * (len(filler) * image.block), given=filler)
    image.add_file(root, "sparse", bytes(1 << 20) + b"end", holes=set(range((1 << 20) // image.block)))
    if image.extents:
        image.add_file(root, "unwritten", b"\x11" * 3 * image.block, unwritten={1})
    return image.finish()


BIND = """\
mkdir /run/where /run/what && echo inside > /run/what/f && mkdir /run/what/d
mount /run/where /run/what && ls /run/where && cat /run/where/f && ls -d /run/where/d
stat -c %i /run/where /run/what | uniq | wc -l
echo more > /run/where/g && cat /run/what/g
find /run/where | sort
rmdir /run/where
mount /run/where /run/what
umount /run/where && ls /run/where | wc -l
umount /run/where
mount /run/missing /run/what
mount /run/where /run/what/f
mount /run/where /dev/null
mount /run /run/what
"""

BIND_WANT = """\
d
f
inside
/run/where/d
1
more
/run/where
/run/where/d
/run/where/f
/run/where/g
0
"""

BIND_ERRORS = """\
rmdir: '/run/where': Device or resource busy
mount: /run/what on /run/where: Device or resource busy
umount: /run/where: Invalid argument
mount: /run/what on /run/missing: No such file or directory
mount: /run/what/f on /run/where: Not a directory
mount: /dev/null on /run/where: Not a directory
mount: /run/what on /run: Device or resource busy
"""

EXT = """\
ls /dev/blk/sda/*/fs
mkdir /run/two /run/four
mount /run/two /dev/blk/sda/1/fs && mount /run/four /dev/blk/sda/2/fs && ls /run/four
cat /run/two/hello.txt /run/four/sub/deep.txt /run/two/link /run/four/long /run/two/long
readlink /run/two/abs; head -1 /run/four/abs
python3 -c '
import os
for root in ("/run/two", "/run/four"):
    big = open(root + "/big", "rb").read()
    frag = open(root + "/frag", "rb").read()
    sparse = open(root + "/sparse", "rb").read()
    print(len(big), big == bytes(i % 251 for i in range(1 << 20)), frag == bytes(i * 7 % 256 for i in range(64 * 1024)), len(sparse), sparse[:4], sparse[-3:])
    f = os.open(root + "/big", os.O_RDONLY)
    print(os.pread(f, 6, 5000) == bytes(i % 251 for i in range(5000, 5006)), os.pread(f, 4, 1 << 19), os.pread(f, 10, (1 << 20) - 4), os.pread(f, 10, 1 << 20))
print(open("/run/four/unwritten", "rb").read() == b"\\x11" * 4096 + bytes(4096) + b"\\x11" * 4096)
'
cd /run/two/sub && pwd -P && ls .. | head -2 && cd /
ls /run/four/sub/../..
stat -c '%F %s %h' /run/two/sub /run/four/big /run/two/link
python3 -c 'import os; s = os.statvfs("/run/four"); print(s.f_bsize, s.f_flag & 1, s.f_blocks)'
for root in /run/two /run/four; do echo new > $root/new.txt && mkdir $root/dir && mv $root/new.txt $root/dir/moved.txt && ln -s hello.txt $root/sym && chmod 600 $root/hello.txt && touch -d @1500000000 $root/hello.txt && rm $root/filler && ls $root/dir; done
rmdir /run/two/dir
for root in /run/two /run/four; do mkdir $root/up $root/up/gone && touch $root/up/gone/x && a=$(stat -c %i $root/up/gone) && stat -c %i $root/up/gone/.. > /dev/null && rm $root/up/gone/x && rmdir $root/up/gone && mkdir $root/back && [ "$(stat -c %i $root/back)" = "$a" ] && test ! -e $root/back/x && [ "$(stat -c %i $root/back/..)" = "$(stat -c %i $root)" ] && echo nothing of gone in back; done
python3 -c '
import os
p = bytes(i * 13 % 256 for i in range(1 << 20))
f = os.open("/run/four/written", os.O_CREAT | os.O_RDWR, 0o644)
print(os.write(f, p), os.pread(f, 1 << 20, 0) == p)
os.ftruncate(f, 1000)
os.write(os.open("/run/four/written", os.O_WRONLY | os.O_APPEND), b"tail")
print(os.fstat(f).st_size, os.pread(f, 4, 1000))
'
python3 -c '
import fcntl, os, time
for root in ("/run/two", "/run/four"):
    a = os.open(root + "/big", os.O_RDWR)
    b = os.open(root + "/big", os.O_RDWR)
    fcntl.flock(a, fcntl.LOCK_EX)
    try: fcntl.flock(b, fcntl.LOCK_SH | fcntl.LOCK_NB); tried = "taken"
    except BlockingIOError: tried = "busy"
    r, w = os.pipe()
    child = os.fork()
    if child == 0:
        fcntl.flock(b, fcntl.LOCK_SH)
        os.write(w, b"c")
        os._exit(0)
    time.sleep(0.2)
    os.write(w, b"p")
    fcntl.flock(a, fcntl.LOCK_UN)
    os.waitpid(child, 0)
    fcntl.lockf(a, fcntl.LOCK_EX | fcntl.LOCK_NB, 10, 0)
    try: fcntl.lockf(b, fcntl.LOCK_SH | fcntl.LOCK_NB, 5, 5); ranged = "taken"
    except OSError: ranged = "busy"
    fcntl.lockf(b, fcntl.LOCK_SH | fcntl.LOCK_NB, 5, 10)
    os.close(a)
    fcntl.lockf(b, fcntl.LOCK_SH | fcntl.LOCK_NB, 5, 5)
    print("locks:", tried, os.read(r, 2), ranged)
'
python3 -c '
import os
for name in "/run/two/hello.txt", "/run/four/hello.txt":
    f = os.open(name, os.O_RDONLY)
    os.fsync(f)
    os.close(f)
'
python3 -c 'import base64, sys; open("/run/watch_dir", "wb").write(base64.b64decode(sys.stdin.read()))' <<'PROGRAM'
{program}
PROGRAM
chmod +x /run/watch_dir
/run/watch_dir names /run/two
/run/watch_dir names /run/four
"""

EXT_WANT = """\
/dev/blk/sda/1/fs
/dev/blk/sda/2/fs
abs
big
filler
frag
hello.txt
link
long
lost+found
sparse
sub
unwritten
hello from ext
deep
hello from ext
deep
deep
/etc/motd
pizdos
1048576 True True 1048579 b'\\x00\\x00\\x00\\x00' b'end'
True b'\\xc8\\xc9\\xca\\xcb' b'\\x91\\x92\\x93\\x94' b''
1048576 True True 1048579 b'\\x00\\x00\\x00\\x00' b'end'
True b'\\xc8\\xc9\\xca\\xcb' b'\\x91\\x92\\x93\\x94' b''
True
/run/two/sub
abs
big
four
two
directory 1024 2
regular file 1048576 1
symbolic link 9 1
4096 0 2043
moved.txt
moved.txt
nothing of gone in back
nothing of gone in back
1048576 True
1004 b'tail'
locks: busy b'pc' busy
locks: busy b'pc' busy
names: existing a
names: idle
quick: existing a
quick: idle
names: added b
names: removed a
names: removed b
names: added c
quick: removed a
quick: added c
quick: Resource temporarily unavailable
names: added d
names: Interrupted system call
names: added e
sub: idle
sub: end
names: removed e
names: removed c
names: removed d
names: existing a
names: idle
quick: existing a
quick: idle
names: added b
names: removed a
names: removed b
names: added c
quick: removed a
quick: added c
quick: Resource temporarily unavailable
names: added d
names: Interrupted system call
names: added e
sub: idle
sub: end
names: removed e
names: removed c
names: removed d
"""

EXT_ERRORS = """\
FUSE2FS (data): Filesystem has unsupported feature(s).
FUSE2FS (data): Please run e2fsck -fy.
rmdir: '/run/two/dir': Directory not empty
"""

# What the first start wrote, there when the system starts again.
AGAIN = """\
mkdir /run/two /run/four
mount /run/two /dev/blk/sda/1/fs && mount /run/four /dev/blk/sda/2/fs
cat /run/two/dir/moved.txt /run/four/dir/moved.txt
readlink /run/two/sym; readlink /run/four/sym
stat -c '%a %Y' /run/two/hello.txt /run/four/hello.txt
ls /run/two /run/four | grep -c filler
python3 -c '
import os
f = os.open("/run/four/written", os.O_RDONLY)
print(os.fstat(f).st_size, os.pread(f, 4, 1000), os.pread(f, 1000, 0) == bytes(i * 13 % 256 for i in range(1000)))
'
"""

AGAIN_WANT = """\
new
new
hello.txt
hello.txt
600 1500000000
600 1500000000
0
1004 b'tail' True
"""

AGAIN_ERRORS = """\
FUSE2FS (data): Filesystem has unsupported feature(s).
FUSE2FS (data): Please run e2fsck -fy.
FUSE2FS (data): Warning: Mounting unchecked fs, running e2fsck is recommended.
FUSE2FS (data): Warning: Mounting unchecked fs, running e2fsck is recommended.
"""


def run(vm, rootfs, flags, script, cwd):
    env = {"W2X_MEMORY_TAIL": "strict", "W2X_TAIL_PROFILE": ""}
    return subprocess.run([vm, "run", *flags, "--", rootfs], input=(script + "exit 3\n").encode(), capture_output=True, cwd=cwd, env=env,
                          timeout=180)


def main():
    vm, rootfs, watch_dir = sys.argv[1:4]
    program = base64.encodebytes(open(watch_dir, "rb").read()).decode().strip()
    with tempfile.TemporaryDirectory(prefix="pizdos-mounts-") as work:
        empty = os.path.join(work, "empty")
        os.mkdir(empty)
        result = run(vm, rootfs, [], BIND, empty)
        assert (result.returncode, result.stdout.decode(), result.stderr.decode()) == (3, BIND_WANT, BIND_ERRORS), result
        owner = os.getuid()
        strange = bytearray(Image(1024, 1024, False, owner).finish())
        struct.pack_into("<I", strange, 1024 + 96, 0x2 | 0x40000000)
        disk = os.path.join(work, "disk.img")
        gpt_image(disk, [tree(Image(1024, 4096, False, owner)), tree(Image(4096, 2048, True, owner)), bytes(strange)])
        result = run(vm, rootfs, ["--disk", f"sda={disk}"], EXT.replace("{program}", program), empty)
        assert (result.returncode, result.stdout.decode(), result.stderr.decode()) == (3, EXT_WANT, EXT_ERRORS), result
        result = run(vm, rootfs, ["--disk", f"sda={disk}"], AGAIN, empty)
        errors = "".join(sorted(result.stderr.decode().splitlines(keepends=True)))
        assert (result.returncode, result.stdout.decode(), errors) == (3, AGAIN_WANT, AGAIN_ERRORS), result
    print("mounts: servers' directories, and the ext2 and ext4 file systems of a disk's partitions through fuse2fs, mount on rootfs's directories")


if __name__ == "__main__":
    main()
