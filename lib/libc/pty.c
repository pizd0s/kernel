/*
 * forkpty, the libcs': openpty's terminal, the kernel's, and a child of
 * fork that leads a session of its own with the terminal as its
 * controlling one and as its standard input, output and error, its master
 * closed; the parent keeps the master. The terminal's settings and size
 * come with their sizes, which go on to openpty as the program's.
 */
#include <pty.h>
#include <sys/ioctl.h>
#include <unistd.h>

int __kernel_openpty(int*, int*, char*, const struct termios*, size_t, const struct winsize*, size_t) __KERNEL(openpty);

pid_t __kernel_forkpty(int* master, char* name, const struct termios* term, size_t termSize, const struct winsize* window,
                       size_t windowSize) {
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
