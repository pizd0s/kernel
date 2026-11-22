#pragma once

#include <__base.h>

__BEGIN_DECLS

typedef int sig_atomic_t;
typedef void (*sighandler_t)(int);
typedef sighandler_t sig_t;

union sigval {
    int sival_int;
    void* sival_ptr;
};

/*
 * What a signal comes with, each instance its own: si_code SI_USER of
 * kill and raise, SI_QUEUE of sigqueue, SI_TIMER of a timer of
 * timer_create, with the sender's si_pid and si_uid, si_value of the last
 * two, and a timer's si_timerid and si_overrun, its expirations the
 * signal stood for after the first. A real-time signal queues, an
 * instance each; another signal sent while it is pending is the first.
 */
typedef struct {
    int si_signo;
    int si_errno;
    int si_code;
    pid_t si_pid;
    uid_t si_uid;
    int si_status;
    void* si_addr;
    long si_band;
    union sigval si_value;
    int si_timerid;
    int si_overrun;
} siginfo_t;

/*
 * How a timer, or another event, is told (sigevent): SIGEV_SIGNAL a
 * signal, sigev_signo, with sigev_value; SIGEV_NONE nothing;
 * SIGEV_THREAD_ID a signal to the thread sigev_notify_thread_id, here the
 * process's one; SIGEV_THREAD a new thread, which the process cannot make.
 */
struct sigevent {
    union sigval sigev_value;
    int sigev_signo;
    int sigev_notify;
    union {
        char __pad[64 - 2 * sizeof(int) - sizeof(union sigval)];
        pid_t sigev_notify_thread_id;
        struct {
            void (*sigev_notify_function)(union sigval);
            void* sigev_notify_attributes;
        } __sev_thread;
    } __sev_fields;
};

#define sigev_notify_thread_id __sev_fields.sigev_notify_thread_id
#define sigev_notify_function __sev_fields.__sev_thread.sigev_notify_function
#define sigev_notify_attributes __sev_fields.__sev_thread.sigev_notify_attributes

#define SIGEV_SIGNAL 0
#define SIGEV_NONE 1
#define SIGEV_THREAD 2
#define SIGEV_THREAD_ID 4

struct sigaction {
    union {
        void (*sa_handler)(int);
        void (*sa_sigaction)(int, siginfo_t*, void*);
    } __sa_handler;
    sigset_t sa_mask;
    int sa_flags;
    void (*sa_restorer)(void);
};

#define sa_handler __sa_handler.sa_handler
#define sa_sigaction __sa_handler.sa_sigaction

typedef struct {
    void* ss_sp;
    int ss_flags;
    size_t ss_size;
} stack_t;

#define SIG_ERR ((void (*)(int)) - 1)
#define SIG_HOLD ((void (*)(int))2)
#define SIG_DFL ((void (*)(int))0)
/* Table index 1 can name a real Wasm function. */
#define SIG_IGN ((void (*)(int)) - 2)

#define SIGHUP 1
#define SIGINT 2
#define SIGQUIT 3
#define SIGILL 4
#define SIGTRAP 5
#define SIGABRT 6
#define SIGIOT SIGABRT
#define SIGBUS 7
#define SIGFPE 8
#define SIGKILL 9
#define SIGUSR1 10
#define SIGSEGV 11
#define SIGUSR2 12
#define SIGPIPE 13
#define SIGALRM 14
#define SIGTERM 15
#define SIGSTKFLT 16
#define SIGCHLD 17
#define SIGCLD SIGCHLD
#define SIGCONT 18
#define SIGSTOP 19
#define SIGTSTP 20
#define SIGTTIN 21
#define SIGTTOU 22
#define SIGURG 23
#define SIGXCPU 24
#define SIGXFSZ 25
#define SIGVTALRM 26
#define SIGPROF 27
#define SIGWINCH 28
#define SIGIO 29
#define SIGPOLL SIGIO
#define SIGPWR 30
#define SIGSYS 31
#define SIGUNUSED SIGSYS
#define SIGRTMIN 34
#define SIGRTMAX 64
#define NSIG 65
#define _NSIG NSIG

#define SA_NOCLDSTOP 1
#define SA_NOCLDWAIT 2
#define SA_SIGINFO 4
#define SA_RESTORER 0x04000000
#define SA_ONSTACK 0x08000000
#define SA_RESTART 0x10000000
#define SA_NODEFER 0x40000000
#define SA_RESETHAND 0x80000000
#define SA_NOMASK SA_NODEFER
#define SA_ONESHOT SA_RESETHAND

#define SIG_BLOCK 0
#define SIG_UNBLOCK 1
#define SIG_SETMASK 2

#define SS_ONSTACK 1
#define SS_DISABLE 2
#define MINSIGSTKSZ 2048
#define SIGSTKSZ 8192

#define SI_USER 0
#define SI_KERNEL 0x80
#define SI_QUEUE (-1)
#define SI_TIMER (-2)
#define CLD_EXITED 1
#define CLD_KILLED 2
#define CLD_DUMPED 3
#define CLD_TRAPPED 4
#define CLD_STOPPED 5
#define CLD_CONTINUED 6

sighandler_t signal(int, sighandler_t);
sighandler_t bsd_signal(int, sighandler_t);
int sigaction(int, const struct sigaction* __restrict, struct sigaction* __restrict);
int sigprocmask(int, const sigset_t* __restrict, sigset_t* __restrict);
int pthread_sigmask(int, const sigset_t* __restrict, sigset_t* __restrict);
int pthread_kill(pthread_t, int);
int sigsuspend(const sigset_t*);
int sigpending(sigset_t*);
int sigwait(const sigset_t* __restrict, int* __restrict);
int sigwaitinfo(const sigset_t* __restrict, siginfo_t* __restrict);
int sigtimedwait(const sigset_t* __restrict, siginfo_t* __restrict, const struct timespec* __restrict);
int sigqueue(pid_t, int, union sigval);
int siginterrupt(int, int);
int sighold(int);
int sigrelse(int);
int sigignore(int);
int sigpause(int);
void (*sigset(int, void (*)(int)))(int);
int sigaltstack(const stack_t* __restrict, stack_t* __restrict);
int sigemptyset(sigset_t*);
int sigfillset(sigset_t*);
int sigaddset(sigset_t*, int);
int sigdelset(sigset_t*, int);
int sigismember(const sigset_t*, int);
int sigisemptyset(const sigset_t*);
int sigorset(sigset_t*, const sigset_t*, const sigset_t*);
int sigandset(sigset_t*, const sigset_t*, const sigset_t*);
int kill(pid_t, int);
int killpg(pid_t, int);
int raise(int);
void psignal(int, const char*);
void psiginfo(const siginfo_t*, const char*);

__END_DECLS
