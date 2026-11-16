//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "unknown kind: EINVAL\nserved: READ of 16 at 0\nread: 'hello'\nserved: READ of 8 at 0\npassed on, read: 'world'\nfchdir: a path's ENOTDIR, a file's ENOTDIR, a socket's ENOTDIR\n"

// A pair a server makes for what it serves, servedpair: the second end is
// born the open file the server describes, and its calls are the file
// protocol's requests on the first end (lib/protocol/protocol.h), whoever
// holds it: the process that made it, or one it passed it on to. The test
// serves the first end itself. What is no directory's handle, a path's, a
// file's or a plain socket's end, is no directory to fchdir to.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/served.h>
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
constexpr uint32_t opOk = 128;

static const char* errorName(int e) {
    switch (e) {
        case EINVAL:
            return "EINVAL";
        case ENOTDIR:
            return "ENOTDIR";
        default:
            return "another error";
    }
}

static int served(uint32_t kind, int sv[2]) {
    const struct served what{kind, O_RDWR, 0, 0, 0};
    return servedpair(&what, sv);
}

// Serves one READ on the server's end: the bytes go into the reader's
// memory, the memfd that came with it, and the count is the answer.
static void serveRead(int end, const char* bytes) {
    unsigned char packet[256];
    alignas(cmsghdr) char control[CMSG_SPACE(4 * sizeof(int))];
    iovec iov{packet, sizeof packet};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    assert(recvmsg(end, &m, 0) == ssize_t(sizeof(Header) + sizeof(Io)));
    const cmsghdr* c = CMSG_FIRSTHDR(&m);
    assert(c && c->cmsg_type == SCM_RIGHTS && c->cmsg_len == CMSG_LEN(2 * sizeof(int)));
    int fds[2];
    memcpy(fds, CMSG_DATA(c), sizeof fds);
    Header h;
    Io io;
    memcpy(&h, packet, sizeof h);
    memcpy(&io, packet + sizeof h, sizeof io);
    printf("served: %s of %u at %llu\n", h.op == opRead ? "READ" : "another call", h.arg, (unsigned long long)io.off);
    fflush(stdout);
    const size_t length = strlen(bytes);
    assert(pwrite(fds[0], bytes, length, io.woff) == ssize_t(length));

    struct {
        Header h;
        uint32_t count;
    } answer{{opOk, 0, 0, 0}, uint32_t(length)};

    assert(send(fds[1], &answer, sizeof answer, 0) == ssize_t(sizeof answer));
    close(fds[0]);
    close(fds[1]);
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
