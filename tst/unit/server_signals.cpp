//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "server signals: ok\n"

// What a server of the system takes of the kernel to do what the kernel
// did: getumask, the mask as umask set it, unchanged; sigqueueinfo, a
// signal of the siginfo given, SI_MESGQ's of another pid and uid, as
// mqueue tells a message queue's notification. A code of 0 or more is
// EPERM, and so is a process that is not privileged; a pid of no process
// ESRCH, and of none of 0 or below EINVAL. host: false: both are the
// system's.

#include <assert.h>
#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

int main() {
    umask(027);
    assert(getumask() == 027 && getumask() == 027 && umask(022) == 027 && getumask() == 022);

    sigset_t usr1;
    sigemptyset(&usr1);
    sigaddset(&usr1, SIGUSR1);
    assert(sigprocmask(SIG_BLOCK, &usr1, nullptr) == 0);
    siginfo_t info{};
    info.si_code = SI_MESGQ;
    info.si_pid = 4242;
    info.si_uid = 77;
    info.si_value.sival_int = 9;
    assert(geteuid() == 0 && sigqueueinfo(getpid(), SIGUSR1, &info) == 0);
    siginfo_t got{};
    timespec wait = {5, 0};
    assert(sigtimedwait(&usr1, &got, &wait) == SIGUSR1 && got.si_code == SI_MESGQ && got.si_pid == 4242 && got.si_uid == 77);
    assert(got.si_value.sival_int == 9);

    info.si_code = SI_USER;
    errno = 0;
    assert(sigqueueinfo(getpid(), SIGUSR1, &info) == -1 && errno == EPERM);
    info.si_code = SI_MESGQ;
    errno = 0;
    assert(sigqueueinfo(0, SIGUSR1, &info) == -1 && errno == EINVAL);
    errno = 0;
    assert(sigqueueinfo(999999, SIGUSR1, &info) == -1 && errno == ESRCH);

    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        _exit(setuid(1000) == 0 && sigqueueinfo(getppid(), SIGUSR1, &info) == -1 && errno == EPERM ? 0 : 1);
    }
    int status = 0;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    wait = {0, 20000000};
    assert(sigtimedwait(&usr1, &got, &wait) == -1 && errno == EAGAIN);
    puts("server signals: ok");
}
