/*
 * A terminal's settings of struct termios alone: the speed is c_cflag's
 * CBAUD bits, for input and output both, and setting an input speed of 0
 * leaves it; raw mode is of 8 bits, no echo, no signals, no translation,
 * reads of 1 byte at least and no time; isatty is whether tcgetattr is.
 * The terminal's calls are its server's, pty's, each a ctl
 * (lib/protocol/calls.h); a group out of the foreground of one that
 * changes the terminal hears SIGTTOU, as the kernel has it.
 */
#include "sized.h"
#include "terminal.h"
#include "../protocol/calls.h"

#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdint.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/types.h>
#include <termios.h>
#include <unistd.h>

speed_t __kernel_cfgetospeed(const struct termios* t, size_t size) {
    sized("cfgetospeed", size, sizeof(struct termios));

    return t->c_cflag & CBAUD;
}

speed_t __kernel_cfgetispeed(const struct termios* t, size_t size) {
    sized("cfgetispeed", size, sizeof(struct termios));

    return t->c_cflag & CBAUD;
}

static int speed(struct termios* t, speed_t s) {
    if (s & ~CBAUD) {
        errno = EINVAL;

        return -1;
    }

    t->c_cflag = (t->c_cflag & ~CBAUD) | s;

    return 0;
}

int __kernel_cfsetospeed(struct termios* t, size_t size, speed_t s) {
    sized("cfsetospeed", size, sizeof(struct termios));

    return speed(t, s);
}

int __kernel_cfsetispeed(struct termios* t, size_t size, speed_t s) {
    sized("cfsetispeed", size, sizeof(struct termios));

    return s == 0 ? 0 : speed(t, s);
}

int __kernel_cfsetspeed(struct termios* t, size_t size, speed_t s) {
    sized("cfsetspeed", size, sizeof(struct termios));

    return speed(t, s);
}

void __kernel_cfmakeraw(struct termios* t, size_t size) {
    sized("cfmakeraw", size, sizeof(struct termios));
    t->c_iflag &= ~(IGNBRK | BRKINT | PARMRK | ISTRIP | INLCR | IGNCR | ICRNL | IXON);
    t->c_oflag &= ~OPOST;
    t->c_cflag = (t->c_cflag & ~(CSIZE | PARENB)) | CS8;
    t->c_lflag &= ~(ECHO | ECHONL | ICANON | ISIG | IEXTEN);
    t->c_cc[VTIME] = 0;
    t->c_cc[VMIN] = 1;
}

int __kernel_isatty(int fd) {
    struct termios t;

    return tcgetattr(fd, &t) == 0;
}

// A terminal's call (ctl, lib/protocol/calls.h) of size bytes, its answer
// into out, room bytes: the answer's size, or -1 and errno.
ssize_t __terminal_call(int fd, const void* in, size_t size, void* out, size_t room, int flags) {
    struct ctl c = {in, size, NULL, 0, out, room, NULL, 0};

    return ctl(fd, &c, flags) < 0 ? -1 : (ssize_t)c.room;
}

// A call of a value: 0, or -1 and errno.
static int valued(int fd, uint32_t call, uint32_t value, int flags) {
    const uint32_t in[2] = {call, value};

    return __terminal_call(fd, in, sizeof in, NULL, 0, flags) < 0 ? -1 : 0;
}

int __kernel_tcgetattr(int fd, struct termios* t, size_t size) {
    sized("tcgetattr", size, sizeof(struct termios));

    const uint32_t call = ctlTcgetattr;

    return __terminal_call(fd, &call, sizeof call, t, sizeof *t, 0) == sizeof *t ? 0 : -1;
}

// The settings, of when: TCSANOW, or once the output is out, TCSADRAIN,
// and the input then gone too, TCSAFLUSH, which a signal restarts.
int __kernel_tcsetattr(int fd, int when, const struct termios* t, size_t size) {
    sized("tcsetattr", size, sizeof(struct termios));

    struct __attribute__((packed)) {
        uint32_t call;
        uint32_t when;
        struct termios settings;
    } in = {ctlTcsetattr, (uint32_t)when, *t};

    if (when < TCSANOW || when > TCSAFLUSH) {
        errno = EINVAL;

        return -1;
    }

    return __terminal_call(fd, &in, sizeof in, NULL, 0, when == TCSANOW ? 0 : CTL_RESTART) < 0 ? -1 : 0;
}

int __kernel_tcsendbreak(int fd, int duration) {
    (void)duration;

    return valued(fd, ctlTcsbrk, 0, 0);
}

int __kernel_tcdrain(int fd) {
    return valued(fd, ctlTcdrain, 0, CTL_RESTART);
}

int __kernel_tcflush(int fd, int queue) {
    return valued(fd, ctlTcflush, (uint32_t)queue, 0);
}

int __kernel_tcflow(int fd, int action) {
    return valued(fd, ctlTcflow, (uint32_t)action, 0);
}

// The terminal's foreground group and the session it controls, 0 for
// none: 0, or -1 and errno.
static int jobs(int fd, uint32_t out[2]) {
    const uint32_t call = ctlGetPgrp;

    return __terminal_call(fd, &call, sizeof call, out, 2 * sizeof *out, 0) == 2 * sizeof *out ? 0 : -1;
}

pid_t __kernel_tcgetpgrp(int fd) {
    uint32_t j[2];

    return jobs(fd, j) == 0 ? (pid_t)j[0] : -1;
}

pid_t __kernel_tcgetsid(int fd) {
    uint32_t j[2];

    if (jobs(fd, j) != 0) {
        return -1;
    }

    if (j[1] == 0) {
        errno = ENOTTY;

        return -1;
    }

    return (pid_t)j[1];
}

// The foreground group, of a group of the caller's session: EPERM of one
// that is none, as far as kill and getsid find it.
int __kernel_tcsetpgrp(int fd, pid_t pgrp) {
    if (fcntl(fd, F_GETFD) < 0) {
        return -1;
    }

    if (pgrp < 0) {
        errno = EINVAL;

        return -1;
    }

    int saved = errno;
    pid_t sid = getsid(pgrp);

    if ((kill(-pgrp, 0) != 0 && errno == ESRCH) || (sid >= 0 && sid != getsid(0))) {
        errno = EPERM;

        return -1;
    }

    errno = saved;

    return valued(fd, ctlSetPgrp, (uint32_t)pgrp, 0);
}

int __kernel_tcgetwinsize(int fd, struct winsize* w, size_t size) {
    sized("tcgetwinsize", size, sizeof(struct winsize));

    const uint32_t call = ctlGetWinsize;

    return __terminal_call(fd, &call, sizeof call, w, sizeof *w, 0) == sizeof *w ? 0 : -1;
}

int __kernel_tcsetwinsize(int fd, const struct winsize* w, size_t size) {
    sized("tcsetwinsize", size, sizeof(struct winsize));

    struct __attribute__((packed)) {
        uint32_t call;
        struct winsize size;
    } in = {ctlSetWinsize, *w};

    return __terminal_call(fd, &in, sizeof in, NULL, 0, 0) < 0 ? -1 : 0;
}

// The path of a terminal's device, or of a master's slave's with peer,
// into name of n bytes: 0, or the errno.
int __terminal_name(int fd, int peer, char* name, size_t n) {
    const uint32_t in[2] = {ctlName, (uint32_t)(peer != 0)};
    char path[64];
    int saved = errno;
    ssize_t got = __terminal_call(fd, in, sizeof in, path, sizeof path - 1, 0);
    int e = errno;

    errno = saved;

    if (got < 0) {
        return e;
    }

    if ((size_t)got >= sizeof path || (size_t)got >= n) {
        return ERANGE;
    }

    memcpy(name, path, (size_t)got);
    name[got] = 0;

    return 0;
}

int __kernel_ttyname_r(int fd, char* name, size_t n) {
    return __terminal_name(fd, 0, name, n);
}

char* __kernel_ttyname(int fd) {
    static char name[64];
    int e = __terminal_name(fd, 0, name, sizeof name);

    if (e != 0) {
        errno = e;

        return NULL;
    }

    return name;
}
