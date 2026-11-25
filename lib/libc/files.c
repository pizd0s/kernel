/*
 * Files' functions over others: utime, utimes, lutimes and futimes are
 * utimensat's and futimens's, of seconds and microseconds; creat is
 * open's; remove is unlink's, or rmdir's of a directory; mkfifo is
 * mknod's; lockf is fcntl's lock of the section from the file's offset;
 * posix_close is close's.
 */
#include "sized.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <unistd.h>
#include <utime.h>

// The times of two timevals, or null for now.
static const struct timespec* times(const struct timeval* tv, struct timespec* ts) {
    if (!tv) {
        return NULL;
    }

    for (int i = 0; i < 2; i++) {
        ts[i] = (struct timespec){tv[i].tv_sec, 1000 * (long)tv[i].tv_usec};
    }

    return ts;
}

int __kernel_utime(const char* path, const struct utimbuf* times, size_t size) {
    sized("utime", size, sizeof(struct utimbuf));

    struct timespec ts[2] = {{times ? times->actime : 0, 0}, {times ? times->modtime : 0, 0}};

    return utimensat(AT_FDCWD, path, times ? ts : NULL, 0);
}

int __kernel_utimes(const char* path, const struct timeval tv[2], size_t size) {
    sized("utimes", size, sizeof(struct timeval));

    struct timespec ts[2];

    return utimensat(AT_FDCWD, path, times(tv, ts), 0);
}

int __kernel_lutimes(const char* path, const struct timeval tv[2], size_t size) {
    sized("lutimes", size, sizeof(struct timeval));

    struct timespec ts[2];

    return utimensat(AT_FDCWD, path, times(tv, ts), AT_SYMLINK_NOFOLLOW);
}

int __kernel_futimes(int fd, const struct timeval tv[2], size_t size) {
    sized("futimes", size, sizeof(struct timeval));

    struct timespec ts[2];

    return futimens(fd, times(tv, ts));
}

int __kernel_creat(const char* path, mode_t mode) {
    return open(path, O_WRONLY | O_CREAT | O_TRUNC, mode);
}

int __kernel_remove(const char* path) {
    if (unlink(path) == 0) {
        return 0;
    }

    return errno == EISDIR || errno == EPERM ? rmdir(path) : -1;
}

int __kernel_mkfifo(const char* path, mode_t mode) {
    return mknod(path, S_IFIFO | (mode & 07777), 0);
}

int __kernel_mkfifoat(int dir, const char* path, mode_t mode) {
    return mknodat(dir, path, S_IFIFO | (mode & 07777), 0);
}

int __kernel_lockf(int fd, int cmd, off_t size) {
    struct flock lock = {.l_type = F_WRLCK, .l_whence = SEEK_CUR, .l_len = size};

    switch (cmd) {
        case F_ULOCK:
            lock.l_type = F_UNLCK;

            return fcntl(fd, F_SETLK, &lock);
        case F_LOCK:
            return fcntl(fd, F_SETLKW, &lock);
        case F_TLOCK:
            return fcntl(fd, F_SETLK, &lock);
        case F_TEST:
            lock.l_type = F_RDLCK;

            if (fcntl(fd, F_GETLK, &lock) != 0) {
                return -1;
            }

            if (lock.l_type != F_UNLCK) {
                errno = EACCES;

                return -1;
            }

            return 0;
    }

    errno = EINVAL;

    return -1;
}

int __kernel_posix_close(int fd, int flags) {
    return close(fd);
}
