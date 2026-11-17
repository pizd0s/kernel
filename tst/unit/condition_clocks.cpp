//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "condition clocks: ok\n"

#include <assert.h>
#include <errno.h>
#include <pthread.h>
#include <stdio.h>
#include <time.h>

// elapsed is the milliseconds of CLOCK_MONOTONIC since from.
static long elapsed(const timespec& from) {
    timespec now;
    assert(clock_gettime(CLOCK_MONOTONIC, &now) == 0);
    return (now.tv_sec - from.tv_sec) * 1000 + (now.tv_nsec - from.tv_nsec) / 1000000;
}

// later is the time of a clock ms milliseconds from now.
static timespec later(clockid_t clock, long ms) {
    timespec at;
    assert(clock_gettime(clock, &at) == 0);
    at.tv_nsec += ms * 1000000;
    at.tv_sec += at.tv_nsec / 1000000000;
    at.tv_nsec %= 1000000000;
    return at;
}

int main() {
    // A condition variable's clock is CLOCK_REALTIME until another is set.
    pthread_condattr_t attr;
    clockid_t clock = -1;
    assert(pthread_condattr_init(&attr) == 0);
    assert(pthread_condattr_getclock(&attr, &clock) == 0 && clock == CLOCK_REALTIME);
    assert(pthread_condattr_setclock(&attr, CLOCK_MONOTONIC) == 0);
    assert(pthread_condattr_getclock(&attr, &clock) == 0 && clock == CLOCK_MONOTONIC);
    assert(pthread_condattr_setclock(&attr, CLOCK_PROCESS_CPUTIME_ID) == EINVAL);
    assert(pthread_condattr_setclock(&attr, CLOCK_THREAD_CPUTIME_ID) == EINVAL);
    assert(pthread_condattr_setclock(&attr, -1) == EINVAL);
    assert(pthread_condattr_getclock(&attr, &clock) == 0 && clock == CLOCK_MONOTONIC);

    // A timed wait nobody signals ends at its deadline, a time of that clock.
    pthread_cond_t monotonic;
    pthread_mutex_t mutex = PTHREAD_MUTEX_INITIALIZER;
    assert(pthread_cond_init(&monotonic, &attr) == 0 && pthread_condattr_destroy(&attr) == 0);
    assert(pthread_mutex_lock(&mutex) == 0);
    timespec start;
    assert(clock_gettime(CLOCK_MONOTONIC, &start) == 0);
    timespec at = later(CLOCK_MONOTONIC, 60);
    assert(pthread_cond_timedwait(&monotonic, &mutex, &at) == ETIMEDOUT);
    long waited = elapsed(start);
    assert(waited >= 55 && waited < 5000);

    // A deadline already past ends the wait at once.
    assert(clock_gettime(CLOCK_MONOTONIC, &start) == 0);
    at = later(CLOCK_MONOTONIC, 0);
    assert(pthread_cond_timedwait(&monotonic, &mutex, &at) == ETIMEDOUT && elapsed(start) < 1000);

    // The default clock's deadline is a time of CLOCK_REALTIME.
    pthread_cond_t realtime = PTHREAD_COND_INITIALIZER;
    assert(clock_gettime(CLOCK_MONOTONIC, &start) == 0);
    at = later(CLOCK_REALTIME, 60);
    assert(pthread_cond_timedwait(&realtime, &mutex, &at) == ETIMEDOUT);
    waited = elapsed(start);
    assert(waited >= 55 && waited < 5000);

    // The mutex is the caller's again after each wait.
    assert(pthread_mutex_unlock(&mutex) == 0);
    assert(pthread_cond_destroy(&monotonic) == 0 && pthread_cond_destroy(&realtime) == 0);
    puts("condition clocks: ok");
}
