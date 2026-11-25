/*
 * The watches of directories, rootfs's, the VFS's (lib/protocol): a SEND,
 * sendmsg here, is a call of how and a value, its rights a directory,
 * which rootfs knows by its id, and an ACCEPT of a handle gives a file of
 * rootfs's. watchdir's is an ACCEPT of the directory: its watcher; of
 * what is no handle of rootfs's, ENOTDIR. inotify_init1 opens the root
 * and asks an events file of it, inotify's instance, which the ACCEPT
 * after gives; inotify_add_watch marks the directory of the path, opened
 * as a directory, not following its last link with IN_DONT_FOLLOW, of
 * which no directory is ENOTDIR, and gives the mark's number, which
 * rootfs sends on a pair of ours; inotify_rm_watch takes the mark away. A
 * socket is no events file, nor is what takes no SEND: EINVAL.
 */
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <string.h>
#include <sys/inotify.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/watchdir.h>
#include <unistd.h>

enum { watchEventsNew = 1, watchEventsAdd = 2, watchEventsRemove = 3 };

// An events file's call, its rights count descriptors: 0, or -1 and errno.
static int call(int fd, uint32_t how, uint32_t value, const int* fds, int count) {
    uint32_t body[2] = {how, value};
    struct iovec io = {body, sizeof body};
    union {
        struct cmsghdr h;
        char room[CMSG_SPACE(2 * sizeof(int))];
    } control;
    struct msghdr m = {0};
    struct stat st;

    if (fstat(fd, &st) == 0 && S_ISSOCK(st.st_mode)) {
        errno = EINVAL;

        return -1;
    }

    m.msg_iov = &io;
    m.msg_iovlen = 1;

    if (count > 0) {
        m.msg_control = control.room;
        m.msg_controllen = CMSG_SPACE(count * sizeof(int));

        struct cmsghdr* c = CMSG_FIRSTHDR(&m);

        c->cmsg_level = SOL_SOCKET;
        c->cmsg_type = SCM_RIGHTS;
        c->cmsg_len = CMSG_LEN(count * sizeof(int));
        memcpy(CMSG_DATA(c), fds, count * sizeof(int));
    }

    if (sendmsg(fd, &m, MSG_NOSIGNAL) < 0) {
        if (errno == ENOTSOCK) {
            errno = EINVAL;
        }

        return -1;
    }

    return 0;
}

static void closeKeeping(int fd) {
    int saved = errno;

    close(fd);
    errno = saved;
}

// accept4's flags of open's O_CLOEXEC and O_NONBLOCK.
static int acceptFlags(int flags) {
    return (flags & O_CLOEXEC ? SOCK_CLOEXEC : 0) | (flags & O_NONBLOCK ? SOCK_NONBLOCK : 0);
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

    int fd = call(root, watchEventsNew, 0, NULL, 0) == 0 ? accept4(root, NULL, NULL, acceptFlags(flags)) : -1;

    closeKeeping(root);

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

    int pair[2];

    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        return -1;
    }

    int dir = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC | (mask & IN_DONT_FOLLOW ? O_NOFOLLOW : 0));
    int fds[2] = {dir, pair[1]};
    uint32_t wd = 0;
    int r = -1;

    if (dir < 0) {
        if (errno == ELOOP && (mask & IN_DONT_FOLLOW)) {
            errno = ENOTDIR;
        }
    } else if (call(fd, watchEventsAdd, mask, fds, 2) == 0) {
        r = recv(pair[0], &wd, sizeof wd, MSG_DONTWAIT) == sizeof wd ? (int)wd : -1;
        errno = r < 0 ? EINVAL : errno;
    }

    if (dir >= 0) {
        closeKeeping(dir);
    }

    closeKeeping(pair[0]);
    closeKeeping(pair[1]);

    return r;
}

int __kernel_inotify_rm_watch(int fd, int wd) {
    return call(fd, watchEventsRemove, (uint32_t)wd, NULL, 0);
}

int __kernel_watchdir(int dirfd, int flags) {
    struct stat st;

    if (fstat(dirfd, &st) != 0) {
        return -1;
    }

    if (flags & ~(O_CLOEXEC | O_NONBLOCK) || S_ISSOCK(st.st_mode)) {
        errno = flags & ~(O_CLOEXEC | O_NONBLOCK) ? EINVAL : ENOTDIR;

        return -1;
    }

    int fd = accept4(dirfd, NULL, NULL, acceptFlags(flags));

    if (fd < 0 && errno == ENOTSOCK) {
        errno = ENOTDIR;
    }

    return fd;
}
