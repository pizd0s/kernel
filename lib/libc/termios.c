/*
 * A terminal's settings of struct termios alone: the speed is c_cflag's
 * CBAUD bits, for input and output both, and setting an input speed of 0
 * leaves it; raw mode is of 8 bits, no echo, no signals, no translation,
 * reads of 1 byte at least and no time; isatty is whether tcgetattr is.
 */
#include "sized.h"

#include <errno.h>
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
