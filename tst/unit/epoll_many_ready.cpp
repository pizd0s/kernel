//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "a thousand ready at once are served, an event the price it is among a hundred\n"

// Many descriptors of an epoll ready at once, taken one event a wait, as
// a server that serves one request a turn takes them: what the kernel
// does for an event does not grow with how many more are ready. The set
// is level-triggered and each event is one receive, so every descriptor
// served is asked once more before the wait finds nothing.
//
// The price of an event among a thousand ready is held to three times
// its price among a hundred, the best of several rounds each: a list of
// the ready rebuilt at every wait made it eight times.

#include <assert.h>
#include <stdio.h>
#include <sys/epoll.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

static double now() {
    timespec t;
    assert(clock_gettime(CLOCK_MONOTONIC, &t) == 0);
    return t.tv_sec * 1e9 + t.tv_nsec;
}

// The least time an event took, of rounds in which count descriptors were
// made ready and served to the last.
static double served(int count, int rounds) {
    static int out[1000];
    assert(count <= 1000);
    const int ep = epoll_create1(0);
    assert(ep >= 0);
    for (int i = 0; i < count; ++i) {
        int sv[2];
        assert(socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_NONBLOCK, 0, sv) == 0);
        epoll_event ev{};
        ev.events = EPOLLIN;
        ev.data.fd = sv[0];
        assert(epoll_ctl(ep, EPOLL_CTL_ADD, sv[0], &ev) == 0);
        out[i] = sv[1];
    }
    double best = 0;
    char c = 'x';
    for (int r = 0; r < rounds; ++r) {
        for (int i = 0; i < count; ++i) {
            assert(write(out[i], &c, 1) == 1);
        }
        const double start = now();
        for (int left = count; left > 0; --left) {
            epoll_event ev;
            assert(epoll_wait(ep, &ev, 1, -1) == 1 && read(ev.data.fd, &c, 1) == 1);
        }
        epoll_event ev;
        assert(epoll_wait(ep, &ev, 1, 0) == 0);
        const double took = (now() - start) / count;
        if (r == 0 || took < best) {
            best = took;
        }
    }
    return best;
}

int main() {
    const rlimit limit{4096, 4096};
    assert(setrlimit(RLIMIT_NOFILE, &limit) == 0);
    const double hundred = served(100, 40);
    const double thousand = served(1000, 8);
    if (thousand > 3 * hundred) {
        printf("an event among a hundred ready %.0f ns, among a thousand %.0f ns\n", hundred, thousand);
        return 1;
    }
    printf("a thousand ready at once are served, an event the price it is among a hundred\n");
    return 0;
}
