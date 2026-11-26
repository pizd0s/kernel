#include <bin/inet/sockets.h>
#include <bin/inet/stack.h>

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <netinet/in.h>
#include <poll.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/uio.h>
#include <time.h>
#include <unistd.h>

#include "ff_api.h"

namespace {
    enum class Outcome {
        Done,
        Read,  // waits for the socket to read
        Write, // waits for it to write
    };

    // A call that waits for its socket: its endpoint, kept; the call; a
    // READ's or WRITE's memfd; and when it has waited its socket's
    // timeout, -1 for none.
    struct Waiting {
        Endpoint* at;
        Header h;
        unsigned char* body;
        size_t size;
        int memfd;
        long long due;
        int tag; // what at is of
    };

    // Of a turn's readiness, before it is told: a side's end, EV_EOF's.
    constexpr uint32_t readEnded = 1u << 30;
    constexpr uint32_t writeEnded = 1u << 31;

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

    // An answer, plain or attached: OK opened, then the call's own, with
    // the other end of a new socket's pair, an ACCEPT's connection's or a
    // listener's of sockmgr's. A call that fails attaches nothing, and the
    // end closes.
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
        const Header h{opOk, 1, 0, 0, 0, 0}; // one open file, what
        const Opened what{openedFile, O_RDWR, 0, 0, POLLOUT};
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

    // An option of a socket's. SO_REUSEADDR of a datagram socket lets
    // another bind its address too, as a datagram server's programs have
    // it of the option (tftpd's socket of a transfer, on the address of
    // inetd's or udpsvd's socket): FreeBSD's is that of SO_REUSEPORT.
    int setOption(int ff, int type, uint32_t level, uint32_t name, const void* value, socklen_t length) {
        if (ff_setsockopt(ff, int(level), int(name), value, length) != 0) {
            return errno;
        }
        if (type == SOCK_DGRAM && level == SOL_SOCKET && name == SO_REUSEADDR) {
            ff_setsockopt(ff, SOL_SOCKET, SO_REUSEPORT, value, length);
        }
        return 0;
    }

    // A timeout, SO_RCVTIMEO's or SO_SNDTIMEO's, of FreeBSD's timeval, its
    // tv_usec a long of 32 bits and 4 of padding: lib/c's is of 64.
    timeval timeoutOf(const unsigned char* value) {
        int64_t seconds;
        int32_t micro;
        memcpy(&seconds, value, sizeof seconds);
        memcpy(&micro, value + 8, sizeof micro);
        return {seconds, micro};
    }

    long long nowUs() {
        timespec ts;
        clock_gettime(CLOCK_MONOTONIC, &ts);
        return (long long)ts.tv_sec * 1000000 + ts.tv_nsec / 1000;
    }

    // When a call that waits from now has waited its socket's timeout:
    // SO_RCVTIMEO's of a receive, SO_SNDTIMEO's of a send, -1 of a timeout
    // of 0 and of a call that has none, a connect or an accept.
    long long dueOf(const Socket& s, uint32_t op) {
        const bool receives = op == opRecv || op == opRead;
        if (!receives && op != opSend && op != opWrite) {
            return -1;
        }
        alignas(8) unsigned char value[16];
        socklen_t length = sizeof value;
        if (ff_getsockopt(s.ff, SOL_SOCKET, receives ? SO_RCVTIMEO : SO_SNDTIMEO, value, &length) != 0 || length < 12) {
            return -1;
        }
        const timeval tv = timeoutOf(value);
        return tv.tv_sec == 0 && tv.tv_usec == 0 ? -1 : nowUs() + (long long)tv.tv_sec * 1000000 + tv.tv_usec;
    }

    // Whether an address is a connected socket's peer's: its family, port
    // and address, those of a sockaddr_in.
    bool isPeer(int ff, const linux_sockaddr* addr, socklen_t length) {
        alignas(8) unsigned char peer[128];
        socklen_t peerLength = sizeof peer;
        return addr && length >= 8 && ff_getpeername(ff, reinterpret_cast<linux_sockaddr*>(peer), &peerLength) == 0 &&
               peerLength >= 8 && memcmp(peer, addr, 8) == 0;
    }

    // A socket of F-Stack's descriptor whose open file is channel, an end
    // of a pair whose other end is the kernel's.
    Socket* adopted(int ff, int type, bool nonblock, int channel) {
        auto* s = static_cast<Socket*>(calloc(1, sizeof(Socket)));
        if (!s) {
            fatal("inet: out of memory");
        }
        s->ff = ff;
        s->type = type;
        s->end = watch(epoll, {channel, false, true, true, s});
        s->end->nonblock = nonblock;
        s->next = sockets;
        sockets = s;
        watched(*s);
        return s;
    }

    // A socket of F-Stack's descriptor, its open file the first end of a
    // new pair, the other end the kernel's, into end.
    Socket* made(int ff, int type, bool nonblock, int& end) {
        int pair[2];
        if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
            return nullptr;
        }
        end = pair[1];
        return adopted(ff, type, nonblock, pair[0]);
    }

    // A read that took what the socket had: POLLIN is told no more, before
    // the read is answered, else a poll after the read finds the socket
    // ready still, and a read after the poll waits for what may never
    // come. What is left, or the end, a peek finds.
    void drained(Socket& s) {
        if ((s.told & POLLIN) == 0) {
            return;
        }
        unsigned char next;
        if (ff_recvfrom(s.ff, &next, 1, MSG_PEEK, nullptr, nullptr) < 0 && errno == EAGAIN) {
            s.ready &= ~uint32_t(POLLIN);
            s.told &= ~uint32_t(POLLIN);
            tell(s.end->fd, s.told);
        }
    }

    void release(Socket& s, int side) {
        Waiting* w = s.waiting[side];
        if (!w) {
            return;
        }
        if (w->memfd >= 0) {
            close(w->memfd);
        }
        if (w->at) {
            unkeep(epoll, w->at);
        }
        free(w->body);
        free(w);
        s.waiting[side] = nullptr;
    }

    // A call of a socket's, the socket never waiting: answered, or to wait.
    Outcome run(Socket& s, const Header& h, const unsigned char* body, size_t size, int memfd, int fd, bool nonblock) {
        const unsigned char* at = body;
        const unsigned char* stop = body + size;
        const linux_sockaddr* addr;
        socklen_t length;
        auto done = [&](int err, const void* data = nullptr, size_t n = 0) {
            reply(fd, false, -1, err, data, n);
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
                ssize_t n = ff_sendto(s.ff, at, size_t(stop - at), flags, addr, length);
                // A datagram to a connected socket's peer, by the peer's
                // address, goes: FreeBSD's sendto refuses the address,
                // EISCONN, POSIX's "may", which a datagram server's programs
                // do not expect of it (tftpd's).
                if (n < 0 && errno == EISCONN && s.type == SOCK_DGRAM && isPeer(s.ff, addr, length)) {
                    n = ff_sendto(s.ff, at, size_t(stop - at), flags, nullptr, 0);
                }
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
                // A receive of no bytes on a datagram socket waits for a
                // datagram, its sender's address the answer, as POSIX has
                // it: FreeBSD's returns at once while none has come.
                if (s.type == SOCK_DGRAM && n == 0 && fromLength == 0) {
                    return again(Outcome::Read, EAGAIN);
                }
                if (s.type == SOCK_STREAM) {
                    fromLength = 0;
                }
                if ((flags & MSG_PEEK) == 0) {
                    drained(s);
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
                drained(s);
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
                // SO_TYPE, which F-Stack's names of Linux's have not, is
                // the socket's type.
                if (level == SOL_SOCKET && name == SO_TYPE) {
                    const int type = s.type;
                    return done(0, &type, sizeof type);
                }
                alignas(8) unsigned char value[256];
                socklen_t valueLength = sizeof value;
                if (ff_getsockopt(s.ff, int(level), int(name), value, &valueLength) != 0) {
                    return done(errno);
                }
                if (level == SOL_SOCKET && (name == SO_RCVTIMEO || name == SO_SNDTIMEO) && valueLength >= 12) {
                    const timeval tv = timeoutOf(value);
                    return done(0, &tv, sizeof tv);
                }
                return done(0, value, valueLength);
            }
            case opSetsockopt: {
                uint32_t level, name;
                if (!take(at, stop, level) || !take(at, stop, name)) {
                    return done(EINVAL);
                }
                return done(setOption(s.ff, s.type, level, name, at, socklen_t(stop - at)));
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
    void wait(Socket& s, Outcome outcome, Request& r) {
        const int side = outcome == Outcome::Read ? 0 : 1;
        if (s.waiting[side]) {
            return fail(r.reply(), EBUSY);
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
        w->due = dueOf(s, w->h.op);
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
        setOption(ff, int(type), level, name, at, socklen_t(length));
        at += length;
    }
    Header h = r.h;
    h.op = r.h.arg;
    h.arg = arg;
    const bool listens = h.op == opListen;
    if (!listens && ((h.op != opBind && h.op != opConnect && h.op != opSend) || r.count < 2)) {
        ff_close(ff);
        return fail(r.reply(), EINVAL);
    }
    // A listener is sockmgr's, its open file a new pair's end, answered
    // attached. Another socket comes with its call: sockmgr's end of its
    // channel, the descriptor before where the call is answered, is the
    // socket's open file from now on, whatever the call's answer, and
    // the call the socket's own.
    int end = -1;
    Socket* s = listens ? made(ff, int(type), nonblock != 0, end) : adopted(ff, int(type), nonblock != 0, r.fds[r.count - 2]);
    if (!s) {
        ff_close(ff);
        return fail(r.reply(), ENOMEM);
    }
    if (!listens) {
        r.fds[r.count - 2] = -1;
    }
    int e = bound && ff_bind(ff, bound, boundLength) != 0 ? errno : 0;
    if (e == 0 && listens && ff_listen(ff, int(arg)) != 0) {
        e = errno;
    }
    if (e != 0) {
        if (listens) {
            close(end);
            forget(s);
        }
        return fail(r.reply(), e);
    }
    if (listens) {
        return reply(r.reply(), true, end, 0, nullptr, 0);
    }
    const size_t offset = size_t(at - r.body);
    const Outcome outcome = run(*s, h, at, size_t(stop - at), -1, r.reply(), nonblock != 0);
    if (outcome == Outcome::Done) {
        return;
    }
    // A call that waits keeps its own body, the socket's call alone.
    Request call = r;
    call.h = h;
    call.body = r.body + offset;
    call.size = size_t(stop - at);
    wait(*s, outcome, call);
    r.fds[r.count - 1] = call.fds[call.count - 1];
}

bool socketsHandle(Endpoint& e, Request& r) {
    stackClock();
    int side;
    if (Socket* s = keeperOf(e.of, side)) {
        if (r.h.op == opCancel) {
            // A CONNECT's connection goes on, as an interrupted connect's does.
            fail(s->waiting[side]->at->fd, EINTR);
            release(*s, side);
        }
        return true;
    }
    Socket* s = socketOf(e.of);
    if (!s) {
        return false;
    }
    const Outcome outcome = run(*s, r.h, r.body, r.size, r.memfd(), r.reply(), e.nonblock);
    if (outcome != Outcome::Done) {
        wait(*s, outcome, r);
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

int socketsTurn() {
    if (kq < 0) {
        return -1;
    }
    for (Socket* s = sockets; s; s = s->next) {
        s->ready = 0;
    }
    struct kevent events[64];
    const timespec zero{0, 0};
    const int n = ff_kevent(kq, nullptr, 0, events, 64, &zero);
    // The end of a side, EV_EOF, is readiness: a read gives 0, a write
    // EPIPE. The hangup, POLLHUP, is both sides' end, as FreeBSD's poll has
    // it: a socket shut for writing only, shutdown's SHUT_WR, still reads.
    for (int i = 0; i < n; ++i) {
        auto* s = static_cast<Socket*>(events[i].udata);
        if (!socketOf(s)) {
            continue;
        }
        const bool read = events[i].filter == EVFILT_READ;
        s->ready |= read ? POLLIN : POLLOUT;
        if ((events[i].flags & EV_EOF) != 0) {
            s->ready |= read ? readEnded : writeEnded;
        }
    }
    for (Socket* s = sockets; s; s = s->next) {
        if ((s->ready & (readEnded | writeEnded)) == (readEnded | writeEnded)) {
            s->ready |= POLLHUP;
        }
        s->ready &= ~(readEnded | writeEnded);
    }
    const long long now = nowUs();
    long long due = -1;
    for (Socket* s = sockets, *next; s; s = next) {
        next = s->next;
        for (int side = 0; side < 2; ++side) {
            Waiting* w = s->waiting[side];
            if (!w) {
                continue;
            }
            const bool ready = (s->ready & (side == 0 ? POLLIN | POLLHUP : POLLOUT | POLLHUP)) != 0;
            if (ready && run(*s, w->h, w->body, w->size, w->memfd, w->at->fd, false) == Outcome::Done) {
                release(*s, side);
            } else if (w->due >= 0 && w->due <= now) {
                fail(w->at->fd, EAGAIN);
                release(*s, side);
            } else if (w->due >= 0 && (due < 0 || w->due < due)) {
                due = w->due;
            }
        }
        if (s->ready != s->told) {
            s->told = s->ready;
            tell(s->end->fd, s->ready);
        }
    }
    if (due < 0) {
        return -1;
    }
    const long long left = (due - now + 999) / 1000;
    return left < INT_MAX ? int(left) : INT_MAX;
}
