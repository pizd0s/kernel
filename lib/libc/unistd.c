/*
 * unistd.h's functions of the system's names and of entropy: ctermid's
 * terminal is /dev/tty; confstr's one value is _CS_PATH's; gethostname
 * and getdomainname are uname's nodename and domainname, the first cut to
 * the room, the second EINVAL of none; getentropy is getrandom's of 256
 * bytes at most, EIO past them.
 */
#include <errno.h>
#include <string.h>
#include <sys/random.h>
#include <sys/utsname.h>
#include <unistd.h>

char* __kernel_ctermid(char* s) {
    static char terminal[] = "/dev/tty";

    return s ? strcpy(s, terminal) : terminal;
}

size_t __kernel_confstr(int name, char* buf, size_t size) {
    static const char path[] = "/bin:/usr/bin";

    if (name != _CS_PATH) {
        errno = EINVAL;

        return 0;
    }

    if (size) {
        size_t n = size - 1 < sizeof path - 1 ? size - 1 : sizeof path - 1;

        memcpy(buf, path, n);
        buf[n] = 0;
    }

    return sizeof path;
}

int __kernel_gethostname(char* name, size_t size) {
    struct utsname u;

    if (uname(&u) != 0) {
        return -1;
    }

    if (size) {
        size_t n = strnlen(u.nodename, size - 1);

        memcpy(name, u.nodename, n);
        name[n] = 0;
    }

    return 0;
}

int __kernel_getdomainname(char* name, size_t size) {
    struct utsname u;

    if (uname(&u) != 0) {
        return -1;
    }

    size_t n = strlen(u.domainname);

    if (n >= size) {
        errno = EINVAL;

        return -1;
    }

    memcpy(name, u.domainname, n + 1);

    return 0;
}

int __kernel_getentropy(void* buf, size_t size) {
    if (size > 256) {
        errno = EIO;

        return -1;
    }

    for (size_t done = 0; done < size;) {
        ssize_t n = getrandom((char*)buf + done, size - done, 0);

        if (n < 0 && errno != EINTR) {
            return -1;
        }

        done += n > 0 ? n : 0;
    }

    return 0;
}
