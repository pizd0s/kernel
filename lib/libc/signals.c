/*
 * Signals' sets, of a bit a signal, from 1, but 32's and 33's, which are
 * of no use and no set's, and the functions of signals over sigaction,
 * kill and pthread_kill: signal's handler restarts the calls it
 * interrupts; raise is the calling thread's, of a signal, not 0; killpg is
 * of a group of more than 1.
 */
#include "sized.h"

#include <errno.h>
#include <pthread.h>
#include <signal.h>

static int member(int sig) {
    if (sig < 1 || sig >= NSIG || sig == 32 || sig == 33) {
        errno = EINVAL;

        return 0;
    }

    return 1;
}

static unsigned long long bit(int sig) {
    return 1ull << (sig - 1);
}

int __kernel_sigemptyset(sigset_t* set, size_t size) {
    sized("sigemptyset", size, sizeof(sigset_t));
    set->__bits = 0;

    return 0;
}

int __kernel_sigfillset(sigset_t* set, size_t size) {
    sized("sigfillset", size, sizeof(sigset_t));
    set->__bits = ~0ull & ~(bit(32) | bit(33));

    return 0;
}

int __kernel_sigaddset(sigset_t* set, size_t size, int sig) {
    sized("sigaddset", size, sizeof(sigset_t));

    if (!member(sig)) {
        return -1;
    }

    set->__bits |= bit(sig);

    return 0;
}

int __kernel_sigdelset(sigset_t* set, size_t size, int sig) {
    sized("sigdelset", size, sizeof(sigset_t));

    if (!member(sig)) {
        return -1;
    }

    set->__bits &= ~bit(sig);

    return 0;
}

int __kernel_sigismember(const sigset_t* set, size_t size, int sig) {
    sized("sigismember", size, sizeof(sigset_t));

    return sig >= 1 && sig < NSIG && (set->__bits & bit(sig)) != 0;
}

int __kernel_sigisemptyset(const sigset_t* set, size_t size) {
    sized("sigisemptyset", size, sizeof(sigset_t));

    return set->__bits == 0;
}

int __kernel_sigorset(sigset_t* set, size_t size, const sigset_t* a, size_t aSize, const sigset_t* b, size_t bSize) {
    sized("sigorset", size, sizeof(sigset_t));
    sized("sigorset", aSize, sizeof(sigset_t));
    sized("sigorset", bSize, sizeof(sigset_t));
    set->__bits = a->__bits | b->__bits;

    return 0;
}

int __kernel_sigandset(sigset_t* set, size_t size, const sigset_t* a, size_t aSize, const sigset_t* b, size_t bSize) {
    sized("sigandset", size, sizeof(sigset_t));
    sized("sigandset", aSize, sizeof(sigset_t));
    sized("sigandset", bSize, sizeof(sigset_t));
    set->__bits = a->__bits & b->__bits;

    return 0;
}

sighandler_t __kernel_signal(int sig, sighandler_t handler) {
    struct sigaction action = {.sa_handler = handler, .sa_flags = SA_RESTART}, old;

    if (sig == SIGKILL || sig == SIGSTOP) {
        errno = EINVAL;

        return SIG_ERR;
    }

    sigemptyset(&action.sa_mask);

    return sigaction(sig, &action, &old) == 0 ? old.sa_handler : SIG_ERR;
}

int __kernel_raise(int sig) {
    int e = sig < 1 || sig >= NSIG ? EINVAL : pthread_kill(pthread_self(), sig);

    if (e) {
        errno = e;

        return -1;
    }

    return 0;
}

int __kernel_killpg(pid_t group, int sig) {
    if (group <= 1) {
        errno = EINVAL;

        return -1;
    }

    return kill(-group, sig);
}
