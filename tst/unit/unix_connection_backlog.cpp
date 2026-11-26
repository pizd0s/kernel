//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "unix connection backlog: ok\n"

// A listener's queue: a connect waits for nothing, its connection is in
// the queue at once or it is refused, and accept takes the connections
// in their order. accept waits: through the signals of a handler that
// restarts it, and until one of a handler that does not.

#include <assert.h>
#include <errno.h>
#include <signal.h>
#include <stddef.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <unistd.h>

// The real-time timer: its first SIGALRM after first microseconds, the next
// ones each interval after, none at 0.
static void alarmIn(long first, long interval) {
    itimerval timer{{0, interval}, {0, first}};
    assert(setitimer(ITIMER_REAL, &timer, nullptr) == 0);
}

static volatile sig_atomic_t interrupts;

static void interrupted(int) {
    interrupts = interrupts + 1;
    if (interrupts > 1000) {
        _exit(89);
    }
}

int main() {
    int listener = socket(AF_UNIX, SOCK_STREAM, 0);
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    strcpy(address.sun_path, "backlog.sock");
    socklen_t length = sizeof(address);
    sockaddr* name = reinterpret_cast<sockaddr*>(&address);
    assert(listener >= 0 && bind(listener, name, length) == 0);
    assert(listen(listener, 0) == 0);
    int queued = socket(AF_UNIX, SOCK_STREAM, 0);
    assert(queued >= 0 && connect(queued, name, length) == 0);
    assert(send(queued, "q", 1, 0) == 1);
#ifdef __wasm__
    // The queue is full, and a listener may refuse what it has no room for.
    int refused = socket(AF_UNIX, SOCK_STREAM, 0);
    assert(refused >= 0 && connect(refused, name, length) == -1 && errno == ECONNREFUSED);
    assert(close(refused) == 0);
    refused = socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK, 0);
    assert(refused >= 0 && connect(refused, name, length) == -1 && errno == ECONNREFUSED);
    assert(close(refused) == 0);
#endif
    int first = accept(listener, nullptr, nullptr);
    char byte = 0;
    assert(first >= 0 && recv(first, &byte, 1, 0) == 1 && byte == 'q');
    assert(close(first) == 0 && close(queued) == 0);

    // There is room again.
    int client = socket(AF_UNIX, SOCK_STREAM, 0);
    assert(client >= 0 && connect(client, name, length) == 0);
    int next = accept(listener, nullptr, nullptr);
    assert(next >= 0 && send(client, "x", 1, 0) == 1 && recv(next, &byte, 1, 0) == 1 && byte == 'x');
    assert(close(next) == 0 && close(client) == 0);

    // A handler that does not restart ends the wait of accept.
    struct sigaction action{}, saved{};
    action.sa_handler = interrupted;
    assert(sigemptyset(&action.sa_mask) == 0 && sigaction(SIGALRM, &action, &saved) == 0);
    alarmIn(20000, 20000);
    assert(accept(listener, nullptr, nullptr) == -1 && errno == EINTR);
    alarmIn(0, 0);
    assert(interrupts > 0);

    // One that restarts leaves accept waiting, for a connection that comes later.
    action.sa_flags = SA_RESTART;
    assert(sigaction(SIGALRM, &action, nullptr) == 0);
    interrupts = 0;
    pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        assert(signal(SIGALRM, SIG_DFL) != SIG_ERR && close(listener) == 0 && usleep(100000) == 0);
        int late = socket(AF_UNIX, SOCK_STREAM, 0);
        assert(late >= 0 && connect(late, name, length) == 0);
        assert(send(late, "y", 1, 0) == 1 && recv(late, &byte, 1, 0) == 1 && byte == 'z');
        _exit(17);
    }
    alarmIn(20000, 20000);
    next = accept(listener, nullptr, nullptr);
    alarmIn(0, 0);
    assert(next >= 0 && interrupts > 0 && sigaction(SIGALRM, &saved, nullptr) == 0);
    assert(recv(next, &byte, 1, 0) == 1 && byte == 'y' && send(next, "z", 1, 0) == 1);
    int status;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 17);
    assert(close(next) == 0 && close(listener) == 0 && unlink("backlog.sock") == 0);
    puts("unix connection backlog: ok");
}
