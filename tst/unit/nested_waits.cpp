//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "a read a handler waited inside of: its bytes after the handler's\nkilled in accept, in a pair's receive, in a pipe's read: each by SIGKILL, the rest goes on\n"

// A call that waits and what comes to it while its process is elsewhere.
// A handler that runs in a waiting read and waits itself, on another
// pipe, takes nothing from the read: what was written for the read while
// the handler waited is there when it returns. And a process killed as
// it waits, in accept, in a socket's receive, in a pipe's read, ends
// alone: who is left goes on.

#include <assert.h>
#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <unistd.h>

static int entered[2], inner[2];

// Tells that it runs, then waits for its own byte.
static void handler(int) {
    char c;
    assert(write(entered[1], "h", 1) == 1);
    assert(read(inner[0], &c, 1) == 1 && c == 'i');
}

// A child that waits as wait says, killed once it had the time to.
static int killedIn(void (*wait)()) {
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        wait();
        _exit(1);
    }
    usleep(50000);
    assert(kill(child, SIGKILL) == 0);
    int status;
    assert(waitpid(child, &status, 0) == child);
    return WIFSIGNALED(status) ? WTERMSIG(status) : 0;
}

static void inAccept() {
    sockaddr_un name{};
    name.sun_family = AF_UNIX;
    snprintf(name.sun_path, sizeof name.sun_path, "nested_waits.%d", int(getpid()));
    const int s = socket(AF_UNIX, SOCK_STREAM, 0);
    assert(s >= 0 && bind(s, reinterpret_cast<sockaddr*>(&name), sizeof name) == 0 && listen(s, 1) == 0);
    accept(s, nullptr, nullptr);
}

static void inReceive() {
    int sv[2];
    char c;
    assert(socketpair(AF_UNIX, SOCK_SEQPACKET, 0, sv) == 0);
    recv(sv[0], &c, 1, 0);
}

static void inRead() {
    int ends[2];
    char c;
    assert(pipe(ends) == 0);
    read(ends[0], &c, 1);
}

int main() {
    int outer[2];
    assert(pipe(outer) == 0 && pipe(entered) == 0 && pipe(inner) == 0);
    struct sigaction action{};
    action.sa_handler = handler;
    action.sa_flags = SA_RESTART;
    assert(sigaction(SIGUSR1, &action, nullptr) == 0);
    const pid_t parent = getpid();
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        // The parent is in its read by now, or takes the signal on the
        // way in; either way the handler runs before the read has bytes.
        char c;
        usleep(50000);
        assert(kill(parent, SIGUSR1) == 0);
        assert(read(entered[0], &c, 1) == 1);
        assert(write(outer[1], "outer", 5) == 5);
        assert(write(inner[1], "i", 1) == 1);
        _exit(0);
    }
    char bytes[8] = {};
    assert(read(outer[0], bytes, sizeof bytes) == 5 && memcmp(bytes, "outer", 5) == 0);
    int status;
    assert(waitpid(child, &status, 0) == child && status == 0);
    printf("a read a handler waited inside of: its bytes after the handler's\n");

    assert(killedIn(inAccept) == SIGKILL && killedIn(inReceive) == SIGKILL && killedIn(inRead) == SIGKILL);
    assert(pipe(outer) == 0 && write(outer[1], "x", 1) == 1 && read(outer[0], bytes, 1) == 1);
    printf("killed in accept, in a pair's receive, in a pipe's read: each by SIGKILL, the rest goes on\n");
    return 0;
}
