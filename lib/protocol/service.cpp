#include "service.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/uio.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>

static bool stopping = false;
static size_t watched = 0;
// What unwatch let go of during a batch of events, freed after it: an
// event of the batch may still name it.
static Endpoint* released;

void fatal(const char* format, ...) {
    va_list args;
    va_start(args, format);
    vdprintf(STDERR_FILENO, format, args);
    va_end(args);
    dprintf(STDERR_FILENO, "\n");
    exit(1);
}

void answer(int fd, uint32_t op, uint32_t arg, const void* data, size_t size) {
    Header h{op, arg, 0, 0, 0, 0};
    const iovec parts[] = {{&h, sizeof h}, {const_cast<void*>(data), size}};
    sendPacket(fd, parts, size ? 2 : 1, nullptr, 0);
}

void fail(int fd, int e) {
    answer(fd, opErr, uint32_t(e));
}

void tell(int fd, uint32_t events) {
    answer(fd, opReady, events);
}

Endpoint* answerOpen(int epoll, const Request& r, Opened what, void* of) {
    what.access = r.h.arg & O_ACCMODE;
    int pair[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        fail(r.reply(), errno);
        return nullptr;
    }
    const uint32_t access = r.h.arg & O_ACCMODE;
    Endpoint opened{pair[0], false, access != O_WRONLY, access != O_RDONLY, of};
    opened.append = (r.h.arg & O_APPEND) != 0;
    opened.nonblock = (r.h.arg & O_NONBLOCK) != 0;
    Endpoint* e = watch(epoll, opened);
    Header h{opOk, 1, 0, 0, 0, 0}; // one open file, what
    const iovec parts[] = {{&h, sizeof h}, {&what, sizeof what}};
    sendPacket(r.reply(), parts, 2, &pair[1], 1); // a gone asker is its own loss
    close(pair[1]);
    return e;
}

int enroll(int registry, const char* name, uint32_t mode, uint32_t uid, uint32_t gid, Device& out) {
    int pair[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        return errno;
    }
    Header h{opRegister, mode, uid, gid, 0, 0};
    const iovec parts[] = {{&h, sizeof h}, {const_cast<char*>(name), strlen(name)}};
    const bool sent = sendPacket(registry, parts, 2, &pair[1], 1) >= 0;
    const int e = errno;
    close(pair[1]);

    struct {
        Header h;
        Stat st;
    } got{};

    const ssize_t n = sent ? recv(registry, &got, sizeof got, 0) : -1;
    if (n == ssize_t(sizeof got) && got.h.op == opOk) {
        out = {pair[0], got.st};
        return 0;
    }
    close(pair[0]);
    if (!sent) {
        return e;
    }
    return n >= ssize_t(sizeof(Header)) && got.h.op == opErr ? int(got.h.arg) : EIO;
}

Device enroll(const char* program, const char* name, uint32_t mode) {
    const int registry = open("/dev/init/ctl", O_RDWR | O_CLOEXEC);
    Device d{};
    const int e = registry < 0 ? errno : enroll(registry, name, mode, uint32_t(geteuid()), uint32_t(getegid()), d);
    if (registry >= 0) {
        close(registry);
    }
    if (e != 0) {
        fatal("%s: /dev/%s: %s", program, name, strerror(e));
    }
    return d;
}

int enrollSocket(const char* program, const char* name, uint32_t mode, int type, Device& registered) {
    registered = enroll(program, name, S_IFSOCK | (mode & 07777));
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    snprintf(address.sun_path, sizeof address.sun_path, "/dev/%s", name);
    const int s = socket(AF_UNIX, type | SOCK_CLOEXEC, 0);
    if (s < 0 || bind(s, reinterpret_cast<const sockaddr*>(&address), sizeof address) != 0 || listen(s, 16) != 0) {
        fatal("%s: %s: %s", program, address.sun_path, strerror(errno));
    }
    return s;
}

int readyDescriptor(int argc, char** argv, int& first) {
    first = 1;
    if (argc > 2 && strcmp(argv[1], "-r") == 0) {
        first = 3;
        return atoi(argv[2]);
    }
    return -1;
}

static Endpoint* watchFor(int epoll, Endpoint e, uint32_t events) {
    auto* at = static_cast<Endpoint*>(malloc(sizeof(Endpoint)));
    if (!at) {
        fatal("service: out of memory");
    }
    *at = e;
    epoll_event ev{};
    ev.events = events;
    ev.data.ptr = at;
    if (epoll_ctl(epoll, EPOLL_CTL_ADD, e.fd, &ev) != 0) {
        fatal("service: epoll_ctl: errno %d", errno);
    }
    ++watched;
    return at;
}

Endpoint* watch(int epoll, Endpoint e) {
    return watchFor(epoll, e, EPOLLIN);
}

static void releaseAll() {
    while (Endpoint* e = released) {
        released = e->next;
        free(e);
    }
}

// Lets go of an endpoint: its descriptor closes, and it is freed after
// the events in hand.
static void drop(int epoll, Endpoint* e) {
    epoll_ctl(epoll, EPOLL_CTL_DEL, e->fd, nullptr);
    close(e->fd);
    e->fd = -1;
    --watched;
    e->next = released;
    released = e;
}

void unwatch(int epoll, Endpoint* e) {
    drop(epoll, e);
}

bool asSocketCall(Request& r) {
    uint32_t call, arg;
    if (r.h.op != opCtl || !r.at(0, call) || !r.at(4, arg) || call < ctlBind || call > ctlShutdown) {
        return false;
    }
    r.h.op = call;
    r.h.arg = arg;
    r.body += 8;
    r.size -= 8;
    return true;
}

Endpoint* keep(int epoll, Request& r, void* of) {
    Endpoint* e = watch(epoll, {r.reply(), false, false, false, of});
    r.fds[r.count - 1] = -1;
    return e;
}

void unkeep(int epoll, Endpoint* e) {
    unwatch(epoll, e);
}

int Request::reply() const {
    return fds[count - 1];
}

int Request::memfd() const {
    return count >= 2 ? fds[0] : -1;
}

bool Request::copy(size_t offset, void* out, size_t length) const {
    if (size < offset + length) {
        return false;
    }
    memcpy(out, body + offset, length);
    return true;
}

bool Request::io(Io& out) const {
    if (size != sizeof out) {
        return false;
    }
    memcpy(&out, body, sizeof out);
    return (out.flags & ~ioAt) == 0;
}

uint64_t offsetOf(const Endpoint& e, const Io& io, bool write, uint64_t size) {
    if ((io.flags & ioAt) != 0) {
        return io.off;
    }
    return write && e.append ? size : e.pos;
}

int seekOf(Endpoint& e, const Request& r, uint64_t size, uint64_t& out) {
    int64_t off;
    if (r.size != sizeof off || !r.at(0, off)) {
        return EINVAL;
    }
    int64_t from = 0;
    switch (r.h.arg) {
        case SEEK_SET:
            break;
        case SEEK_CUR:
            from = int64_t(e.pos);
            break;
        case SEEK_END:
            from = int64_t(size);
            break;
        default:
            return EINVAL;
    }
    if (off > 0 && from > INT64_MAX - off) {
        return EOVERFLOW;
    }
    if (from + off < 0) {
        return EINVAL;
    }
    out = uint64_t(from + off);
    e.pos = out;
    return 0;
}

void answerSeek(Endpoint& e, const Request& r, uint64_t size) {
    uint64_t pos;
    if (const int err = seekOf(e, r, size, pos); err != 0) {
        return fail(r.reply(), err);
    }
    answer(r.reply(), opOk, 0, &pos, sizeof pos);
}

// GETFL and SETFL of an open file: what its open lets it do and the
// flags its endpoint keeps.
static void flagsOf(Endpoint& e, const Request& r) {
    if (r.h.op == opSetfl) {
        e.append = (r.h.arg & O_APPEND) != 0;
        e.nonblock = (r.h.arg & O_NONBLOCK) != 0;
        return answer(r.reply(), opOk, 0);
    }
    const uint32_t flags = (e.reads && e.writes ? O_RDWR : e.writes ? O_WRONLY : O_RDONLY) | (e.append ? O_APPEND : 0) |
                           (e.nonblock ? O_NONBLOCK : 0);
    answer(r.reply(), opOk, 0, &flags, sizeof flags);
}

void stopServing() {
    stopping = true;
}

// Serves one event of the loop's: a connection to accept, a request to
// handle, or the end of an endpoint.
static void serveEvent(int epoll, Endpoint* e, Handle handle, Gone gone, Accepted accepted, Raw raw) {
    alignas(Header) static unsigned char packet[65536];
    if (e->fd < 0) {
        return; // let go of during this batch
    }
    if (e->raw) {
        if (raw) {
            raw(*e);
        }
        return;
    }
    if (e->listens) {
        const int connection = accept4(e->fd, nullptr, nullptr, SOCK_CLOEXEC);
        if (connection >= 0 && accepted) {
            accepted(*e, connection);
        } else if (connection >= 0) {
            close(connection);
        }
        return;
    }
    Request r{};
    const ssize_t got = receivePacket(e->fd, packet, sizeof packet, r.fds, r.count, 0);
    if (got <= 0) {
        // The other end closed: an open file's, or who asked the request
        // kept here.
        if (gone && !e->channel) {
            gone(*e);
        }
        unwatch(epoll, e);
        return;
    }
    if (size_t(got) < sizeof(Header) || (r.count == 0 && e->channel)) {
        for (size_t k = 0; k < r.count; ++k) {
            close(r.fds[k]);
        }
        return;
    }
    memcpy(&r.h, packet, sizeof r.h);
    r.body = packet + sizeof(Header);
    r.size = size_t(got) - sizeof(Header);
    if ((e->answers || r.count == 0) && r.count < maxFds) {
        // Answered on the endpoint it came on: a question, in the order
        // the questions came, and the CANCEL of a request kept there.
        r.fds[r.count++] = e->fd;
        r.onEndpoint = true;
    }
    if (e->channel && r.h.op != opOpen) {
        fail(r.reply(), EINVAL);
    } else if (!e->channel && !e->answers && (r.h.op == opGetfl || r.h.op == opSetfl)) {
        flagsOf(*e, r);
    } else {
        handle(*e, r);
    }
    for (size_t k = 0; k < r.count; ++k) {
        if (r.fds[k] >= 0 && !(r.onEndpoint && k + 1 == r.count)) {
            close(r.fds[k]);
        }
    }
}

void serve(int epoll, Handle handle, Gone gone, Accepted accepted, Expire expire, Raw raw) {
    while (!stopping && watched > 0) {
        epoll_event events[16];
        const int timeout = expire ? expire() : -1;
        const int n = epoll_wait(epoll, events, 16, timeout);
        if (n < 0 && errno != EINTR) {
            fatal("service: epoll_wait: errno %d", errno);
        }
        // The ends that closed first: a request the batch has came after
        // them, its caller having seen them close, and finds them gone, a
        // slave's read after its master's close the hung-up read.
        for (int pass = 0; pass < 2; ++pass) {
            for (int i = 0; i < n; ++i) {
                const bool hangup = (events[i].events & (EPOLLHUP | EPOLLERR)) != 0;
                if (hangup == (pass == 0)) {
                    serveEvent(epoll, static_cast<Endpoint*>(events[i].data.ptr), handle, gone, accepted, raw);
                }
            }
        }
        releaseAll();
    }
}
