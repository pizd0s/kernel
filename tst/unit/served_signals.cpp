//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "killed before its request is taken: by SIGKILL\nkilled in a call: by SIGTERM, by SIGKILL\na handler in a call: ran before the answer, then fstat 0 of 5 bytes\nstopped in a call: by SIGSTOP, continued, then fstat 0 of 5 bytes\n"

// A process takes its signals in a call to a server as in any wait: one
// that ends it ends it though the server never answers or never takes
// the request, a handler runs at once, and a stop stops
// it, the call going on after to its answer. The test serves a file
// itself, of a device of its own, and holds the answer of an FSTAT back.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
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

constexpr uint32_t opFstat = 67;

static int channel;

// An open file of ours: its endpoint first, the open file second.
static int servedFile(int sv[2]) {
    sv[1] = opened(channel, {openedFile, O_RDWR, 0, 0, POLLIN | POLLOUT}, sv[0]);
    return sv[1] >= 0 ? 0 : -1;
}

// Takes the FSTAT of the file whose endpoint end is: where it is
// answered, the request's descriptor; who asked waits for the answer
// from now on.
static int takeFstat(int end) {
    Header h;
    alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))] = {};
    iovec iov{&h, sizeof h};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    assert(recvmsg(end, &m, 0) == ssize_t(sizeof h) && h.op == opFstat);
    cmsghdr* c = CMSG_FIRSTHDR(&m);
    assert(c && c->cmsg_level == SOL_SOCKET && c->cmsg_type == SCM_RIGHTS && c->cmsg_len == CMSG_LEN(sizeof(int)));
    int reply;
    memcpy(&reply, CMSG_DATA(c), sizeof(int));
    return reply;
}

// Answers an FSTAT where its request said: a regular file of 5 bytes.
static void answerFstat(int reply) {
    struct {
        Header h;
        Stat st;
    } answer{{opOk, 0, 0, 0, 0, 0}, {1, 5, 0, S_IFREG | 0644, 1, 0, 0, 0, 1, 0}};

    assert(send(reply, &answer, sizeof answer, 0) == ssize_t(sizeof answer));
    close(reply);
}

// A child that asks the file's stat and ends with whether it got it.
static pid_t asker(int file, void (*before)()) {
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        if (before) {
            before();
        }
        struct stat st;
        const int r = fstat(file, &st);
        _exit(r == 0 && st.st_size == 5 ? 0 : 1);
    }
    return child;
}

static const char* asked(int status) {
    return WIFEXITED(status) && WEXITSTATUS(status) == 0 ? "fstat 0 of 5 bytes" : "fstat failed";
}

static int told[2];

static void handler(int) {
    assert(write(told[1], "h", 1) == 1);
}

static void handles() {
    assert(signal(SIGUSR1, handler) != SIG_ERR);
}

int main() {
    alarm(60);
    channel = device("served_signals");
    int sv[2];
    int status;

    // The server never takes the request; a signal that ends who waits
    // for its answer ends it.
    assert(servedFile(sv) == 0);
    pid_t waiting = asker(sv[1], nullptr);
    pollfd sent{sv[0], POLLIN, 0};
    assert(poll(&sent, 1, -1) == 1);
    assert(kill(waiting, SIGKILL) == 0);
    assert(waitpid(waiting, &status, 0) == waiting);
    printf("killed before its request is taken: by %s\n", WIFSIGNALED(status) && WTERMSIG(status) == SIGKILL ? "SIGKILL" : "something else");
    close(sv[0]);
    close(sv[1]);

    // The server never answers; a signal that ends who waits for it ends
    // it.
    const char* by[2];
    const int fatal[2] = {SIGTERM, SIGKILL};
    for (int i = 0; i < 2; ++i) {
        assert(servedFile(sv) == 0);
        const pid_t child = asker(sv[1], nullptr);
        const int reply = takeFstat(sv[0]);
        assert(kill(child, fatal[i]) == 0);
        assert(waitpid(child, &status, 0) == child);
        by[i] = WIFSIGNALED(status) && WTERMSIG(status) == fatal[i] ? (i == 0 ? "SIGTERM" : "SIGKILL") : "something else";
        close(reply);
        close(sv[0]);
        close(sv[1]);
    }
    printf("killed in a call: by %s, by %s\n", by[0], by[1]);

    // A handler runs while the answer is held back; the call ends with
    // the answer, after it.
    assert(servedFile(sv) == 0 && pipe(told) == 0);
    pid_t child = asker(sv[1], handles);
    int reply = takeFstat(sv[0]);
    assert(kill(child, SIGUSR1) == 0);
    char c;
    assert(read(told[0], &c, 1) == 1 && c == 'h');
    answerFstat(reply);
    assert(waitpid(child, &status, 0) == child);
    printf("a handler in a call: ran before the answer, then %s\n", asked(status));
    close(sv[0]);
    close(sv[1]);

    // SIGSTOP stops who waits for the answer and SIGCONT continues it;
    // the answer ends its call.
    assert(servedFile(sv) == 0);
    child = asker(sv[1], nullptr);
    reply = takeFstat(sv[0]);
    assert(kill(child, SIGSTOP) == 0);
    assert(waitpid(child, &status, WUNTRACED) == child);
    const bool stopped = WIFSTOPPED(status) && WSTOPSIG(status) == SIGSTOP;
    assert(kill(child, SIGCONT) == 0);
    assert(waitpid(child, &status, WCONTINUED) == child);
    const bool continued = WIFCONTINUED(status);
    answerFstat(reply);
    assert(waitpid(child, &status, 0) == child);
    printf("stopped in a call: %s, %s, then %s\n", stopped ? "by SIGSTOP" : "not stopped", continued ? "continued" : "not continued", asked(status));
    return 0;
}
