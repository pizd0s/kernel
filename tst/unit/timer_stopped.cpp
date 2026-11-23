//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "a stopped timer sends nothing: 10000 times\n"

// A timer that is stopped sends no signal after the call that stops it
// returns: what it sent before is taken by then, with the handler the
// process still has. So the default action, put back right after, is
// never met by a late SIGALRM, which would end the process. The timer is
// stopped about when it fires, a little before or after.

#include <assert.h>
#include <signal.h>
#include <stdio.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

// The real-time timer: its first SIGALRM after first microseconds, the next
// ones each interval after, none at 0.
static void alarmIn(long first, long interval) {
    itimerval timer{{0, interval}, {0, first}};
    assert(setitimer(ITIMER_REAL, &timer, nullptr) == 0);
}

static volatile sig_atomic_t ran;

static void handler(int) {
    ran = 1;
}

static long long now() {
    timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec * 1000000000LL + ts.tv_nsec;
}

static void wait(long long nanoseconds) {
    const long long until = now() + nanoseconds;
    while (now() < until) {
    }
}

int main() {
    struct sigaction caught{}, plain{};
    caught.sa_handler = handler;
    plain.sa_handler = SIG_DFL;
    assert(sigemptyset(&caught.sa_mask) == 0 && sigemptyset(&plain.sa_mask) == 0);
    const int times = 10000;
    for (int i = 0; i < times; ++i) {
        assert(sigaction(SIGALRM, &caught, nullptr) == 0);
        alarmIn(50, 50);
        wait(30000 + i % 40 * 1000);
        alarmIn(0, 0);
        assert(sigaction(SIGALRM, &plain, nullptr) == 0);
        wait(20000);
    }
    assert(ran);
    printf("a stopped timer sends nothing: %d times\n", times);
    return 0;
}
