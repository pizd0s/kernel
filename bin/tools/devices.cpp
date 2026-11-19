// devices: the devices every system has, a service of rootfs's /dev/init
// (lib/protocol/service.h): null, which reads nothing; zero, which reads
// zeros; full, which reads zeros and has no room for a write, ENOSPC;
// random and urandom, which read the kernel's entropy. The others take
// everything written to them. An open of one is an endpoint of this process,
// rootfs hands it on: a READ's bytes go straight into the reader's
// memory, through the memfd it brings, and a WRITE's are never looked at.
//
//     devices [-r fd]
//
// -r: a descriptor to close once the devices are there; rootfs starts the
// shell when it has been.

#include <bin/tools/tools.h>
#include <lib/protocol/service.h>

#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/param.h>
#include <sys/stat.h>
#include <unistd.h>

namespace {
    enum class Kind {
        Null,
        Zero,
        Full,
        Random,
    };

    struct Source {
        const char* name;
        Kind kind;
        Device registered;
    };
}

static Source sources[] = {
    {"null", Kind::Null, {}},
    {"zero", Kind::Zero, {}},
    {"full", Kind::Full, {}},
    {"random", Kind::Random, {}},
    {"urandom", Kind::Random, {}},
};

static const unsigned char zeros[65536] = {};
static unsigned char entropy[65536];

// As many bytes as asked go into the reader's memory: zeros, or the
// kernel's random bytes; none of null.
static void readSource(const Source& s, const Endpoint& e, const Request& r) {
    Io io;
    if (!e.reads) {
        return fail(r.reply(), EBADF);
    }
    if (r.window() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    const size_t n = s.kind == Kind::Null ? 0 : r.h.arg;
    for (size_t at = 0; at < n; at += sizeof zeros) {
        const size_t count = MIN(sizeof zeros, n - at);
        const unsigned char* bytes = zeros;
        if (s.kind == Kind::Random) {
            for (size_t k = 0; k < count; k += 256) {
                if (getentropy(entropy + k, MIN(size_t(256), count - k)) != 0) {
                    return fail(r.reply(), EIO);
                }
            }
            bytes = entropy;
        }
        if (pwrite(r.window(), bytes, count, off_t(io.woff) + off_t(at)) != ssize_t(count)) {
            return fail(r.reply(), EIO);
        }
    }
    const uint32_t count = uint32_t(n);
    answer(r.reply(), opOk, 0, &count, sizeof count);
}

// Everything written is taken, where it was written, but by full.
static void writeSource(const Source& s, const Endpoint& e, const Request& r) {
    Io io;
    if (!e.writes) {
        return fail(r.reply(), EBADF);
    }
    if (r.window() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    if (s.kind == Kind::Full) {
        return fail(r.reply(), ENOSPC);
    }
    unsigned char taken[12];
    memcpy(taken, &r.h.arg, 4);
    memcpy(taken + 4, &io.off, 8);
    answer(r.reply(), opOk, 0, taken, sizeof taken);
}

static int epoll = -1;

static void handle(Endpoint& e, Request& r) {
    const Source& s = *static_cast<Source*>(e.of);
    const Stat& st = s.registered.st;
    if (e.channel) {
        answerOpen(epoll, r, {openedFile, 0, 0, 0, 0}, e.of);
        return;
    }
    switch (r.h.op) {
        case opRead:
            readSource(s, e, r);
            break;
        case opWrite:
            writeSource(s, e, r);
            break;
        case opFstat:
            answer(r.reply(), opOk, 0, &st, sizeof st);
            break;
        case opFsync:
            answer(r.reply(), opOk, 0);
            break;
        case opFchmod:
        case opFchown:
        case opFutimes:
            fail(r.reply(), EPERM);
            break;
        default:
            fail(r.reply(), unserved(r.h.op));
    }
}

int devicesMain(int argc, char** argv) {
    int first;
    const int ready = readyDescriptor(argc, argv, first);
    if (first != argc) {
        dprintf(STDERR_FILENO, "usage: devices [-r fd]\n");
        return 2;
    }
    epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0) {
        fatal("devices: epoll_create1: errno %d", errno);
    }
    for (Source& s : sources) {
        s.registered = enroll("devices", s.name, S_IFCHR | 0666);
        watch(epoll, {s.registered.end, true, false, false, &s});
    }
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle);
    return 0;
}
