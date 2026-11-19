//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "a loop of sets: joins\nalone: nothing ready\na pipe in a: b reports a, a reports the pipe\ndrained: nothing ready\nthrough the loop: woken by a write of another process\na stale candidate, then another member: woken\nfour writers through the loop: 4000 bytes\n"

// Sets of epoll may hold each other: a loop of them is no error, and it
// makes none of them ready by itself. What is ready in a set of the loop
// reaches whoever waits on any of them; a set reports what is ready in
// it, not itself through the others. Whoever watches a set hears of news
// in it since they last looked, though the set still holds a member that
// was ready once and is not now. Linux refuses loops (ELOOP), so the test
// is the system's only.

#include <assert.h>
#include <errno.h>
#include <poll.h>
#include <stdio.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

static int add(int set, int fd) {
    epoll_event event{};
    event.events = EPOLLIN;
    event.data.fd = fd;
    return epoll_ctl(set, EPOLL_CTL_ADD, fd, &event);
}

// The fds a set reports as ready, into got: how many.
static int ready(int set, int* got, int timeout) {
    epoll_event events[8];
    const int n = epoll_wait(set, events, 8, timeout);
    assert(n >= 0);
    for (int i = 0; i < n; ++i) {
        got[i] = events[i].data.fd;
    }
    return n;
}

static void sleepMs(int ms) {
    const timespec t{0, ms * 1000000L};
    nanosleep(&t, nullptr);
}

// A child that writes a byte to fd after ms.
static pid_t later(int fd, int ms) {
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        sleepMs(ms);
        assert(write(fd, "x", 1) == 1);
        _exit(0);
    }
    return child;
}

static void waitFor(pid_t child) {
    int status;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
}

int main() {
    alarm(60);
    const int a = epoll_create1(0);
    const int b = epoll_create1(0);
    int p[2];
    assert(a >= 0 && b >= 0 && pipe(p) == 0 && add(a, p[0]) == 0);
    const bool joins = add(a, b) == 0 && add(b, a) == 0;
    printf("a loop of sets: %s\n", joins ? "joins" : "refused");

    int got[8];
    pollfd sets[2] = {{a, POLLIN, 0}, {b, POLLIN, 0}};
    const bool alone = ready(a, got, 0) == 0 && ready(b, got, 0) == 0 && poll(sets, 2, 0) == 0;
    printf("alone: %s\n", alone ? "nothing ready" : "something ready");

    assert(write(p[1], "x", 1) == 1);
    const bool fromB = ready(b, got, 0) == 1 && got[0] == a;
    const bool fromA = ready(a, got, 0) == 1 && got[0] == p[0];
    printf("a pipe in a: b reports %s, a reports %s\n", fromB ? "a" : "something else", fromA ? "the pipe" : "something else");

    char bytes[256];
    assert(read(p[0], bytes, 1) == 1);
    const bool drained = ready(a, got, 0) == 0 && ready(b, got, 0) == 0 && poll(sets, 2, 0) == 0;
    printf("drained: %s\n", drained ? "nothing ready" : "something ready");

    // Waiting in b, for a write of another process to the pipe in a.
    pid_t child = later(p[1], 50);
    const bool woken = ready(b, got, 5000) == 1 && got[0] == a;
    waitFor(child);
    assert(read(p[0], bytes, 1) == 1);
    printf("through the loop: %s\n", woken ? "woken by a write of another process" : "not woken");

    // A set in outer holds a member that was ready and is not: outer
    // looked and found nothing; then another member of it gets a byte.
    const int outer = epoll_create1(0);
    const int inner = epoll_create1(0);
    int q[2];
    int r[2];
    assert(outer >= 0 && inner >= 0 && pipe(q) == 0 && pipe(r) == 0);
    assert(add(inner, q[0]) == 0 && add(inner, r[0]) == 0 && add(outer, inner) == 0);
    assert(write(q[1], "x", 1) == 1 && ready(outer, got, 0) == 1);
    assert(read(q[0], bytes, 1) == 1 && ready(outer, got, 0) == 0);
    child = later(r[1], 50);
    const bool news = ready(outer, got, 5000) == 1 && got[0] == inner;
    waitFor(child);
    printf("a stale candidate, then another member: %s\n", news ? "woken" : "not woken");

    // Four processes write a thousand bytes each, one at a time, to pipes
    // of their own in a; we wait in b and take what a has.
    int w[4][2];
    pid_t writers[4];
    for (int i = 0; i < 4; ++i) {
        assert(pipe(w[i]) == 0 && add(a, w[i][0]) == 0);
    }
    for (int i = 0; i < 4; ++i) {
        fflush(stdout);
        writers[i] = fork();
        assert(writers[i] >= 0);
        if (writers[i] == 0) {
            for (int k = 0; k < 1000; ++k) {
                assert(write(w[i][1], "x", 1) == 1);
                if (k % 100 == 0) {
                    sleepMs(1);
                }
            }
            _exit(0);
        }
    }
    int total = 0;
    while (total < 4000 && ready(b, got, 5000) > 0) {
        const int n = ready(a, got, 0);
        for (int i = 0; i < n; ++i) {
            const ssize_t k = read(got[i], bytes, sizeof bytes);
            total += k > 0 ? int(k) : 0;
        }
    }
    for (const pid_t writer : writers) {
        waitFor(writer);
    }
    printf("four writers through the loop: %d bytes\n", total);
    return 0;
}
