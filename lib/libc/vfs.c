/*
 * The calls of paths, the VFS's, each a ctl (lib/protocol/calls.h) of the
 * handle the path starts at: the directory descriptor of an *at call or
 * the working directory, AT_FDCWD, of a relative path, the root,
 * AT_FDROOT, of an absolute one; the root is the call's first descriptor,
 * which the walk never leaves. The kernel knows no path: chdir and chroot
 * are fchdir and fchroot of what open finds, access checks a stat as
 * the kernel's checks would, and pathconf is fpathconf's. What is no
 * handle to walk from is ENOTDIR; a path empty, ENOENT, and of PATH_MAX
 * bytes or more, ENAMETOOLONG; AT_EMPTY_PATH's is the descriptor's own.
 */
#include "sized.h"
#include "kernel.h"
#include "../protocol/calls.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/statfs.h>
#include <sys/statvfs.h>
#include <sys/un.h>
#include <time.h>
#include <unistd.h>

// The protocol's stat and statfs (lib/protocol/protocol.h).
struct __attribute__((packed)) Stat {
    uint64_t ino;
    uint64_t size;
    uint64_t mtime;
    uint32_t mode;
    uint32_t nlink;
    uint32_t uid;
    uint32_t gid;
    uint64_t atime;
    uint64_t dev;
    uint64_t ctime;
};

struct __attribute__((packed)) Statfs {
    uint64_t blocks;
    uint64_t bfree;
    uint64_t files;
    uint64_t ffree;
    uint64_t dev;
    uint32_t bsize;
    uint32_t namelen;
    uint32_t flags;
};

// What a call says after its own: its fields, then a path, or two, the
// first ended by its NUL.
struct Call {
    uint32_t call;
    uint32_t arg;
    const void* fields;
    size_t size;
    const char* path;
    const char* second;
};

// Where a path starts: the root of an absolute path, else dirfd.
static int startOf(int dirfd, const char* path) {
    return path[0] == '/' ? AT_FDROOT : dirfd;
}

static int checked(const char* path) {
    if (!path[0]) {
        errno = ENOENT;

        return -1;
    }

    if (strlen(path) >= PATH_MAX) {
        errno = ENAMETOOLONG;

        return -1;
    }

    return 0;
}

// A call of the VFS's of the handle at, the root and another descriptor
// but for -1 with it, AT_FDCWD and AT_FDROOT descriptors too, its answer into out of room and its descriptor
// into got but for null, of the ctl's flags: the answer's size, or -1 and
// errno, ENOTDIR of what is no handle.
static ssize_t call(int at, const struct Call* c, int other, void* out, size_t room, int* got, int flags) {
    unsigned char in[8 + 32 + 2 * PATH_MAX];
    size_t n = strlen(c->path);
    size_t m = c->second ? strlen(c->second) : 0;
    size_t size = 8 + c->size + n + (c->second ? 1 + m : 0);
    int fds[2] = {AT_FDROOT, other};

    if (size > sizeof in) {
        errno = ENAMETOOLONG;

        return -1;
    }

    memcpy(in, &c->call, 4);
    memcpy(in + 4, &c->arg, 4);
    memcpy(in + 8, c->fields, c->size);
    memcpy(in + 8 + c->size, c->path, n);

    if (c->second) {
        in[8 + c->size + n] = 0;
        memcpy(in + 8 + c->size + n + 1, c->second, m);
    }

    struct ctl x = {in, size, fds, other != -1 ? 2 : 1, out, room, got, got != NULL};

    if (ctl(at, &x, flags) != 0) {
        if (errno == ENOTTY) {
            errno = ENOTDIR;
        }

        return -1;
    }

    if (got && x.ngot != 1) {
        errno = EIO;

        return -1;
    }

    return (ssize_t)x.room;
}

// A call of one path from dirfd: as call.
static ssize_t pathCall(int dirfd, const char* path, uint32_t kind, uint32_t arg, const void* fields, size_t size, void* out,
                        size_t room) {
    if (checked(path) != 0) {
        return -1;
    }

    struct Call c = {kind, arg, fields, size, path, NULL};

    return call(startOf(dirfd, path), &c, -1, out, room, NULL, 0);
}

static void timeOf(uint64_t ns, struct timespec* out) {
    out->tv_sec = (time_t)(ns / 1000000000);
    out->tv_nsec = (long)(ns % 1000000000);
}

// The protocol's stat, as the kernel's fstat has it.
static void statOf(const struct Stat* in, struct stat* out) {
    memset(out, 0, sizeof *out);
    out->st_dev = in->dev;
    out->st_ino = in->ino;
    out->st_mode = in->mode;
    out->st_nlink = in->nlink;
    out->st_uid = in->uid;
    out->st_gid = in->gid;
    out->st_size = (off_t)in->size;
    out->st_blksize = 4096;
    out->st_blocks = (blkcnt_t)((in->size + 511) / 512);
    timeOf(in->atime, &out->st_atim);
    timeOf(in->mtime, &out->st_mtim);
    timeOf(in->ctime, &out->st_ctim);
}

int __kernel_openat(int dirfd, const char* path, int flags, va_list ap) {
    struct {
        uint32_t mode;
    } f = {0};
    int fd = -1;

    if ((flags & O_CREAT) || (flags & O_TMPFILE) == O_TMPFILE) {
        f.mode = va_arg(ap, mode_t) & 07777 & ~getumask();
    }

    if (checked(path) != 0) {
        return -1;
    }

    struct Call c = {ctlOpen, (uint32_t)(flags & ~O_CLOEXEC), &f, sizeof f, path, NULL};

    return call(startOf(dirfd, path), &c, -1, NULL, 0, &fd, flags & (O_CLOEXEC | O_NONBLOCK | O_NOCTTY)) < 0 ? -1 : fd;
}

int __kernel_open(const char* path, int flags, va_list ap) {
    return __kernel_openat(AT_FDCWD, path, flags, ap);
}

// A descriptor's own, AT_EMPTY_PATH's: of the working directory, ".".
static int emptyOf(int dirfd, const char* path, int flags) {
    return !path[0] && (flags & AT_EMPTY_PATH);
}

static int statAt(int dirfd, const char* path, struct stat* st, int flags) {
    struct Stat got;

    if (emptyOf(dirfd, path, flags)) {
        if (dirfd != AT_FDCWD) {
            return fstat(dirfd, st);
        }

        path = ".";
    }

    if (pathCall(dirfd, path, ctlStat, !(flags & AT_SYMLINK_NOFOLLOW), NULL, 0, &got, sizeof got) != sizeof got) {
        if (errno == 0) {
            errno = EIO;
        }

        return -1;
    }

    statOf(&got, st);

    return 0;
}

int __kernel_fstatat(int dirfd, const char* restrict path, struct stat* restrict st, size_t size, int flags) {
    sized("fstatat", size, sizeof(struct stat));

    return statAt(dirfd, path, st, flags);
}

int __kernel_stat(const char* restrict path, struct stat* restrict st, size_t size) {
    sized("stat", size, sizeof(struct stat));

    return statAt(AT_FDCWD, path, st, 0);
}

int __kernel_lstat(const char* restrict path, struct stat* restrict st, size_t size) {
    sized("lstat", size, sizeof(struct stat));

    return statAt(AT_FDCWD, path, st, AT_SYMLINK_NOFOLLOW);
}

// Whether ids may do what of want a file of st lets: the owner's bits of
// the owner, the group's of a group of the ids', else the others'; 0 may
// read and write anything, and execute what anyone may or a directory.
static int permits(const struct stat* st, int want, int effective) {
    uid_t uid = effective ? geteuid() : getuid();
    gid_t gid = effective ? getegid() : getgid();
    mode_t bits = st->st_mode;

    if (uid == 0) {
        return !(want & X_OK) || S_ISDIR(st->st_mode) || (st->st_mode & 0111);
    }

    if (uid == st->st_uid) {
        bits >>= 6;
    } else {
        gid_t groups[NGROUPS_MAX];
        int n = getgroups(NGROUPS_MAX, groups);
        int in = gid == st->st_gid;

        for (int i = 0; i < n && !in; i++) {
            in = groups[i] == st->st_gid;
        }

        if (in) {
            bits >>= 3;
        }
    }

    return (bits & want) == want;
}

int __kernel_faccessat(int dirfd, const char* path, int mode, int flags) {
    struct stat st;

    if ((mode & ~(R_OK | W_OK | X_OK)) || (flags & ~(AT_EACCESS | AT_SYMLINK_NOFOLLOW | AT_EMPTY_PATH))) {
        errno = EINVAL;

        return -1;
    }

    if (statAt(dirfd, path, &st, flags & (AT_SYMLINK_NOFOLLOW | AT_EMPTY_PATH)) != 0) {
        return -1;
    }

    if (mode != F_OK && !permits(&st, mode, flags & AT_EACCESS)) {
        errno = EACCES;

        return -1;
    }

    return 0;
}

int __kernel_access(const char* path, int mode) {
    return __kernel_faccessat(AT_FDCWD, path, mode, 0);
}

int __kernel_eaccess(const char* path, int mode) {
    return __kernel_faccessat(AT_FDCWD, path, mode, AT_EACCESS);
}

int __kernel_mkdirat(int dirfd, const char* path, mode_t mode) {
    return pathCall(dirfd, path, ctlMkdir, mode & 07777 & ~getumask(), NULL, 0, NULL, 0) < 0 ? -1 : 0;
}

int __kernel_mkdir(const char* path, mode_t mode) {
    return __kernel_mkdirat(AT_FDCWD, path, mode);
}

// A FIFO's, a socket's or a regular file's node: EPERM of a device's.
int __kernel_mknodat(int dirfd, const char* path, mode_t mode, dev_t dev) {
    (void)dev;

    switch (mode & S_IFMT) {
        case 0:
        case S_IFREG:
        case S_IFIFO:
        case S_IFSOCK:
            break;
        default:
            errno = EPERM;

            return -1;
    }

    return pathCall(dirfd, path, ctlMknod, mode & ~(getumask() & 0777), NULL, 0, NULL, 0) < 0 ? -1 : 0;
}

int __kernel_mknod(const char* path, mode_t mode, dev_t dev) {
    return __kernel_mknodat(AT_FDCWD, path, mode, dev);
}

int __kernel_symlinkat(const char* target, int dirfd, const char* path) {
    if (!target[0]) {
        errno = ENOENT;

        return -1;
    }

    if (checked(path) != 0) {
        return -1;
    }

    struct Call c = {ctlSymlink, 0, NULL, 0, target, path};

    return call(startOf(dirfd, path), &c, -1, NULL, 0, NULL, 0) < 0 ? -1 : 0;
}

int __kernel_symlink(const char* target, const char* path) {
    return __kernel_symlinkat(target, AT_FDCWD, path);
}

// A link's target, of O_PATH's descriptor of it with an empty path.
ssize_t __kernel_readlinkat(int dirfd, const char* restrict path, char* restrict buf, size_t n) {
    char target[PATH_MAX];
    ssize_t got;

    if (!path[0]) {
        int flags = dirfd == AT_FDCWD ? O_PATH : fcntl(dirfd, F_GETFL);

        if (flags < 0) {
            return -1;
        }

        if (!(flags & O_PATH)) {
            errno = ENOENT;

            return -1;
        }

        struct Call c = {ctlReadlink, 0, NULL, 0, "", NULL};

        got = call(dirfd, &c, -1, target, sizeof target, NULL, 0);
    } else {
        got = pathCall(dirfd, path, ctlReadlink, 0, NULL, 0, target, sizeof target);
    }

    if (got < 0) {
        return -1;
    }

    size_t k = (size_t)got < n ? (size_t)got : n;

    memcpy(buf, target, k);

    return (ssize_t)k;
}

ssize_t __kernel_readlink(const char* restrict path, char* restrict buf, size_t n) {
    return __kernel_readlinkat(AT_FDCWD, path, buf, n);
}

int __kernel_unlinkat(int dirfd, const char* path, int flags) {
    if (flags & ~AT_REMOVEDIR) {
        errno = EINVAL;

        return -1;
    }

    return pathCall(dirfd, path, ctlUnlink, (uint32_t)flags, NULL, 0, NULL, 0) < 0 ? -1 : 0;
}

int __kernel_unlink(const char* path) {
    return __kernel_unlinkat(AT_FDCWD, path, 0);
}

int __kernel_rmdir(const char* path) {
    return __kernel_unlinkat(AT_FDCWD, path, AT_REMOVEDIR);
}

// A call of two paths, the second's start its other descriptor.
static int twoPaths(uint32_t kind, uint32_t arg, int olddir, const char* old, int newdir, const char* new) {
    if (checked(old) != 0 || checked(new) != 0) {
        return -1;
    }

    struct Call c = {kind, arg, NULL, 0, old, new};

    return call(startOf(olddir, old), &c, startOf(newdir, new), NULL, 0, NULL, 0) < 0 ? -1 : 0;
}

int __kernel_renameat(int olddir, const char* old, int newdir, const char* new) {
    return twoPaths(ctlRename, 0, olddir, old, newdir, new);
}

int __kernel_rename(const char* old, const char* new) {
    return twoPaths(ctlRename, 0, AT_FDCWD, old, AT_FDCWD, new);
}

int __kernel_linkat(int olddir, const char* old, int newdir, const char* new, int flags) {
    if (flags & ~AT_SYMLINK_FOLLOW) {
        errno = EINVAL;

        return -1;
    }

    return twoPaths(ctlLink, (flags & AT_SYMLINK_FOLLOW) != 0, olddir, old, newdir, new);
}

int __kernel_link(const char* old, const char* new) {
    return twoPaths(ctlLink, 0, AT_FDCWD, old, AT_FDCWD, new);
}

int __kernel_fchmodat(int dirfd, const char* path, mode_t mode, int flags) {
    const uint32_t f = (uint32_t)(flags & AT_SYMLINK_NOFOLLOW);

    if (emptyOf(dirfd, path, flags)) {
        return dirfd == AT_FDCWD ? __kernel_fchmodat(AT_FDCWD, ".", mode, 0) : fchmod(dirfd, mode);
    }

    return pathCall(dirfd, path, ctlChmod, mode & 07777, &f, sizeof f, NULL, 0) < 0 ? -1 : 0;
}

int __kernel_chmod(const char* path, mode_t mode) {
    return __kernel_fchmodat(AT_FDCWD, path, mode, 0);
}

int __kernel_lchmod(const char* path, mode_t mode) {
    return __kernel_fchmodat(AT_FDCWD, path, mode, AT_SYMLINK_NOFOLLOW);
}

int __kernel_fchownat(int dirfd, const char* path, uid_t uid, gid_t gid, int flags) {
    const uint32_t f[3] = {(uint32_t)(flags & AT_SYMLINK_NOFOLLOW), uid, gid};

    if (emptyOf(dirfd, path, flags)) {
        return dirfd == AT_FDCWD ? __kernel_fchownat(AT_FDCWD, ".", uid, gid, 0) : fchown(dirfd, uid, gid);
    }

    return pathCall(dirfd, path, ctlChown, 0, f, sizeof f, NULL, 0) < 0 ? -1 : 0;
}

int __kernel_chown(const char* path, uid_t uid, gid_t gid) {
    return __kernel_fchownat(AT_FDCWD, path, uid, gid, 0);
}

int __kernel_lchown(const char* path, uid_t uid, gid_t gid) {
    return __kernel_fchownat(AT_FDCWD, path, uid, gid, AT_SYMLINK_NOFOLLOW);
}

// The times of utimensat, now, kept or set: when's bits, the times in
// nanoseconds; 0, or -1 and EINVAL.
static int timesOf(const struct timespec* t, uint32_t* when, uint64_t ns[2]) {
    *when = 0;
    ns[0] = ns[1] = 0;

    if (!t) {
        *when = 2 | 4;

        return 0;
    }

    for (int i = 0; i < 2; i++) {
        if (t[i].tv_nsec == UTIME_NOW) {
            *when |= i == 0 ? 2 : 4;
        } else if (t[i].tv_nsec == UTIME_OMIT) {
            *when |= i == 0 ? 8 : 16;
        } else if (t[i].tv_nsec < 0 || t[i].tv_nsec >= 1000000000) {
            errno = EINVAL;

            return -1;
        } else {
            ns[i] = (uint64_t)t[i].tv_sec * 1000000000 + (uint64_t)t[i].tv_nsec;
        }
    }

    return 0;
}

int __kernel_utimensat(int dirfd, const char* path, const struct timespec t[2], size_t size, int flags) {
    sized("utimensat", size, sizeof(struct timespec));

    struct __attribute__((packed)) {
        uint32_t flags;
        uint64_t ns[2];
    } f = {(uint32_t)(flags & AT_SYMLINK_NOFOLLOW), {0, 0}};
    uint32_t when;

    if (!path) {
        return futimens(dirfd, t);
    }

    if (timesOf(t, &when, f.ns) != 0) {
        return -1;
    }

    return pathCall(dirfd, path, ctlUtimes, when, &f, sizeof f, NULL, 0) < 0 ? -1 : 0;
}

int __kernel_truncate(const char* path, off_t size) {
    const uint64_t n = (uint64_t)size;

    if (size < 0) {
        errno = EINVAL;

        return -1;
    }

    return pathCall(AT_FDCWD, path, ctlTruncate, 0, &n, sizeof n, NULL, 0) < 0 ? -1 : 0;
}

static int statfsOf(const char* path, struct Statfs* out) {
    return pathCall(AT_FDCWD, path, ctlStatfs, 0, NULL, 0, out, sizeof *out) == sizeof *out ? 0 : -1;
}

int __kernel_statfs(const char* path, struct statfs* st, size_t size) {
    sized("statfs", size, sizeof(struct statfs));

    struct Statfs got;

    if (statfsOf(path, &got) != 0) {
        return -1;
    }

    memset(st, 0, sizeof *st);
    st->f_type = 0x70697a64;
    st->f_bsize = got.bsize;
    st->f_blocks = got.blocks;
    st->f_bfree = got.bfree;
    st->f_bavail = got.bfree;
    st->f_files = got.files;
    st->f_ffree = got.ffree;
    st->f_fsid.__val[0] = (int)(uint32_t)got.dev;
    st->f_fsid.__val[1] = (int)(uint32_t)(got.dev >> 32);
    st->f_namelen = got.namelen;
    st->f_frsize = got.bsize;
    st->f_flags = got.flags;

    return 0;
}

int __kernel_statvfs(const char* restrict path, struct statvfs* restrict st, size_t size) {
    sized("statvfs", size, sizeof(struct statvfs));

    struct Statfs got;

    if (statfsOf(path, &got) != 0) {
        return -1;
    }

    memset(st, 0, sizeof *st);
    st->f_bsize = got.bsize;
    st->f_frsize = got.bsize;
    st->f_blocks = got.blocks;
    st->f_bfree = got.bfree;
    st->f_bavail = got.bfree;
    st->f_files = got.files;
    st->f_ffree = got.ffree;
    st->f_favail = got.ffree;
    st->f_fsid = (uint32_t)got.dev;
    st->f_flag = got.flags;
    st->f_namemax = got.namelen;

    return 0;
}

// The path of the root to path, of no link, "." or "..": into resolved,
// of PATH_MAX bytes, or new memory.
char* __kernel_realpath(const char* restrict path, char* restrict resolved) {
    char out[PATH_MAX + 1];
    ssize_t n = pathCall(AT_FDCWD, path, ctlRealpath, 0, NULL, 0, out, sizeof out);

    if (n < 0) {
        return NULL;
    }

    if (n >= PATH_MAX) {
        errno = ENAMETOOLONG;

        return NULL;
    }

    out[n] = 0;

    return resolved ? strcpy(resolved, out) : strdup(out);
}

char* __kernel_getcwd(char* buf, size_t size) {
    char out[PATH_MAX + 1];
    struct Call c = {ctlPath, 0, NULL, 0, "", NULL};
    ssize_t n = call(AT_FDCWD, &c, -1, out, sizeof out, NULL, 0);

    if (n < 0) {
        return NULL;
    }

    if (n >= PATH_MAX) {
        errno = ENAMETOOLONG;

        return NULL;
    }

    out[n] = 0;

    if (!buf) {
        return strdup(out);
    }

    if ((size_t)n >= size) {
        errno = size ? ERANGE : EINVAL;

        return NULL;
    }

    return strcpy(buf, out);
}

// A directory's handle, O_PATH's, its descriptor called with, then
// closed, the call's errno kept.
static int withDirectory(const char* path, int (*with)(int)) {
    int d = open(path, O_PATH | O_DIRECTORY | O_CLOEXEC);

    if (d < 0) {
        return -1;
    }

    int r = with(d);
    int saved = errno;

    close(d);
    errno = saved;

    return r;
}

int __kernel_chdir(const char* path) {
    return withDirectory(path, fchdir);
}

int __kernel_chroot(const char* path) {
    return withDirectory(path, fchroot);
}

long __kernel_pathconf(const char* path, int name) {
    int f = open(path, O_PATH | O_CLOEXEC);

    if (f < 0) {
        return -1;
    }

    long r = fpathconf(f, name);
    int saved = errno;

    close(f);
    errno = saved;

    return r;
}

// A mount of a directory, or of the file system whose socket source is,
// connected to: target is mounted on.
int __kernel_mount(const char* source, const char* target, const char* type, unsigned long flags, const void* data) {
    (void)type;
    (void)flags;
    (void)data;

    struct stat st;
    int conn = -1;

    if (checked(source) != 0 || checked(target) != 0) {
        return -1;
    }

    if (stat(source, &st) == 0 && S_ISSOCK(st.st_mode)) {
        struct sockaddr_un a = {AF_UNIX, {0}};

        if (strlen(source) >= sizeof a.sun_path) {
            errno = ENAMETOOLONG;

            return -1;
        }

        strcpy(a.sun_path, source);

        if ((conn = socket(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0)) < 0) {
            return -1;
        }

        if (connect(conn, (const struct sockaddr*)&a, sizeof a) != 0) {
            int saved = errno;

            close(conn);
            errno = saved;

            return -1;
        }
    }

    struct Call c = {ctlMount, 0, NULL, 0, target, source};
    int r = call(AT_FDCWD, &c, conn, NULL, 0, NULL, 0) < 0 ? -1 : 0;
    int saved = errno;

    if (conn >= 0) {
        close(conn);
    }

    errno = saved;

    return r;
}

int __kernel_umount2(const char* target, int flags) {
    return pathCall(AT_FDCWD, target, ctlUnmount, (uint32_t)(flags & UMOUNT_NOFOLLOW), NULL, 0, NULL, 0) < 0 ? -1 : 0;
}

int __kernel_umount(const char* target) {
    return __kernel_umount2(target, 0);
}
