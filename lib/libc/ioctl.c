/*
 * ioctl: a terminal's requests are its server's calls (termios.c,
 * terminal.h), the others the kernel's (__kernel_fdioctl), FIONREAD of
 * what the kernel keeps and else the server's count. TCSBRK of 0 is a
 * break, of another the output drained; TCXONC, TCFLSH and TCSBRK take
 * their values, the others where their structures are. posix_devctl is
 * ioctl's, of data of at least the request's size, its result into info,
 * errno unchanged, the errno its result.
 */
#include "sized.h"
#include "terminal.h"
#include "kernel.h"
#include "../protocol/calls.h"

#include <devctl.h>
#include <errno.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <termios.h>
#include <unistd.h>

// The size of the structure of a request whose size is its version.
static size_t versioned(unsigned long request) {
    switch (request) {
        case TCGETS:
        case TCSETS:
        case TCSETSW:
        case TCSETSF:
            return sizeof(struct termios);
        case TIOCGWINSZ:
        case TIOCSWINSZ:
            return sizeof(struct winsize);
    }

    return 0;
}

// A count of a terminal's, into at: 0, or -1 and errno.
static int counted(int fd, uint32_t call, int* at) {
    uint32_t n;

    if (__terminal_call(fd, &call, sizeof call, &n, sizeof n, 0) != sizeof n) {
        return -1;
    }

    *at = (int)n;

    return 0;
}

int __kernel_ioctl(int fd, unsigned long request, va_list ap, size_t size) {
    sized("ioctl", size, versioned(request));

    void* arg = va_arg(ap, void*);
    uintptr_t value = (uintptr_t)arg;
    char name[64];
    int e;

    switch (request) {
        case TCGETS:
            return tcgetattr(fd, arg);
        case TCSETS:
        case TCSETSW:
        case TCSETSF:
            return tcsetattr(fd, (int)(request - TCSETS), arg);
        case TCSBRK:
            return value == 0 ? tcsendbreak(fd, 0) : tcdrain(fd);
        case TCXONC:
            return tcflow(fd, (int)value);
        case TCFLSH:
            return tcflush(fd, (int)value);
        case TIOCGWINSZ:
            return tcgetwinsize(fd, arg);
        case TIOCSWINSZ:
            return tcsetwinsize(fd, arg);
        case TIOCGPGRP:
        case TIOCGSID: {
            pid_t got = request == TIOCGPGRP ? tcgetpgrp(fd) : tcgetsid(fd);

            if (got < 0) {
                return -1;
            }

            *(pid_t*)arg = got;

            return 0;
        }
        case TIOCSPGRP:
            return tcsetpgrp(fd, *(const pid_t*)arg);
        case TIOCOUTQ:
            return counted(fd, ctlOutq, arg);
        case FIONREAD:
            e = errno;

            if (__kernel_fdioctl(fd, request, arg) == 0) {
                return 0;
            }

            if (errno != ENOTTY) {
                return -1;
            }

            errno = e;

            return counted(fd, ctlInq, arg);
        case TIOCGPTN:
        case TIOCSPTLCK:
            if ((e = __terminal_name(fd, 1, name, sizeof name)) != 0) {
                errno = e;

                return -1;
            }

            if (request == TIOCGPTN) {
                *(unsigned*)arg = (unsigned)atoi(name + strlen("/dev/pts/"));
            }

            return 0;
    }

    return __kernel_fdioctl(fd, request, arg);
}

// The size of data a request takes where it takes some, 0 of another.
static size_t devctlSize(int request) {
    switch ((unsigned)request) {
        case FIONBIO:
        case TIOCGPGRP:
        case TIOCSPGRP:
        case FIONREAD:
        case TIOCOUTQ:
        case TIOCGPTN:
        case TIOCGSID:
            return 4;
        case TIOCGWINSZ:
        case TIOCSWINSZ:
            return sizeof(struct winsize);
        case TCGETS:
        case TCSETS:
        case TCSETSW:
        case TCSETSF:
            return sizeof(struct termios);
    }

    return 0;
}

int __kernel_posix_devctl(int fd, int request, void* restrict data, size_t n, int* restrict info) {
    size_t need = devctlSize(request);
    uintptr_t arg = (uintptr_t)data;

    if (need && (!data || (n != 0 && n < need))) {
        return EINVAL;
    }

    if ((unsigned)request == TCFLSH || (unsigned)request == TCXONC || (unsigned)request == TCSBRK) {
        arg = 0;

        if (data) {
            if (n != 0 && n < sizeof(int)) {
                return EINVAL;
            }

            arg = (uintptr_t) * (const int*)data;
        }
    }

    int saved = errno;
    int r = ioctl(fd, (unsigned long)request, arg);

    if (r == -1) {
        int e = errno;

        errno = saved;

        return e;
    }

    if (info) {
        *info = r;
    }

    return 0;
}
