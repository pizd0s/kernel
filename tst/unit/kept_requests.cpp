//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "three calls: 3 FSTAT\nanother process's two: 5 FSTAT\na handler's call in a call: read EINTR, the handler's fstat 0, 6 FSTAT, 1 READ, 1 CANCEL\nthe call after: 7 FSTAT\na request let go of unanswered: EIO\na request handed on: answered by who it was handed to, fstat 0\na request handed on to one that dies: EIO\nan open's answer whose asker died: its file let go of\n"

// A call on an open file of a server is a request sent on the open
// file's endpoint, which brings where it is answered, the last of its
// descriptors (lib/protocol/protocol.h, bin/vm/answer.go): the server
// writes the answer there and closes it, which ends the answer. Every
// process that holds the file sends on the same endpoint, and a call a
// handler makes while a call of the process waits at the server is one
// more request. A request the server keeps hears the CANCEL of who
// asked there; one closed with nothing written is EIO to who asked,
// whoever closed it last: the server, or a process the server handed
// it on to, alive or dead. What an answer brought is let go of when who
// asked dies before taking it. The test serves a file itself, of a
// device of its own, and counts what comes.

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
constexpr uint32_t opErr = 129;

// What the server counted.
struct Counts {
    int fstats;
    int reads;
    int cancels;
};

// A request off the open file's endpoint: its header, and where it is
// answered, the last of its descriptors; one before it, a READ's memfd,
// is closed, as the test reads nothing into it. False at the end of the
// endpoint.
static bool request(int end, Header& h, int& reply) {
    unsigned char packet[64];
    const ssize_t n = receiveWith(end, packet, sizeof packet, reply);
    if (n < ssize_t(sizeof h)) {
        return false;
    }
    memcpy(&h, packet, sizeof h);
    return true;
}

static void answerFstat(int reply) {
    struct {
        Header h;
        Stat st;
    } answer{{opOk, 0, 0, 0}, {1, 5, 0, S_IFREG | 0644, 1, 0, 0, 0, 1, 0}};

    assert(send(reply, &answer, sizeof answer, MSG_NOSIGNAL) == ssize_t(sizeof answer));
    close(reply);
}

static int control[2], report[2];

// What the next FSTAT gets: its answer; nothing, its descriptor closed;
// its answer from a child it was handed on to; or that child's death.
enum : char { answered = 'a', dropped = 'd', handed = 'h', died = 'x' };

// Serves the file whose endpoint end is until it closes: FSTAT as the
// mode says, a regular file of 5 bytes, and a READ kept until its
// CANCEL, EINTR then, the reader, our parent, signalled as it comes. A
// byte on control is a question of the counts, answered on report, and
// the mode from then on.
[[noreturn]] static void serve(int end) {
    Counts counts{};
    char mode = answered;
    int kept = -1;
    for (;;) {
        pollfd fds[3] = {{end, POLLIN, 0}, {control[0], POLLIN, 0}, {kept, POLLIN, 0}};
        assert(poll(fds, kept >= 0 ? 3 : 2, -1) > 0);
        if (kept >= 0 && fds[2].revents != 0) {
            // What comes where a kept request is answered: its CANCEL.
            Header h;
            assert(recv(kept, &h, sizeof h, 0) == ssize_t(sizeof h) && h.op == opCancel);
            ++counts.cancels;
            const Header interrupted{opErr, EINTR, 0, 0};
            assert(send(kept, &interrupted, sizeof interrupted, MSG_NOSIGNAL) == ssize_t(sizeof interrupted));
            close(kept);
            kept = -1;
        } else if (fds[0].revents != 0) {
            Header h;
            int reply;
            if (!request(end, h, reply)) {
                _exit(0);
            }
            assert(reply >= 0);
            if (h.op == opRead) {
                ++counts.reads;
                kept = reply;
                assert(kill(getppid(), SIGUSR1) == 0);
            } else if (h.op == opFstat && mode == answered) {
                ++counts.fstats;
                answerFstat(reply);
            } else if (h.op == opFstat && mode == dropped) {
                close(reply);
            } else if (h.op == opFstat) {
                // Handed on: the child has where to answer, and we let
                // go of ours.
                int carrier[2];
                assert(socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, carrier) == 0);
                const pid_t child = fork();
                assert(child >= 0);
                if (child == 0) {
                    char byte;
                    int passed;
                    assert(receiveWith(carrier[1], &byte, 1, passed) == 1 && passed >= 0);
                    if (mode == handed) {
                        answerFstat(passed);
                    }
                    _exit(0);
                }
                sendWith(carrier[0], "r", 1, reply);
                close(reply);
                close(carrier[0]);
                close(carrier[1]);
                int status;
                assert(waitpid(child, &status, 0) == child);
            } else {
                close(reply);
            }
        } else if (fds[1].revents != 0) {
            assert(read(control[0], &mode, 1) == 1);
            assert(write(report[1], &counts, sizeof counts) == ssize_t(sizeof counts));
        }
    }
}

static int file;

// The stat of the file: 0 for the 5 bytes the server says, else the errno.
static int size() {
    struct stat st;
    if (fstat(file, &st) != 0) {
        return errno;
    }
    return st.st_size == 5 ? 0 : -1;
}

// The counts so far, and the server's mode from now on.
static Counts counted(char mode = answered) {
    Counts c;
    assert(write(control[1], &mode, 1) == 1 && read(report[0], &c, sizeof c) == ssize_t(sizeof c));
    return c;
}

static int handled = -1;

static void handler(int) {
    handled = size();
}

int main() {
    alarm(60);
    int sv[2];
    const int channel = device("kept_requests");
    sv[1] = opened(channel, {openedFile, O_RDWR, 0, 0, 0}, sv[0]);
    assert(sv[1] >= 0 && pipe(control) == 0 && pipe(report) == 0);
    fflush(stdout);
    const pid_t server = fork();
    assert(server >= 0);
    if (server == 0) {
        close(sv[1]);
        serve(sv[0]);
    }
    close(sv[0]);
    file = sv[1];

    assert(size() == 0 && size() == 0 && size() == 0);
    Counts c = counted();
    printf("three calls: %d FSTAT\n", c.fstats);

    fflush(stdout);
    const pid_t other = fork();
    assert(other >= 0);
    if (other == 0) {
        _exit(size() == 0 && size() == 0 ? 0 : 1);
    }
    int status;
    assert(waitpid(other, &status, 0) == other && status == 0);
    c = counted();
    printf("another process's two: %d FSTAT\n", c.fstats);

    // The server keeps the READ and signals us: the handler's call is a
    // request of its own, answered while the READ waits; the READ ends
    // with its CANCEL.
    struct sigaction action{};
    action.sa_handler = handler;
    assert(sigaction(SIGUSR1, &action, nullptr) == 0);
    char bytes[8];
    const ssize_t got = read(file, bytes, sizeof bytes);
    const int why = errno;
    c = counted();
    printf("a handler's call in a call: read %s, the handler's fstat %d, %d FSTAT, %d READ, %d CANCEL\n",
           got == -1 && why == EINTR ? "EINTR" : "something else", handled, c.fstats, c.reads, c.cancels);

    assert(size() == 0);
    c = counted(dropped);
    printf("the call after: %d FSTAT\n", c.fstats);

    const int let = size();
    counted(handed);
    printf("a request let go of unanswered: %s\n", let == EIO ? "EIO" : "something else");

    const int passed = size();
    counted(died);
    printf("a request handed on: answered by who it was handed to, fstat %d\n", passed);

    const int lost = size();
    printf("a request handed on to one that dies: %s\n", lost == EIO ? "EIO" : "something else");

    // We answer an OPEN with an open file, then kill who opened before we
    // close where it is answered: the open file goes with the answer.
    fflush(stdout);
    const pid_t opener = fork();
    assert(opener >= 0);
    if (opener == 0) {
        open(devicePath, O_RDWR);
        _exit(0);
    }
    unsigned char packet[64];
    int reply;
    assert(receiveWith(channel, packet, sizeof packet, reply) >= ssize_t(sizeof(Header)) && reply >= 0);
    int pair[2];
    assert(socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) == 0);

    struct {
        Header h;
        Opened what;
    } ok{{opOk, 0, 0, 0}, {openedFile, O_RDWR, 0, 0, 0}};

    sendWith(reply, &ok, sizeof ok, pair[1]);
    close(pair[1]);
    assert(kill(opener, SIGKILL) == 0 && waitpid(opener, &status, 0) == opener);
    close(reply);
    pollfd hup{pair[0], POLLIN, 0};
    const bool released = poll(&hup, 1, 2000) == 1 && (hup.revents & POLLHUP) != 0;
    printf("an open's answer whose asker died: %s\n", released ? "its file let go of" : "its file held");
    close(pair[0]);

    close(file);
    assert(waitpid(server, &status, 0) == server && status == 0);
    return 0;
}
