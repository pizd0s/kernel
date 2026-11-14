#include "service.h"

#include <errno.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <sys/uio.h>
#include <unistd.h>

namespace {
    static bool stopping = false;
    static size_t watched = 0;
}

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

int enroll(int registry, const char* name, uint32_t mode, Device& out) {
    int pair[2];
    int reply[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        return errno;
    }
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, reply) != 0) {
        const int e = errno;
        close(pair[0]);
        close(pair[1]);
        return e;
    }
    Header h{opRegister, mode, uint32_t(geteuid()), uint32_t(getegid())};
    const iovec parts[] = {{&h, sizeof h}, {const_cast<char*>(name), strlen(name)}};
    const int fds[] = {pair[1], reply[1]};
    const bool sent = sendPacket(registry, parts, 2, fds, 2) >= 0;
    const int e = errno;
    close(pair[1]);
    close(reply[1]);

    struct {
        Header h;
        Stat st;
    } got{};

    const ssize_t n = sent ? recv(reply[0], &got, sizeof got, 0) : -1;
    close(reply[0]);
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
    const int e = registry < 0 ? errno : enroll(registry, name, mode, d);
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

void watch(int epoll, Endpoint e) {
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

void stopServing() {
    stopping = true;
}

void serve(int epoll, Handle handle, Gone gone) {
    alignas(Header) static unsigned char packet[65536];
    while (!stopping && watched > 0) {
        epoll_event events[16];
        const int n = epoll_wait(epoll, events, 16, -1);
        if (n < 0 && errno != EINTR) {
            fatal("service: epoll_wait: errno %d", errno);
        }
        for (int i = 0; i < n; ++i) {
            auto* e = static_cast<Endpoint*>(events[i].data.ptr);
            Request r{};
            const ssize_t got = receivePacket(e->fd, packet, sizeof packet, r.fds, r.count);
            if (got <= 0) {
                if (gone && !e->channel) {
                    gone(*e);
                }
                epoll_ctl(epoll, EPOLL_CTL_DEL, e->fd, nullptr);
                close(e->fd);
                free(e);
                --watched;
                continue;
            }
            if (r.count == 0 || size_t(got) < sizeof(Header)) {
                for (size_t k = 0; k < r.count; ++k) {
                    close(r.fds[k]);
                }
                continue;
            }
            memcpy(&r.h, packet, sizeof r.h);
            r.body = packet + sizeof(Header);
            r.size = size_t(got) - sizeof(Header);
            if (e->channel && r.h.op == opOpen) {
                if (handle(*e, r)) {
                    const uint32_t access = r.h.arg & O_ACCMODE;
                    watch(epoll, {r.reply(), false, access != O_WRONLY, access != O_RDONLY, e->of});
                    r.fds[r.count - 1] = -1;
                }
            } else if (e->channel) {
                fail(r.reply(), EINVAL);
            } else {
                handle(*e, r);
            }
            for (size_t k = 0; k < r.count; ++k) {
                if (r.fds[k] >= 0) {
                    close(r.fds[k]);
                }
            }
        }
    }
}
