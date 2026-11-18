//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "unknown kind: EINVAL\nan end another descriptor has: EINVAL\nwhat is no pair's end: ENOTSOCK\nserved: CONNECT, then READ of 16 at 0\nread: 'hello'\nserved: CONNECT, then READ of 8 at 0\npassed on, read: 'world'\nfchdir: a path's ENOTDIR, a file's ENOTDIR, a socket's ENOTDIR\n"

// A pair a server makes for what it serves: SO_SERVED makes its second
// end the open file the server describes, and its calls are the file
// protocol's (lib/protocol/protocol.h), whoever holds it: the process
// that made it, or one it passed it on to. A call is a connection asked
// on the first end, CONNECT, which the server answers there with the
// asking end of a call pair it makes, and the request sent on that
// connection, answered on it. The test serves the first end itself. What
// is no directory's handle, a path's, a file's or a plain socket's end,
// is no directory to fchdir to.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/uio.h>
#include <sys/wait.h>
#include <unistd.h>

// The protocol's: a request's header, a READ's body and op, an OK's op.
struct Header {
    uint32_t op;
    uint32_t arg;
    uint32_t uid;
    uint32_t gid;
};

struct Io {
    uint64_t off;
    uint32_t woff;
    uint32_t flags;
};

constexpr uint32_t opRead = 64;
constexpr uint32_t opConnect = 76;
constexpr uint32_t opOk = 128;

static const char* errorName(int e) {
    switch (e) {
        case EINVAL:
            return "EINVAL";
        case ENOTDIR:
            return "ENOTDIR";
        case ENOTSOCK:
            return "ENOTSOCK";
        default:
            return "another error";
    }
}

static int served(uint32_t kind, int sv[2]) {
    const struct served what{kind, O_RDWR, 0, 0, 0};
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, sv) != 0) {
        return -1;
    }
    if (setsockopt(sv[1], SOL_SOCKET, SO_SERVED, &what, sizeof what) != 0) {
        const int e = errno;
        close(sv[0]);
        close(sv[1]);
        errno = e;
        return -1;
    }
    return 0;
}

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
    assert(sendmsg(to, &m, 0) == ssize_t(size));
}

// Serves one READ of the open file whose endpoint end is: the connection
// who reads asks for first, a call pair of ours whose asking end the OK
// brings, then the READ on it. The bytes go into the reader's memory, the
// memfd that came with the READ, and the count is the answer.
static void serveRead(int end, const char* bytes) {
    Header asked;
    assert(recv(end, &asked, sizeof asked, 0) == ssize_t(sizeof asked) && asked.op == opConnect);
    int call[2];
    assert(socketpair(AF_UNIX, SOCK_CALL | SOCK_CLOEXEC, 0, call) == 0);
    const Header ok{opOk, 0, 0, 0};
    sendWith(end, &ok, sizeof ok, call[1]);
    close(call[1]);

    unsigned char packet[256];
    alignas(cmsghdr) char control[CMSG_SPACE(4 * sizeof(int))];
    iovec iov{packet, sizeof packet};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    assert(recvmsg(call[0], &m, 0) == ssize_t(sizeof(Header) + sizeof(Io)));
    const cmsghdr* c = CMSG_FIRSTHDR(&m);
    assert(c && c->cmsg_type == SCM_RIGHTS && c->cmsg_len == CMSG_LEN(sizeof(int)));
    int window;
    memcpy(&window, CMSG_DATA(c), sizeof window);
    Header h;
    Io io;
    memcpy(&h, packet, sizeof h);
    memcpy(&io, packet + sizeof h, sizeof io);
    printf("served: CONNECT, then %s of %u at %llu\n", h.op == opRead ? "READ" : "another call", h.arg, (unsigned long long)io.off);
    fflush(stdout);
    const size_t length = strlen(bytes);
    assert(pwrite(window, bytes, length, io.woff) == ssize_t(length));

    struct {
        Header h;
        uint32_t count;
    } answer{{opOk, 0, 0, 0}, uint32_t(length)};

    assert(send(call[0], &answer, sizeof answer, 0) == ssize_t(sizeof answer));
    close(window);
    close(call[0]);
}

// A child that serves one READ of the first end, then ends.
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

static void waitFor(pid_t child) {
    int status;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
}

int main() {
    int sv[2];
    errno = 0;
    assert(served(99, sv) == -1);
    printf("unknown kind: %s\n", errorName(errno));

    // The end is the open file's alone, and a pair's.
    const struct served what{SERVED_FILE, O_RDWR, 0, 0, 0};
    assert(socketpair(AF_UNIX, SOCK_SEQPACKET, 0, sv) == 0);
    const int twin = dup(sv[1]);
    assert(setsockopt(sv[1], SOL_SOCKET, SO_SERVED, &what, sizeof what) == -1);
    printf("an end another descriptor has: %s\n", errorName(errno));
    close(twin);
    close(sv[0]);
    close(sv[1]);
    assert(setsockopt(STDOUT_FILENO, SOL_SOCKET, SO_SERVED, &what, sizeof what) == -1);
    printf("what is no pair's end: %s\n", errorName(errno));

    // A read of the file is a READ on the server's end.
    assert(served(SERVED_FILE, sv) == 0);
    pid_t child = server(sv[0], "hello");
    char got[16] = {};
    assert(read(sv[1], got, sizeof got) == 5);
    waitFor(child);
    printf("read: '%s'\n", got);
    close(sv[0]);
    close(sv[1]);

    // Passed on to another process, the file is still a file there.
    assert(served(SERVED_FILE, sv) == 0);
    int carrier[2];
    assert(socketpair(AF_UNIX, SOCK_SEQPACKET, 0, carrier) == 0);
    child = server(sv[0], "world");
    fflush(stdout);
    const pid_t reader = fork();
    assert(reader >= 0);
    if (reader == 0) {
        char byte;
        alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))];
        iovec iov{&byte, 1};
        msghdr m{};
        m.msg_iov = &iov;
        m.msg_iovlen = 1;
        m.msg_control = control;
        m.msg_controllen = sizeof control;
        assert(recvmsg(carrier[1], &m, 0) == 1);
        int file;
        memcpy(&file, CMSG_DATA(CMSG_FIRSTHDR(&m)), sizeof file);
        char bytes[8] = {};
        const ssize_t n = read(file, bytes, sizeof bytes);
        printf("passed on, read: '%.*s'\n", int(n), bytes);
        fflush(stdout);
        _exit(0);
    }
    char byte = 'f';
    alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))] = {};
    iovec iov{&byte, 1};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    cmsghdr* c = CMSG_FIRSTHDR(&m);
    c->cmsg_level = SOL_SOCKET;
    c->cmsg_type = SCM_RIGHTS;
    c->cmsg_len = CMSG_LEN(sizeof(int));
    memcpy(CMSG_DATA(c), &sv[1], sizeof(int));
    assert(sendmsg(carrier[0], &m, 0) == 1);
    close(sv[0]);
    close(sv[1]);
    waitFor(reader);
    waitFor(child);

    // fchdir takes a directory's handle only: a path's, a file's and a
    // plain socket's end are none.
    int path[2];
    int file[2];
    assert(served(SERVED_PATH, path) == 0 && served(SERVED_FILE, file) == 0);
    const int a = fchdir(path[1]) == -1 ? errno : 0;
    const int b = fchdir(file[1]) == -1 ? errno : 0;
    const int d = fchdir(carrier[0]) == -1 ? errno : 0;
    printf("fchdir: a path's %s, a file's %s, a socket's %s\n", errorName(a), errorName(b), errorName(d));
    return 0;
}
