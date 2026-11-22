//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "one name, one file: busy 1, a range busy 1, free after the close 1\nanother ino: free 1\nanother dev: free 1\nanother process, the same name: busy 1\nno ino: EINVAL 1\n"

// The kernel keeps the locks of flock and fcntl by what names a file: the
// dev and ino its server says of the open file in its OPEN's OK
// (lib/protocol/protocol.h). The open files given one dev and ino are one
// file to lock, whichever process opened them, as rootfs says a
// directory's handle's with its file system's numbers; those of another
// ino or dev are other files. A file with no ino has no locks. The test
// is the server: a device of its own, whose opens it answers.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/file.h>
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
    uint64_t id;
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

static int channel;

// An open file of a dev and ino, served by nobody: a lock is no request.
static int file(uint64_t dev, uint64_t ino) {
    int end;
    const int fd = opened(channel, {openedFile, O_RDWR, dev, ino, 0, POLLIN | POLLOUT}, end);
    assert(fd >= 0);
    return fd;
}

static int busy(int fd) {
    return flock(fd, LOCK_EX | LOCK_NB) == -1 && errno == EAGAIN;
}

static int taken(int fd) {
    return flock(fd, LOCK_EX | LOCK_NB) == 0;
}

int main() {
    alarm(60);
    channel = device("served_locks");
    const int a = file(7, 42);
    const int b = file(7, 42);
    assert(flock(a, LOCK_EX) == 0);
    const int held = busy(b);
    struct flock range{};
    range.l_type = F_WRLCK;
    range.l_whence = SEEK_SET;
    range.l_len = 8;
    assert(fcntl(a, F_SETLK, &range) == 0);
    const int ranged = fcntl(b, F_SETLK, &range) == -1 && errno == EAGAIN;
    assert(close(a) == 0);
    const int after = taken(b) && fcntl(b, F_SETLK, &range) == 0;
    printf("one name, one file: busy %d, a range busy %d, free after the close %d\n", held, ranged, after);

    printf("another ino: free %d\n", taken(file(7, 43)));
    printf("another dev: free %d\n", taken(file(8, 42)));

    // A child is another process: its file of the name b has is the same
    // file, which b holds.
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        printf("another process, the same name: busy %d\n", busy(file(7, 42)));
        fflush(stdout);
        _exit(0);
    }
    int status;
    assert(waitpid(child, &status, 0) == child && status == 0);

    const int nameless = file(7, 0);
    printf("no ino: EINVAL %d\n", flock(nameless, LOCK_EX) == -1 && errno == EINVAL);
    return 0;
}
