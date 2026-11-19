//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "three calls: 1 connection, 3 FSTAT\nanother process's two: 2 connections, 5 FSTAT\na handler's call in a call: read EINTR, the handler's fstat 0, 3 connections, 6 FSTAT, 1 READ, 1 CANCEL\nthe call after: 3 connections, 7 FSTAT\nafter the server let go of them: fstat 0, 4 connections, 8 FSTAT\n"

// A process keeps its connection to an open file of a server for its
// next calls on it (bin/vm/remote_node.go): it connects once, CONNECT
// on the open file's endpoint, and every call after goes on that
// connection. Another process connects for itself. A call a handler
// makes while a call of the process waits at the server goes on a
// connection of its own, the kept one being in that call, which hears
// its CANCEL; and a connection the server let go of is made again. The
// test serves a file itself, of a device of its own, and counts what
// comes.

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
};

struct Opened {
    uint32_t kind;
    uint32_t flags;
    uint64_t dev;
    uint64_t ino;
    uint64_t id;
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

// A device of ours in /dev, registered at /dev/init: its path, and our
// end of the pair devfs hands its opens on to.
static char devicePath[64];

static int device(const char* name) {
    char registered[48];
    snprintf(registered, sizeof registered, "%s_%d", name, int(getpid()));
    snprintf(devicePath, sizeof devicePath, "/dev/%s", registered);
    const int registry = open("/dev/init", O_RDWR | O_CLOEXEC);
    int pair[2];
    assert(registry >= 0 && socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) == 0);

    struct {
        Header h;
        char name[48];
    } request{{opRegister, S_IFCHR | 0666, uint32_t(geteuid()), uint32_t(getegid())}, {}};

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
    } ok{{opOk, 0, 0, 0}, what};

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

constexpr uint32_t opRead = 64;
constexpr uint32_t opFstat = 67;
constexpr uint32_t opCancel = 74;
constexpr uint32_t opConnect = 76;
constexpr uint32_t opErr = 129;

// What the server counted.
struct Counts {
    int connections;
    int fstats;
    int reads;
    int cancels;
};

// A request of a connection: its header, and the descriptor it brought,
// a READ's memfd, closed, as the test reads nothing into it. False at
// the connection's end.
static bool request(int connection, Header& h) {
    unsigned char packet[64];
    alignas(cmsghdr) char control[CMSG_SPACE(4 * sizeof(int))];
    iovec iov{packet, sizeof packet};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    const ssize_t n = recvmsg(connection, &m, 0);
    if (n < ssize_t(sizeof h)) {
        return false;
    }
    memcpy(&h, packet, sizeof h);
    for (cmsghdr* c = CMSG_FIRSTHDR(&m); c; c = CMSG_NXTHDR(&m, c)) {
        if (c->cmsg_type == SCM_RIGHTS && c->cmsg_len == CMSG_LEN(sizeof(int))) {
            int fd;
            memcpy(&fd, CMSG_DATA(c), sizeof fd);
            close(fd);
        }
    }
    return true;
}

// Serves the file whose endpoint end is until it closes: a connection to
// who asks, FSTAT answered at once, a regular file of 5 bytes, and a READ
// kept until its CANCEL, EINTR then, the reader, our parent, signalled as
// it comes. A byte on control is a question of the counts, answered on
// report: 'd' lets go of the connections first.
[[noreturn]] static void serve(int end, int control, int report) {
    int connections[8];
    int count = 0;
    Counts counts{};
    for (;;) {
        pollfd fds[10] = {{end, POLLIN, 0}, {control, POLLIN, 0}};
        for (int i = 0; i < count; ++i) {
            fds[2 + i] = {connections[i], POLLIN, 0};
        }
        assert(poll(fds, nfds_t(2 + count), -1) > 0);
        // The calls first: a question of the counts is of those before it.
        for (int i = count - 1; i >= 0; --i) {
            if (fds[2 + i].revents == 0) {
                continue;
            }
            Header h;
            if (!request(connections[i], h)) {
                close(connections[i]);
                connections[i] = connections[--count];
                continue;
            }
            if (h.op == opFstat) {
                ++counts.fstats;

                struct {
                    Header h;
                    Stat st;
                } answer{{opOk, 0, 0, 0}, {1, 5, 0, S_IFREG | 0644, 1, 0, 0, 0, 1, 0}};

                assert(send(connections[i], &answer, sizeof answer, MSG_NOSIGNAL) == ssize_t(sizeof answer));
            } else if (h.op == opRead) {
                ++counts.reads;
                assert(kill(getppid(), SIGUSR1) == 0);
            } else if (h.op == opCancel) {
                ++counts.cancels;
                const Header interrupted{opErr, EINTR, 0, 0};
                assert(send(connections[i], &interrupted, sizeof interrupted, MSG_NOSIGNAL) == ssize_t(sizeof interrupted));
            }
        }
        if (fds[0].revents != 0) {
            Header h;
            if (recv(end, &h, sizeof h, 0) != ssize_t(sizeof h)) {
                _exit(0);
            }
            assert(h.op == opConnect && count < 8);
            int call[2];
            assert(socketpair(AF_UNIX, SOCK_CALL | SOCK_CLOEXEC, 0, call) == 0);
            const Header ok{opOk, 0, 0, 0};
            sendWith(end, &ok, sizeof ok, call[1]);
            close(call[1]);
            connections[count++] = call[0];
            ++counts.connections;
        } else if (fds[1].revents != 0) {
            char command;
            assert(read(control, &command, 1) == 1);
            if (command == 'd') {
                while (count > 0) {
                    close(connections[--count]);
                }
            }
            assert(write(report, &counts, sizeof counts) == ssize_t(sizeof counts));
        }
    }
}

static int file;
static int control[2];
static int report[2];
static int handled = -1;

static Counts counted(char command = 'q') {
    Counts counts;
    assert(write(control[1], &command, 1) == 1 && read(report[0], &counts, sizeof counts) == ssize_t(sizeof counts));
    return counts;
}

static int size() {
    struct stat st;
    return fstat(file, &st) == 0 && st.st_size == 5 ? 0 : -1;
}

static void handler(int) {
    handled = size();
}

int main() {
    alarm(60);
    int sv[2];
    sv[1] = opened(device("kept_connections"), {openedFile, O_RDWR, 0, 0, 0}, sv[0]);
    assert(sv[1] >= 0 && pipe(control) == 0 && pipe(report) == 0);
    fflush(stdout);
    const pid_t server = fork();
    assert(server >= 0);
    if (server == 0) {
        close(sv[1]);
        serve(sv[0], control[0], report[1]);
    }
    close(sv[0]);
    file = sv[1];

    assert(size() == 0 && size() == 0 && size() == 0);
    Counts c = counted();
    printf("three calls: %d connection, %d FSTAT\n", c.connections, c.fstats);

    fflush(stdout);
    const pid_t other = fork();
    assert(other >= 0);
    if (other == 0) {
        _exit(size() == 0 && size() == 0 ? 0 : 1);
    }
    int status;
    assert(waitpid(other, &status, 0) == other && status == 0);
    c = counted();
    printf("another process's two: %d connections, %d FSTAT\n", c.connections, c.fstats);

    // The server keeps the READ and signals us: the handler's call finds
    // the connection in the READ and makes one of its own; the READ ends
    // with its CANCEL.
    struct sigaction action{};
    action.sa_handler = handler;
    assert(sigaction(SIGUSR1, &action, nullptr) == 0);
    char bytes[8];
    const ssize_t got = read(file, bytes, sizeof bytes);
    const int why = errno;
    c = counted();
    printf("a handler's call in a call: read %s, the handler's fstat %d, %d connections, %d FSTAT, %d READ, %d CANCEL\n",
           got == -1 && why == EINTR ? "EINTR" : "something else", handled, c.connections, c.fstats, c.reads, c.cancels);

    assert(size() == 0);
    c = counted();
    printf("the call after: %d connections, %d FSTAT\n", c.connections, c.fstats);

    counted('d');
    const int after = size();
    c = counted();
    printf("after the server let go of them: fstat %d, %d connections, %d FSTAT\n", after, c.connections, c.fstats);

    close(file);
    assert(waitpid(server, &status, 0) == server && status == 0);
    return 0;
}
