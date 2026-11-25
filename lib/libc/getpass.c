/*
 * getpass: the prompt on /dev/tty and a line read there with no echo and
 * no signals of its characters, in canonical mode with carriage returns as
 * newlines, the terminal's input flushed first; its settings are as they
 * were after. A password is at most 127 bytes, as musl's, the rest of a
 * longer line dropped; an end of file is an empty one. Its room is the
 * heap's, taken at the first call.
 */
#include <errno.h>
#include <fcntl.h>
#include <stdlib.h>
#include <string.h>
#include <termios.h>
#include <unistd.h>

enum {
    room = 128,
};

static char* password;

char* __kernel_getpass(const char* prompt) {
    int fd = open("/dev/tty", O_RDWR | O_NOCTTY | O_CLOEXEC);

    if (fd < 0) {
        return NULL;
    }

    struct termios saved;

    if (!password) {
        password = malloc(room + 1);
    }

    if (!password || tcgetattr(fd, &saved) != 0) {
        int e = password ? errno : ENOMEM;

        close(fd);
        errno = e;

        return NULL;
    }

    struct termios quiet = saved;

    quiet.c_iflag = (quiet.c_iflag & ~(INLCR | IGNCR)) | ICRNL;
    quiet.c_lflag = (quiet.c_lflag & ~(ECHO | ISIG)) | ICANON;
    tcsetattr(fd, TCSAFLUSH, &quiet);
    write(fd, prompt, strlen(prompt));

    ssize_t n = read(fd, password, room);
    int e = errno;

    if (n >= 0) {
        if ((n > 0 && password[n - 1] == '\n') || n == room) {
            n--;
        }

        password[n] = 0;
    }

    tcsetattr(fd, TCSAFLUSH, &saved);
    write(fd, "\n", 1);
    close(fd);

    if (n < 0) {
        errno = e;

        return NULL;
    }

    return password;
}
