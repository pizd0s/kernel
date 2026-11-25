/*
 * The watches of directories, rootfs's, the VFS's, by WATCH of their
 * server (kernel.h): watchdir's of no body, and inotify's, whose
 * instance is an events file of rootfs's named by its id, its fstat's
 * ino. inotify_init1 asks one of the root; inotify_add_watch marks the
 * directory of the path, opened as a directory, not following its last
 * link with IN_DONT_FOLLOW, of which no directory is ENOTDIR, and gives
 * the mark's number; inotify_rm_watch takes the mark away. The bodies
 * are lib/protocol's: how, then a mask or a number, and the id.
 */
#include "kernel.h"

#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <sys/inotify.h>
#include <sys/stat.h>
#include <sys/watchdir.h>
#include <unistd.h>

enum { watchEventsNew = 1, watchEventsAdd = 2, watchEventsRemove = 3 };

struct __attribute__((packed)) Body {
    uint32_t how;
    uint32_t value;
    uint64_t id;
};

int __kernel_watchdir(int dirfd, int flags) {
    return __kernel_watch(dirfd, flags, NULL, 0);
}

// A WATCH of the body of a directory of the path, opened with flags and
// closed after, errno the WATCH's.
static int watchOf(const char* path, int flags, int watchFlags, const struct Body* body, size_t size) {
    int dir = open(path, flags | O_CLOEXEC);

    if (dir < 0) {
        return -1;
    }

    int r = __kernel_watch(dir, watchFlags, body, size);
    int saved = errno;

    close(dir);
    errno = saved;

    return r;
}

int __kernel_inotify_init1(int flags) {
    if (flags & ~(IN_CLOEXEC | IN_NONBLOCK)) {
        errno = EINVAL;

        return -1;
    }

    const struct Body body = {watchEventsNew, 0, 0};

    return watchOf("/", O_RDONLY | O_DIRECTORY, flags, &body, sizeof body.how);
}

int __kernel_inotify_init(void) {
    return __kernel_inotify_init1(0);
}

// The id of an events file, or 0 with errno.
static uint64_t idOf(int fd) {
    struct stat st;

    return fstat(fd, &st) == 0 ? st.st_ino : 0;
}

int __kernel_inotify_add_watch(int fd, const char* path, uint32_t mask) {
    if (!(mask & IN_ALL_EVENTS) || ((mask & IN_MASK_ADD) && (mask & IN_MASK_CREATE))) {
        errno = EINVAL;

        return -1;
    }

    const struct Body body = {watchEventsAdd, mask, idOf(fd)};

    if (!body.id) {
        return -1;
    }

    int wd = watchOf(path, O_RDONLY | O_DIRECTORY | (mask & IN_DONT_FOLLOW ? O_NOFOLLOW : 0), 0, &body, sizeof body);

    if (wd < 0 && errno == ELOOP && (mask & IN_DONT_FOLLOW)) {
        errno = ENOTDIR;
    }

    return wd;
}

int __kernel_inotify_rm_watch(int fd, int wd) {
    const struct Body body = {watchEventsRemove, (uint32_t)wd, idOf(fd)};

    if (!body.id) {
        return -1;
    }

    return watchOf("/", O_RDONLY | O_DIRECTORY, 0, &body, sizeof body) < 0 ? -1 : 0;
}
