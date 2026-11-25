/*
 * system and daemon. system runs the command by /bin/sh -c, its child,
 * ignoring SIGINT and SIGQUIT and holding SIGCHLD while it waits, which
 * the shell has as they were; a shell that cannot be run is as one that
 * exits 127. system(NULL) says whether /bin/sh may be run. daemon moves
 * the process to /, its standard input, output and error to /dev/null,
 * where it is not asked not to, then goes on in a child of fork that
 * leads a session of its own, its parent's exit, 0, the system's first
 * process its parent once it returns. The first process itself stays, as
 * the system ends with it.
 */
#include "kernel.h"

#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <spawn.h>
#include <stdlib.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

int __kernel_system(const char* command) {
    if (!command) {
        return access("/bin/sh", X_OK) == 0;
    }

    struct sigaction ignore = {.sa_handler = SIG_IGN}, interrupt, quit;
    sigset_t held, saved, reset;
    posix_spawnattr_t attr;
    pid_t pid;
    int status = 127 << 8;

    sigaction(SIGINT, &ignore, &interrupt);
    sigaction(SIGQUIT, &ignore, &quit);
    sigemptyset(&held);
    sigaddset(&held, SIGCHLD);
    sigprocmask(SIG_BLOCK, &held, &saved);
    sigemptyset(&reset);

    if (interrupt.sa_handler != SIG_IGN) {
        sigaddset(&reset, SIGINT);
    }

    if (quit.sa_handler != SIG_IGN) {
        sigaddset(&reset, SIGQUIT);
    }

    posix_spawnattr_init(&attr);
    posix_spawnattr_setsigmask(&attr, &saved);
    posix_spawnattr_setsigdefault(&attr, &reset);
    posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETSIGDEF | POSIX_SPAWN_SETSIGMASK);

    char* argv[] = {"sh", "-c", (char*)command, NULL};
    int e = posix_spawn(&pid, "/bin/sh", NULL, &attr, argv, *__program->environAt);

    posix_spawnattr_destroy(&attr);

    if (!e) {
        while (waitpid(pid, &status, 0) < 0 && errno == EINTR) {
        }
    }

    e = errno;
    sigaction(SIGINT, &interrupt, NULL);
    sigaction(SIGQUIT, &quit, NULL);
    sigprocmask(SIG_SETMASK, &saved, NULL);
    errno = e;

    return status;
}

int __kernel_daemon(int nochdir, int noclose) {
    if (!nochdir && chdir("/") != 0) {
        return -1;
    }

    if (!noclose) {
        int fd = open("/dev/null", O_RDWR);

        if (fd < 0) {
            return -1;
        }

        for (int i = 0; i < 3; i++) {
            if (fd != i) {
                dup2(fd, i);
            }
        }

        if (fd > 2) {
            close(fd);
        }
    }

    pid_t parent = getpid();

    if (parent == 1) {
        return 0;
    }

    pid_t child = fork();

    if (child < 0) {
        return -1;
    }

    if (child > 0) {
        _exit(0);
    }

    if (setsid() < 0) {
        return -1;
    }

    while (getppid() == parent) {
        nanosleep(&(struct timespec){0, 1000000}, NULL);
    }

    return 0;
}
