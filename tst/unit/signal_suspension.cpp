//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "signal suspension: ok\n"

#include <assert.h>
#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <sys/time.h>
#include <unistd.h>

static volatile sig_atomic_t caught;

static void alarmed(int signal) {
    assert(signal == SIGALRM);
    caught = caught + 1;
}

int main() {
    struct sigaction action{};
    action.sa_handler = alarmed;
    assert(sigemptyset(&action.sa_mask) == 0 && sigaction(SIGALRM, &action, nullptr) == 0);
    sigset_t blocked, previous, temporary, current;
    assert(sigemptyset(&blocked) == 0 && sigaddset(&blocked, SIGALRM) == 0);
    assert(pthread_sigmask(SIG_BLOCK, &blocked, &previous) == 0);
    assert(sigemptyset(&temporary) == 0);
    itimerval timer{};
    timer.it_value.tv_usec = 10000;
    assert(setitimer(ITIMER_REAL, &timer, nullptr) == 0);
    assert(sigsuspend(&temporary) == -1 && errno == EINTR && caught == 1);
    assert(sigprocmask(SIG_SETMASK, nullptr, &current) == 0 && sigismember(&current, SIGALRM) == 1);
    assert(raise(SIGALRM) == 0 && caught == 1);
    assert(sigpending(&current) == 0 && sigismember(&current, SIGALRM) == 1);
    assert(sigsuspend(&temporary) == -1 && errno == EINTR && caught == 2);
    assert(sigpending(&current) == 0 && sigismember(&current, SIGALRM) == 0);
    assert(sigprocmask(SIG_SETMASK, &previous, nullptr) == 0);

    // XSI's simplified ones (POSIX.1-2017): sighold and sigrelse block and
    // unblock; sigpause waits with the signal unblocked, then puts the
    // mask back; sigset's SIG_HOLD blocks, a disposition unblocks, and
    // each reports SIG_HOLD for a blocked signal, else the disposition;
    // sigignore ignores; a bad signal is EINVAL.
    caught = 0;
    assert(sighold(SIGALRM) == 0 && raise(SIGALRM) == 0 && caught == 0);
    assert(sigpause(SIGALRM) == -1 && errno == EINTR && caught == 1);
    assert(sigprocmask(SIG_SETMASK, nullptr, &current) == 0 && sigismember(&current, SIGALRM) == 1);
    assert(sigrelse(SIGALRM) == 0 && sigprocmask(SIG_SETMASK, nullptr, &current) == 0);
    assert(sigismember(&current, SIGALRM) == 0);
    assert(sigset(SIGALRM, SIG_HOLD) == alarmed);
    assert(sigprocmask(SIG_SETMASK, nullptr, &current) == 0 && sigismember(&current, SIGALRM) == 1);
    assert(sigset(SIGALRM, alarmed) == SIG_HOLD);
    assert(sigprocmask(SIG_SETMASK, nullptr, &current) == 0 && sigismember(&current, SIGALRM) == 0);
    assert(raise(SIGALRM) == 0 && caught == 2);
    assert(sigignore(SIGALRM) == 0 && raise(SIGALRM) == 0 && caught == 2);
    assert(sigset(SIGALRM, SIG_DFL) == SIG_IGN);
    errno = 0;
    assert(sighold(1000) == -1 && errno == EINVAL);
    errno = 0;
    assert(sigset(SIGKILL, alarmed) == SIG_ERR && errno == EINVAL);
    puts("signal suspension: ok");
}
