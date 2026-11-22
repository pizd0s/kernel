// blk: a disk as a block device, a service of rootfs's /dev/init/ctl
// (lib/protocol/service.h). The disk is a file of the host vm gives the
// system (vm run --disk), descriptor fd here, and /dev/blk/<name>/data its
// bytes: the host copies a READ's straight into the memory of who reads,
// through the memfd the READ brings, and a WRITE's straight from the
// writer's, one copy (copy_file_range). Each partition of the disk's GUID
// partition table, 512-byte blocks, is a block device of blk's too,
// /dev/blk/<name>/<n>/data: the disk's bytes [first, first + length),
// served the same, cut at the partition's end. A partition with an ext2,
// ext3 or ext4 file system gets /bin/fuse2fs for it, which binds
// /dev/blk/<name>/<n>/fs.
//
//     blk [-r fd] <name> <fd>
//
// -r: a descriptor to close once the disk and its partitions are there,
// which the services of the file systems get too.

#include <bin/tools/tools.h>
#include <lib/protocol/service.h>

#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/param.h>
#include <sys/stat.h>
#include <unistd.h>

static constexpr uint64_t block = 512;

// A block device of blk's: the disk's bytes [first, first + length), the
// whole disk's or a partition's, and its stat as rootfs has its name.
struct Area {
    uint64_t first;
    uint64_t length;
    Device device;
};

static const char* name;
static int disk = -1;
static Area whole;
static int epoll = -1;

// A READ: the area's bytes at the open file's position or off, as many
// as asked up to its end, into the memfd; the position moves by them.
static void readBlocks(const Area& a, Endpoint& e, const Request& r) {
    Io io;
    if (!e.reads) {
        return fail(r.reply(), EBADF);
    }
    if (r.memfd() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    const uint64_t off = offsetOf(e, io, false, a.length);
    const uint64_t count = off >= a.length ? 0 : MIN(uint64_t(r.h.arg), a.length - off);
    uint64_t done = 0;
    while (done < count) {
        off_t from = off_t(a.first + off + done);
        off_t to = off_t(done);
        const ssize_t n = copy_file_range(disk, &from, r.memfd(), &to, size_t(count - done), 0);
        if (n < 0 && done == 0) {
            return fail(r.reply(), errno);
        }
        if (n <= 0) {
            break;
        }
        done += uint64_t(n);
    }
    if ((io.flags & ioAt) == 0) {
        e.pos = off + done;
    }
    const uint32_t got = uint32_t(done);
    answer(r.reply(), opOk, 0, &got, sizeof got);
}

// A WRITE: the memfd's bytes, as many as fit before the area's end, onto
// it at the open file's position or off; none fit, ENOSPC. An area has no
// end to append at: one that appends writes at its position.
static void writeBlocks(const Area& a, Endpoint& e, const Request& r) {
    Io io;
    if (!e.writes) {
        return fail(r.reply(), EBADF);
    }
    if (r.memfd() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    const uint64_t off = (io.flags & ioAt) != 0 ? io.off : e.pos;
    const uint32_t length = r.h.arg;
    if (length > 0 && off >= a.length) {
        return fail(r.reply(), ENOSPC);
    }
    const uint64_t count = length == 0 ? 0 : MIN(uint64_t(length), a.length - off);
    uint64_t done = 0;
    while (done < count) {
        off_t from = off_t(done);
        off_t to = off_t(a.first + off + done);
        const ssize_t n = copy_file_range(r.memfd(), &from, disk, &to, size_t(count - done), 0);
        if (n < 0 && done == 0) {
            return fail(r.reply(), errno);
        }
        if (n <= 0) {
            break;
        }
        done += uint64_t(n);
    }
    if ((io.flags & ioAt) == 0) {
        e.pos = off + done;
    }
    const uint32_t got = uint32_t(done);
    answer(r.reply(), opOk, 0, &got, sizeof got);
}

// A request of an area's channel, an OPEN, or of an open file of it: the
// endpoint's of is the area.
static void handle(Endpoint& e, Request& r) {
    if (e.channel) {
        answerOpen(epoll, r, {openedFile, 0, 0, 0, 0, POLLIN | POLLOUT}, e.of);
        return;
    }
    const Area& a = *static_cast<const Area*>(e.of);
    switch (r.h.op) {
        case opRead:
            readBlocks(a, e, r);
            break;
        case opWrite:
            writeBlocks(a, e, r);
            break;
        case opFstat: {
            Stat st = a.device.st;
            st.size = a.length;
            answer(r.reply(), opOk, 0, &st, sizeof st);
            break;
        }
        case opFsync:
            if ((r.h.arg != 0 ? fdatasync(disk) : fsync(disk)) != 0) {
                fail(r.reply(), errno);
            } else {
                answer(r.reply(), opOk, 0);
            }
            break;
        case opFchmod:
        case opFchown:
        case opFutimes:
            fail(r.reply(), EPERM);
            break;
        case opSeek:
            answerSeek(e, r, a.length);
            break;
        default:
            fail(r.reply(), unserved(r.h.op));
    }
}

// Registers an area of name on /dev/init/ctl's registry and serves its opens;
// false when it is not there, which is told.
static bool registered(int registry, const char* named, Area& a) {
    if (const int e = enroll(registry, named, S_IFBLK | 0660, uint32_t(geteuid()), uint32_t(getegid()), a.device)) {
        dprintf(STDERR_FILENO, "blk: /dev/%s: %s\n", named, strerror(e));
        return false;
    }
    watch(epoll, {a.device.end, true, false, false, &a});
    return true;
}

// Starts /bin/fuse2fs for the file system of the partition number, with
// the descriptor ready, which it closes once it is up.
static void fileSystem(uint32_t number, int ready) {
    char path[128];
    char fsName[128];
    char options[32];
    snprintf(path, sizeof path, "/dev/blk/%s/%u/data", name, number);
    snprintf(fsName, sizeof fsName, "blk/%s/%u/fs", name, number);
    snprintf(options, sizeof options, "ready=%d", ready);
    char fuse2fs[] = "fuse2fs";
    char option[] = "-o";
    char* const withReady[] = {fuse2fs, path, fsName, option, options, nullptr};
    char* const without[] = {fuse2fs, path, fsName, nullptr};
    const pid_t pid = fork();
    if (pid == 0) {
        execv("/bin/fuse2fs", ready >= 0 ? withReady : without);
        dprintf(STDERR_FILENO, "blk: /bin/fuse2fs: %s\n", strerror(errno));
        _exit(127);
    }
    if (pid < 0) {
        dprintf(STDERR_FILENO, "blk: fork: %s\n", strerror(errno));
    }
}

// The partitions of the disk's GUID partition table: its header in the
// second block, its entries where the header says; an entry of no type
// is none, and one past the disk is left out. Each is registered, and one
// with an ext2, ext3 or ext4 file system, its superblock's magic at 1024
// + 56, gets its service.
static void partitions(int registry, int ready) {
    unsigned char header[block];
    if (pread(disk, header, block, block) != ssize_t(block) || memcmp(header, "EFI PART", 8) != 0) {
        return;
    }
    uint64_t entries;
    uint32_t count;
    uint32_t entrySize;
    memcpy(&entries, header + 72, 8);
    memcpy(&count, header + 80, 4);
    memcpy(&entrySize, header + 84, 4);
    if (entrySize < 128 || entrySize > 4096 || count > 1024) {
        return;
    }
    unsigned char entry[4096];
    static const unsigned char none[16] = {};
    for (uint32_t i = 0; i < count; ++i) {
        if (pread(disk, entry, entrySize, off_t(entries * block + uint64_t(i) * entrySize)) != ssize_t(entrySize)) {
            return;
        }
        uint64_t firstBlock;
        uint64_t lastBlock;
        memcpy(&firstBlock, entry + 32, 8);
        memcpy(&lastBlock, entry + 40, 8);
        if (memcmp(entry, none, sizeof none) == 0 || lastBlock < firstBlock || lastBlock >= whole.length / block) {
            continue;
        }
        auto* a = static_cast<Area*>(calloc(1, sizeof(Area)));
        if (!a) {
            fatal("blk: out of memory");
        }
        a->first = firstBlock * block;
        a->length = (lastBlock - firstBlock + 1) * block;
        char part[128];
        snprintf(part, sizeof part, "blk/%s/%u/data", name, i + 1);
        if (!registered(registry, part, *a)) {
            free(a);
            continue;
        }
        unsigned char magic[2];
        if (pread(disk, magic, sizeof magic, off_t(a->first + 1024 + 56)) == ssize_t(sizeof magic) && magic[0] == 0x53 && magic[1] == 0xEF) {
            fileSystem(i + 1, ready);
        }
    }
}

int blkMain(int argc, char** argv) {
    int first;
    const int ready = readyDescriptor(argc, argv, first);
    if (argc - first != 2) {
        dprintf(STDERR_FILENO, "usage: blk [-r fd] <name> <fd>\n");
        return 2;
    }
    name = argv[first];
    disk = atoi(argv[first + 1]);
    const off_t end = lseek(disk, 0, SEEK_END);
    if (end < 0 || fcntl(disk, F_SETFD, FD_CLOEXEC) != 0) {
        fatal("blk: %s: %s", name, strerror(errno));
    }
    whole.length = uint64_t(end);
    signal(SIGCHLD, SIG_IGN);
    epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0) {
        fatal("blk: epoll_create1: errno %d", errno);
    }
    const int registry = open("/dev/init/ctl", O_RDWR | O_CLOEXEC);
    if (registry < 0) {
        fatal("blk: /dev/init/ctl: %s", strerror(errno));
    }
    char data[128];
    snprintf(data, sizeof data, "blk/%s/data", name);
    if (!registered(registry, data, whole)) {
        exit(1);
    }
    partitions(registry, ready);
    close(registry);
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle);
    return 0;
}
