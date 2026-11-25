//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "of no server's: ENOTTY; no descriptor: EBADF; a flag: EINVAL; 4 descriptors: EINVAL; too big: EMSGSIZE\nserved: CTL 'ask' of uid ok, with 'x'\nanswer: 'answ' of 7, 2 descriptors, close-on-exec: 1 1; an open file of the server's ready: POLLIN; a pipe's: 'y'\nserved: CTL 'no'\nthe server's error: EPERM\nserved: CTL 'all'\nno room: 0 bytes of 7, 2 descriptors\n"

// ctl, a call of a file's server (unistd.h, lib/protocol's CTL): the
// request's bytes and descriptors go to the server with the header the
// kernel says, who calls, and the answer's come back, its bytes as many
// as there is room for and their count, its descriptors the same, a
// server's file of those its openeds say, close-on-exec of O_CLOEXEC. An
// error of the server's is the call's; a file no server serves is
// ENOTTY. The test is the server: a device of its own, whose open it
// answers, then the CTLs of the open file.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/param.h>
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
    } ok{{opOk, 0, 0, 0, 0, 0}, what};

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

constexpr uint32_t opCtl = 79;
constexpr uint32_t opErr = 129;

static const char* errorName(int e) {
    switch (e) {
        case ENOTTY:
            return "ENOTTY";
        case EBADF:
            return "EBADF";
        case EINVAL:
            return "EINVAL";
        case EMSGSIZE:
            return "EMSGSIZE";
        case EPERM:
            return "EPERM";
        default:
            return "another";
    }
}

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

// Sends a packet with two descriptors.
static void sendTwo(int to, const void* data, size_t size, int a, int b) {
    alignas(cmsghdr) char control[CMSG_SPACE(2 * sizeof(int))] = {};
    iovec iov{const_cast<void*>(data), size};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    cmsghdr* c = CMSG_FIRSTHDR(&m);
    c->cmsg_level = SOL_SOCKET;
    c->cmsg_type = SCM_RIGHTS;
    c->cmsg_len = CMSG_LEN(2 * sizeof(int));
    const int fds[2] = {a, b};
    memcpy(CMSG_DATA(c), fds, sizeof fds);
    assert(sendmsg(to, &m, MSG_NOSIGNAL) == ssize_t(size));
}

// A CTL of the open file whose endpoint end is: its bytes, printed, and
// where it is answered; the descriptor it brings, -1 of none.
static void served(int end, char* body, size_t room, int& with, int& reply) {
    unsigned char packet[256];
    const ssize_t n = receiveTwo(end, packet, sizeof packet, with, reply);
    Header h;
    assert(n >= ssize_t(sizeof h) && reply >= 0);
    memcpy(&h, packet, sizeof h);
    const size_t size = MIN(room - 1, size_t(n) - sizeof h);
    memcpy(body, packet + sizeof h, size);
    body[size] = 0;
    printf("served: %s '%s'", h.op == opCtl ? "CTL" : "another call", body);
    if (with >= 0) {
        char byte = 0;
        assert(read(with, &byte, 1) == 1);
        printf(" of uid %s, with '%c'", h.uid == geteuid() && h.gid == getegid() ? "ok" : "another", byte);
    }
    printf("\n");
    fflush(stdout);
}

// A child that serves the CTLs: the first answered with an open file of
// ours and a pipe's end that has "y", the second the error EPERM, the
// third with the open file and the pipe's end again.
static pid_t server(int end) {
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child != 0) {
        return child;
    }
    char body[64];
    int with;
    int reply;
    int pair[2];
    int pipes[2];
    assert(socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) == 0 && pipe(pipes) == 0 && write(pipes[1], "y", 1) == 1);

    struct [[gnu::packed]] {
        Header h;
        Opened what;
        char answer[7];
    } ok{{opOk, 1, 0, 0, 0, 0}, {openedFile, O_RDWR, 0, 0, POLLIN}, {'a', 'n', 's', 'w', 'e', 'r', '!'}};

    served(end, body, sizeof body, with, reply);
    sendTwo(reply, &ok, sizeof ok, pair[1], pipes[0]);
    close(reply);
    close(with);
    served(end, body, sizeof body, with, reply);
    const Header no{opErr, EPERM, 0, 0, 0, 0};
    assert(send(reply, &no, sizeof no, 0) == ssize_t(sizeof no));
    close(reply);
    served(end, body, sizeof body, with, reply);
    sendTwo(reply, &ok, sizeof ok, pair[1], pipes[0]);
    close(reply);
    _exit(0);
}

int main() {
    alarm(60);
    int pipes[2];
    assert(pipe(pipes) == 0);
    char room[16];
    const int four[4] = {0, 1, 2, 0};
    static char big[70000];
    struct ctl c{"ask", 3, nullptr, 0, room, sizeof room, nullptr, 0};
    errno = 0;
    assert(ctl(pipes[0], &c, 0) == -1);
    printf("of no server's: %s", errorName(errno));
    errno = 0;
    assert(ctl(-1, &c, 0) == -1);
    printf("; no descriptor: %s", errorName(errno));
    errno = 0;
    assert(ctl(pipes[0], &c, 4) == -1);
    printf("; a flag: %s", errorName(errno));
    c.fds = four;
    c.nfds = 4;
    errno = 0;
    assert(ctl(pipes[0], &c, 0) == -1);
    printf("; 4 descriptors: %s", errorName(errno));
    c = {big, sizeof big, nullptr, 0, room, sizeof room, nullptr, 0};
    errno = 0;
    assert(ctl(pipes[0], &c, 0) == -1);
    printf("; too big: %s\n", errorName(errno));

    const int channel = device("served_ctl");
    int end;
    const int file = opened(channel, {openedFile, O_RDWR, 0, 0, POLLIN | POLLOUT}, end);
    assert(file >= 0);
    const pid_t child = server(end);
    assert(write(pipes[1], "x", 1) == 1);
    int got[2] = {-1, -1};
    c = {"ask", 3, &pipes[0], 1, room, 4, got, 2};
    assert(ctl(file, &c, O_CLOEXEC) == 0 && c.room == 7 && c.ngot == 2);
    pollfd ready{got[0], POLLIN | POLLOUT, 0};
    char byte = 0;
    assert(poll(&ready, 1, 0) == 1 && read(got[1], &byte, 1) == 1);
    printf("answer: '%.4s' of %zu, %zu descriptors, close-on-exec: %d %d; an open file of the server's ready: %s; a pipe's: '%c'\n", room,
           c.room, c.ngot, fcntl(got[0], F_GETFD) & FD_CLOEXEC, fcntl(got[1], F_GETFD) & FD_CLOEXEC,
           ready.revents == POLLIN ? "POLLIN" : "another", byte);
    fflush(stdout);
    c = {"no", 2, nullptr, 0, room, sizeof room, nullptr, 0};
    errno = 0;
    assert(ctl(file, &c, 0) == -1);
    printf("the server's error: %s\n", errorName(errno));
    fflush(stdout);
    c = {"all", 3, nullptr, 0, room, 0, nullptr, 0};
    assert(ctl(file, &c, 0) == 0);
    printf("no room: 0 bytes of %zu, %zu descriptors\n", c.room, c.ngot);
    int status;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
}
