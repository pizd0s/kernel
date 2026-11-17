// The system's libfuse: libfuse 2.9's high-level API (fuse.h) as a file
// system of the VFS's (lib/protocol/protocol.h). fuse_main binds the file
// system's socket, SOCK_SEQPACKET, at /dev/<name>, the name its mount
// point argument gives (mount <where> /dev/<name>), and serves it on one
// thread: a request is a call of the file system's operations on a path,
// answered before the next. The context of each call is the request's uid
// and gid.
//
// mount(2) connects to the socket: the connection is a channel of the
// file system's, on which rootfs's calls come by node, an inode as the file system's getattr says it, at
// the path it was last looked up by. The file system knows nothing of the
// VFS's paths, mounts or roots: rootfs walks a path and asks one name at a
// time, and the file system checks who may, by the call's ids. An open
// file is an endpoint of its own, which the kernel reads and writes: a
// file's bytes pass through this process's memory, the file system reads
// into it, and they go on into the reader's through the memfd a READ
// brings. A directory's entries are read from the file system when a
// reading starts, their positions their places then.
//
// The locks of flock and fcntl are kept here, by node, an open file's
// each, as libfuse has it for a file system with no lock and flock of its
// own: the system keeps them, and one that has them is not asked. A lock
// that waits is a request kept until the lock is the open file's, those
// that wait the first to come first, as bin/tools/lib.cpp keeps tmpfs's
// and erofs's.
//
// When the last channel closes, the file system unmounted, it is destroyed
// and fuse_main returns, as libfuse's does when its mount point is
// unmounted.

#include "fuse.h"

#include <lib/protocol/service.h>

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/file.h>
#include <sys/param.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <sys/uio.h>
#include <unistd.h>

namespace {
    // What an endpoint of ours is of, the first of what it points at.
    enum class Kind : uint8_t {
        Channel, // rootfs's: its calls by node come on it
        File,    // an open file: its requests come on it
        Lock,    // a lock's request that waits: its CANCEL comes on it
    };

    struct Open;

    // A lock of an open file on a node, held or waited for: flock's,
    // LOCK_SH or LOCK_EX, or fcntl's on the bytes [start, stop), F_RDLCK
    // or F_WRLCK. One waited for is what the endpoint of its request is
    // of: the request is answered there once the lock is held.
    struct Lock {
        Kind kind;
        Open* owner;
        bool ranged;
        uint32_t type;
        uint64_t start;
        uint64_t stop;
        Endpoint* reply;
        Lock* next;
    };

    // A node of the file system, an inode, at the path it was last looked
    // up by, and the locks on it.
    struct Node {
        uint64_t ino;
        char* path;
        Lock* held;
        Lock* waiting; // the first to come first
        Node* next;    // in its bucket
    };

    // An entry of a directory, as a READDIR from its start read them.
    struct Entry {
        uint64_t ino;
        uint8_t type;
        char* name;
    };

    // The entries of a directory a reading from the start read.
    struct Listing {
        uint64_t ino;
        Entry* entries;
        size_t count;
        size_t room;
        Listing* next;
    };

    // An endpoint's: a channel of rootfs's, or an open file of a node at
    // its path, with the file system's information of the open.
    struct Open {
        Kind kind;
        Node* node;
        char* path;
        bool opened;
        fuse_file_info info;
        Open* next;
        Open* previous;
    };
}

static fuse_operations operations;
static fuse_context context;
static uint64_t device;
static int epoll = -1;
static Open* opens;
static size_t channels;
static char bytes[128 * 1024];
static Node* nodes[1024];
static Listing* listings;

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

static Node** bucketOf(uint64_t ino) {
    return &nodes[(ino * 0x9e3779b97f4a7c15ull) >> 54];
}

// The path a node was last looked up by, or none.
static const char* pathOf(uint64_t ino) {
    for (Node* n = *bucketOf(ino); n; n = n->next) {
        if (n->ino == ino) {
            return n->path;
        }
    }
    return nullptr;
}

// A node is at path now: the node.
static Node* know(uint64_t ino, const char* path) {
    Node** bucket = bucketOf(ino);
    for (Node* n = *bucket; n; n = n->next) {
        if (n->ino == ino) {
            if (strcmp(n->path, path) != 0) {
                free(n->path);
                n->path = copied(path, strlen(path));
            }
            return n;
        }
    }
    auto* n = static_cast<Node*>(allocate(sizeof(Node)));
    *n = {ino, copied(path, strlen(path)), nullptr, nullptr, *bucket};
    *bucket = n;
    return n;
}

// A path under from, or from itself, is under to now: what follows from.
static char* movedPath(const char* path, const char* from, const char* to) {
    const size_t length = strlen(from);
    if (strncmp(path, from, length) != 0 || (path[length] != 0 && path[length] != '/')) {
        return nullptr;
    }
    const size_t tail = strlen(path + length);
    auto* moved = static_cast<char*>(allocate(strlen(to) + tail + 1));
    memcpy(moved, to, strlen(to));
    memcpy(moved + strlen(to), path + length, tail);
    return moved;
}

// A rename: the nodes and the open files under from are under to now.
static void moved(const char* from, const char* to) {
    for (Node* bucket : nodes) {
        for (Node* n = bucket; n; n = n->next) {
            if (char* path = movedPath(n->path, from, to)) {
                free(n->path);
                n->path = path;
            }
        }
    }
    for (Open* o = opens; o; o = o->next) {
        if (o->path) {
            if (char* path = movedPath(o->path, from, to)) {
                free(o->path);
                o->path = path;
            }
        }
    }
}

// What an endpoint of ours is, a channel or an open file of a path, among
// the others; the endpoint watched is the caller's.
static Open* listed(Kind kind, char* path) {
    auto* o = static_cast<Open*>(allocate(sizeof(Open)));
    *o = {};
    o->kind = kind;
    o->path = path;
    o->next = opens;
    if (opens) {
        opens->previous = o;
    }
    opens = o;
    return o;
}

// An open goes: the file system's release of an open file, and what we
// kept of it.
static void unlisted(Open* o) {
    if (o->opened) {
        if (operations.flush) {
            operations.flush(o->path, &o->info);
        }
        if (operations.release) {
            operations.release(o->path, &o->info);
        }
    }
    if (o->previous) {
        o->previous->next = o->next;
    } else {
        opens = o->next;
    }
    if (o->next) {
        o->next->previous = o->previous;
    }
    free(o->path);
    free(o);
}

// A name of a call, after what comes before it in the body.
static bool nameAt(const Request& r, size_t at, const char*& name, size_t& length) {
    if (r.size <= at) {
        return false;
    }
    name = reinterpret_cast<const char*>(r.body + at);
    length = strnlen(name, r.size - at);
    return length > 0;
}

// An open of a file at path, on the reply's endpoint: emptied with
// O_TRUNC, the file system's open of it; made with O_CREAT. A link is
// ELOOP, a FIFO is its stat, the kernel makes its pipe, a device has no
// driver here.
static void openFile(Request& r, char* path, uint32_t mode, bool creates) {
    const uint32_t flags = r.h.arg;
    const uint32_t access = flags & O_ACCMODE;
    const bool writes = access != O_RDONLY;
    struct stat st;
    fuse_file_info info{};
    info.flags = int(flags & ~uint32_t(O_CREAT | O_EXCL | O_NOCTTY | O_TRUNC));
    bool opened = false;
    int e = attributes(path, st);
    if (e == ENOENT && creates) {
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
    } else if (e == 0 && creates && (flags & O_EXCL) != 0) {
        e = EEXIST;
    } else if (e == 0 && S_ISLNK(st.st_mode)) {
        e = (flags & O_DIRECTORY) != 0 ? ENOTDIR : ELOOP;
    } else if (e == 0 && S_ISDIR(st.st_mode)) {
        e = EISDIR;
    } else if (e == 0 && (flags & O_DIRECTORY) != 0) {
        e = ENOTDIR;
    } else if (e == 0 && (S_ISCHR(st.st_mode) || S_ISBLK(st.st_mode) || S_ISSOCK(st.st_mode))) {
        e = ENXIO;
    } else if (e == 0 && S_ISFIFO(st.st_mode)) {
        free(path);
        return answerStat(r.reply(), st);
    }
    if (e == 0 && !opened) {
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
    Node* node = know(uint64_t(st.st_ino), path);
    Open* o = listed(Kind::File, path);
    o->node = node;
    o->opened = opened;
    o->info = info;
    if (!answerOpen(epoll, r, {SERVED_FILE, 0, 0, 0, 0}, o)) {
        unlisted(o);
    }
}

// The times of UTIMES and FUTIMES: when's bits say now or kept, else
// the nanoseconds given.
static void timesOf(uint32_t when, const Request& r, size_t at, timespec times[2]) {
    uint64_t given[2] = {0, 0};
    r.at(at, given[0]);
    r.at(at + 8, given[1]);
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

static int changeTimes(const char* path, uint32_t when, const Request& r, size_t at) {
    timespec times[2];
    timesOf(when, r, at, times);
    return operations.utimens ? result(operations.utimens(path, times)) : ENOSYS;
}

static int changeOwner(const char* path, const Request& r, size_t at) {
    uint32_t uid;
    uint32_t gid;
    if (!r.at(at, uid) || !r.at(at + 4, gid)) {
        return EINVAL;
    }
    return operations.chown ? result(operations.chown(path, uid_t(uid), gid_t(gid))) : ENOSYS;
}

// A READ: what the file system reads at off, into the memfd at woff.
static void readFile(Open& o, const Endpoint& e, const Request& r) {
    Io io;
    if (!e.reads || !o.opened) {
        return fail(r.reply(), EBADF);
    }
    if (r.window() < 0 || !r.io(io) || !operations.read) {
        return fail(r.reply(), EINVAL);
    }
    const uint64_t off = io.off;
    const uint32_t woff = io.woff;
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
    Io io;
    if (!e.writes || !o.opened) {
        return fail(r.reply(), EBADF);
    }
    if (r.window() < 0 || !r.io(io) || !operations.write) {
        return fail(r.reply(), EINVAL);
    }
    uint64_t off = io.off;
    const uint32_t length = r.h.arg;
    const uint32_t woff = io.woff;
    if ((io.flags & ioAppend) != 0) {
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
    auto& l = *static_cast<Listing*>(buffer);
    if (strcmp(name, ".") == 0 || strcmp(name, "..") == 0) {
        return 0;
    }
    if (l.count == l.room) {
        l.room = l.room ? l.room * 2 : 32;
        l.entries = static_cast<Entry*>(realloc(l.entries, l.room * sizeof(Entry)));
        if (!l.entries) {
            fatal("fuse: out of memory");
        }
    }
    l.entries[l.count++] = {st ? uint64_t(st->st_ino) : 0, st ? uint8_t((st->st_mode >> 12) & 017) : uint8_t(DT_UNKNOWN), copied(name, strlen(name))};
    return 0;
}

// The entries of a directory after a position, the index of one plus
// one, as many as one reply holds; a reading from the start reads them
// from the file system again.
static void readDirectory(uint64_t ino, const char* path, const Request& r) {
    uint64_t after;
    if (!r.at(8, after) || !operations.readdir) {
        return fail(r.reply(), EINVAL);
    }
    Listing* l = listings;
    while (l && l->ino != ino) {
        l = l->next;
    }
    if (!l) {
        l = static_cast<Listing*>(allocate(sizeof(Listing)));
        l->ino = ino;
        l->next = listings;
        listings = l;
    }
    if (after == 0) {
        for (size_t i = 0; i < l->count; ++i) {
            free(l->entries[i].name);
        }
        l->count = 0;
        fuse_file_info info{};
        if (operations.opendir) {
            if (const int e = result(operations.opendir(path, &info))) {
                return fail(r.reply(), e);
            }
        }
        const int e = result(operations.readdir(path, l, fill, 0, &info));
        if (operations.releasedir) {
            operations.releasedir(path, &info);
        }
        if (e != 0) {
            return fail(r.reply(), e);
        }
    }
    static unsigned char out[65536 - sizeof(Header)];
    size_t size = 0;
    for (uint64_t i = after; i < l->count; ++i) {
        const Entry& entry = l->entries[i];
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

// A call of rootfs's on a channel, by node: the file system's
// operations on the node's path, or on a name of it.
static void fsCall(Request& r) {
    uint64_t ino;
    if (r.h.op == fsRoot) {
        struct stat st;
        if (const int failed = attributes("/", st)) {
            return fail(r.reply(), failed);
        }
        know(uint64_t(st.st_ino), "/");
        return answerStat(r.reply(), st);
    }
    if (!r.at(0, ino)) {
        return fail(r.reply(), EINVAL);
    }
    const char* at = pathOf(ino);
    if (!at) {
        return fail(r.reply(), ENOENT);
    }
    const char* name = nullptr;
    size_t length = 0;
    struct stat st;
    int e = 0;
    switch (r.h.op) {
        case fsLookup: {
            if (!nameAt(r, 8, name, length)) {
                return fail(r.reply(), EINVAL);
            }
            char* path = length == 2 && memcmp(name, "..", 2) == 0 ? parentOf(at) : joined(at, name, length);
            e = attributes(path, st);
            if (e == 0) {
                know(uint64_t(st.st_ino), path);
                answerStat(r.reply(), st);
            }
            free(path);
            break;
        }
        case fsGetattr:
            e = attributes(at, st);
            if (e == 0) {
                return answerStat(r.reply(), st);
            }
            break;
        case fsReadlink: {
            static char target[PATH_MAX];
            e = operations.readlink ? result(operations.readlink(at, target, sizeof target)) : ENOSYS;
            if (e == 0) {
                return answer(r.reply(), opOk, 0, target, strlen(target));
            }
            break;
        }
        case fsReaddir:
            return readDirectory(ino, at, r);
        case fsOpen:
            return openFile(r, copied(at, strlen(at)), 0, false);
        case fsCreate: {
            uint32_t mode;
            if (!r.at(8, mode) || !nameAt(r, 12, name, length)) {
                return fail(r.reply(), EINVAL);
            }
            return openFile(r, joined(at, name, length), mode, true);
        }
        case fsMkdir:
        case fsMknod:
        case fsUnlink: {
            if (!nameAt(r, 8, name, length)) {
                return fail(r.reply(), EINVAL);
            }
            char* path = joined(at, name, length);
            if (r.h.op == fsMkdir) {
                e = operations.mkdir ? result(operations.mkdir(path, mode_t(r.h.arg & 07777))) : ENOSYS;
            } else if (r.h.op == fsMknod) {
                e = operations.mknod ? result(operations.mknod(path, mode_t(r.h.arg), 0)) : ENOSYS;
            } else if ((r.h.arg & AT_REMOVEDIR) != 0) {
                e = operations.rmdir ? result(operations.rmdir(path)) : ENOSYS;
            } else {
                e = operations.unlink ? result(operations.unlink(path)) : ENOSYS;
            }
            free(path);
            break;
        }
        case fsSymlink: {
            const char* target;
            size_t targetLength;
            if (!nameAt(r, 8, target, targetLength) || !nameAt(r, 8 + targetLength + 1, name, length)) {
                return fail(r.reply(), EINVAL);
            }
            char* path = joined(at, name, length);
            e = operations.symlink ? result(operations.symlink(target, path)) : ENOSYS;
            free(path);
            break;
        }
        case fsLink:
        case fsRename: {
            uint64_t toIno;
            const char* old = nullptr;
            size_t oldLength = 0;
            const size_t names = 16;
            if (!r.at(8, toIno)) {
                return fail(r.reply(), EINVAL);
            }
            const char* to = pathOf(toIno);
            if (!to) {
                return fail(r.reply(), ENOENT);
            }
            if (r.h.op == fsRename) {
                if (!nameAt(r, names, old, oldLength) || !nameAt(r, names + oldLength + 1, name, length)) {
                    return fail(r.reply(), EINVAL);
                }
            } else if (!nameAt(r, names, name, length)) {
                return fail(r.reply(), EINVAL);
            }
            char* target = joined(to, name, length);
            if (r.h.op == fsLink) {
                e = operations.link ? result(operations.link(at, target)) : ENOSYS;
            } else {
                char* from = joined(at, old, oldLength);
                e = operations.rename ? result(operations.rename(from, target)) : ENOSYS;
                if (e == 0) {
                    moved(from, target);
                }
                free(from);
            }
            free(target);
            break;
        }
        case fsChmod:
            e = operations.chmod ? result(operations.chmod(at, mode_t(r.h.arg & 07777))) : ENOSYS;
            break;
        case fsChown:
            e = changeOwner(at, r, 8);
            break;
        case fsUtimes:
            e = changeTimes(at, r.h.arg, r, 8);
            break;
        case fsTruncate: {
            uint64_t size;
            e = !r.at(8, size) ? EINVAL : operations.truncate ? result(operations.truncate(at, off_t(size))) : ENOSYS;
            break;
        }
        case fsStatfs:
            return answerStatfs(r.reply(), "/");
        case fsPath:
            return answer(r.reply(), opOk, 0, at, strlen(at));
        default:
            e = ENOSYS;
    }
    if (e != 0) {
        fail(r.reply(), e);
    } else if (r.h.op != fsLookup) {
        answer(r.reply(), opOk, 0);
    }
}

// Whether two locks of a node are in each other's way: two open files',
// flock's with one exclusive, or fcntl's on bytes they share with one to
// write.
static bool crossing(const Lock& a, const Lock& b) {
    if (a.owner == b.owner || a.ranged != b.ranged) {
        return false;
    }
    if (!a.ranged) {
        return a.type == LOCK_EX || b.type == LOCK_EX;
    }
    return a.start < b.stop && b.start < a.stop && (a.type == F_WRLCK || b.type == F_WRLCK);
}

// The lock held on a node in the way of one.
static const Lock* inWay(const Node& n, const Lock& want) {
    for (const Lock* l = n.held; l; l = l->next) {
        if (crossing(*l, want)) {
            return l;
        }
    }
    return nullptr;
}

// Whether a lock is its open file's to take now: none held in its way,
// and none waited for before it, self, or by any for one that does not
// wait yet. None is taken past one that waits for it.
static bool ready(const Node& n, const Lock& want, const Lock* self) {
    if (inWay(n, want)) {
        return false;
    }
    for (const Lock* w = n.waiting; w != self; w = w->next) {
        if (crossing(*w, want)) {
            return false;
        }
    }
    return true;
}

// What an open file holds on a node goes: flock's lock, or with ranged
// fcntl's of the bytes [start, stop), a lock across an edge cut there.
static void drop(Node& n, const Open* owner, bool ranged, uint64_t start, uint64_t stop) {
    for (Lock** at = &n.held; *at;) {
        Lock* l = *at;
        if (l->owner != owner || l->ranged != ranged || (ranged && (l->stop <= start || l->start >= stop))) {
            at = &l->next;
        } else if (ranged && l->start < start && l->stop > stop) {
            // The middle goes: what is past it is a lock of its own.
            auto* rest = static_cast<Lock*>(allocate(sizeof(Lock)));
            *rest = *l;
            rest->start = stop;
            l->stop = start;
            l->next = rest;
            at = &rest->next;
        } else if (ranged && l->start < start) {
            l->stop = start;
            at = &l->next;
        } else if (ranged && l->stop > stop) {
            l->start = stop;
            at = &l->next;
        } else {
            *at = l->next;
            free(l);
        }
    }
}

// A lock becomes its open file's, in place of what it held there.
static void take(Node& n, const Lock& want) {
    drop(n, want.owner, want.ranged, want.start, want.stop);
    auto* l = static_cast<Lock*>(allocate(sizeof(Lock)));
    *l = want;
    l->reply = nullptr;
    l->next = n.held;
    n.held = l;
}

// A lock waits no more: it leaves its node's queue, and its request's
// endpoint is let go of, unless serve does that, the asker gone.
static void settle(Node& n, Lock* w, bool ours) {
    for (Lock** at = &n.waiting; *at; at = &(*at)->next) {
        if (*at == w) {
            *at = w->next;
            break;
        }
    }
    if (ours) {
        unwatch(epoll, w->reply);
    }
    free(w);
}

// Gives the locks waited for on a node to those they are free for now,
// the first to come first, each answered OK on its request's endpoint.
// One that takes a range in place of what it held may free an earlier
// one's: from the first again then.
static void grant(Node& n) {
    for (Lock* w = n.waiting; w;) {
        if (!ready(n, *w, w)) {
            w = w->next;
            continue;
        }
        take(n, *w);
        answer(w->reply->fd, opOk, 0);
        settle(n, w, true);
        w = n.waiting;
    }
}

// The lock a request asks, answered on its reply's endpoint: taken when
// it is the open file's to take; else EAGAIN, or the request waits: its
// endpoint is kept and watched, a CANCEL comes on it, and what the open
// file held of flock's lock goes meanwhile, as a change of flock's lock
// is no single step.
static void lock(Node& n, const Lock& want, bool waits, Request& r) {
    if (ready(n, want, nullptr)) {
        take(n, want);
        answer(r.reply(), opOk, 0);
        return grant(n);
    }
    if (!waits) {
        return fail(r.reply(), EAGAIN);
    }
    if (r.onEndpoint) {
        return fail(r.reply(), EINVAL); // no endpoint of its own to wait on
    }
    if (!want.ranged) {
        drop(n, want.owner, false, 0, 0);
    }
    auto* w = static_cast<Lock*>(allocate(sizeof(Lock)));
    *w = want;
    w->reply = watch(epoll, {r.reply(), false, false, false, w});
    r.fds[r.count - 1] = -1;
    Lock** last = &n.waiting;
    while (*last) {
        last = &(*last)->next;
    }
    *last = w;
    grant(n);
}

// flock's lock of an open file on its node: shared, exclusive, or none.
static void flockFile(Open& o, Request& r) {
    Node& n = *o.node;
    const uint32_t kind = r.h.arg & ~uint32_t(LOCK_NB);
    switch (kind) {
        case LOCK_UN:
            drop(n, &o, false, 0, 0);
            answer(r.reply(), opOk, 0);
            return grant(n);
        case LOCK_SH:
        case LOCK_EX:
            return lock(n, {Kind::Lock, &o, false, kind, 0, 0, nullptr, nullptr}, (r.h.arg & LOCK_NB) == 0, r);
        default:
            return fail(r.reply(), EINVAL);
    }
}

// fcntl's lock of an open file on a range of its node, to the end and
// past with a length of 0: with lockGet the first lock of another open
// file in the way of the one described, else F_UNLCK; else the lock is
// set in place of what the open file held of the range, at once or, with
// lockWait, once it is free; F_UNLCK lets the range go.
static void lockFile(Open& o, Request& r) {
    Node& n = *o.node;
    uint32_t type;
    uint64_t start;
    uint64_t length;
    if (!r.at(0, type) || !r.at(4, start) || !r.at(12, length) || type > F_UNLCK || r.h.arg > lockWait) {
        return fail(r.reply(), EINVAL);
    }
    const uint64_t stop = length == 0 || start > UINT64_MAX - length ? UINT64_MAX : start + length;
    const Lock want{Kind::Lock, &o, true, type, start, stop, nullptr, nullptr};
    if (r.h.arg == lockGet) {
        const Lock* way = inWay(n, want);
        const uint32_t found = way ? way->type : uint32_t(F_UNLCK);
        const uint64_t from = way ? way->start : start;
        const uint64_t size = !way ? length : way->stop == UINT64_MAX ? 0 : way->stop - way->start;
        unsigned char out[20];
        memcpy(out, &found, 4);
        memcpy(out + 4, &from, 8);
        memcpy(out + 12, &size, 8);
        return answer(r.reply(), opOk, 0, out, sizeof out);
    }
    if (type != F_UNLCK) {
        return lock(n, want, r.h.arg == lockWait, r);
    }
    drop(n, &o, true, start, stop);
    answer(r.reply(), opOk, 0);
    grant(n);
}

// What an open file that closes held of its node's locks goes, and what
// it waited for there; those that waited for it get theirs.
static void unlock(Open& o) {
    Node* n = o.node;
    if (!n) {
        return;
    }
    for (Lock** at = &n->held; *at;) {
        Lock* l = *at;
        if (l->owner == &o) {
            *at = l->next;
            free(l);
        } else {
            at = &l->next;
        }
    }
    for (Lock* w = n->waiting; w;) {
        Lock* next = w->next;
        if (w->owner == &o) {
            settle(*n, w, true);
        }
        w = next;
    }
    grant(*n);
}

// A CANCEL of a lock's request that waits: it is taken back, EINTR, and
// those behind it may have theirs.
static void cancel(Lock& w) {
    Node& n = *w.owner->node;
    fail(w.reply->fd, EINTR);
    settle(n, &w, true);
    grant(n);
}

// The calls on an open file.
static void fileCall(Open& o, const Endpoint& e, Request& r) {
    struct stat st;
    int failed = 0;
    switch (r.h.op) {
        case opRead:
            return readFile(o, e, r);
        case opWrite:
            return writeFile(o, e, r);
        case opReaddir:
            return fail(r.reply(), ENOTDIR);
        case opFstat:
            if (o.opened && operations.fgetattr) {
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
            failed = changeOwner(o.path, r, 0);
            break;
        case opFutimes:
            failed = changeTimes(o.path, r.h.arg, r, 0);
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
            if (o.opened && operations.fsync) {
                failed = result(operations.fsync(o.path, int(r.h.arg), &o.info));
            }
            break;
        case opFlock:
            return flockFile(o, r);
        case opLock:
            return lockFile(o, r);
        default:
            failed = ENOSYS;
    }
    if (failed != 0) {
        return fail(r.reply(), failed);
    }
    answer(r.reply(), opOk, 0);
}

// What an endpoint is of starts with its kind.
static Kind kindOf(const void* of) {
    return *static_cast<const Kind*>(of);
}

static void handle(Endpoint& e, Request& r) {
    context.uid = uid_t(r.h.uid);
    context.gid = gid_t(r.h.gid);
    switch (kindOf(e.of)) {
        case Kind::Channel:
            return fsCall(r);
        case Kind::File:
            return fileCall(*static_cast<Open*>(e.of), e, r);
        case Kind::Lock:
            if (r.h.op == opCancel) {
                cancel(*static_cast<Lock*>(e.of));
            }
    }
}

// A connection mount(2) made to the file system's socket: a channel of
// rootfs's calls, ROOT the first.
static void accepted(Endpoint&, int connection) {
    watch(epoll, {connection, false, true, true, listed(Kind::Channel, nullptr)});
    ++channels;
}

// An endpoint closed: who asked a lock that waits went, and it waits no
// more, serve letting go of its endpoint; or the file system's release
// of an open file, its locks gone first; the last channel's end is the
// unmount.
static void gone(Endpoint& e) {
    if (!e.of) {
        return;
    }
    if (kindOf(e.of) == Kind::Lock) {
        auto* w = static_cast<Lock*>(e.of);
        Node& n = *w->owner->node;
        settle(n, w, false);
        return grant(n);
    }
    auto* o = static_cast<Open*>(e.of);
    if (o->kind == Kind::Channel && --channels == 0) {
        stopServing();
    }
    unlock(*o);
    unlisted(o);
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
    // The file system's socket, /dev/<name>, one a file system before it
    // left taken away first.
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    snprintf(address.sun_path, sizeof address.sun_path, "/dev/%s", name);
    const int listening = socket(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0);
    unlink(address.sun_path);
    if (listening < 0 || bind(listening, reinterpret_cast<const sockaddr*>(&address), sizeof address) != 0 || listen(listening, 16) != 0) {
        dprintf(STDERR_FILENO, "fuse: %s: %s\n", address.sun_path, strerror(errno));
        if (operations.destroy) {
            operations.destroy(context.private_data);
        }
        return 3;
    }
    Endpoint socketEndpoint{listening, false, false, false, nullptr};
    socketEndpoint.listens = true;
    watch(epoll, socketEndpoint);
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle, gone, accepted);
    unlink(address.sun_path);
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
