//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "killed as it connects: by SIGKILL\nkilled in a call: by SIGTERM, by SIGKILL\na handler in a call: ran before the answer, then fstat 0 of 5 bytes\nstopped in a call: by SIGSTOP, continued, then fstat 0 of 5 bytes\n"

// A process takes its signals in a call to a server as in any wait: one
// that ends it ends it though the server never answers, its call or the
// connection it asks for first, a handler runs at once, and a stop stops
// it, the call going on after to its answer. The test serves a file
// itself (SO_SERVED) and holds the answer of an FSTAT back.

#include <assert.h>
#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/uio.h>
#include <sys/wait.h>
#include <unistd.h>

// The protocol's (lib/protocol/protocol.h): a packet's header, FSTAT and
// OK, and the stat an FSTAT's OK is.
struct Header {
    uint32_t op;
    uint32_t arg;
    uint32_t uid;
    uint32_t gid;
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

constexpr uint32_t opFstat = 67;
constexpr uint32_t opConnect = 76;
constexpr uint32_t opOk = 128;

static int servedFile(int sv[2]) {
    const struct served what{SERVED_FILE, O_RDWR, 0, 0, 0};
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, sv) != 0) {
        return -1;
    }
    return setsockopt(sv[1], SOL_SOCKET, SO_SERVED, &what, sizeof what);
}

// Takes the FSTAT of the file whose endpoint end is: the connection who
// asks wants first, a call pair of ours whose asking end the OK brings,
// then the FSTAT on it. The connection, where the answer goes: who asked
// waits for it from now on.
static int takeFstat(int end) {
    Header h;
    assert(recv(end, &h, sizeof h, 0) == ssize_t(sizeof h) && h.op == opConnect);
    int call[2];
    assert(socketpair(AF_UNIX, SOCK_CALL | SOCK_CLOEXEC, 0, call) == 0);
    const Header ok{opOk, 0, 0, 0};
    alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))] = {};
    iovec iov{const_cast<Header*>(&ok), sizeof ok};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    cmsghdr* c = CMSG_FIRSTHDR(&m);
    c->cmsg_level = SOL_SOCKET;
    c->cmsg_type = SCM_RIGHTS;
    c->cmsg_len = CMSG_LEN(sizeof(int));
    memcpy(CMSG_DATA(c), &call[1], sizeof(int));
    assert(sendmsg(end, &m, 0) == ssize_t(sizeof ok));
    close(call[1]);
    assert(recv(call[0], &h, sizeof h, 0) == ssize_t(sizeof h) && h.op == opFstat);
    return call[0];
}

// Answers an FSTAT on its connection: a regular file of 5 bytes.
static void answerFstat(int reply) {
    struct {
        Header h;
        Stat st;
    } answer{{opOk, 0, 0, 0}, {1, 5, 0, S_IFREG | 0644, 1, 0, 0, 0, 1, 0}};

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
    int sv[2];
    int status;

    // The server never answers who connects; a signal that ends who waits
    // for it ends it.
    assert(servedFile(sv) == 0);
    pid_t connecting = asker(sv[1], nullptr);
    Header first;
    assert(recv(sv[0], &first, sizeof first, 0) == ssize_t(sizeof first) && first.op == opConnect);
    assert(kill(connecting, SIGKILL) == 0);
    assert(waitpid(connecting, &status, 0) == connecting);
    printf("killed as it connects: by %s\n", WIFSIGNALED(status) && WTERMSIG(status) == SIGKILL ? "SIGKILL" : "something else");
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
