/*
 * The waits over wait4, and the process's nice and group over
 * getpriority, setpriority and setpgid: nice's value is clamped to -20
 * and 19, and lowering it of no privilege is EPERM.
 */
#include "sized.h"

#include <errno.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <unistd.h>

pid_t __kernel_wait(int* status) {
    return wait4(-1, status, 0, NULL);
}

pid_t __kernel_wait3(int* status, int options, struct rusage* usage, size_t size) {
    sized("wait3", size, sizeof(struct rusage));

    return wait4(-1, status, options, usage);
}

pid_t __kernel_waitpid(pid_t pid, int* status, int options) {
    return wait4(pid, status, options, NULL);
}

int __kernel_nice(int increment) {
    int saved = errno;

    errno = 0;

    int old = getpriority(PRIO_PROCESS, 0);

    if (old == -1 && errno) {
        return -1;
    }

    long n = (long)old + increment;

    n = n < -20 ? -20 : n > 19 ? 19 : n;

    if (setpriority(PRIO_PROCESS, 0, n) != 0) {
        errno = errno == EACCES ? EPERM : errno;

        return -1;
    }

    errno = saved;

    return n;
}

int __kernel_setpgrp(void) {
    return setpgid(0, 0);
}
