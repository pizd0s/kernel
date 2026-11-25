/*
 * Pseudoterminals: posix_openpt opens /dev/ptmx, the kernel's master of a
 * new one; openpty opens one and its terminal, /dev/pts/N, with the
 * settings and size given, the kernel's grantpt, unlockpt and ptsname
 * between; forkpty, the libcs', is openpty's terminal and a child of fork
 * that leads a session of its own with the terminal as its controlling one
 * and as its standard input, output and error, its master closed; the
 * parent keeps the master. The terminal's settings and size come with
 * their sizes, the one layout each has had.
 */
#include "sized.h"

#include <errno.h>
#include <fcntl.h>
#include <pty.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <termios.h>
#include <unistd.h>

int __kernel_posix_openpt(int flags) {
    return open("/dev/ptmx", flags);
}

int __kernel_openpty(int* master, int* terminal, char* name, const struct termios* term, size_t termSize, const struct winsize* window,
                     size_t windowSize) {
    sized("openpty", termSize, sizeof(struct termios));
    sized("openpty", windowSize, sizeof(struct winsize));

    char path[32];
    int primary = open("/dev/ptmx", O_RDWR | O_NOCTTY), secondary = -1;
    int named = primary >= 0 ? ptsname_r(primary, path, sizeof path) : -1;

    if (named > 0) {
        errno = named;
    }

    int ok = named == 0 && (secondary = open(path, O_RDWR | O_NOCTTY)) >= 0 && (!term || tcsetattr(secondary, TCSANOW, term) == 0) &&
             (!window || ioctl(secondary, TIOCSWINSZ, window) == 0);

    if (!ok) {
        int e = errno;

        if (primary >= 0) {
            close(primary);
        }

        if (secondary >= 0) {
            close(secondary);
        }

        errno = e;

        return -1;
    }

    *master = primary;
    *terminal = secondary;

    if (name) {
        strcpy(name, path);
    }

    return 0;
}

pid_t __kernel_forkpty(int* master, char* name, const struct termios* term, size_t termSize, const struct winsize* window,
                       size_t windowSize) {
    sized("forkpty", termSize, sizeof(struct termios));
    sized("forkpty", windowSize, sizeof(struct winsize));

    int primary, terminal;

    if (__kernel_openpty(&primary, &terminal, name, term, termSize, window, windowSize) < 0) {
        return -1;
    }

    pid_t pid = fork();

    if (pid == 0) {
        close(primary);

        if (setsid() < 0 || ioctl(terminal, TIOCSCTTY, 0) < 0 || dup2(terminal, 0) < 0 || dup2(terminal, 1) < 0 || dup2(terminal, 2) < 0) {
            _exit(1);
        }

        if (terminal > 2) {
            close(terminal);
        }

        return 0;
    }

    close(terminal);

    if (pid < 0) {
        close(primary);

        return -1;
    }

    *master = primary;

    return pid;
}
