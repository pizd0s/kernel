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
    Header h{op, arg, 0, 0};
    const iovec parts[] = {{&h, sizeof h}, {const_cast<void*>(data), size}};
    sendPacket(fd, parts, size ? 2 : 1, nullptr, 0);
}

void fail(int fd, int e) {
    answer(fd, opErr, uint32_t(e));
}

void tell(int fd, uint32_t events, uint32_t foreground, uint32_t flags) {
    const uint32_t state[] = {foreground, flags};
    answer(fd, opReady, events, state, sizeof state);
}

Endpoint* answerOpen(int epoll, const Request& r, Opened what, void* of) {
    what.flags = r.h.arg & (O_ACCMODE | O_APPEND | O_NONBLOCK);
    int pair[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        fail(r.reply(), errno);
        return nullptr;
    }
    const uint32_t access = r.h.arg & O_ACCMODE;
    Endpoint* e = watch(epoll, {pair[0], false, access != O_WRONLY, access != O_RDONLY, of});
    Header h{opOk, 0, 0, 0};
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
    Header h{opRegister, mode, uid, gid};
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
    const int registry = open("/dev/init", O_RDWR | O_CLOEXEC);
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

int readyDescriptor(int argc, char** argv, int& first) {
    first = 1;
    if (argc > 2 && strcmp(argv[1], "-r") == 0) {
        first = 3;
        return atoi(argv[2]);
    }
    return -1;
}

Endpoint* watch(int epoll, Endpoint e) {
    auto* at = static_cast<Endpoint*>(malloc(sizeof(Endpoint)));
    if (!at) {
        fatal("service: out of memory");
    }
    *at = e;
    epoll_event ev{};
    ev.events = EPOLLIN;
    ev.data.ptr = at;
    if (epoll_ctl(epoll, EPOLL_CTL_ADD, e.fd, &ev) != 0) {
        fatal("service: epoll_ctl: errno %d", errno);
    }
    ++watched;
    return at;
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

// A connection goes: out of its open file's.
static void disconnect(int epoll, Endpoint* c) {
    for (Endpoint** at = &c->file->first; *at; at = &(*at)->after) {
        if (*at == c) {
            *at = c->after;
            break;
        }
    }
    drop(epoll, c);
}

void unwatch(int epoll, Endpoint* e) {
    while (Endpoint* c = e->first) {
        disconnect(epoll, c);
    }
    drop(epoll, e);
}

// An open file's CONNECT: a call pair, whose serving end is a connection
// of the file's from now on and whose asking end the OK brings, on the
// file's endpoint, the question's.
static void connect(int epoll, Endpoint* e) {
    int pair[2];
    if (socketpair(AF_UNIX, SOCK_CALL | SOCK_CLOEXEC, 0, pair) != 0) {
        fail(e->fd, errno);
        return;
    }
    Endpoint* c = watch(epoll, {pair[0], false, e->reads, e->writes, e->of});
    c->file = e;
    c->after = e->first;
    e->first = c;
    Header h{opOk, 0, 0, 0};
    const iovec part{&h, sizeof h};
    sendPacket(e->fd, &part, 1, &pair[1], 1); // a gone asker is its own loss
    close(pair[1]);
}

Endpoint* keep(int epoll, Request& r, void* of) {
    if (Endpoint* c = r.connection) {
        c->kept = true;
        c->of = of;
        return c;
    }
    Endpoint* e = watch(epoll, {r.reply(), false, false, false, of});
    r.fds[r.count - 1] = -1;
    return e;
}

void unkeep(int epoll, Endpoint* e) {
    if (e->file) {
        e->kept = false;
        e->of = e->file->of;
    } else {
        unwatch(epoll, e);
    }
}

int Request::reply() const {
    return fds[count - 1];
}

int Request::window() const {
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
    const uint32_t known = h.op == opWrite ? ioAppend | ioNonblock : ioNonblock;
    return (out.flags & ~known) == 0;
}

void stopServing() {
    stopping = true;
}

// Serves one event of the loop's: a connection to accept, a request to
// handle, or the end of an endpoint.
static void serveEvent(int epoll, Endpoint* e, Handle handle, Gone gone, Accepted accepted) {
    alignas(Header) static unsigned char packet[65536];
    if (e->fd < 0) {
        return; // let go of during this batch
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
    const ssize_t got = receivePacket(e->fd, packet, sizeof packet, r.fds, r.count);
    if (got <= 0) {
        if (e->file) {
            // A connection whose asker let go of it, a request kept on it
            // gone with it.
            if (e->kept && gone) {
                gone(*e);
            }
            disconnect(epoll, e);
            return;
        }
        // An open file closed: what is kept of it on its connections
        // first.
        for (Endpoint* c = e->first; c; c = c->after) {
            if (c->kept && gone) {
                gone(*c);
            }
        }
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
    if ((e->file || e->answers || r.count == 0) && r.count < maxFds) {
        // Answered on the endpoint it came on: a connection's request, and
        // a question, in the order the questions came.
        r.fds[r.count++] = e->fd;
        r.onEndpoint = true;
    }
    if (r.h.op == opConnect && !e->channel && !e->file && !e->answers) {
        connect(epoll, e);
    } else if (e->channel && r.h.op != opOpen) {
        fail(r.reply(), EINVAL);
    } else if (e->file && !e->kept) {
        // The CANCEL of a request nobody kept has nothing to end.
        if (r.h.op != opCancel) {
            r.connection = e;
            handle(*e->file, r);
        }
    } else {
        handle(*e, r);
    }
    for (size_t k = 0; k < r.count; ++k) {
        if (r.fds[k] >= 0 && !(r.onEndpoint && k + 1 == r.count)) {
            close(r.fds[k]);
        }
    }
}

void serve(int epoll, Handle handle, Gone gone, Accepted accepted, Expire expire) {
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
                    serveEvent(epoll, static_cast<Endpoint*>(events[i].data.ptr), handle, gone, accepted);
                }
            }
        }
        releaseAll();
    }
}
