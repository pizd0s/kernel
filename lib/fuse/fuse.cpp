// The system's libfuse: libfuse 2.9's high-level API (fuse.h) over the
// file protocol (lib/protocol). fuse_main registers the file system at
// rootfs's /dev/init by the name its mount point argument gives, S_IFSOCK,
// a file system to mount (mount <where> /dev/<name>), and serves it on one
// thread: a request on an endpoint is a call of the file system's
// operations on the path the endpoint is open on, answered before the
// next. The context of each call is the request's uid and gid.
//
// Every endpoint is an open directory or file of the file system: the root
// an OPEN rootfs hands on from the name opens, a name an OPEN opens, or the
// directory a walk ends at. A walk always ends kept, its reply's endpoint
// that directory: the kernel may have asked another server, which handed
// the walk on. A walk out of the root by ".." goes on to rootfs, "../" and
// the names left, through the endpoint of the mount point it came in by
// (MOUNTED), and the path of a directory starts with the mount point's,
// which rootfs gives. A file's bytes pass through this process's memory:
// the file system reads into it, and they go on into the reader's through
// the memfd a READ brings.
//
// The file system is mounted once: when rootfs lets go of the root it was
// mounted by, it is destroyed and fuse_main returns, as libfuse's does
// when its mount point is unmounted.

#include "fuse.h"

#include <lib/protocol/service.h>

#include <dirent.h>
#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/param.h>
#include <sys/socket.h>
#include <sys/uio.h>
#include <unistd.h>

namespace {
    // A mount the file system is under: the endpoint of the mount point
    // rootfs gave it, while an endpoint that came in by it is open.
    struct Mount {
        int up;
        uint32_t refs;
    };

    // An entry of a directory, as a READDIR from its start read them.
    struct Entry {
        uint64_t ino;
        uint8_t type;
        char* name;
    };

    // An endpoint's: the path it is open on, from the file system's root;
    // the file system's information of the open, once it has opened it; the
    // mount it came in by, root for the endpoint rootfs mounted.
    struct Open {
        char* path;
        bool directory;
        bool opened;
        bool root;
        fuse_file_info info;
        Mount* mount;
        Entry* entries;
        size_t count;
        size_t room;
        Open* next;
        Open* previous;
    };

    // The names of a walk: the path's, and before them the targets of the
    // links it went through, the last on top.
    struct Part {
        const char* at;
        size_t length;
        char* own;
    };

    struct Walk {
        Part stack[41];
        int depth;
    };

    static fuse_operations operations;
    static fuse_context context;
    static uint64_t device;
    static int epoll = -1;
    static Open* opens;
    static char bytes[128 * 1024];

    static void* allocate(size_t size) {
        void* p = calloc(1, size);
        if (!p) {
            fatal("fuse: out of memory");
        }
        return p;
    }

    static char* copied(const char* s, size_t length) {
        auto* copy = static_cast<char*>(allocate(length + 1));
        memcpy(copy, s, length);
        return copy;
    }

    // A name of a directory's path: its path and the name.
    static char* joined(const char* directory, const char* name, size_t length) {
        const size_t base = strcmp(directory, "/") == 0 ? 0 : strlen(directory);
        auto* path = static_cast<char*>(allocate(base + 1 + length + 1));
        memcpy(path, directory, base);
        path[base] = '/';
        memcpy(path + base + 1, name, length);
        return path;
    }

    // The directory of a path: "/" for the root and what is in it.
    static char* parentOf(const char* path) {
        const char* slash = strrchr(path, '/');
        if (!slash || slash == path) {
            return copied("/", 1);
        }
        return copied(path, size_t(slash - path));
    }

    static int result(int returned) {
        return returned < 0 ? -returned : 0;
    }

    static int attributes(const char* path, struct stat& st) {
        memset(&st, 0, sizeof st);
        return operations.getattr ? result(operations.getattr(path, &st)) : ENOSYS;
    }

    static uint64_t nanoseconds(const timespec& t) {
        return uint64_t(t.tv_sec) * 1000000000 + uint64_t(t.tv_nsec);
    }

    static Stat protocolStat(const struct stat& st) {
        Stat out{};
        out.ino = uint64_t(st.st_ino);
        out.size = uint64_t(st.st_size);
        out.mtime = nanoseconds(st.st_mtim);
        out.mode = uint32_t(st.st_mode);
        out.nlink = uint32_t(st.st_nlink);
        out.uid = uint32_t(st.st_uid);
        out.gid = uint32_t(st.st_gid);
        out.atime = nanoseconds(st.st_atim);
        out.dev = device;
        out.ctime = nanoseconds(st.st_ctim);
        return out;
    }

    static void answerStat(int fd, const struct stat& st) {
        const Stat out = protocolStat(st);
        answer(fd, opOk, 0, &out, sizeof out);
    }

    static void answerStatfs(int fd, const char* path) {
        struct statvfs st{};
        if (!operations.statfs) {
            return fail(fd, ENOSYS);
        }
        if (const int e = result(operations.statfs(path, &st))) {
            return fail(fd, e);
        }
        const Statfs out{st.f_blocks, st.f_bfree, st.f_files, st.f_ffree, device, uint32_t(st.f_bsize), uint32_t(st.f_namemax), (st.f_flag & ST_RDONLY) ? 1u : 0u};
        answer(fd, opOk, 0, &out, sizeof out);
    }

    // Whether path is the root of the process that asks, which a walk does
    // not go above.
    static bool isProcessRoot(const char* path, uint64_t dev, uint64_t ino) {
        struct stat st;
        return dev == device && attributes(path, st) == 0 && uint64_t(st.st_ino) == ino;
    }

    static void push(Walk& w, const char* at, size_t length, char* own) {
        w.stack[w.depth++] = {at, length, own};
    }

    // The next name of a walk, or false at its end.
    static bool take(Walk& w, const char*& name, size_t& length) {
        for (; w.depth > 0; --w.depth) {
            Part& top = w.stack[w.depth - 1];
            while (top.length && *top.at == '/') {
                ++top.at;
                --top.length;
            }
            if (top.length) {
                const auto* slash = static_cast<const char*>(memchr(top.at, '/', top.length));
                name = top.at;
                length = slash ? size_t(slash - top.at) : top.length;
                top.at += length;
                top.length -= length;
                return true;
            }
            free(top.own);
            top.own = nullptr;
        }
        return false;
    }

    static bool more(const Walk& w) {
        for (int i = 0; i < w.depth; ++i) {
            for (size_t k = 0; k < w.stack[i].length; ++k) {
                if (w.stack[i].at[k] != '/') {
                    return true;
                }
            }
        }
        return false;
    }

    static void forget(Walk& w) {
        for (int i = 0; i < w.depth; ++i) {
            free(w.stack[i].own);
        }
        w.depth = 0;
    }

    // What is left of a walk, after first and joined by "/", into out: the
    // names a walk handed on goes on with. False when it does not fit.
    static bool rest(Walk& w, const char* first, char* out, size_t room, size_t& length) {
        length = strlen(first);
        if (length > room) {
            return false;
        }
        memcpy(out, first, length);
        const char* name;
        size_t n;
        while (take(w, name, n)) {
            if (n + 1 > room - length) {
                return false;
            }
            out[length++] = '/';
            memcpy(out + length, name, n);
            length += n;
        }
        return true;
    }

    // An endpoint of path, watched: the request's reply endpoint, the open's
    // from now on.
    static Open* keep(Request& r, char* path, bool directory, Mount* mount, bool reads, bool writes) {
        auto* o = static_cast<Open*>(allocate(sizeof(Open)));
        o->path = path;
        o->directory = directory;
        o->mount = mount;
        if (mount) {
            ++mount->refs;
        }
        o->next = opens;
        if (opens) {
            opens->previous = o;
        }
        opens = o;
        watch(epoll, {r.reply(), false, reads, writes, o});
        r.fds[r.count - 1] = -1;
        return o;
    }

    static void clearEntries(Open& o) {
        for (size_t i = 0; i < o.count; ++i) {
            free(o.entries[i].name);
        }
        o.count = 0;
    }

    // A walk handed on through a mount's endpoint, as RESOLVE: the kernel's
    // reply endpoint goes with it, rootfs answers.
    static void handUp(const Request& r, const Mount* mount, uint64_t dev, uint64_t ino, uint32_t links, const char* path, size_t length) {
        unsigned char body[20];
        memcpy(body, &dev, 8);
        memcpy(body + 8, &ino, 8);
        memcpy(body + 16, &links, 4);
        Header h{opResolve, resolveHanded, r.h.uid, r.h.gid};
        const iovec parts[] = {{&h, sizeof h}, {body, sizeof body}, {const_cast<char*>(path), length}};
        if (sendPacket(mount->up, parts, 3, r.fds, r.count) < 0) {
            fail(r.reply(), EIO);
        }
    }

    // A walk to an absolute link's target: the kernel goes on from the root
    // of the process with the names left.
    static void fromRoot(Walk& w, const Request& r, uint32_t links, bool slash) {
        static char left[PATH_MAX + 2];
        static unsigned char out[16 + sizeof left];
        size_t length;
        if (!rest(w, "", left, sizeof left - 1, length)) {
            return fail(r.reply(), ENAMETOOLONG);
        }
        if (slash) {
            left[length++] = '/';
        }
        const size_t skip = length > 0 && left[0] == '/' ? 1 : 0;
        const uint32_t fields[4] = {stepRoot, links, lastNone, 0};
        memcpy(out, fields, sizeof fields);
        memcpy(out + sizeof fields, left + skip, length - skip);
        answer(r.reply(), opOk, 0, out, sizeof fields + length - skip);
    }

    // A walk from an endpoint's directory to the directory of its last name,
    // as rootfs walks one (rootfs.cpp resolve).
    static void resolve(Open& o, Request& r) {
        uint64_t dev;
        uint64_t ino;
        uint32_t links;
        if (!r.at(0, dev) || !r.at(8, ino) || !r.at(16, links)) {
            return fail(r.reply(), EINVAL);
        }
        const char* path = reinterpret_cast<const char*>(r.body + 20);
        const size_t size = r.size - 20;
        const bool slash = size > 0 && path[size - 1] == '/';
        Walk w{};
        push(w, path, size, nullptr);
        char* here = copied(o.path, strlen(o.path));
        const char* name = nullptr;
        size_t length = 0;
        static char left[PATH_MAX + 4];
        size_t leftLength;
        bool have = take(w, name, length);
        for (; have && more(w); have = take(w, name, length)) {
            if (length == 1 && name[0] == '.') {
                continue;
            }
            if (length == 2 && name[0] == '.' && name[1] == '.') {
                if (isProcessRoot(here, dev, ino)) {
                    continue;
                }
                if (strcmp(here, "/") == 0) {
                    if (o.mount) {
                        const bool fits = rest(w, "..", left, sizeof left, leftLength);
                        free(here);
                        forget(w);
                        return fits ? handUp(r, o.mount, dev, ino, links, left, leftLength) : fail(r.reply(), ENAMETOOLONG);
                    }
                    continue;
                }
                char* up = parentOf(here);
                free(here);
                here = up;
                continue;
            }
            char* next = joined(here, name, length);
            struct stat st;
            int e = attributes(next, st);
            if (e == 0 && S_ISLNK(st.st_mode)) {
                e = links >= 40 ? ELOOP : 0;
                char* target = e == 0 ? static_cast<char*>(allocate(PATH_MAX)) : nullptr;
                if (e == 0) {
                    e = operations.readlink ? result(operations.readlink(next, target, PATH_MAX)) : ENOSYS;
                }
                free(next);
                if (e == 0 && target[0] == 0) {
                    e = ENOENT;
                }
                if (e != 0) {
                    free(target);
                    free(here);
                    forget(w);
                    return fail(r.reply(), e);
                }
                ++links;
                push(w, target, strlen(target), target);
                if (target[0] == '/') {
                    free(here);
                    fromRoot(w, r, links, slash);
                    return forget(w);
                }
                continue;
            }
            if (e == 0 && !S_ISDIR(st.st_mode)) {
                e = ENOTDIR;
            }
            if (e != 0) {
                free(next);
                free(here);
                forget(w);
                return fail(r.reply(), e);
            }
            free(here);
            here = next;
        }
        uint32_t last = lastNone;
        if (have && length == 1 && name[0] == '.') {
            last = lastDot;
        } else if (have && length == 2 && name[0] == '.' && name[1] == '.') {
            if (strcmp(here, "/") == 0 && o.mount && !isProcessRoot(here, dev, ino)) {
                free(here);
                forget(w);
                return handUp(r, o.mount, dev, ino, links, "..", 2);
            }
            last = lastDotdot;
            if (!isProcessRoot(here, dev, ino)) {
                char* up = parentOf(here);
                free(here);
                here = up;
            }
        } else if (have && length > NAME_MAX) {
            free(here);
            forget(w);
            return fail(r.reply(), ENAMETOOLONG);
        } else if (have) {
            last = lastName;
        }
        static unsigned char out[16 + NAME_MAX];
        const uint32_t fields[4] = {stepDone, links, last, (slash && last == lastName ? stepSlash : 0) | stepKept};
        const size_t named = last == lastName ? length : 0;
        memcpy(out, fields, sizeof fields);
        memcpy(out + sizeof fields, name, named);
        answer(r.reply(), opOk, 0, out, sizeof fields + named);
        keep(r, here, true, o.mount, false, false);
        forget(w);
    }

    // The name a call is on, after what comes before it in the body: the
    // path of it in the endpoint's directory, or of the directory itself
    // for "".
    static char* pathOfName(const Open& o, const Request& r, size_t at, bool& own) {
        const char* name = reinterpret_cast<const char*>(r.body + at);
        const size_t length = r.size > at ? strnlen(name, r.size - at) : 0;
        own = true;
        return length == 0 ? copied(o.path, strlen(o.path)) : joined(o.path, name, length);
    }

    // An open of a name of the directory, or of the directory itself: made
    // with O_CREAT, emptied with O_TRUNC, the file system's open or opendir
    // of it; a link is ELOOP for the kernel to follow but with O_PATH and
    // O_NOFOLLOW. A FIFO is the kernel's pipe, a device has no driver here.
    static void openName(Open& o, Request& r) {
        uint32_t mode;
        bool own;
        if (!r.at(0, mode)) {
            return fail(r.reply(), EINVAL);
        }
        const uint32_t flags = r.h.arg;
        const bool opath = (flags & O_PATH) != 0;
        const uint32_t access = flags & O_ACCMODE;
        const bool writes = !opath && access != O_RDONLY;
        const bool named = r.size > 4 && r.body[4] != 0;
        char* path = pathOfName(o, r, 4, own);
        struct stat st;
        fuse_file_info info{};
        info.flags = int(flags & ~uint32_t(O_CREAT | O_EXCL | O_NOCTTY | O_TRUNC));
        bool opened = false;
        int e = attributes(path, st);
        if (e == ENOENT && named && (flags & O_CREAT) != 0) {
            if (operations.create) {
                info.flags = int(flags & ~uint32_t(O_NOCTTY | O_TRUNC));
                e = result(operations.create(path, S_IFREG | (mode & 07777), &info));
                opened = e == 0;
            } else if (operations.mknod) {
                e = result(operations.mknod(path, S_IFREG | (mode & 07777), 0));
            }
            if (e == 0) {
                e = attributes(path, st);
            }
        } else if (e == 0 && named && (flags & O_CREAT) != 0 && (flags & O_EXCL) != 0) {
            e = EEXIST;
        } else if (e == 0 && S_ISLNK(st.st_mode) && !(opath && (flags & O_NOFOLLOW) != 0)) {
            e = ELOOP;
        } else if (e == 0 && (flags & O_DIRECTORY) != 0 && !S_ISDIR(st.st_mode)) {
            e = ENOTDIR;
        } else if (e == 0 && S_ISDIR(st.st_mode) && writes) {
            e = EISDIR;
        } else if (e == 0 && !opath && (S_ISCHR(st.st_mode) || S_ISBLK(st.st_mode) || S_ISSOCK(st.st_mode))) {
            e = ENXIO;
        }
        if (e == 0 && !opened && !opath && S_ISDIR(st.st_mode) && operations.opendir) {
            e = result(operations.opendir(path, &info));
            opened = e == 0;
        } else if (e == 0 && !opened && !opath && S_ISREG(st.st_mode)) {
            if (writes && (flags & O_TRUNC) != 0 && operations.truncate) {
                e = result(operations.truncate(path, 0));
            }
            if (e == 0 && operations.open) {
                e = result(operations.open(path, &info));
                opened = e == 0;
            }
            if (e == 0) {
                e = attributes(path, st);
            }
        }
        if (e != 0) {
            if (opened && operations.release) {
                operations.release(path, &info);
            }
            free(path);
            return fail(r.reply(), e);
        }
        answerStat(r.reply(), st);
        Open* kept = keep(r, path, S_ISDIR(st.st_mode), o.mount, !opath && access != O_WRONLY, writes);
        kept->opened = opened;
        kept->info = info;
    }

    // The directory of a device and inode, a server's: one of this file
    // system's an endpoint is open on, or none.
    static const char* directoryOf(uint64_t dev, uint64_t ino) {
        if (dev != device) {
            return nullptr;
        }
        for (const Open* o = opens; o; o = o->next) {
            struct stat st;
            if (o->directory && attributes(o->path, st) == 0 && uint64_t(st.st_ino) == ino) {
                return o->path;
            }
        }
        return nullptr;
    }

    // The paths of the endpoints under from are under to now.
    static void moved(const char* from, const char* to) {
        const size_t length = strlen(from);
        for (Open* o = opens; o; o = o->next) {
            if (strncmp(o->path, from, length) != 0 || (o->path[length] != 0 && o->path[length] != '/')) {
                continue;
            }
            const size_t tail = strlen(o->path + length);
            auto* path = static_cast<char*>(allocate(strlen(to) + tail + 1));
            memcpy(path, to, strlen(to));
            memcpy(path + strlen(to), o->path + length, tail);
            free(o->path);
            o->path = path;
        }
    }

    // RENAME and LINK: a name of the directory and a name of the directory
    // of device and inode, the file system's or EXDEV.
    static int across(const Open& o, const Request& r) {
        uint64_t dev;
        uint64_t ino;
        if (!r.at(0, dev) || !r.at(8, ino) || r.size < 17) {
            return EINVAL;
        }
        const char* old = reinterpret_cast<const char*>(r.body + 16);
        const size_t oldLength = strnlen(old, r.size - 16);
        if (16 + oldLength + 1 >= r.size) {
            return EINVAL;
        }
        const char* name = old + oldLength + 1;
        const size_t length = r.size - 16 - oldLength - 1;
        const char* directory = directoryOf(dev, ino);
        if (!directory) {
            return EXDEV;
        }
        char* from = joined(o.path, old, oldLength);
        char* to = joined(directory, name, length);
        int e = ENOSYS;
        if (r.h.op == opRename && operations.rename) {
            e = result(operations.rename(from, to));
            if (e == 0) {
                moved(from, to);
            }
        } else if (r.h.op == opLink && operations.link) {
            e = result(operations.link(from, to));
        }
        free(from);
        free(to);
        return e;
    }

    // The times of UTIMES and FUTIMES: when's bits say now or kept, else
    // the nanoseconds given.
    static void timesOf(uint32_t when, const Request& r, timespec times[2]) {
        uint64_t given[2] = {0, 0};
        r.at(0, given[0]);
        r.at(8, given[1]);
        for (int i = 0; i < 2; ++i) {
            times[i].tv_sec = time_t(given[i] / 1000000000);
            times[i].tv_nsec = long(given[i] % 1000000000);
        }
        if ((when & whenAtimeNow) != 0) {
            times[0].tv_nsec = UTIME_NOW;
        } else if ((when & whenAtimeKept) != 0) {
            times[0].tv_nsec = UTIME_OMIT;
        }
        if ((when & whenMtimeNow) != 0) {
            times[1].tv_nsec = UTIME_NOW;
        } else if ((when & whenMtimeKept) != 0) {
            times[1].tv_nsec = UTIME_OMIT;
        }
    }

    static int changeTimes(const char* path, uint32_t when, const Request& r) {
        timespec times[2];
        timesOf(when, r, times);
        return operations.utimens ? result(operations.utimens(path, times)) : ENOSYS;
    }

    static int changeOwner(const char* path, const Request& r) {
        uint32_t uid;
        uint32_t gid;
        if (!r.at(0, uid) || !r.at(4, gid)) {
            return EINVAL;
        }
        return operations.chown ? result(operations.chown(path, uid_t(uid), gid_t(gid))) : ENOSYS;
    }

    // The calls on a name of the directory, or on the directory itself.
    static void nameCall(Open& o, Request& r) {
        size_t at = 0;
        switch (r.h.op) {
            case opChown:
            case opTruncate:
                at = 8;
                break;
            case opUtimes:
                at = 16;
                break;
            case opSymlink:
                at = r.size > 0 ? strnlen(reinterpret_cast<const char*>(r.body), r.size) + 1 : 0;
                break;
        }
        bool own;
        char* path = pathOfName(o, r, at, own);
        struct stat st;
        int e = 0;
        switch (r.h.op) {
            case opStat:
                e = attributes(path, st);
                if (e == 0) {
                    answerStat(r.reply(), st);
                }
                break;
            case opReadlink: {
                static char target[PATH_MAX];
                e = operations.readlink ? result(operations.readlink(path, target, sizeof target)) : ENOSYS;
                if (e == 0) {
                    answer(r.reply(), opOk, 0, target, strlen(target));
                }
                break;
            }
            case opStatfs:
                answerStatfs(r.reply(), path);
                break;
            case opMkdir:
                e = operations.mkdir ? result(operations.mkdir(path, mode_t(r.h.arg & 07777))) : ENOSYS;
                break;
            case opMknod:
                e = operations.mknod ? result(operations.mknod(path, mode_t(r.h.arg), 0)) : ENOSYS;
                break;
            case opUnlink:
                if ((r.h.arg & AT_REMOVEDIR) != 0) {
                    e = operations.rmdir ? result(operations.rmdir(path)) : ENOSYS;
                } else {
                    e = operations.unlink ? result(operations.unlink(path)) : ENOSYS;
                }
                break;
            case opSymlink:
                e = operations.symlink ? result(operations.symlink(reinterpret_cast<const char*>(r.body), path)) : ENOSYS;
                break;
            case opChmod:
                e = operations.chmod ? result(operations.chmod(path, mode_t(r.h.arg & 07777))) : ENOSYS;
                break;
            case opChown:
                e = changeOwner(path, r);
                break;
            case opUtimes:
                e = changeTimes(path, r.h.arg, r);
                break;
            case opTruncate: {
                uint64_t size;
                e = !r.at(0, size) ? EINVAL : operations.truncate ? result(operations.truncate(path, off_t(size))) : ENOSYS;
                break;
            }
        }
        free(path);
        if (e != 0) {
            fail(r.reply(), e);
        } else if (r.h.op != opStat && r.h.op != opReadlink && r.h.op != opStatfs) {
            answer(r.reply(), opOk, 0);
        }
    }

    // A READ: what the file system reads at off, into the memfd at woff.
    static void readFile(Open& o, const Endpoint& e, const Request& r) {
        uint64_t off;
        uint32_t woff;
        if (o.directory) {
            return fail(r.reply(), EISDIR);
        }
        if (!e.reads || !o.opened) {
            return fail(r.reply(), EBADF);
        }
        if (r.window() < 0 || !r.at(0, off) || !r.at(8, woff) || !operations.read) {
            return fail(r.reply(), EINVAL);
        }
        size_t done = 0;
        while (done < r.h.arg) {
            const size_t ask = MIN(sizeof bytes, size_t(r.h.arg) - done);
            const int got = operations.read(o.path, bytes, ask, off_t(off + done), &o.info);
            if (got < 0 && done == 0) {
                return fail(r.reply(), -got);
            }
            if (got <= 0) {
                break;
            }
            if (pwrite(r.window(), bytes, size_t(got), off_t(woff + done)) != got) {
                return fail(r.reply(), EIO);
            }
            done += size_t(got);
            if (size_t(got) < ask) {
                break;
            }
        }
        const uint32_t count = uint32_t(done);
        answer(r.reply(), opOk, 0, &count, sizeof count);
    }

    // A WRITE: the memfd's bytes at woff, what the file system writes at
    // off, or at its end with append.
    static void writeFile(Open& o, const Endpoint& e, const Request& r) {
        uint64_t off;
        uint32_t length;
        uint32_t woff;
        if (o.directory) {
            return fail(r.reply(), EISDIR);
        }
        if (!e.writes || !o.opened) {
            return fail(r.reply(), EBADF);
        }
        if (r.window() < 0 || !r.at(0, off) || !r.at(8, length) || !r.at(12, woff) || !operations.write) {
            return fail(r.reply(), EINVAL);
        }
        if ((r.h.arg & 1) != 0) {
            struct stat st;
            if (const int failed = attributes(o.path, st)) {
                return fail(r.reply(), failed);
            }
            off = uint64_t(st.st_size);
        }
        size_t done = 0;
        while (done < length) {
            const size_t n = MIN(sizeof bytes, size_t(length) - done);
            if (pread(r.window(), bytes, n, off_t(woff + done)) != ssize_t(n)) {
                return fail(r.reply(), EIO);
            }
            const int wrote = operations.write(o.path, bytes, n, off_t(off + done), &o.info);
            if (wrote < 0 && done == 0) {
                return fail(r.reply(), -wrote);
            }
            if (wrote <= 0) {
                break;
            }
            done += size_t(wrote);
            if (size_t(wrote) < n) {
                break;
            }
        }
        unsigned char wrote[12];
        const uint32_t count = uint32_t(done);
        memcpy(wrote, &count, 4);
        memcpy(wrote + 4, &off, 8);
        answer(r.reply(), opOk, 0, wrote, sizeof wrote);
    }

    static int fill(void* buffer, const char* name, const struct stat* st, off_t) {
        auto& o = *static_cast<Open*>(buffer);
        if (strcmp(name, ".") == 0 || strcmp(name, "..") == 0) {
            return 0;
        }
        if (o.count == o.room) {
            o.room = o.room ? o.room * 2 : 32;
            o.entries = static_cast<Entry*>(realloc(o.entries, o.room * sizeof(Entry)));
            if (!o.entries) {
                fatal("fuse: out of memory");
            }
        }
        o.entries[o.count++] = {st ? uint64_t(st->st_ino) : 0, st ? uint8_t((st->st_mode >> 12) & 017) : uint8_t(DT_UNKNOWN), copied(name, strlen(name))};
        return 0;
    }

    // The entries of the directory after a position, the index of one plus
    // one, as many as one reply holds; a reading from the start reads them
    // from the file system again.
    static void readDirectory(Open& o, const Request& r) {
        uint64_t after;
        if (!o.directory) {
            return fail(r.reply(), ENOTDIR);
        }
        if (!r.at(0, after) || !operations.readdir) {
            return fail(r.reply(), EINVAL);
        }
        if (!o.opened && operations.opendir) {
            if (const int e = result(operations.opendir(o.path, &o.info))) {
                return fail(r.reply(), e);
            }
        }
        o.opened = true;
        if (after == 0) {
            clearEntries(o);
            if (const int e = result(operations.readdir(o.path, &o, fill, 0, &o.info))) {
                return fail(r.reply(), e);
            }
        }
        static unsigned char out[65536 - sizeof(Header)];
        size_t size = 0;
        for (uint64_t i = after; i < o.count; ++i) {
            const Entry& entry = o.entries[i];
            const size_t length = strlen(entry.name);
            if (size + 17 + length + 1 > sizeof out) {
                break;
            }
            const uint64_t pos = i + 1;
            memcpy(out + size, &entry.ino, 8);
            memcpy(out + size + 8, &pos, 8);
            out[size + 16] = entry.type;
            memcpy(out + size + 17, entry.name, length + 1);
            size += 17 + length + 1;
        }
        answer(r.reply(), opOk, 0, out, size);
    }

    // The path of the endpoint from the root of the process: up to it, when
    // it is in the file system, or to the file system's root and then the
    // mount point's path, which rootfs gives.
    static void pathOf(const Open& o, const Request& r) {
        uint64_t dev;
        uint64_t ino;
        if (!r.at(0, dev) || !r.at(8, ino)) {
            return fail(r.reply(), EINVAL);
        }
        static char got[sizeof(Header) + PATH_MAX];
        const size_t length = strlen(o.path);
        for (size_t end = length; end > 0; --end) {
            if (end != length && o.path[end] != '/') {
                continue;
            }
            char* prefix = end == 1 ? copied("/", 1) : copied(o.path, end);
            const bool root = isProcessRoot(prefix, dev, ino);
            free(prefix);
            if (root) {
                const char* tail = end == 1 ? o.path : o.path + end;
                return answer(r.reply(), opOk, 0, *tail ? tail : "/", *tail ? strlen(tail) : 1);
            }
        }
        if (!o.mount) {
            return fail(r.reply(), ENOENT);
        }
        int pair[2];
        if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
            return fail(r.reply(), errno);
        }
        Header h{opPath, 0, r.h.uid, r.h.gid};
        const iovec parts[] = {{&h, sizeof h}, {const_cast<unsigned char*>(r.body), 16}};
        const bool sent = sendPacket(o.mount->up, parts, 2, &pair[1], 1) >= 0;
        close(pair[1]);
        const ssize_t n = sent ? recv(pair[0], got, sizeof got, 0) : -1;
        close(pair[0]);
        Header reply;
        if (n < ssize_t(sizeof reply)) {
            return fail(r.reply(), EIO);
        }
        memcpy(&reply, got, sizeof reply);
        if (reply.op != opOk) {
            return fail(r.reply(), int(reply.arg));
        }
        size_t prefix = size_t(n) - sizeof reply;
        if (prefix == 1 && got[sizeof reply] == '/' && strcmp(o.path, "/") != 0) {
            prefix = 0;
        }
        const size_t tail = strcmp(o.path, "/") == 0 ? 0 : length;
        if (prefix + tail > PATH_MAX) {
            return fail(r.reply(), ENAMETOOLONG);
        }
        memcpy(got + sizeof reply + prefix, o.path, tail);
        answer(r.reply(), opOk, 0, got + sizeof reply, prefix + tail);
    }

    // The calls on the endpoint's own file.
    static void fileCall(Open& o, const Endpoint& e, Request& r) {
        struct stat st;
        int failed = 0;
        switch (r.h.op) {
            case opFstat:
                if (o.opened && !o.directory && operations.fgetattr) {
                    memset(&st, 0, sizeof st);
                    failed = result(operations.fgetattr(o.path, &st, &o.info));
                } else {
                    failed = attributes(o.path, st);
                }
                if (failed == 0) {
                    return answerStat(r.reply(), st);
                }
                break;
            case opFstatfs:
                return answerStatfs(r.reply(), o.path);
            case opFchmod:
                failed = operations.chmod ? result(operations.chmod(o.path, mode_t(r.h.arg & 07777))) : ENOSYS;
                break;
            case opFchown:
                failed = changeOwner(o.path, r);
                break;
            case opFutimes:
                failed = changeTimes(o.path, r.h.arg, r);
                break;
            case opFtruncate: {
                uint64_t size;
                if (!e.writes || !r.at(0, size)) {
                    failed = EINVAL;
                } else if (o.opened && operations.ftruncate) {
                    failed = result(operations.ftruncate(o.path, off_t(size), &o.info));
                } else {
                    failed = operations.truncate ? result(operations.truncate(o.path, off_t(size))) : ENOSYS;
                }
                break;
            }
            case opFsync:
                if (o.opened && o.directory && operations.fsyncdir) {
                    failed = result(operations.fsyncdir(o.path, int(r.h.arg), &o.info));
                } else if (o.opened && !o.directory && operations.fsync) {
                    failed = result(operations.fsync(o.path, int(r.h.arg), &o.info));
                }
                break;
            case opLock: {
                // No lock of the file system's is in the way of another.
                uint32_t type;
                uint64_t start;
                uint64_t length;
                if (r.h.arg == 1 && r.at(0, type) && r.at(4, start) && r.at(12, length)) {
                    unsigned char none[20];
                    const uint32_t unlocked = F_UNLCK;
                    memcpy(none, &unlocked, 4);
                    memcpy(none + 4, &start, 8);
                    memcpy(none + 12, &length, 8);
                    return answer(r.reply(), opOk, 0, none, sizeof none);
                }
                break;
            }
        }
        if (failed != 0) {
            return fail(r.reply(), failed);
        }
        answer(r.reply(), opOk, 0);
    }

    static bool handle(Endpoint& e, Request& r) {
        context.uid = uid_t(r.h.uid);
        context.gid = gid_t(r.h.gid);
        if (e.channel) {
            // The root: what rootfs hands on of the name, mount(2)'s open.
            struct stat st;
            if (const int failed = attributes("/", st)) {
                fail(r.reply(), failed);
                return false;
            }
            answerStat(r.reply(), st);
            keep(r, copied("/", 1), true, nullptr, true, false);
            return false;
        }
        Open& o = *static_cast<Open*>(e.of);
        switch (r.h.op) {
            case opMounted:
                if (!o.mount && r.count == 1) {
                    o.mount = static_cast<Mount*>(allocate(sizeof(Mount)));
                    *o.mount = {r.fds[0], 1};
                    o.root = true;
                    r.fds[0] = -1;
                }
                break;
            case opResolve:
                resolve(o, r);
                break;
            case opOpen:
                openName(o, r);
                break;
            case opRename:
            case opLink:
                if (const int failed = across(o, r)) {
                    fail(r.reply(), failed);
                } else {
                    answer(r.reply(), opOk, 0);
                }
                break;
            case opStat:
            case opReadlink:
            case opStatfs:
            case opMkdir:
            case opMknod:
            case opUnlink:
            case opSymlink:
            case opChmod:
            case opChown:
            case opUtimes:
            case opTruncate:
                nameCall(o, r);
                break;
            case opRead:
                readFile(o, e, r);
                break;
            case opWrite:
                writeFile(o, e, r);
                break;
            case opReaddir:
                readDirectory(o, r);
                break;
            case opPath:
                pathOf(o, r);
                break;
            case opFlock:
                answer(r.reply(), opOk, 0);
                break;
            default:
                fileCall(o, e, r);
        }
        return false;
    }

    // An endpoint closed: the file system's release of its open, and the
    // mount's end with the root rootfs mounted.
    static void gone(Endpoint& e) {
        auto* o = static_cast<Open*>(e.of);
        if (!o) {
            return;
        }
        if (o->opened && o->directory && operations.releasedir) {
            operations.releasedir(o->path, &o->info);
        } else if (o->opened && !o->directory) {
            if (operations.flush) {
                operations.flush(o->path, &o->info);
            }
            if (operations.release) {
                operations.release(o->path, &o->info);
            }
        }
        if (o->root) {
            stopServing();
        }
        if (o->mount && --o->mount->refs == 0) {
            close(o->mount->up);
            free(o->mount);
        }
        if (o->previous) {
            o->previous->next = o->next;
        } else {
            opens = o->next;
        }
        if (o->next) {
            o->next->previous = o->previous;
        }
        clearEntries(*o);
        free(o->entries);
        free(o->path);
        free(o);
    }

    // A device for the file system's files: the number of a pair, which no
    // other pair in the kernel has, as rootfs's are.
    static uint64_t deviceNumber() {
        int ends[2];
        struct stat st;
        if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, ends) != 0 || fstat(ends[0], &st) != 0) {
            fatal("fuse: a pair for a device: errno %d", errno);
        }
        close(ends[0]);
        close(ends[1]);
        return uint64_t(st.st_ino);
    }

    // The options of -o: ready=fd taken, libfuse's own let be.
    static void options(const char* list, int& ready) {
        for (const char* at = list; *at;) {
            const char* comma = strchr(at, ',');
            const size_t length = comma ? size_t(comma - at) : strlen(at);
            if (length > 6 && strncmp(at, "ready=", 6) == 0) {
                ready = atoi(at + 6);
            }
            at += length + (comma ? 1 : 0);
        }
    }
}

int fuse_main_real(int argc, char* argv[], const fuse_operations* op, size_t size, void* data) {
    const char* name = nullptr;
    int ready = -1;
    for (int i = 1; i < argc; ++i) {
        const char* arg = argv[i];
        if (strcmp(arg, "-o") == 0 && i + 1 < argc) {
            options(argv[++i], ready);
        } else if (strncmp(arg, "-o", 2) == 0) {
            options(arg + 2, ready);
        } else if (arg[0] == '-') {
            continue;
        } else if (name) {
            dprintf(STDERR_FILENO, "fuse: %s: one mount point is all\n", arg);
            return 1;
        } else {
            name = arg;
        }
    }
    if (!name) {
        dprintf(STDERR_FILENO, "fuse: no mount point\n");
        return 2;
    }
    if (const char* slash = strrchr(name, '/')) {
        name = slash + 1;
    }
    memset(&operations, 0, sizeof operations);
    memcpy(&operations, op, MIN(size, sizeof operations));
    context.private_data = data;
    context.umask = 022;
    device = deviceNumber();
    epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0) {
        fatal("fuse: epoll_create1: errno %d", errno);
    }
    if (operations.init) {
        fuse_conn_info conn{};
        conn.proto_major = 7;
        conn.proto_minor = 19;
        conn.max_write = sizeof bytes;
        conn.capable = FUSE_CAP_BIG_WRITES | FUSE_CAP_ATOMIC_O_TRUNC;
        context.private_data = operations.init(&conn);
    }
    Device registered{};
    const int registry = open("/dev/init", O_RDWR | O_CLOEXEC);
    const int e = registry < 0 ? errno : enroll(registry, name, S_IFSOCK | 0660, registered);
    if (registry >= 0) {
        close(registry);
    }
    if (e != 0) {
        dprintf(STDERR_FILENO, "fuse: /dev/%s: %s\n", name, strerror(e));
        if (operations.destroy) {
            operations.destroy(context.private_data);
        }
        return 3;
    }
    watch(epoll, {registered.end, true, false, false, nullptr});
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle, gone);
    if (operations.destroy) {
        operations.destroy(context.private_data);
    }
    return 0;
}

fuse_context* fuse_get_context() {
    return &context;
}

int fuse_getgroups(int size, gid_t list[]) {
    if (size >= 1) {
        list[0] = context.gid;
    }
    return 1;
}

int fuse_interrupted() {
    return 0;
}
