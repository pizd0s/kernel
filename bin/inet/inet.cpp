// inet: a card of the system's network, the network device vm gives the
// system (vm run --net), and the TCP/IP stack on it, FreeBSD's as F-Stack
// runs it, in one thread: a process a card, its drivers and protocols in
// one program. devfs starts it for each card vm gave, and it is the
// card's in /dev, files of a directory of its name, which it registers
// at /dev/init:
//
//     net/<name>/ctl     commands, a line each: "stack" starts the stack,
//                        its address by DHCP; read, the card's state
//     net/<name>/stats   its frames and bytes in and out, dropped
//     net/<name>/addr    its hardware address
//
// and, once the stack has its lease, the stack's:
//
//     net/<name>/iface/status   its address, router, name server, lease
//
// and the stack joins sockmgr (bin/tools/sockmgr), when there is one: of
// sockmgr/stack, a STACK of its state, and the HANDs of sockets sockmgr
// gives it (sockets.cpp). The stack keeps its time by ticks of hz a
// second, run as they are due: none while nothing is.
//
//     inet [-r fd] <name> <fd>
//     inet probe <address> <port> <text>
//
// -r: a descriptor to close once the card's files are there. probe, of
// the card a process vm runs with --net has itself, after the disks:
// DHCP, the stack, a TCP connection to the address, the text sent, and
// what comes back printed until the other side closes.

#include <bin/inet/dhcp.h>
#include <bin/inet/host.h>
#include <bin/inet/sockets.h>
#include <bin/inet/stack.h>
#include <lib/protocol/service.h>

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <netinet/in.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

#include "ff_api.h"

namespace {
    enum class Stack {
        Off,
        Dhcp,
        Up,
    };

    enum class File {
        Ctl,
        Stats,
        Addr,
        Status,
    };

    // A file of the card's in /dev: what it is and its device.
    struct Entry {
        File file;
        Device device;
    };

    int epoll = -1;
    int card = -1;
    char name[NAME_MAX + 1];
    uint8_t mac[6];
    Stack stack = Stack::Off;
    Entry entries[4];
    size_t entryCount;
    int sockmgrTag; // what sockmgr's end of ours is of

    long long nowMs() {
        timespec ts;
        clock_gettime(CLOCK_MONOTONIC, &ts);
        return (long long)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
    }

    // A card's hardware address, locally administered, of its name.
    void macOf(const char* named) {
        uint32_t h = 2166136261u;
        for (const char* c = named; *c; ++c) {
            h = (h ^ uint8_t(*c)) * 16777619u;
        }
        const uint8_t made[6] = {0x02, 0x70, 0x7a, 0x64, uint8_t(h >> 8), uint8_t(h)};
        memcpy(mac, made, sizeof mac);
    }

    // A file's text now.
    size_t textOf(File file, char* out, size_t room) {
        char a[INET_ADDRSTRLEN], r[INET_ADDRSTRLEN], d[INET_ADDRSTRLEN];
        const Lease& l = dhcpLease();
        switch (file) {
            case File::Ctl: {
                static const char* const states[] = {"off", "dhcp", "up"};
                return size_t(snprintf(out, room, "stack %s\nmtu 1500\n", states[int(stack)]));
            }
            case File::Stats:
                return size_t(snprintf(out, room, "in %llu frames %llu bytes\nout %llu frames %llu bytes\ndropped %llu\n",
                                       (unsigned long long)inetCounts.inFrames, (unsigned long long)inetCounts.inBytes,
                                       (unsigned long long)inetCounts.outFrames, (unsigned long long)inetCounts.outBytes,
                                       (unsigned long long)inetCounts.dropped));
            case File::Addr:
                return size_t(snprintf(out, room, "%02x:%02x:%02x:%02x:%02x:%02x\n", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]));
            case File::Status:
                inet_ntop(AF_INET, &l.address, a, sizeof a);
                inet_ntop(AF_INET, &l.router, r, sizeof r);
                inet_ntop(AF_INET, &l.dns, d, sizeof d);
                return size_t(snprintf(out, room, "inet %s/%d\ngateway %s\ndns %s\nlease %u\n", a, __builtin_popcount(ntohl(l.mask)),
                                       r, d, l.seconds));
        }
        return 0;
    }

    void enrolled(const char* path, File file, uint32_t mode) {
        char full[PATH_MAX];
        snprintf(full, sizeof full, "net/%s/%s", name, path);
        Entry& e = entries[entryCount++];
        e.file = file;
        e.device = enroll("inet", full, mode);
        watch(epoll, {e.device.end, true, false, false, &e});
    }

    // A file read at the open file's position, or at off.
    void readText(const Entry& f, Endpoint& e, const Request& r) {
        Io io;
        if (r.memfd() < 0 || !r.io(io)) {
            return fail(r.reply(), EINVAL);
        }
        char text[512];
        const size_t size = textOf(f.file, text, sizeof text);
        const uint64_t off = offsetOf(e, io, false, size);
        const size_t part = off < size ? (r.h.arg < size - off ? size_t(r.h.arg) : size_t(size - off)) : 0;
        if (part > 0 && pwrite(r.memfd(), text + off, part, 0) != ssize_t(part)) {
            return fail(r.reply(), EIO);
        }
        if ((io.flags & ioAt) == 0) {
            e.pos = off + part;
        }
        const uint32_t count = uint32_t(part);
        answer(r.reply(), opOk, 0, &count, sizeof count);
    }

    // A command of ctl's, a line.
    int command(const char* line, size_t length) {
        while (length > 0 && (line[length - 1] == '\r' || line[length - 1] == ' ')) {
            --length;
        }
        if (length == 5 && memcmp(line, "stack", 5) == 0) {
            if (stack != Stack::Off) {
                return EBUSY;
            }
            stack = Stack::Dhcp;
            dhcpStart(card, mac);
            return 0;
        }
        return EINVAL;
    }

    void writeCtl(const Request& r) {
        Io io;
        char text[256];
        if (r.memfd() < 0 || !r.io(io) || r.h.arg > sizeof text) {
            return fail(r.reply(), EINVAL);
        }
        if (pread(r.memfd(), text, r.h.arg, 0) != ssize_t(r.h.arg)) {
            return fail(r.reply(), EIO);
        }
        for (size_t at = 0; at < r.h.arg;) {
            const char* end = static_cast<const char*>(memchr(text + at, '\n', r.h.arg - at));
            const size_t length = end ? size_t(end - (text + at)) : r.h.arg - at;
            if (length > 0) {
                if (const int e = command(text + at, length)) {
                    return fail(r.reply(), e);
                }
            }
            at += length + 1;
        }
        answer(r.reply(), opOk, 0, &r.h.arg, sizeof r.h.arg);
    }

    // The stack's state, as STACK tells sockmgr and iface/status has it.
    size_t stateOf(char* out, size_t room) {
        char a[INET_ADDRSTRLEN], r[INET_ADDRSTRLEN], d[INET_ADDRSTRLEN];
        const Lease& l = dhcpLease();
        inet_ntop(AF_INET, &l.address, a, sizeof a);
        inet_ntop(AF_INET, &l.router, r, sizeof r);
        inet_ntop(AF_INET, &l.dns, d, sizeof d);
        return size_t(snprintf(out, room, "card %s\ninet %s/%d\ngateway %s\ndns %s\n", name, a, __builtin_popcount(ntohl(l.mask)), r, d));
    }

    // Joins sockmgr, when there is one, telling it the stack's state.
    void join() {
        const int fd = open("/dev/sockmgr/stack", O_RDWR | O_CLOEXEC);
        if (fd < 0) {
            return;
        }
        char state[256];
        const size_t size = stateOf(state, sizeof state);
        const Header h{opStack, 0, 0, 0, 0, 0};
        const iovec parts[] = {{const_cast<Header*>(&h), sizeof h}, {state, size}};
        sendPacket(fd, parts, 2, nullptr, 0);
        watch(epoll, {fd, false, true, true, &sockmgrTag});
    }

    void handle(Endpoint& e, Request& r) {
        if (e.of == &sockmgrTag) {
            return r.h.op == opHand ? socketsHand(r) : fail(r.reply(), EINVAL);
        }
        if (socketsHandle(e, r)) {
            return;
        }
        Entry& f = *static_cast<Entry*>(e.of);
        if (e.channel) {
            answerOpen(epoll, r, {openedFile, 0, f.device.st.dev, f.device.st.ino, 0, POLLIN | POLLOUT}, e.of);
            return;
        }
        switch (r.h.op) {
            case opRead:
                return readText(f, e, r);
            case opWrite:
                return f.file == File::Ctl ? writeCtl(r) : fail(r.reply(), EBADF);
            case opFstat:
                return answer(r.reply(), opOk, 0, &f.device.st, sizeof f.device.st);
            case opSeek:
                return answerSeek(e, r, 0);
            default:
                return fail(r.reply(), unserved(r.h.op));
        }
    }

    void gone(Endpoint& e) {
        if (e.of == &sockmgrTag || socketsGone(e)) {
            return;
        }
        if (e.channel) {
            stopServing();
        }
    }

    // The card's frames: the stack's once it is up, DHCP's until then,
    // none of anyone's before it.
    void frames(Endpoint& e) {
        if (stack == Stack::Up) {
            stackFrames();
            return socketsTurn();
        }
        unsigned char frame[2048];
        for (ssize_t n; (n = recv(e.fd, frame, sizeof frame, MSG_DONTWAIT)) != 0;) {
            if (n < 0) {
                return;
            }
            ++inetCounts.inFrames;
            inetCounts.inBytes += uint64_t(n);
            if (stack != Stack::Dhcp) {
                ++inetCounts.dropped;
            } else if (dhcpFrame(frame, size_t(n))) {
                stackStart(card, name, mac, dhcpLease());
                stack = Stack::Up;
                if (epoll >= 0) {
                    socketsStart(epoll);
                    enrolled("iface/status", File::Status, S_IFCHR | 0444);
                    join();
                }
                return;
            }
        }
        fprintf(stderr, "inet: %s: the card is gone\n", name);
        exit(1);
    }

    int expire() {
        switch (stack) {
            case Stack::Dhcp:
                return dhcpTimer();
            case Stack::Up: {
                const int due = stackTicks();
                socketsTurn();
                return due;
            }
            default:
                return -1;
        }
    }

    int serveCard(int argc, char** argv) {
        int at;
        const int ready = readyDescriptor(argc, argv, at);
        if (argc - at != 2) {
            fprintf(stderr, "usage: inet [-r fd] <name> <fd>\n       inet probe <address> <port> <text>\n");
            return 2;
        }
        snprintf(name, sizeof name, "%s", argv[at]);
        card = atoi(argv[at + 1]);
        fcntl(card, F_SETFD, FD_CLOEXEC);
        macOf(name);
        epoll = epoll_create1(EPOLL_CLOEXEC);
        if (epoll < 0) {
            fatal("inet: epoll_create1: errno %d", errno);
        }
        enrolled("ctl", File::Ctl, S_IFCHR | 0644);
        enrolled("stats", File::Stats, S_IFCHR | 0444);
        enrolled("addr", File::Addr, S_IFCHR | 0444);
        Endpoint raw{card, false, true, false, nullptr};
        raw.raw = true;
        watch(epoll, raw);
        if (ready >= 0) {
            close(ready);
        }
        serve(epoll, handle, gone, nullptr, expire, frames);
        return 0;
    }

    // The card of a process vm ran with --net: after the disks DISKS
    // names, which NETS names.
    int device() {
        if (!getenv("NETS")) {
            return -1;
        }
        int fd = 3;
        if (const char* disks = getenv("DISKS")) {
            for (const char* at = disks; *at;) {
                while (*at == ' ') {
                    ++at;
                }
                if (*at) {
                    ++fd;
                }
                while (*at && *at != ' ') {
                    ++at;
                }
            }
        }
        return fd;
    }

    // One turn of a probe's: the frames there are, or a wait for them up
    // to what is due.
    void turn(int waitMs) {
        const int due = expire();
        pollfd p{card, POLLIN, 0};
        if (poll(&p, 1, due >= 0 && due < waitMs ? due : waitMs) > 0) {
            Endpoint e{card, false, true, false, nullptr};
            frames(e);
        }
        expire();
    }

    int probe(const char* host, int toPort, const char* text) {
        const long long deadline = nowMs() + 10000;
        stack = Stack::Dhcp;
        dhcpStart(card, mac);
        while (stack != Stack::Up && nowMs() < deadline) {
            turn(10);
        }
        if (stack != Stack::Up) {
            printf("dhcp: no lease\n");
            return 1;
        }
        const int s = ff_socket(AF_INET, SOCK_STREAM, 0);
        if (s < 0) {
            printf("socket: %s\n", strerror(errno));
            return 1;
        }
        int on = 1;
        if (ff_ioctl(s, FIONBIO, &on) < 0) {
            printf("ioctl FIONBIO: %s\n", strerror(errno));
            return 1;
        }
        sockaddr_in to{};
        to.sin_family = AF_INET;
        to.sin_port = htons(uint16_t(toPort));
        inet_pton(AF_INET, host, &to.sin_addr);
        if (ff_connect(s, reinterpret_cast<linux_sockaddr*>(&to), sizeof to) < 0 && errno != EINPROGRESS) {
            printf("connect: %s\n", strerror(errno));
            return 1;
        }
        size_t sent = 0;
        const size_t length = strlen(text);
        bool told = false;
        while (nowMs() < deadline) {
            turn(10);
            if (sent < length) {
                const ssize_t n = ff_write(s, text + sent, length - sent);
                if (n < 0 && errno != EAGAIN && errno != ENOTCONN) {
                    printf("write: %s\n", strerror(errno));
                    return 1;
                }
                if (n > 0 && !told) {
                    printf("connected to %s:%d\n", host, toPort);
                    told = true;
                }
                sent += n > 0 ? size_t(n) : 0;
                continue;
            }
            char got[512];
            const ssize_t n = ff_read(s, got, sizeof got);
            if (n == 0) {
                printf("closed\n");
                ff_close(s);
                return 0;
            }
            if (n < 0 && errno != EAGAIN) {
                printf("read: %s\n", strerror(errno));
                return 1;
            }
            if (n > 0) {
                printf("received: %.*s\n", int(n), got);
            }
        }
        printf("timed out\n");
        return 1;
    }
}

int main(int argc, char** argv) {
    if (argc == 5 && strcmp(argv[1], "probe") == 0) {
        card = device();
        if (card < 0) {
            fprintf(stderr, "inet: no network device (vm run --net)\n");
            return 1;
        }
        snprintf(name, sizeof name, "%s", getenv("NETS"));
        macOf(name);
        return probe(argv[2], atoi(argv[3]), argv[4]);
    }
    return serveCard(argc, argv);
}
