//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "a kind the kernel does not know: EIO\nready as the OPEN said: POLLOUT, then as READY said: POLLIN POLLOUT\nserved: READ of 16 at 0\nread: 'hello'\nserved: READ of 8 at 0\npassed on, read: 'world'\nserved: SEND of 'ping'\nserved: RECV of 8\nsend and recv on it: 4 'pong'\nfchdir: a path's ENOTDIR, a file's ENOTDIR, a stream socket's ENOTDIR\nserved: FSTAT\nserved: CTL ctlStat of 'name'\na pair's end: fchdir ENOTDIR; a directory over it, the working directory: fchdir 0, a name there ENOENT\n"

// An open file is the end of a pair its server made, which an OPEN's OK
// brings with what it is (lib/protocol/protocol.h): the kernel makes it
// that open file, and its calls are the file protocol's, whoever holds
// it, the process that opened it or one it passed it on to. A call is a
// request sent on the server's end, which brings where it is answered,
// the last of its descriptors: the server writes the answer there and
// closes it. The test is the
// server: a device of its own, whose opens it answers. What is no
// directory's handle, a path's, a file's or a stream socket, is no
// directory to fchdir to, nor is an end of a seqpacket pair; attach makes
// a directory over one, the root of the server behind it, as rootfs gives
// its processes theirs. An
// open file is ready as the OPEN's OK says, and then as each READY of its
// server says. A socket's call on an open file is the server's too: a
// SEND brings the bytes, a RECV asks for them, as a service of sockets
// would serve them.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/uio.h>
#include <sys/wait.h>
#include <unistd.h>

// The protocol's (lib/protocol/protocol.h): a packet's header, what an
// OPEN's OK says of the open file it brings, and the stat a REGISTER's
// and an FSTAT's OK is.
struct Header {
    uint32_t op;
    uint32_t arg;
    uint32_t uid;
    uint32_t gid;
    uint32_t pgrp;
    uint32_t sid;
};

struct Opened {
    uint32_t kind;
    uint32_t access;
    uint64_t dev;
    uint64_t ino;
    uint32_t events;
};

struct Stat {
    uint64_t ino;
    uint64_t size;
    uint64_t mtime;
    uint32_t mode;
    uint32_t nlink;
    uint32_t uid;
    uint32_t gid;
    uint64_t atime;
    uint64_t dev;
    uint64_t ctime;
};

constexpr uint32_t opOpen = 96;
constexpr uint32_t opRegister = 97;
constexpr uint32_t opOk = 128;
constexpr uint32_t openedFile = 1;

// A packet with one descriptor, or none.
static void sendWith(int to, const void* data, size_t size, int fd) {
    alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))] = {};
    iovec iov{const_cast<void*>(data), size};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    if (fd >= 0) {
        m.msg_control = control;
        m.msg_controllen = sizeof control;
        cmsghdr* c = CMSG_FIRSTHDR(&m);
        c->cmsg_level = SOL_SOCKET;
        c->cmsg_type = SCM_RIGHTS;
        c->cmsg_len = CMSG_LEN(sizeof(int));
        memcpy(CMSG_DATA(c), &fd, sizeof(int));
    }
    assert(sendmsg(to, &m, MSG_NOSIGNAL) == ssize_t(size));
}

// A packet into data, of at most size bytes: its length, and the last of
// its descriptors, -1 for none; the others are closed.
static ssize_t receiveWith(int from, void* data, size_t size, int& fd) {
    alignas(cmsghdr) char control[CMSG_SPACE(4 * sizeof(int))];
    iovec iov{data, size};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    const ssize_t n = recvmsg(from, &m, 0);
    fd = -1;
    for (cmsghdr* c = n >= 0 ? CMSG_FIRSTHDR(&m) : nullptr; c; c = CMSG_NXTHDR(&m, c)) {
        if (c->cmsg_level != SOL_SOCKET || c->cmsg_type != SCM_RIGHTS) {
            continue;
        }
        for (size_t at = 0; at + sizeof(int) <= c->cmsg_len - CMSG_LEN(0); at += sizeof(int)) {
            if (fd >= 0) {
                close(fd);
            }
            memcpy(&fd, CMSG_DATA(c) + at, sizeof(int));
        }
    }
    return n;
}

// A device of ours in /dev, registered at /dev/init/ctl: its path, and our
// end of the pair devfs hands its opens on to.
static char devicePath[64];

static int device(const char* name) {
    char registered[48];
    snprintf(registered, sizeof registered, "%s_%d", name, int(getpid()));
    snprintf(devicePath, sizeof devicePath, "/dev/%s", registered);
    const int registry = open("/dev/init/ctl", O_RDWR | O_CLOEXEC);
    int pair[2];
    assert(registry >= 0 && socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) == 0);

    struct {
        Header h;
        char name[48];
    } request{{opRegister, S_IFCHR | 0666, uint32_t(geteuid()), uint32_t(getegid()), 0, 0}, {}};

    memcpy(request.name, registered, strlen(registered));
    sendWith(registry, &request, sizeof(Header) + strlen(registered), pair[1]);

    struct {
        Header h;
        Stat st;
    } answer;

    assert(recv(registry, &answer, sizeof answer, 0) == ssize_t(sizeof answer) && answer.h.op == opOk);
    close(pair[1]);
    close(registry);
    return pair[0];
}

// An open file of the device, as its OPEN's OK says it is: a child opens
// the device and sends us what it got, and we answer its OPEN, which
// came on the device's channel with where to answer it, with the second
// end of a pair of ours and what. The open file, and in end our end of
// the pair, the open file's endpoint; or -1 and the errno of the open.
static int opened(int channel, Opened what, int& end) {
    int carrier[2];
    assert(socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, carrier) == 0);
    fflush(stdout);
    const pid_t opener = fork();
    assert(opener >= 0);
    if (opener == 0) {
        const int fd = open(devicePath, O_RDWR);
        if (fd < 0) {
            _exit(errno);
        }
        sendWith(carrier[1], "f", 1, fd);
        _exit(0);
    }
    Header h;
    unsigned char request[64];
    int reply;
    assert(receiveWith(channel, request, sizeof request, reply) >= ssize_t(sizeof h) && reply >= 0);
    memcpy(&h, request, sizeof h);
    assert(h.op == opOpen);
    int pair[2];
    assert(socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) == 0);

    struct {
        Header h;
        Opened what;
    } ok{{opOk, 1, 0, 0, 0, 0}, what};

    sendWith(reply, &ok, sizeof ok, pair[1]);
    close(pair[1]);
    close(reply);
    end = pair[0];
    int status;
    assert(waitpid(opener, &status, 0) == opener && WIFEXITED(status));
    int fd = -1;
    if (WEXITSTATUS(status) == 0) {
        char byte;
        assert(receiveWith(carrier[0], &byte, 1, fd) == 1 && fd >= 0);
    } else {
        errno = WEXITSTATUS(status);
        close(end);
        end = -1;
    }
    close(carrier[0]);
    close(carrier[1]);
    return fd;
}

struct [[gnu::packed]] Io {
    uint64_t off;
    uint32_t flags;
};

constexpr uint32_t opFstat = 67;
constexpr uint32_t opCtl = 79;
constexpr uint32_t ctlStat = 129;
constexpr uint32_t opSend = 148;
constexpr uint32_t opRecv = 149;
constexpr uint32_t opRead = 64;
constexpr uint32_t opReady = 75;
constexpr uint32_t opErr = 129;
constexpr uint32_t openedPath = 4;

static const char* errorName(int e) {
    switch (e) {
        case 0:
            return "0";
        case EIO:
            return "EIO";
        case ENOENT:
            return "ENOENT";
        case ENOTDIR:
            return "ENOTDIR";
        default:
            return "another error";
    }
}

// A packet into data with up to two descriptors: its length, the last of
// them in last and the one before it in first, -1 for none.
static ssize_t receiveTwo(int from, void* data, size_t size, int& first, int& last) {
    alignas(cmsghdr) char control[CMSG_SPACE(4 * sizeof(int))];
    iovec iov{data, size};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    const ssize_t n = recvmsg(from, &m, 0);
    first = last = -1;
    for (cmsghdr* c = n >= 0 ? CMSG_FIRSTHDR(&m) : nullptr; c; c = CMSG_NXTHDR(&m, c)) {
        if (c->cmsg_level != SOL_SOCKET || c->cmsg_type != SCM_RIGHTS) {
            continue;
        }
        for (size_t at = 0; at + sizeof(int) <= c->cmsg_len - CMSG_LEN(0); at += sizeof(int)) {
            if (first >= 0) {
                close(first);
            }
            first = last;
            memcpy(&last, CMSG_DATA(c) + at, sizeof(int));
        }
    }
    return n;
}

// Serves one READ of the open file whose endpoint end is: the request
// comes there with the reader's memory, a memfd, and where to answer.
// The bytes go into the memory, and the count is the answer.
static void serveRead(int end, const char* bytes) {
    unsigned char packet[256];
    int memfd;
    int reply;
    assert(receiveTwo(end, packet, sizeof packet, memfd, reply) == ssize_t(sizeof(Header) + sizeof(Io)) && memfd >= 0 && reply >= 0);
    Header h;
    Io io;
    memcpy(&h, packet, sizeof h);
    memcpy(&io, packet + sizeof h, sizeof io);
    printf("served: %s of %u at %llu\n", h.op == opRead ? "READ" : "another call", h.arg, (unsigned long long)io.off);
    fflush(stdout);
    const size_t length = strlen(bytes);
    assert(pwrite(memfd, bytes, length, 0) == ssize_t(length));

    struct {
        Header h;
        uint32_t count;
    } answer{{opOk, 0, 0, 0, 0, 0}, uint32_t(length)};

    assert(send(reply, &answer, sizeof answer, 0) == ssize_t(sizeof answer));
    close(memfd);
    close(reply);
}

// A child that serves one READ of the endpoint, then ends.
static pid_t server(int end, const char* bytes) {
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        serveRead(end, bytes);
        _exit(0);
    }
    return child;
}

// A child that serves one SEND and one RECV of the open file whose
// endpoint end is: SEND's len u32 of no address and its bytes, whose
// count is the answer, then RECV's want u32, answered flags 0, len 0 and
// "pong".
static pid_t socketServer(int end) {
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child != 0) {
        return child;
    }
    unsigned char packet[256];
    int reply;
    Header h;
    uint32_t len;
    const ssize_t n = receiveWith(end, packet, sizeof packet, reply);
    assert(n >= ssize_t(sizeof h + 4) && reply >= 0);
    memcpy(&h, packet, sizeof h);
    memcpy(&len, packet + sizeof h, sizeof len);
    const int sent = int(n - ssize_t(sizeof h + 4 + len));
    printf("served: %s of '%.*s'\n", h.op == opSend && len == 0 ? "SEND" : "another call", sent, reinterpret_cast<const char*>(packet) + sizeof h + 4 + len);
    fflush(stdout);

    struct {
        Header h;
        uint32_t count;
    } counted{{opOk, 0, 0, 0, 0, 0}, uint32_t(sent)};

    assert(send(reply, &counted, sizeof counted, 0) == ssize_t(sizeof counted));
    close(reply);
    uint32_t want;
    assert(receiveWith(end, packet, sizeof packet, reply) == ssize_t(sizeof h + 4) && reply >= 0);
    memcpy(&h, packet, sizeof h);
    memcpy(&want, packet + sizeof h, sizeof want);
    printf("served: %s of %u\n", h.op == opRecv ? "RECV" : "another call", want);
    fflush(stdout);

    struct [[gnu::packed]] {
        Header h;
        uint32_t flags;
        uint32_t len;
        char data[4];
    } given{{opOk, 0, 0, 0, 0, 0}, 0, 0, {'p', 'o', 'n', 'g'}};

    assert(send(reply, &given, sizeof given, 0) == ssize_t(sizeof given));
    close(reply);
    _exit(0);
}

// A child that serves end as a directory until it closes: the stat of
// the directory itself, and no name in it.
static pid_t directoryServer(int end, int other) {
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child != 0) {
        return child;
    }
    close(other);
    for (;;) {
        unsigned char packet[256];
        int reply;
        const ssize_t n = receiveWith(end, packet, sizeof packet, reply);
        if (n < ssize_t(sizeof(Header)) || reply < 0) {
            _exit(0);
        }
        Header h;
        memcpy(&h, packet, sizeof h);
        // A path's call: a CTL, its call, its arg, then the path.
        uint32_t call = 0;
        const int length = h.op == opCtl && n >= ssize_t(sizeof h + 8) ? int(n - ssize_t(sizeof h) - 8) : 0;
        if (length > 0 || h.op == opCtl) {
            memcpy(&call, packet + sizeof h, sizeof call);
            printf("served: CTL %s of '%.*s'\n", call == ctlStat ? "ctlStat" : "another call", length,
                   reinterpret_cast<const char*>(packet) + sizeof h + 8);
        } else {
            printf("served: %s\n", h.op == opFstat ? "FSTAT" : "another call");
        }
        fflush(stdout);
        if (h.op == opFstat) {
            struct {
                Header h;
                Stat st;
            } answer{{opOk, 0, 0, 0, 0, 0}, {1, 0, 0, S_IFDIR | 0755, 2, 0, 0, 0, 1, 0}};

            assert(send(reply, &answer, sizeof answer, 0) == ssize_t(sizeof answer));
        } else {
            const Header none{opErr, ENOENT, 0, 0, 0, 0};
            assert(send(reply, &none, sizeof none, 0) == ssize_t(sizeof none));
        }
        close(reply);
    }
}

// A child that serves two ends of what is no directory until both close:
// every request is ENOTDIR, where it is answered. The open files of the
// ends, which it gets with the fork, it lets go of.
static pid_t noDirectories(int a, int b, int aFile, int bFile) {
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child != 0) {
        return child;
    }
    close(aFile);
    close(bFile);
    pollfd ends[2] = {{a, POLLIN, 0}, {b, POLLIN, 0}};
    while (ends[0].fd >= 0 || ends[1].fd >= 0) {
        assert(poll(ends, 2, -1) > 0);
        for (pollfd& e : ends) {
            if (e.fd < 0 || e.revents == 0) {
                continue;
            }
            unsigned char packet[256];
            int reply;
            if (receiveWith(e.fd, packet, sizeof packet, reply) <= 0 || reply < 0) {
                close(e.fd);
                e.fd = -1;
                continue;
            }
            const Header no{opErr, ENOTDIR, 0, 0, 0, 0};
            assert(send(reply, &no, sizeof no, 0) == ssize_t(sizeof no));
            close(reply);
        }
    }
    _exit(0);
}

static void waitFor(pid_t child) {
    int status;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
}

int main() {
    alarm(60);
    const int channel = device("served_pairs");
    int end;
    errno = 0;
    assert(opened(channel, {99, O_RDWR, 0, 0, POLLIN | POLLOUT}, end) == -1);
    printf("a kind the kernel does not know: %s\n", errorName(errno));

    // Writable as it opens, nothing to read; then the server says there is.
    int file = opened(channel, {openedFile, O_RDWR, 0, 0, POLLOUT}, end);
    assert(file >= 0);
    pollfd ready{file, POLLIN | POLLOUT, 0};
    assert(poll(&ready, 1, 0) == 1);
    const short first = ready.revents;

    const Header told{opReady, POLLIN | POLLOUT, 0, 0, 0, 0};

    assert(send(end, &told, sizeof told, 0) == ssize_t(sizeof told));
    assert(poll(&ready, 1, 1000) == 1);
    printf("ready as the OPEN said: %s, then as READY said: %s%s\n", first == POLLOUT ? "POLLOUT" : "something else",
           (ready.revents & POLLIN) != 0 ? "POLLIN " : "", (ready.revents & POLLOUT) != 0 ? "POLLOUT" : "");
    close(end);
    close(file);

    // A read of the file is a READ sent on the server's end.
    file = opened(channel, {openedFile, O_RDWR, 0, 0, POLLIN | POLLOUT}, end);
    assert(file >= 0);
    pid_t child = server(end, "hello");
    char got[16] = {};
    assert(read(file, got, sizeof got) == 5);
    waitFor(child);
    printf("read: '%s'\n", got);
    close(end);
    close(file);

    // Passed on to another process, the file is still a file there.
    file = opened(channel, {openedFile, O_RDWR, 0, 0, POLLIN | POLLOUT}, end);
    assert(file >= 0);
    int carrier[2];
    assert(socketpair(AF_UNIX, SOCK_SEQPACKET, 0, carrier) == 0);
    child = server(end, "world");
    fflush(stdout);
    const pid_t reader = fork();
    assert(reader >= 0);
    if (reader == 0) {
        char byte;
        int passed;
        assert(receiveWith(carrier[1], &byte, 1, passed) == 1 && passed >= 0);
        char bytes[8] = {};
        const ssize_t n = read(passed, bytes, sizeof bytes);
        printf("passed on, read: '%.*s'\n", int(n), bytes);
        fflush(stdout);
        _exit(0);
    }
    sendWith(carrier[0], "f", 1, file);
    close(end);
    close(file);
    waitFor(reader);
    waitFor(child);

    // A send and a recv on an open file go to its server.
    file = opened(channel, {openedFile, O_RDWR, 0, 0, POLLIN | POLLOUT}, end);
    assert(file >= 0);
    child = socketServer(end);
    assert(send(file, "ping", 4, 0) == 4);
    char pong[8] = {};
    const ssize_t heard = recv(file, pong, sizeof pong, 0);
    waitFor(child);
    printf("send and recv on it: %zd '%.*s'\n", heard, int(heard), pong);
    close(end);
    close(file);

    // fchdir asks what it is given what it is: the servers of a path's
    // handle and of a file say ENOTDIR to a call by path, as what is no
    // directory does; a stream socket speaks no protocol and is none.
    int pathEnd;
    int fileEnd;
    int stream[2];
    const int path = opened(channel, {openedPath, O_RDWR, 0, 0, POLLIN | POLLOUT}, pathEnd);
    file = opened(channel, {openedFile, O_RDWR, 0, 0, POLLIN | POLLOUT}, fileEnd);
    assert(path >= 0 && file >= 0 && socketpair(AF_UNIX, SOCK_STREAM, 0, stream) == 0);
    const pid_t none = noDirectories(pathEnd, fileEnd, path, file);
    const int a = fchdir(path) == -1 ? errno : 0;
    const int b = fchdir(file) == -1 ? errno : 0;
    const int d = fchdir(stream[0]) == -1 ? errno : 0;
    close(pathEnd);
    close(fileEnd);
    close(path);
    close(file);
    waitFor(none);
    printf("fchdir: a path's %s, a file's %s, a stream socket's %s\n", errorName(a), errorName(b), errorName(d));

    // An end of a seqpacket pair is no directory, and attach makes one
    // over it to fchdir to: the calls with a path from it go to who serves
    // the other end.
    int root[2];
    const int back = open(".", O_RDONLY | O_DIRECTORY);
    assert(back >= 0 && socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, root) == 0);
    child = directoryServer(root[0], root[1]);
    close(root[0]);
    const int bare = fchdir(root[1]) == -1 ? errno : 0;
    const int over = attach(root[1]);
    assert(over >= 0);
    const int entered = fchdir(over) == -1 ? errno : 0;
    struct stat st;
    const int named = stat("name", &st) == -1 ? errno : 0;
    assert(fchdir(back) == 0);
    close(over);
    close(root[1]);
    waitFor(child);
    printf("a pair's end: fchdir %s; a directory over it, the working directory: fchdir %s, a name there %s\n", errorName(bare),
           errorName(entered), errorName(named));
    return 0;
}
