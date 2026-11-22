//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "event descriptors: ok\n"

// eventfd and timerfd, Linux's. An eventfd: its counter read whole or,
// as a semaphore, by 1; a read at 0 and a write past 2^64 - 2 EAGAIN
// when non-blocking; 2^64 - 1, a read of fewer than 8 bytes and a write
// of other than 8 EINVAL; poll's IN
// and OUT; eventfd_read and eventfd_write; a blocking read a child's
// write ends. A timerfd: its expirations counted and read whole, a
// periodic one's missed ones too; timerfd_gettime's time left and
// interval; a setting resetting the count; TFD_TIMER_ABSTIME's past time
// at once and future one at it; disarming; a blocking read waiting for
// the expiry; the flags, clocks and descriptors it refuses.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <stdint.h>
#include <stdio.h>
#include <sys/eventfd.h>
#include <sys/timerfd.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

namespace {
    short revents(int fd, short events) {
        pollfd p = {fd, events, 0};
        assert(poll(&p, 1, 0) >= 0);
        return p.revents;
    }

    uint64_t value(int fd) {
        uint64_t v = 0;
        assert(read(fd, &v, 8) == 8);
        return v;
    }

    long long elapsed(const timespec& from) {
        timespec now;
        assert(clock_gettime(CLOCK_MONOTONIC, &now) == 0);
        return (now.tv_sec - from.tv_sec) * 1000LL + (now.tv_nsec - from.tv_nsec) / 1000000;
    }

    timespec later(timespec t, long ms) {
        t.tv_nsec += ms * 1000000;
        while (t.tv_nsec >= 1000000000) {
            t.tv_nsec -= 1000000000;
            t.tv_sec++;
        }
        return t;
    }
}

int main() {
    int e = eventfd(3, EFD_NONBLOCK | EFD_CLOEXEC);
    assert(e >= 0 && (fcntl(e, F_GETFD) & FD_CLOEXEC) && (fcntl(e, F_GETFL) & O_NONBLOCK));
    assert(revents(e, POLLIN | POLLOUT) == (POLLIN | POLLOUT));
    assert(value(e) == 3);
    uint64_t v;
    errno = 0;
    assert(read(e, &v, 8) == -1 && errno == EAGAIN && revents(e, POLLIN | POLLOUT) == POLLOUT);
    v = 5;
    assert(write(e, &v, 8) == 8 && write(e, &v, 8) == 8 && value(e) == 10);
    errno = 0;
    assert(read(e, &v, 4) == -1 && errno == EINVAL);
    errno = 0;
    assert(write(e, &v, 4) == -1 && errno == EINVAL);
    v = UINT64_MAX;
    errno = 0;
    assert(write(e, &v, 8) == -1 && errno == EINVAL);
    v = UINT64_MAX - 1;
    assert(write(e, &v, 8) == 8 && revents(e, POLLIN | POLLOUT) == POLLIN);
    v = 1;
    errno = 0;
    assert(write(e, &v, 8) == -1 && errno == EAGAIN);
    assert(value(e) == UINT64_MAX - 1);
    v = 0;
    assert(write(e, &v, 8) == 8 && revents(e, POLLIN) == 0);
    uint64_t pair[2] = {7, 99};
    errno = 0;
    assert(write(e, pair, 16) == -1 && errno == EINVAL);
    assert(write(e, pair, 8) == 8 && read(e, pair, 16) == 8 && pair[0] == 7);
    assert(eventfd_write(e, 42) == 0);
    eventfd_t got = 0;
    assert(eventfd_read(e, &got) == 0 && got == 42);
    errno = 0;
    assert(eventfd_read(e, &got) == -1 && errno == EAGAIN);
    close(e);

    int s = eventfd(2, EFD_SEMAPHORE | EFD_NONBLOCK);
    assert(s >= 0 && value(s) == 1 && value(s) == 1);
    errno = 0;
    assert(read(s, &v, 8) == -1 && errno == EAGAIN);
    close(s);
    errno = 0;
    assert(eventfd(0, 0x100) == -1 && errno == EINVAL);

    int b = eventfd(0, 0);
    assert(b >= 0);
    pid_t pid = fork();
    if (pid == 0) {
        usleep(20000);
        _exit(eventfd_write(b, 9) == 0 ? 0 : 1);
    }
    assert(pid > 0 && eventfd_read(b, &got) == 0 && got == 9);
    int status;
    assert(waitpid(pid, &status, 0) == pid && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    close(b);

    int t = timerfd_create(CLOCK_MONOTONIC, TFD_NONBLOCK | TFD_CLOEXEC);
    assert(t >= 0 && (fcntl(t, F_GETFD) & FD_CLOEXEC));
    itimerspec spec = {};
    itimerspec old;
    assert(timerfd_gettime(t, &old) == 0 && old.it_value.tv_sec == 0 && old.it_value.tv_nsec == 0);
    errno = 0;
    assert(read(t, &v, 8) == -1 && errno == EAGAIN && revents(t, POLLIN) == 0);
    spec.it_value.tv_nsec = 30 * 1000000;
    assert(timerfd_settime(t, 0, &spec, &old) == 0 && old.it_value.tv_sec == 0 && old.it_value.tv_nsec == 0);
    assert(timerfd_gettime(t, &old) == 0 && old.it_value.tv_sec == 0);
    assert(old.it_value.tv_nsec > 0 && old.it_value.tv_nsec <= 30 * 1000000 && old.it_interval.tv_nsec == 0);
    pollfd p = {t, POLLIN, 0};
    assert(poll(&p, 1, 2000) == 1 && p.revents == POLLIN && value(t) == 1);
    assert(timerfd_gettime(t, &old) == 0 && old.it_value.tv_sec == 0 && old.it_value.tv_nsec == 0);

    spec.it_value.tv_nsec = 10 * 1000000;
    spec.it_interval.tv_nsec = 10 * 1000000;
    assert(timerfd_settime(t, 0, &spec, nullptr) == 0);
    usleep(55000);
    uint64_t n = value(t);
    assert(n >= 4);
    assert(timerfd_gettime(t, &old) == 0 && old.it_interval.tv_sec == 0 && old.it_interval.tv_nsec == 10 * 1000000);
    itimerspec zero = {};
    assert(timerfd_settime(t, 0, &zero, &old) == 0 && old.it_interval.tv_nsec == 10 * 1000000);
    assert(timerfd_gettime(t, &old) == 0 && old.it_value.tv_sec == 0 && old.it_value.tv_nsec == 0);
    assert(old.it_interval.tv_nsec == 0);
    spec.it_interval.tv_nsec = 0;
    spec.it_value.tv_nsec = 1000000;
    assert(timerfd_settime(t, 0, &spec, nullptr) == 0 && poll(&p, 1, 2000) == 1);
    spec.it_value.tv_sec = 100;
    assert(timerfd_settime(t, 0, &spec, nullptr) == 0);
    errno = 0;
    assert(read(t, &v, 8) == -1 && errno == EAGAIN);

    timespec now;
    assert(clock_gettime(CLOCK_MONOTONIC, &now) == 0);
    spec = {};
    spec.it_value = now;
    spec.it_value.tv_sec -= 1;
    assert(timerfd_settime(t, TFD_TIMER_ABSTIME, &spec, nullptr) == 0);
    assert(poll(&p, 1, 2000) == 1 && value(t) == 1);
    assert(clock_gettime(CLOCK_MONOTONIC, &now) == 0);
    spec.it_value = later(now, 40);
    assert(timerfd_settime(t, TFD_TIMER_ABSTIME | TFD_TIMER_CANCEL_ON_SET, &spec, nullptr) == 0);
    assert(poll(&p, 1, 2000) == 1 && value(t) == 1 && elapsed(now) >= 39);

    errno = 0;
    assert(timerfd_settime(t, 4, &spec, nullptr) == -1 && errno == EINVAL);
    spec.it_value.tv_nsec = 1000000000;
    errno = 0;
    assert(timerfd_settime(t, 0, &spec, nullptr) == -1 && errno == EINVAL);
    errno = 0;
    assert(read(t, &v, 4) == -1 && errno == EINVAL);
    errno = 0;
    assert(write(t, &v, 8) == -1 && errno == EINVAL);
    close(t);
    errno = 0;
    assert(timerfd_create(12345, 0) == -1 && errno == EINVAL);
    errno = 0;
    assert(timerfd_create(CLOCK_MONOTONIC, 0x100) == -1 && errno == EINVAL);
    int other = eventfd(0, 0);
    errno = 0;
    assert(timerfd_gettime(other, &old) == -1 && errno == EINVAL);
    close(other);
    errno = 0;
    assert(timerfd_gettime(-1, &old) == -1 && errno == EBADF);

    int w = timerfd_create(CLOCK_REALTIME, 0);
    assert(w >= 0);
    spec = {};
    spec.it_value.tv_nsec = 20 * 1000000;
    timespec start;
    assert(clock_gettime(CLOCK_MONOTONIC, &start) == 0 && timerfd_settime(w, 0, &spec, nullptr) == 0);
    assert(value(w) == 1 && elapsed(start) >= 19);
    timespec wall;
    assert(clock_gettime(CLOCK_REALTIME, &wall) == 0);
    spec.it_value = later(wall, 30);
    assert(timerfd_settime(w, TFD_TIMER_ABSTIME, &spec, nullptr) == 0 && value(w) == 1);
    close(w);
    int boot = timerfd_create(CLOCK_BOOTTIME, 0);
    assert(boot >= 0 && close(boot) == 0);

    printf("event descriptors: ok\n");
}
