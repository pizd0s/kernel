// blk: a disk as a block device, a service of rootfs's /dev/init
// (lib/protocol/service.h). The disk is a file of the host vm gives the
// system (vm run --disk), descriptor fd here, and /dev/<name> its bytes:
// the host copies a READ's straight into the memory of who reads, through
// the memfd the READ brings, and a WRITE's straight from the writer's, one
// copy (copy_file_range). blk reads the disk's GUID partition table,
// 512-byte blocks, and starts /bin/gpt for each partition, <name><n>, or
// <name>p<n> when name ends in a digit.
//
//     blk [-r fd] <name> <fd>
//
// -r: a descriptor to close once the disk is there, which the partitions'
// services get too.

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

static const char* name;
static int disk = -1;
static uint64_t size = 0;
static Device device;

static Stat diskStat() {
    Stat st = device.st;
    st.size = size;
    return st;
}

// A READ: the disk's bytes at off, as many as asked up to its end, into
// the memfd at woff.
static void readBlocks(const Endpoint& e, const Request& r) {
    Io io;
    if (!e.reads) {
        return fail(r.reply(), EBADF);
    }
    if (r.window() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    const uint64_t off = io.off;
    const uint32_t woff = io.woff;
    const uint64_t count = off >= size ? 0 : MIN(uint64_t(r.h.arg), size - off);
    uint64_t done = 0;
    while (done < count) {
        off_t from = off_t(off + done);
        off_t to = off_t(woff) + off_t(done);
        const ssize_t n = copy_file_range(disk, &from, r.window(), &to, size_t(count - done), 0);
        if (n < 0 && done == 0) {
            return fail(r.reply(), errno);
        }
        if (n <= 0) {
            break;
        }
        done += uint64_t(n);
    }
    const uint32_t got = uint32_t(done);
    answer(r.reply(), opOk, 0, &got, sizeof got);
}

// A WRITE: the memfd's bytes at woff, as many as fit before the disk's
// end, onto the disk at off; none fit, ENOSPC.
static void writeBlocks(const Endpoint& e, const Request& r) {
    Io io;
    if (!e.writes) {
        return fail(r.reply(), EBADF);
    }
    if (r.window() < 0 || !r.io(io) || (io.flags & ioAppend) != 0) {
        return fail(r.reply(), EINVAL);
    }
    const uint64_t off = io.off;
    const uint32_t length = r.h.arg;
    const uint32_t woff = io.woff;
    if (length > 0 && off >= size) {
        return fail(r.reply(), ENOSPC);
    }
    const uint64_t count = length == 0 ? 0 : MIN(uint64_t(length), size - off);
    uint64_t done = 0;
    while (done < count) {
        off_t from = off_t(woff) + off_t(done);
        off_t to = off_t(off + done);
        const ssize_t n = copy_file_range(r.window(), &from, disk, &to, size_t(count - done), 0);
        if (n < 0 && done == 0) {
            return fail(r.reply(), errno);
        }
        if (n <= 0) {
            break;
        }
        done += uint64_t(n);
    }
    unsigned char wrote[12];
    const uint32_t got = uint32_t(done);
    memcpy(wrote, &got, 4);
    memcpy(wrote + 4, &off, 8);
    answer(r.reply(), opOk, 0, wrote, sizeof wrote);
}

static bool handle(Endpoint& e, Request& r) {
    const Stat st = diskStat();
    if (e.channel) {
        answer(r.reply(), opOk, 0, &st, sizeof st);
        return true;
    }
    switch (r.h.op) {
        case opRead:
            readBlocks(e, r);
            break;
        case opWrite:
            writeBlocks(e, r);
            break;
        case opFstat:
            answer(r.reply(), opOk, 0, &st, sizeof st);
            break;
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
        default:
            fail(r.reply(), EINVAL);
    }
    return false;
}

// Starts /bin/gpt for a partition, the bytes [first, first + length) of
// the disk, with the descriptor ready, which it closes once it is up.
static void start(uint32_t index, uint64_t first, uint64_t length, int ready) {
    const size_t n = strlen(name);
    const bool digit = n > 0 && name[n - 1] >= '0' && name[n - 1] <= '9';
    char path[64];
    char part[64];
    char from[24];
    char bytes[24];
    char fd[12];
    snprintf(path, sizeof path, "/dev/%s", name);
    snprintf(part, sizeof part, "%s%s%u", name, digit ? "p" : "", index);
    snprintf(from, sizeof from, "%llu", (unsigned long long)first);
    snprintf(bytes, sizeof bytes, "%llu", (unsigned long long)length);
    snprintf(fd, sizeof fd, "%d", ready);
    char gpt[] = "gpt";
    char option[] = "-r";
    char* const withReady[] = {gpt, option, fd, path, part, from, bytes, nullptr};
    char* const without[] = {gpt, path, part, from, bytes, nullptr};
    const pid_t pid = fork();
    if (pid == 0) {
        execv("/bin/gpt", ready >= 0 ? withReady : without);
        dprintf(STDERR_FILENO, "blk: /bin/gpt: %s\n", strerror(errno));
        _exit(127);
    }
    if (pid < 0) {
        dprintf(STDERR_FILENO, "blk: fork: %s\n", strerror(errno));
    }
}

// The partitions of the disk's GUID partition table: its header in the
// second block, its entries where the header says; an entry of no type
// is none, and one past the disk is left out.
static void partitions(int ready) {
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
        if (memcmp(entry, none, sizeof none) == 0 || lastBlock < firstBlock || lastBlock >= size / block) {
            continue;
        }
        start(i + 1, firstBlock * block, (lastBlock - firstBlock + 1) * block, ready);
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
    size = uint64_t(end);
    signal(SIGCHLD, SIG_IGN);
    const int epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0) {
        fatal("blk: epoll_create1: errno %d", errno);
    }
    device = enroll("blk", name, S_IFBLK | 0660);
    watch(epoll, {device.end, true, false, false, nullptr});
    partitions(ready);
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle);
    return 0;
}
