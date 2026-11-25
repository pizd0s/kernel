/*
 * The watches of directories, rootfs's, the VFS's, each a ctl
 * (lib/protocol/calls.h): watchdir's of the directory, whose answer is
 * its watcher; inotify_init1's of the root, whose answer is an events
 * file, inotify's instance; inotify_add_watch's of the events file, with
 * the directory of the path, opened as a directory, not following its
 * last link with IN_DONT_FOLLOW, of which no directory is ENOTDIR, which
 * rootfs knows by its id, answered the mark's number; inotify_rm_watch's,
 * of the number. What is no events file is EINVAL, and what is no
 * directory of rootfs's ENOTDIR.
 */
#include "../protocol/calls.h"

#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <sys/inotify.h>
#include <sys/watchdir.h>
#include <unistd.h>

// A file of a call of a handle's, of O_NONBLOCK and O_CLOEXEC of flags:
// its descriptor, or -1 and errno.
static int fileOf(int fd, uint32_t call, int flags) {
    const uint32_t in[2] = {call, (uint32_t)(flags & O_NONBLOCK)};
    int got = -1;
    struct ctl c = {in, sizeof in, NULL, 0, NULL, 0, &got, 1};

    if (ctl(fd, &c, flags & O_CLOEXEC) != 0) {
        return -1;
    }

    if (c.ngot != 1) {
        errno = EIO;

        return -1;
    }

    return got;
}

// An events file's call, with a directory but for -1, its answer into
// out but for null: 0, or -1 and errno, EINVAL of what is no events file.
static int eventsCall(int fd, uint32_t call, uint32_t value, int dir, uint32_t* out) {
    const uint32_t in[2] = {call, value};
    struct ctl c = {in, sizeof in, &dir, dir >= 0, out, out ? sizeof *out : 0, NULL, 0};

    if (ctl(fd, &c, 0) != 0) {
        if (errno == ENOTTY) {
            errno = EINVAL;
        }

        return -1;
    }

    return 0;
}

int __kernel_inotify_init1(int flags) {
    if (flags & ~(IN_CLOEXEC | IN_NONBLOCK)) {
        errno = EINVAL;

        return -1;
    }

    int root = open("/", O_RDONLY | O_DIRECTORY | O_CLOEXEC);

    if (root < 0) {
        return -1;
    }

    int fd = fileOf(root, ctlWatchEvents, flags);
    int saved = errno;

    close(root);
    errno = saved;

    return fd;
}

int __kernel_inotify_init(void) {
    return __kernel_inotify_init1(0);
}

int __kernel_inotify_add_watch(int fd, const char* path, uint32_t mask) {
    if (fcntl(fd, F_GETFD) < 0) {
        return -1;
    }

    if (!(mask & IN_ALL_EVENTS) || ((mask & IN_MASK_ADD) && (mask & IN_MASK_CREATE))) {
        errno = EINVAL;

        return -1;
    }

    int dir = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC | (mask & IN_DONT_FOLLOW ? O_NOFOLLOW : 0));

    if (dir < 0) {
        if (errno == ELOOP && (mask & IN_DONT_FOLLOW)) {
            errno = ENOTDIR;
        }

        return -1;
    }

    uint32_t wd = 0;
    int r = eventsCall(fd, ctlWatchAdd, mask, dir, &wd);
    int saved = errno;

    close(dir);
    errno = saved;

    return r < 0 ? -1 : (int)wd;
}

int __kernel_inotify_rm_watch(int fd, int wd) {
    return eventsCall(fd, ctlWatchRemove, (uint32_t)wd, -1, NULL);
}

int __kernel_watchdir(int dirfd, int flags) {
    if (flags & ~(O_CLOEXEC | O_NONBLOCK)) {
        errno = EINVAL;

        return -1;
    }

    int fd = fileOf(dirfd, ctlWatchDirectory, flags);

    if (fd < 0 && errno == ENOTTY) {
        errno = ENOTDIR;
    }

    return fd;
}
