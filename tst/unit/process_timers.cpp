//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "process timers: ok\n"

// POSIX's timers and what a signal comes with. timer_create with no
// sigevent sends SIGALRM with the timer's id as its value; with one its
// signal and value, SI_TIMER, or nothing with SIGEV_NONE; timer_settime
// from now or at a time of its clock, a past one at once, periodic, its
// old setting returned, 0 disarming it; timer_gettime's time left;
// expirations while its signal is pending counted, in si_overrun and
// timer_getoverrun; timer_delete; a child of fork has no timers.
// sigqueue's SI_QUEUE and value, kill's SI_USER, both with the sender's
// si_pid and si_uid; a real-time signal queued an instance each, in
// order, a standard one pending once with its first information;
// sigwaitinfo's and sigtimedwait's information.

#include <assert.h>
#include <errno.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

namespace {
    struct Seen {
        int signo;
        int code;
        int value;
        pid_t pid;
        uid_t uid;
    };

    volatile int count;
    Seen seen[16];

    void caught(int signo, siginfo_t* info, void*) {
        if (count < 16) {
            seen[count] = {signo, info->si_code, info->si_value.sival_int, info->si_pid, info->si_uid};
        }
        count = count + 1;
    }

    void handle(int signo) {
        struct sigaction a = {};
        a.sa_sigaction = caught;
        a.sa_flags = SA_SIGINFO;
        sigemptyset(&a.sa_mask);
        assert(sigaction(signo, &a, nullptr) == 0);
    }

    sigset_t only(int signo) {
        sigset_t set;
        sigemptyset(&set);
        sigaddset(&set, signo);
        return set;
    }

    void block(int signo, int how) {
        sigset_t set = only(signo);
        assert(sigprocmask(how, &set, nullptr) == 0);
    }

    void await(int n) {
        for (int i = 0; i < 2000 && count < n; i++) {
            usleep(1000);
        }
        assert(count >= n);
    }

    itimerspec after(long ms, long every) {
        itimerspec s = {};
        s.it_value.tv_sec = ms / 1000;
        s.it_value.tv_nsec = ms % 1000 * 1000000;
        s.it_interval.tv_sec = every / 1000;
        s.it_interval.tv_nsec = every % 1000 * 1000000;
        return s;
    }

    timespec later(timespec t, long ms) {
        t.tv_nsec += ms * 1000000;
        while (t.tv_nsec >= 1000000000) {
            t.tv_nsec -= 1000000000;
            t.tv_sec++;
        }
        return t;
    }

    long long elapsed(const timespec& from) {
        timespec now;
        assert(clock_gettime(CLOCK_MONOTONIC, &now) == 0);
        return (now.tv_sec - from.tv_sec) * 1000LL + (now.tv_nsec - from.tv_nsec) / 1000000;
    }
}

int main() {
    handle(SIGALRM);
    handle(SIGUSR2);
    handle(SIGRTMIN);

    timer_t plain;
    assert(timer_create(CLOCK_MONOTONIC, nullptr, &plain) == 0);
    itimerspec spec = after(10, 0);
    itimerspec old;
    assert(timer_settime(plain, 0, &spec, &old) == 0 && old.it_value.tv_sec == 0 && old.it_value.tv_nsec == 0);
    await(1);
    assert(seen[0].signo == SIGALRM && seen[0].code == SI_TIMER && seen[0].value == (int)(intptr_t)plain);
    assert(timer_gettime(plain, &old) == 0 && old.it_value.tv_sec == 0 && old.it_value.tv_nsec == 0);
    assert(timer_delete(plain) == 0);
    errno = 0;
    assert(timer_gettime(plain, &old) == -1 && errno == EINVAL);

    sigevent ev = {};
    ev.sigev_notify = SIGEV_SIGNAL;
    ev.sigev_signo = SIGUSR1;
    ev.sigev_value.sival_int = 77;
    timer_t periodic;
    assert(timer_create(CLOCK_REALTIME, &ev, &periodic) == 0);
    block(SIGUSR1, SIG_BLOCK);
    spec = after(5, 5);
    assert(timer_settime(periodic, 0, &spec, nullptr) == 0);
    usleep(60000);
    assert(timer_gettime(periodic, &old) == 0 && old.it_interval.tv_sec == 0 && old.it_interval.tv_nsec == 5000000);
    sigset_t usr1 = only(SIGUSR1);
    siginfo_t info;
    timespec wait = {2, 0};
    assert(sigtimedwait(&usr1, &info, &wait) == SIGUSR1 && info.si_code == SI_TIMER && info.si_value.sival_int == 77);
    assert(info.si_overrun >= 5 && timer_getoverrun(periodic) == info.si_overrun);
    itimerspec zero = {};
    assert(timer_settime(periodic, 0, &zero, &old) == 0 && old.it_interval.tv_nsec == 5000000);
    assert(timer_delete(periodic) == 0);
    timespec none = {0, 0};
    sigtimedwait(&usr1, &info, &none);
    block(SIGUSR1, SIG_UNBLOCK);

    count = 0;
    ev = {};
    ev.sigev_notify = SIGEV_NONE;
    timer_t quiet;
    assert(timer_create(CLOCK_MONOTONIC, &ev, &quiet) == 0);
    spec = after(1000, 0);
    assert(timer_settime(quiet, 0, &spec, nullptr) == 0);
    assert(timer_gettime(quiet, &old) == 0 && old.it_value.tv_sec == 0 && old.it_value.tv_nsec > 900000000);
    spec = after(5, 0);
    assert(timer_settime(quiet, 0, &spec, &old) == 0 && old.it_value.tv_nsec > 0);
    usleep(20000);
    assert(count == 0 && timer_gettime(quiet, &old) == 0 && old.it_value.tv_sec == 0 && old.it_value.tv_nsec == 0);
    assert(timer_delete(quiet) == 0);

    ev = {};
    ev.sigev_notify = SIGEV_SIGNAL;
    ev.sigev_signo = SIGUSR2;
    ev.sigev_value.sival_int = 5;
    timer_t at;
    assert(timer_create(CLOCK_REALTIME, &ev, &at) == 0);
    timespec wall;
    assert(clock_gettime(CLOCK_REALTIME, &wall) == 0);
    spec = {};
    spec.it_value = wall;
    spec.it_value.tv_sec -= 1;
    count = 0;
    assert(timer_settime(at, TIMER_ABSTIME, &spec, nullptr) == 0);
    await(1);
    assert(seen[0].signo == SIGUSR2 && seen[0].code == SI_TIMER && seen[0].value == 5);
    timespec start;
    assert(clock_gettime(CLOCK_MONOTONIC, &start) == 0 && clock_gettime(CLOCK_REALTIME, &wall) == 0);
    spec.it_value = later(wall, 30);
    assert(timer_settime(at, TIMER_ABSTIME, &spec, nullptr) == 0);
    await(2);
    assert(elapsed(start) >= 29);
    spec.it_value.tv_nsec = 1000000000;
    errno = 0;
    assert(timer_settime(at, 0, &spec, nullptr) == -1 && errno == EINVAL);
    assert(timer_delete(at) == 0);
    errno = 0;
    assert(timer_create(12345, nullptr, &at) == -1 && errno == EINVAL);
    ev.sigev_notify = 99;
    errno = 0;
    assert(timer_create(CLOCK_MONOTONIC, &ev, &at) == -1 && errno == EINVAL);
    ev.sigev_notify = SIGEV_SIGNAL;
    ev.sigev_signo = 0;
    errno = 0;
    assert(timer_create(CLOCK_MONOTONIC, &ev, &at) == -1 && errno == EINVAL);

    timer_t kept;
    assert(timer_create(CLOCK_MONOTONIC, nullptr, &kept) == 0);
    pid_t pid = fork();
    if (pid == 0) {
        itimerspec s;
        errno = 0;
        _exit(timer_gettime(kept, &s) == -1 && errno == EINVAL ? 0 : 1);
    }
    int status;
    assert(pid > 0 && waitpid(pid, &status, 0) == pid && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    assert(timer_delete(kept) == 0);

    count = 0;
    block(SIGRTMIN, SIG_BLOCK);
    for (int v = 1; v <= 3; v++) {
        union sigval sv;
        sv.sival_int = v;
        assert(sigqueue(getpid(), SIGRTMIN, sv) == 0);
    }
    block(SIGRTMIN, SIG_UNBLOCK);
    assert(count == 3 && seen[0].value == 1 && seen[1].value == 2 && seen[2].value == 3);
    assert(seen[0].signo == SIGRTMIN && seen[0].code == SI_QUEUE && seen[0].pid == getpid() && seen[0].uid == getuid());

    count = 0;
    block(SIGUSR2, SIG_BLOCK);
    union sigval first;
    first.sival_int = 10;
    union sigval second;
    second.sival_int = 20;
    assert(sigqueue(getpid(), SIGUSR2, first) == 0 && sigqueue(getpid(), SIGUSR2, second) == 0);
    block(SIGUSR2, SIG_UNBLOCK);
    assert(count == 1 && seen[0].value == 10 && seen[0].code == SI_QUEUE);

    count = 0;
    assert(kill(getpid(), SIGUSR2) == 0);
    assert(count == 1 && seen[0].code == SI_USER && seen[0].pid == getpid() && seen[0].uid == getuid());

    block(SIGUSR1, SIG_BLOCK);
    union sigval value;
    value.sival_int = 99;
    assert(sigqueue(getpid(), SIGUSR1, value) == 0);
    assert(sigwaitinfo(&usr1, &info) == SIGUSR1 && info.si_code == SI_QUEUE && info.si_value.sival_int == 99);
    assert(info.si_pid == getpid() && info.si_uid == getuid());
    block(SIGUSR1, SIG_UNBLOCK);

    printf("process timers: ok\n");
}
