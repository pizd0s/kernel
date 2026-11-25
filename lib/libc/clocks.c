/*
 * The clocks and the sleeps over clock_gettime, nanosleep and setitimer:
 * time and gettimeofday are CLOCK_REALTIME's, clock is the process's CPU
 * time in microseconds; sleep and alarm give the seconds that were left,
 * rounded up, and keep errno; usleep is EINTR where a signal ends it.
 */
#include "sized.h"

#include <errno.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>

unsigned __kernel_sleep(unsigned seconds) {
    struct timespec want = {seconds, 0}, left = {0, 0};
    int saved = errno;

    if (nanosleep(&want, &left) == 0) {
        left = (struct timespec){0, 0};
    }

    errno = saved;

    return left.tv_sec + (left.tv_nsec > 0);
}

int __kernel_usleep(useconds_t us) {
    struct timespec want = {us / 1000000, us % 1000000 * 1000};

    return nanosleep(&want, NULL);
}

unsigned __kernel_alarm(unsigned seconds) {
    struct itimerval value = {{0, 0}, {seconds, 0}}, old;
    int saved = errno;

    if (setitimer(ITIMER_REAL, &value, &old) != 0) {
        errno = saved;

        return 0;
    }

    return old.it_value.tv_sec + (old.it_value.tv_usec > 0);
}

clock_t __kernel_clock(void) {
    struct timespec t;

    if (clock_gettime(CLOCK_PROCESS_CPUTIME_ID, &t) != 0) {
        return -1;
    }

    return (clock_t)t.tv_sec * 1000000 + t.tv_nsec / 1000;
}

time_t __kernel_time(time_t* at) {
    struct timespec t;

    clock_gettime(CLOCK_REALTIME, &t);

    if (at) {
        *at = t.tv_sec;
    }

    return t.tv_sec;
}

int __kernel_gettimeofday(struct timeval* restrict tv, size_t size, void* restrict zone) {
    sized("gettimeofday", size, sizeof(struct timeval));

    if (tv) {
        struct timespec t;

        clock_gettime(CLOCK_REALTIME, &t);
        *tv = (struct timeval){t.tv_sec, t.tv_nsec / 1000};
    }

    return 0;
}

