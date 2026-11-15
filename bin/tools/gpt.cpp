// gpt: a partition of a block device as a block device of its own, a
// service of rootfs's /dev/init (lib/protocol/service.h): /dev/<name> is
// the bytes [first, first + length) of device. A READ or WRITE goes on to
// the device as it came, moved by first and cut at the partition's end,
// its memfd and its reply's endpoint with it: the device's service puts
// the bytes into the memory of who reads, or takes them from who writes,
// and answers them itself. This process never has the bytes, nor waits
// for the device. A partition with an ext2, ext3 or ext4 file system gets
// /bin/fuse2fs for it, <name>fs.
//
//     gpt [-r fd] <device> <name> <first> <length>
//
// -r: a descriptor to close once the partition is there, which the file
// system's service gets too.

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
#include <sys/uio.h>
#include <unistd.h>

static int device = -1;
static uint64_t first = 0;
static uint64_t length = 0;
static Device partition;

static Stat partitionStat() {
    Stat st = partition.st;
    st.size = length;
    return st;
}

// Hands a request on to the device: its header, the body given, the
// request's descriptors. A device that is gone is EIO, and this service
// goes too: its partition with it.
static void forward(const Request& r, uint32_t op, uint32_t arg, const void* body, size_t size) {
    Header h{op, arg, r.h.uid, r.h.gid};
    const iovec parts[] = {{&h, sizeof h}, {const_cast<void*>(body), size}};
    if (sendPacket(device, parts, 2, r.fds, r.count) < 0) {
        fail(r.reply(), EIO);
        if (errno == EPIPE) {
            exit(0);
        }
    }
}

// A READ: up to the partition's end, at the device's offset.
static void readPartition(const Endpoint& e, const Request& r) {
    uint64_t off;
    uint32_t woff;
    if (!e.reads) {
        return fail(r.reply(), EBADF);
    }
    if (r.window() < 0 || !r.at(0, off) || !r.at(8, woff)) {
        return fail(r.reply(), EINVAL);
    }
    if (off >= length || r.h.arg == 0) {
        const uint32_t none = 0;
        return answer(r.reply(), opOk, 0, &none, sizeof none);
    }
    unsigned char body[12];
    const uint64_t at = first + off;
    memcpy(body, &at, 8);
    memcpy(body + 8, &woff, 4);
    forward(r, opRead, uint32_t(MIN(uint64_t(r.h.arg), length - off)), body, sizeof body);
}

// A WRITE: what fits before the partition's end, at the device's
// offset; none fits, ENOSPC. A partition has no end to append at.
static void writePartition(const Endpoint& e, const Request& r) {
    uint64_t off;
    uint32_t size;
    uint32_t woff;
    if (!e.writes) {
        return fail(r.reply(), EBADF);
    }
    if (r.window() < 0 || !r.at(0, off) || !r.at(8, size) || !r.at(12, woff) || (r.h.arg & 1) != 0) {
        return fail(r.reply(), EINVAL);
    }
    if (size == 0) {
        unsigned char none[12] = {};
        memcpy(none + 4, &off, 8);
        return answer(r.reply(), opOk, 0, none, sizeof none);
    }
    if (off >= length) {
        return fail(r.reply(), ENOSPC);
    }
    unsigned char body[16];
    const uint64_t at = first + off;
    const uint32_t count = uint32_t(MIN(uint64_t(size), length - off));
    memcpy(body, &at, 8);
    memcpy(body + 8, &count, 4);
    memcpy(body + 12, &woff, 4);
    forward(r, opWrite, 0, body, sizeof body);
}

// Starts /bin/fuse2fs for the partition when it has an ext2 file system:
// its superblock's magic, 1024 bytes in, at 56.
static void probe(const char* name, int ready) {
    unsigned char magic[2];
    if (pread(device, magic, sizeof magic, off_t(first + 1024 + 56)) != ssize_t(sizeof magic) || magic[0] != 0x53 || magic[1] != 0xEF) {
        return;
    }
    char path[64];
    char fsName[64];
    char options[32];
    snprintf(path, sizeof path, "/dev/%s", name);
    snprintf(fsName, sizeof fsName, "%sfs", name);
    snprintf(options, sizeof options, "ready=%d", ready);
    char fuse2fs[] = "fuse2fs";
    char option[] = "-o";
    char* const withReady[] = {fuse2fs, path, fsName, option, options, nullptr};
    char* const without[] = {fuse2fs, path, fsName, nullptr};
    const pid_t pid = fork();
    if (pid == 0) {
        execv("/bin/fuse2fs", ready >= 0 ? withReady : without);
        dprintf(STDERR_FILENO, "gpt: /bin/fuse2fs: %s\n", strerror(errno));
        _exit(127);
    }
}

static bool handle(Endpoint& e, Request& r) {
    const Stat st = partitionStat();
    if (e.channel) {
        answer(r.reply(), opOk, 0, &st, sizeof st);
        return true;
    }
    switch (r.h.op) {
        case opRead:
            readPartition(e, r);
            break;
        case opWrite:
            writePartition(e, r);
            break;
        case opFsync:
            forward(r, opFsync, r.h.arg, nullptr, 0);
            break;
        case opFstat:
            answer(r.reply(), opOk, 0, &st, sizeof st);
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

int gptMain(int argc, char** argv) {
    int at;
    const int ready = readyDescriptor(argc, argv, at);
    if (argc - at != 4) {
        dprintf(STDERR_FILENO, "usage: gpt [-r fd] <device> <name> <first> <length>\n");
        return 2;
    }
    const char* name = argv[at + 1];
    first = strtoull(argv[at + 2], nullptr, 10);
    length = strtoull(argv[at + 3], nullptr, 10);
    device = open(argv[at], O_RDWR | O_CLOEXEC);
    if (device < 0) {
        fatal("gpt: %s: %s", argv[at], strerror(errno));
    }
    const int epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0) {
        fatal("gpt: epoll_create1: errno %d", errno);
    }
    partition = enroll("gpt", name, S_IFBLK | 0660);
    watch(epoll, {partition.end, true, false, false, nullptr});
    signal(SIGCHLD, SIG_IGN);
    probe(name, ready);
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle);
    return 0;
}
