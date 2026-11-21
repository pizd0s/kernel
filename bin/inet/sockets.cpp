#include <bin/inet/sockets.h>
#include <bin/inet/stack.h>

#include <errno.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <poll.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/uio.h>
#include <unistd.h>

#include "ff_api.h"

namespace {
    enum class Outcome {
        Done,
        Read,  // waits for the socket to read
        Write, // waits for it to write
    };

    // A call that waits for its socket: its endpoint, kept; the call; a
    // READ's or WRITE's memfd; and of a HAND's, the kernel's end of the
    // socket's pair, which goes with the answer, attached.
    struct Waiting {
        Endpoint* at;
        Header h;
        unsigned char* body;
        size_t size;
        int memfd;
        bool attach;
        int end;
        int tag; // what at is of
    };

    struct Socket {
        int ff;
        int type;
        Endpoint* end;
        Waiting* waiting[2]; // reading, writing
        uint32_t ready;      // as the kqueue found it this turn
        uint32_t told;       // as the kernel was told
        Socket* next;
    };

    int epoll = -1;
    int kq = -1;
    Socket* sockets;

    template <typename T>
    bool take(const unsigned char*& at, const unsigned char* end, T& out) {
        if (size_t(end - at) < sizeof out) {
            return false;
        }
        memcpy(&out, at, sizeof out);
        at += sizeof out;
        return true;
    }

    // A sockaddr of a call's body: its length, then it.
    bool address(const unsigned char*& at, const unsigned char* end, const linux_sockaddr*& addr, socklen_t& length) {
        uint32_t n;
        if (!take(at, end, n) || size_t(end - at) < n) {
            return false;
        }
        addr = n ? reinterpret_cast<const linux_sockaddr*>(at) : nullptr;
        length = socklen_t(n);
        at += n;
        return true;
    }

    // An answer, plain or attached: OK opened, then the call's own, the
    // kernel's end of the socket's pair with it. A call that fails does
    // not attach the socket, whose end closes, but CONNECT, whose errno
    // is its answer's own.
    void reply(int fd, bool attach, int end, int err, const void* data, size_t size) {
        if (err) {
            if (attach) {
                close(end);
            }
            return fail(fd, err);
        }
        if (!attach) {
            return answer(fd, opOk, 0, data, size);
        }
        const Header h{opOk, 0, 0, 0, 0, 0};
        const Opened what{openedFile, O_RDWR, 0, 0, 0, POLLOUT};
        const iovec parts[] = {{const_cast<Header*>(&h), sizeof h}, {const_cast<Opened*>(&what), sizeof what},
                               {const_cast<void*>(data), size}};
        sendPacket(fd, parts, 3, &end, 1);
        close(end);
    }

    void watched(Socket& s) {
        struct kevent change[2];
        EV_SET(&change[0], s.ff, EVFILT_READ, EV_ADD, 0, 0, &s);
        EV_SET(&change[1], s.ff, EVFILT_WRITE, EV_ADD, 0, 0, &s);
        ff_kevent(kq, change, 2, nullptr, 0, nullptr);
    }

    // A socket of F-Stack's descriptor, its open file the first end of a
    // new pair, the other end the kernel's, into end.
    Socket* made(int ff, int type, bool nonblock, int& end) {
        int pair[2];
        if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
            return nullptr;
        }
        auto* s = static_cast<Socket*>(calloc(1, sizeof(Socket)));
        if (!s) {
            fatal("inet: out of memory");
        }
        s->ff = ff;
        s->type = type;
        s->end = watch(epoll, {pair[0], false, true, true, s});
        s->end->nonblock = nonblock;
        s->next = sockets;
        sockets = s;
        watched(*s);
        end = pair[1];
        return s;
    }

    void release(Socket& s, int side) {
        Waiting* w = s.waiting[side];
        if (!w) {
            return;
        }
        if (w->memfd >= 0) {
            close(w->memfd);
        }
        if (w->attach && w->end >= 0) {
            close(w->end);
        }
        if (w->at) {
            unkeep(epoll, w->at);
        }
        free(w->body);
        free(w);
        s.waiting[side] = nullptr;
    }

    // A call of a socket's, the socket never waiting: answered, or to wait.
    Outcome run(Socket& s, const Header& h, const unsigned char* body, size_t size, int memfd, int fd, bool nonblock, bool attach,
                int end) {
        const unsigned char* at = body;
        const unsigned char* stop = body + size;
        const linux_sockaddr* addr;
        socklen_t length;
        auto done = [&](int err, const void* data = nullptr, size_t n = 0) {
            reply(fd, attach, end, err, data, n);
            return Outcome::Done;
        };
        auto again = [&](Outcome wait, int err) {
            return err == EAGAIN && !nonblock ? wait : done(err);
        };
        switch (h.op) {
            case opBind:
                if (!address(at, stop, addr, length)) {
                    return done(EINVAL);
                }
                return done(ff_bind(s.ff, addr, length) == 0 ? 0 : errno);
            case opConnect: {
                if (!address(at, stop, addr, length)) {
                    return done(EINVAL);
                }
                int err = ff_connect(s.ff, addr, length) == 0 ? 0 : errno;
                if (err == EISCONN) {
                    err = 0;
                }
                if ((err == EINPROGRESS || err == EALREADY) && !nonblock) {
                    return Outcome::Write;
                }
                if (attach) {
                    const uint32_t result = uint32_t(err == EALREADY ? EINPROGRESS : err);
                    return done(0, &result, sizeof result);
                }
                return done(err);
            }
            case opListen:
                return done(ff_listen(s.ff, int(h.arg)) == 0 ? 0 : errno);
            case opAccept: {
                alignas(8) unsigned char peer[128];
                socklen_t peerLength = sizeof peer;
                const int conn = ff_accept(s.ff, reinterpret_cast<linux_sockaddr*>(peer), &peerLength);
                if (conn < 0) {
                    return again(Outcome::Read, errno);
                }
                int on = 1;
                ff_ioctl(conn, FIONBIO, &on);
                int connEnd;
                if (!made(conn, s.type, (h.arg & SOCK_NONBLOCK) != 0, connEnd)) {
                    ff_close(conn);
                    return done(ENOMEM);
                }
                reply(fd, true, connEnd, 0, peer, peerLength);
                return Outcome::Done;
            }
            case opSend: {
                if (!address(at, stop, addr, length)) {
                    return done(EINVAL);
                }
                const int flags = int(h.arg) & ~(MSG_DONTWAIT | MSG_NOSIGNAL);
                const ssize_t n = ff_sendto(s.ff, at, size_t(stop - at), flags, addr, length);
                if (n < 0) {
                    return again(Outcome::Write, errno);
                }
                const uint32_t count = uint32_t(n);
                return done(0, &count, sizeof count);
            }
            case opRecv: {
                uint32_t want;
                if (!take(at, stop, want)) {
                    return done(EINVAL);
                }
                static unsigned char out[65536];
                constexpr size_t head = 8 + 128;
                alignas(8) unsigned char from[128];
                socklen_t fromLength = sizeof from;
                const size_t room = want < sizeof out - head - sizeof(Header) ? want : sizeof out - head - sizeof(Header);
                const int flags = int(h.arg) & ~(MSG_DONTWAIT | MSG_NOSIGNAL);
                const ssize_t n = ff_recvfrom(s.ff, out + head, room, flags, reinterpret_cast<linux_sockaddr*>(from), &fromLength);
                if (n < 0) {
                    return again(Outcome::Read, errno);
                }
                if (s.type == SOCK_STREAM) {
                    fromLength = 0;
                }
                const uint32_t outFlags = 0, addrLength = fromLength;
                unsigned char* p = out + head - 8 - fromLength;
                memcpy(p, &outFlags, 4);
                memcpy(p + 4, &addrLength, 4);
                memcpy(p + 8, from, fromLength);
                return done(0, p, 8 + fromLength + size_t(n));
            }
            case opRead: {
                Io io;
                if (memfd < 0 || size != sizeof io || (memcpy(&io, body, sizeof io), (io.flags & ioAt) != 0)) {
                    return done(memfd < 0 ? EINVAL : ESPIPE);
                }
                static unsigned char buffer[65536];
                const ssize_t n = ff_read(s.ff, buffer, h.arg < sizeof buffer ? h.arg : sizeof buffer);
                if (n < 0) {
                    return again(Outcome::Read, errno);
                }
                if (n > 0 && pwrite(memfd, buffer, size_t(n), 0) != n) {
                    return done(EIO);
                }
                const uint32_t count = uint32_t(n);
                return done(0, &count, sizeof count);
            }
            case opWrite: {
                Io io;
                if (memfd < 0 || size != sizeof io || (memcpy(&io, body, sizeof io), (io.flags & ioAt) != 0)) {
                    return done(memfd < 0 ? EINVAL : ESPIPE);
                }
                static unsigned char buffer[65536];
                const size_t want = h.arg < sizeof buffer ? h.arg : sizeof buffer;
                if (pread(memfd, buffer, want, 0) != ssize_t(want)) {
                    return done(EIO);
                }
                const ssize_t n = ff_write(s.ff, buffer, want);
                if (n < 0) {
                    return again(Outcome::Write, errno);
                }
                const uint32_t count = uint32_t(n);
                return done(0, &count, sizeof count);
            }
            case opSockname: {
                alignas(8) unsigned char name[128];
                socklen_t nameLength = sizeof name;
                const int r = h.arg ? ff_getpeername(s.ff, reinterpret_cast<linux_sockaddr*>(name), &nameLength)
                                    : ff_getsockname(s.ff, reinterpret_cast<linux_sockaddr*>(name), &nameLength);
                return r == 0 ? done(0, name, nameLength) : done(errno);
            }
            case opGetsockopt: {
                uint32_t level, name;
                if (!take(at, stop, level) || !take(at, stop, name)) {
                    return done(EINVAL);
                }
                alignas(8) unsigned char value[256];
                socklen_t valueLength = sizeof value;
                return ff_getsockopt(s.ff, int(level), int(name), value, &valueLength) == 0 ? done(0, value, valueLength) : done(errno);
            }
            case opSetsockopt: {
                uint32_t level, name;
                if (!take(at, stop, level) || !take(at, stop, name)) {
                    return done(EINVAL);
                }
                return done(ff_setsockopt(s.ff, int(level), int(name), at, socklen_t(stop - at)) == 0 ? 0 : errno);
            }
            case opShutdown:
                return done(ff_shutdown(s.ff, int(h.arg)) == 0 ? 0 : errno);
            case opSocktype: {
                const uint32_t type = uint32_t(s.type);
                return done(0, &type, sizeof type);
            }
            case opFstat: {
                Stat st{};
                st.mode = S_IFSOCK | 0777;
                st.nlink = 1;
                return done(0, &st, sizeof st);
            }
            default:
                return done(unserved(h.op));
        }
    }

    // A call that waits, kept: where it is answered becomes the loop's.
    void wait(Socket& s, Outcome outcome, Request& r, bool attach, int end) {
        const int side = outcome == Outcome::Read ? 0 : 1;
        if (s.waiting[side]) {
            return reply(r.reply(), attach, end, EBUSY, nullptr, 0);
        }
        auto* w = static_cast<Waiting*>(calloc(1, sizeof(Waiting)));
        if (!w) {
            fatal("inet: out of memory");
        }
        w->h = r.h;
        w->body = static_cast<unsigned char*>(malloc(r.size ? r.size : 1));
        memcpy(w->body, r.body, r.size);
        w->size = r.size;
        w->memfd = r.memfd();
        if (w->memfd >= 0) {
            r.fds[0] = -1; // the waiting call's
        }
        w->attach = attach;
        w->end = end;
        w->at = keep(epoll, r, &w->tag);
        s.waiting[side] = w;
    }

    Socket* socketOf(const void* of) {
        for (Socket* s = sockets; s; s = s->next) {
            if (s == of) {
                return s;
            }
        }
        return nullptr;
    }

    Socket* keeperOf(const void* of, int& side) {
        for (Socket* s = sockets; s; s = s->next) {
            for (side = 0; side < 2; ++side) {
                if (s->waiting[side] && &s->waiting[side]->tag == of) {
                    return s;
                }
            }
        }
        return nullptr;
    }

    void forget(Socket* s) {
        release(*s, 0);
        release(*s, 1);
        ff_close(s->ff);
        for (Socket** at = &sockets; *at; at = &(*at)->next) {
            if (*at == s) {
                *at = s->next;
                break;
            }
        }
        free(s);
    }
}

void socketsStart(int loop) {
    epoll = loop;
    kq = ff_kqueue();
    if (kq < 0) {
        fatal("inet: ff_kqueue: errno %d", errno);
    }
}

void socketsHand(Request& r) {
    stackClock();
    const unsigned char* at = r.body;
    const unsigned char* stop = r.body + r.size;
    uint32_t domain, type, protocol, nonblock, arg, options;
    const linux_sockaddr* bound;
    socklen_t boundLength;
    if (!take(at, stop, domain) || !take(at, stop, type) || !take(at, stop, protocol) || !take(at, stop, nonblock) ||
        !take(at, stop, arg) || !address(at, stop, bound, boundLength) || !take(at, stop, options)) {
        return fail(r.reply(), EINVAL);
    }
    const int ff = ff_socket(int(domain), int(type), int(protocol));
    if (ff < 0) {
        return fail(r.reply(), errno);
    }
    int on = 1;
    ff_ioctl(ff, FIONBIO, &on);
    for (uint32_t i = 0; i < options; ++i) {
        uint32_t level, name, length;
        if (!take(at, stop, level) || !take(at, stop, name) || !take(at, stop, length) || size_t(stop - at) < length) {
            ff_close(ff);
            return fail(r.reply(), EINVAL);
        }
        ff_setsockopt(ff, int(level), int(name), at, socklen_t(length));
        at += length;
    }
    if (bound && ff_bind(ff, bound, boundLength) != 0) {
        const int e = errno;
        ff_close(ff);
        return fail(r.reply(), e);
    }
    int end;
    Socket* s = made(ff, int(type), nonblock != 0, end);
    if (!s) {
        ff_close(ff);
        return fail(r.reply(), ENOMEM);
    }
    // The call it brings, as the socket's own, answered attached.
    Header h = r.h;
    h.op = r.h.arg;
    h.arg = arg;
    if (h.op == opListen) {
        if (ff_listen(ff, int(arg)) != 0) {
            const int e = errno;
            close(end);
            forget(s);
            return fail(r.reply(), e);
        }
        return reply(r.reply(), true, end, 0, nullptr, 0);
    }
    if (h.op != opBind && h.op != opConnect && h.op != opSend) {
        close(end);
        forget(s);
        return fail(r.reply(), EINVAL);
    }
    const size_t offset = size_t(at - r.body);
    const Outcome outcome = run(*s, h, at, size_t(stop - at), -1, r.reply(), nonblock != 0, true, end);
    if (outcome == Outcome::Done) {
        return;
    }
    // A call that waits keeps its own body, the socket's call alone.
    Request call = r;
    call.h = h;
    call.body = r.body + offset;
    call.size = size_t(stop - at);
    wait(*s, outcome, call, true, end);
    r.fds[r.count - 1] = call.fds[call.count - 1];
}

bool socketsHandle(Endpoint& e, Request& r) {
    stackClock();
    int side;
    if (Socket* s = keeperOf(e.of, side)) {
        if (r.h.op == opCancel) {
            // A CONNECT that a HAND brought attaches the socket even so: its
            // connection goes on, as an interrupted connect's does.
            Waiting* w = s->waiting[side];
            const uint32_t interrupted = EINTR;
            if (w->attach && w->h.op == opConnect) {
                reply(w->at->fd, true, w->end, 0, &interrupted, sizeof interrupted);
            } else {
                reply(w->at->fd, w->attach, w->end, EINTR, nullptr, 0);
            }
            w->end = -1;
            release(*s, side);
        }
        return true;
    }
    Socket* s = socketOf(e.of);
    if (!s) {
        return false;
    }
    const Outcome outcome = run(*s, r.h, r.body, r.size, r.memfd(), r.reply(), e.nonblock, false, -1);
    if (outcome != Outcome::Done) {
        wait(*s, outcome, r, false, -1);
    }
    return true;
}

bool socketsGone(Endpoint& e) {
    stackClock();
    int side;
    if (Socket* s = keeperOf(e.of, side)) {
        // Who asked went: serve closes the endpoint itself.
        s->waiting[side]->at = nullptr;
        release(*s, side);
        return true;
    }
    if (Socket* s = socketOf(e.of)) {
        forget(s);
        return true;
    }
    return false;
}

void socketsTurn() {
    if (kq < 0) {
        return;
    }
    for (Socket* s = sockets; s; s = s->next) {
        s->ready = 0;
    }
    struct kevent events[64];
    const timespec zero{0, 0};
    const int n = ff_kevent(kq, nullptr, 0, events, 64, &zero);
    for (int i = 0; i < n; ++i) {
        auto* s = static_cast<Socket*>(events[i].udata);
        if (!socketOf(s)) {
            continue;
        }
        s->ready |= events[i].filter == EVFILT_READ ? POLLIN : POLLOUT;
        if ((events[i].flags & EV_EOF) != 0) {
            s->ready |= events[i].filter == EVFILT_READ ? POLLIN | POLLHUP : POLLHUP;
        }
    }
    for (Socket* s = sockets, *next; s; s = next) {
        next = s->next;
        for (int side = 0; side < 2; ++side) {
            Waiting* w = s->waiting[side];
            if (!w || (s->ready & (side == 0 ? POLLIN | POLLHUP : POLLOUT | POLLHUP)) == 0) {
                continue;
            }
            if (run(*s, w->h, w->body, w->size, w->memfd, w->at->fd, false, w->attach, w->end) == Outcome::Done) {
                w->end = -1; // gone with the answer
                release(*s, side);
            }
        }
        if (s->ready != s->told) {
            s->told = s->ready;
            tell(s->end->fd, s->ready);
        }
    }
}
