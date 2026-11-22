// The system's libfuse: libfuse 2.9's high-level API (fuse.h) as a file
// system of the VFS's (lib/protocol/protocol.h). fuse_main binds the file
// system's socket, SOCK_SEQPACKET, at its mount point argument, a path of
// /dev's when it is relative, which it registers there first (mount
// <where> /dev/<name>), and serves it on one thread: a request is a call of
// the file system's operations on a path,
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
// A directory is watched as <sys/watchdir.h> says (WATCH): a watcher
// tells the names a reading of it finds as it starts, watchIdle, then
// what the calls that come here add to it and take out of it, a name that
// came and went between two READs not at all, a rename a removal and an
// addition; a READ with nothing to tell waits, kept, until there is, or
// until its CANCEL; the directory gone, its watcher's READs give 0. As
// every change of the file system's names comes as a call of ours, the
// watchers see them all.
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
#include <sys/param.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <sys/uio.h>
#include <unistd.h>

namespace {
    // A node of the file system, an inode, at the path it was last looked
    // up by.
    struct Node {
        uint64_t ino;
        char* path;
        Node* next; // in its bucket
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

    // A record a watcher has yet to tell: watchExisting, watchAdded or
    // watchRemoved, of a name.
    struct Change {
        uint8_t kind;
        char* name;
    };

    // A watcher of a directory (WATCH): the directory's inode, its open
    // file's endpoint, what it has yet to tell, in order, whether it told
    // watchIdle, whether the directory is gone, and a READ it keeps, where
    // that is answered, its memfd and length.
    struct Watcher {
        uint64_t dir;
        Endpoint* end;
        Change* changes;
        size_t count;
        size_t room;
        bool idle;
        bool gone;
        Endpoint* kept;
        int memfd;
        uint32_t length;
        int keptTag; // what the kept READ's endpoint is of
        Watcher* next;
    };

    // An endpoint's: a channel of rootfs's, or an open file at its path,
    // with the file system's information of the open.
    struct Open {
        bool channel;
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
static Watcher* watchers;
static Device registered{-1, {}}; // the socket's name in /dev, ours while we are up

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

// A node is at path now.
static void know(uint64_t ino, const char* path) {
    Node** bucket = bucketOf(ino);
    for (Node* n = *bucket; n; n = n->next) {
        if (n->ino == ino) {
            if (strcmp(n->path, path) != 0) {
                free(n->path);
                n->path = copied(path, strlen(path));
            }
            return;
        }
    }
    auto* n = static_cast<Node*>(allocate(sizeof(Node)));
    *n = {ino, copied(path, strlen(path)), *bucket};
    *bucket = n;
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
static Open* listed(bool channel, char* path) {
    auto* o = static_cast<Open*>(allocate(sizeof(Open)));
    *o = {};
    o->channel = channel;
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

// An open of a file at path, answered where its request says: emptied with
// O_TRUNC, the file system's open of it; made with O_CREAT. A link is
// ELOOP, a FIFO is its stat, the kernel makes its pipe, a device has no
// driver here.
static void added(uint64_t dir, const char* name, size_t length);

// Opens a file at path, a name of dir's when it creates one.
static void openFile(Request& r, char* path, uint32_t mode, bool creates, uint64_t dir) {
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
            const char* name = strrchr(path, '/') + 1;
            added(dir, name, strlen(name));
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
    know(uint64_t(st.st_ino), path);
    Open* o = listed(false, path);
    o->opened = opened;
    o->info = info;
    if (!answerOpen(epoll, r, {openedFile, 0, device, uint64_t(st.st_ino), 0, POLLIN | POLLOUT}, o)) {
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

// A READ: what the file system reads at the open file's position or off,
// into the memfd; the position moves by it.
static void readFile(Open& o, Endpoint& e, const Request& r) {
    Io io;
    if (!e.reads || !o.opened) {
        return fail(r.reply(), EBADF);
    }
    if (r.memfd() < 0 || !r.io(io) || !operations.read) {
        return fail(r.reply(), EINVAL);
    }
    const uint64_t off = offsetOf(e, io, false, 0);
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
        if (pwrite(r.memfd(), bytes, size_t(got), off_t(done)) != got) {
            return fail(r.reply(), EIO);
        }
        done += size_t(got);
        if (size_t(got) < ask) {
            break;
        }
    }
    if ((io.flags & ioAt) == 0) {
        e.pos = off + done;
    }
    const uint32_t count = uint32_t(done);
    answer(r.reply(), opOk, 0, &count, sizeof count);
}

// A WRITE: the memfd's bytes, what the file system writes at the open
// file's position or off, or at its end when it appends; the position
// after it.
static void writeFile(Open& o, Endpoint& e, const Request& r) {
    Io io;
    if (!e.writes || !o.opened) {
        return fail(r.reply(), EBADF);
    }
    if (r.memfd() < 0 || !r.io(io) || !operations.write) {
        return fail(r.reply(), EINVAL);
    }
    uint64_t off = offsetOf(e, io, true, 0);
    const uint32_t length = r.h.arg;
    if ((io.flags & ioAt) == 0 && e.append) {
        struct stat st;
        if (const int failed = attributes(o.path, st)) {
            return fail(r.reply(), failed);
        }
        off = uint64_t(st.st_size);
    }
    size_t done = 0;
    while (done < length) {
        const size_t n = MIN(sizeof bytes, size_t(length) - done);
        if (pread(r.memfd(), bytes, n, off_t(done)) != ssize_t(n)) {
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
    if ((io.flags & ioAt) == 0) {
        e.pos = off + done;
    }
    const uint32_t count = uint32_t(done);
    answer(r.reply(), opOk, 0, &count, sizeof count);
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
    size_t room = r.h.arg;
    for (uint64_t i = after; i < l->count; ++i) {
        const Entry& entry = l->entries[i];
        const size_t length = strlen(entry.name);
        if (size + 17 + length + 1 > sizeof out || direntRoom(length) > room) {
            break;
        }
        room -= direntRoom(length);
        const uint64_t pos = i + 1;
        memcpy(out + size, &entry.ino, 8);
        memcpy(out + size + 8, &pos, 8);
        out[size + 16] = entry.type;
        memcpy(out + size + 17, entry.name, length + 1);
        size += 17 + length + 1;
    }
    answer(r.reply(), opOk, 0, out, size);
}

// ---- watchers -----------------------------------------------------------------

static void note(Watcher& w, uint8_t kind, const char* name, size_t length) {
    if (w.count == w.room) {
        w.room = w.room ? 2 * w.room : 16;
        w.changes = static_cast<Change*>(realloc(w.changes, w.room * sizeof(Change)));
        if (!w.changes) {
            fatal("fuse: out of memory");
        }
    }
    w.changes[w.count++] = {kind, copied(name, length)};
}

// Whether a watcher has something to tell: watchIdle, a change, or that
// its directory is gone.
static bool pending(const Watcher& w) {
    return !w.idle || w.count > 0 || w.gone;
}

static bool record(unsigned char* to, size_t room, size_t& size, uint8_t kind, const char* name) {
    const size_t length = strlen(name);
    if (room - size < length + 2) {
        return false;
    }
    to[size] = kind;
    memcpy(to + size + 1, name, length + 1);
    size += length + 2;
    return true;
}

// The records a watcher tells now, as many as room holds, which it then
// has told: its changes in order, watchIdle after the names there as it
// started.
static size_t records(Watcher& w, unsigned char* to, size_t room) {
    size_t size = 0;
    size_t told = 0;
    for (; told < w.count; ++told) {
        const Change& c = w.changes[told];
        if (!w.idle && c.kind != watchExisting) {
            if (!record(to, room, size, watchIdle, "")) {
                break;
            }
            w.idle = true;
        }
        if (!record(to, room, size, c.kind, c.name)) {
            break;
        }
    }
    if (told == w.count && !w.idle && record(to, room, size, watchIdle, "")) {
        w.idle = true;
    }
    for (size_t i = 0; i < told; ++i) {
        free(w.changes[i].name);
    }
    memmove(w.changes, w.changes + told, (w.count - told) * sizeof(Change));
    w.count -= told;
    return size;
}

// The records of a READ into its memfd, its count answered on fd: 0 of a
// directory gone, once all is told; none of a READ too short for the
// next record, EINVAL.
static void answerRecords(Watcher& w, int memfd, uint32_t length, int fd) {
    static unsigned char out[65536 - sizeof(Header)];
    const size_t size = records(w, out, length < sizeof out ? size_t(length) : sizeof out);
    if (size == 0 && (w.count > 0 || !w.idle)) {
        return fail(fd, EINVAL);
    }
    if (size > 0 && pwrite(memfd, out, size, 0) != ssize_t(size)) {
        return fail(fd, EIO);
    }
    const uint32_t count = uint32_t(size);
    answer(fd, opOk, 0, &count, sizeof count);
}

// Tells the kernel whether a watcher is readable.
static void ready(const Watcher& w) {
    tell(w.end->fd, pending(w) ? POLLIN | (w.gone ? POLLHUP : 0) : 0);
}

static void letKeptGo(Watcher& w) {
    close(w.memfd);
    unkeep(epoll, w.kept);
    w.kept = nullptr;
    w.memfd = -1;
}

// What a watcher has to tell changed: a READ it keeps is answered.
static void told(Watcher& w) {
    if (w.kept && pending(w)) {
        answerRecords(w, w.memfd, w.length, w.kept->fd);
        letKeptGo(w);
    }
    ready(w);
}

// A name a call added to a directory or took out of it, for its
// watchers: one taken out before it was told is not told at all.
static void added(uint64_t dir, const char* name, size_t length) {
    for (Watcher* w = watchers; w; w = w->next) {
        if (w->dir == dir && !w->gone) {
            note(*w, watchAdded, name, length);
            told(*w);
        }
    }
}

static void removed(uint64_t dir, const char* name, size_t length) {
    for (Watcher* w = watchers; w; w = w->next) {
        if (w->dir != dir || w->gone) {
            continue;
        }
        size_t at = w->count;
        for (size_t i = 0; i < w->count; ++i) {
            const Change& c = w->changes[i];
            if (c.kind != watchRemoved && strlen(c.name) == length && memcmp(c.name, name, length) == 0) {
                at = i;
            }
        }
        if (at < w->count) {
            free(w->changes[at].name);
            memmove(w->changes + at, w->changes + at + 1, (w->count - at - 1) * sizeof(Change));
            --w->count;
        } else {
            note(*w, watchRemoved, name, length);
        }
        told(*w);
    }
}

// A directory gone: its watchers tell what is left, then their end.
static void dirGone(uint64_t dir) {
    for (Watcher* w = watchers; w; w = w->next) {
        if (w->dir == dir && !w->gone) {
            w->gone = true;
            told(*w);
        }
    }
}

// WATCH of a directory: a watcher, an open file of the flags asked,
// O_NONBLOCK of them, whose READs give its records: the names a reading
// finds, watchIdle, then what changes.
static void watchDirectory(uint64_t ino, const char* path, const Request& r) {
    struct stat st;
    if (const int e = attributes(path, st)) {
        return fail(r.reply(), e);
    }
    if (!S_ISDIR(st.st_mode)) {
        return fail(r.reply(), ENOTDIR);
    }
    if (operations.access) {
        if (const int e = result(operations.access(path, R_OK))) {
            return fail(r.reply(), e);
        }
    }
    if (!operations.readdir) {
        return fail(r.reply(), ENOSYS);
    }
    Listing names{};
    fuse_file_info info{};
    int e = operations.opendir ? result(operations.opendir(path, &info)) : 0;
    if (e == 0) {
        e = result(operations.readdir(path, &names, fill, 0, &info));
        if (operations.releasedir) {
            operations.releasedir(path, &info);
        }
    }
    auto* w = static_cast<Watcher*>(allocate(sizeof(Watcher)));
    w->dir = ino;
    w->memfd = -1;
    for (size_t i = 0; i < names.count; ++i) {
        if (e == 0) {
            note(*w, watchExisting, names.entries[i].name, strlen(names.entries[i].name));
        }
        free(names.entries[i].name);
    }
    free(names.entries);
    if (e == 0) {
        w->end = answerOpen(epoll, r, {openedFile, 0, device, 0, 0, POLLIN}, w);
    } else {
        fail(r.reply(), e);
    }
    if (!w->end) {
        for (size_t i = 0; i < w->count; ++i) {
            free(w->changes[i].name);
        }
        free(w->changes);
        free(w);
        return;
    }
    w->next = watchers;
    watchers = w;
}

static Watcher* watcherOf(const void* of, bool& kept) {
    for (Watcher* w = watchers; w; w = w->next) {
        if (w == of || &w->keptTag == of) {
            kept = &w->keptTag == of;
            return w;
        }
    }
    return nullptr;
}

// A READ of a watcher's: its records, or, with none, the READ kept until
// there are, but of O_NONBLOCK; one at a time.
static void readWatcher(Watcher& w, const Endpoint& e, Request& r) {
    Io io;
    if (r.memfd() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    if ((io.flags & ioAt) != 0) {
        return fail(r.reply(), ESPIPE);
    }
    if (pending(w)) {
        answerRecords(w, r.memfd(), r.h.arg, r.reply());
        return ready(w);
    }
    if (e.nonblock) {
        return fail(r.reply(), EAGAIN);
    }
    if (w.kept) {
        return fail(r.reply(), EBUSY);
    }
    w.kept = keep(epoll, r, &w.keptTag);
    w.memfd = r.memfd();
    w.length = r.h.arg;
    r.fds[0] = -1;
}

static void forgetWatcher(Watcher* w) {
    if (w->kept) {
        letKeptGo(*w);
    }
    for (Watcher** at = &watchers; *at; at = &(*at)->next) {
        if (*at == w) {
            *at = w->next;
            break;
        }
    }
    for (size_t i = 0; i < w->count; ++i) {
        free(w->changes[i].name);
    }
    free(w->changes);
    free(w);
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
            return openFile(r, copied(at, strlen(at)), 0, false, 0);
        case fsCreate: {
            uint32_t mode;
            if (!r.at(8, mode) || !nameAt(r, 12, name, length)) {
                return fail(r.reply(), EINVAL);
            }
            return openFile(r, joined(at, name, length), mode, true, ino);
        }
        case fsMkdir:
        case fsMknod:
        case fsUnlink: {
            if (!nameAt(r, 8, name, length)) {
                return fail(r.reply(), EINVAL);
            }
            char* path = joined(at, name, length);
            struct stat gone;
            const bool removesDir = r.h.op == fsUnlink && (r.h.arg & AT_REMOVEDIR) != 0;
            const bool known = removesDir && attributes(path, gone) == 0;
            if (r.h.op == fsMkdir) {
                e = operations.mkdir ? result(operations.mkdir(path, mode_t(r.h.arg & 07777))) : ENOSYS;
            } else if (r.h.op == fsMknod) {
                e = operations.mknod ? result(operations.mknod(path, mode_t(r.h.arg), 0)) : ENOSYS;
            } else if (removesDir) {
                e = operations.rmdir ? result(operations.rmdir(path)) : ENOSYS;
            } else {
                e = operations.unlink ? result(operations.unlink(path)) : ENOSYS;
            }
            if (e == 0 && r.h.op == fsUnlink) {
                removed(ino, name, length);
                if (known) {
                    dirGone(uint64_t(gone.st_ino));
                }
            } else if (e == 0) {
                added(ino, name, length);
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
            if (e == 0) {
                added(ino, name, length);
            }
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
                if (e == 0) {
                    added(toIno, name, length);
                }
            } else {
                char* from = joined(at, old, oldLength);
                struct stat was;
                const bool replaces = strcmp(from, target) != 0 && attributes(target, was) == 0;
                e = operations.rename ? result(operations.rename(from, target)) : ENOSYS;
                if (e == 0) {
                    moved(from, target);
                }
                if (e == 0 && strcmp(from, target) != 0) {
                    removed(ino, old, oldLength);
                    if (replaces) {
                        removed(toIno, name, length);
                        if (S_ISDIR(was.st_mode)) {
                            dirGone(uint64_t(was.st_ino));
                        }
                    }
                    added(toIno, name, length);
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
        case fsWatch:
            return watchDirectory(ino, at, r);
        default:
            e = ENOSYS;
    }
    if (e != 0) {
        fail(r.reply(), e);
    } else if (r.h.op != fsLookup) {
        answer(r.reply(), opOk, 0);
    }
}

// The calls on an open file.
static void fileCall(Open& o, Endpoint& e, Request& r) {
    struct stat st;
    int failed = 0;
    switch (r.h.op) {
        case opRead:
            return readFile(o, e, r);
        case opWrite:
            return writeFile(o, e, r);
        case opReaddir:
            return fail(r.reply(), ENOTDIR);
        case opSeek:
            if (r.h.arg == SEEK_END) {
                if ((failed = attributes(o.path, st)) != 0) {
                    break;
                }
                return answerSeek(e, r, uint64_t(st.st_size));
            }
            return answerSeek(e, r, 0);
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
        default:
            failed = ENOSYS;
    }
    if (failed != 0) {
        return fail(r.reply(), failed);
    }
    answer(r.reply(), opOk, 0);
}

static void handle(Endpoint& e, Request& r) {
    context.uid = uid_t(r.h.uid);
    context.gid = gid_t(r.h.gid);
    bool kept = false;
    if (Watcher* w = watcherOf(e.of, kept)) {
        if (kept && r.h.op == opCancel) {
            fail(w->kept->fd, EINTR);
            letKeptGo(*w);
        } else if (!kept && r.h.op == opRead) {
            readWatcher(*w, e, r);
        } else if (!kept) {
            fail(r.reply(), unserved(r.h.op));
        }
        return;
    }
    Open& o = *static_cast<Open*>(e.of);
    if (o.channel) {
        fsCall(r);
    } else {
        fileCall(o, e, r);
    }
}

// A connection mount(2) made to the file system's socket: a channel of
// rootfs's calls, ROOT the first.
static void accepted(Endpoint&, int connection) {
    watch(epoll, {connection, false, true, true, listed(true, nullptr)});
    ++channels;
}

// An endpoint closed: the file system's release of an open file; the
// last channel's end is the unmount.
static void gone(Endpoint& e) {
    bool kept = false;
    if (Watcher* w = watcherOf(e.of, kept); w && kept) {
        // Who asked went: serve closes the endpoint itself.
        close(w->memfd);
        w->kept = nullptr;
        w->memfd = -1;
        return;
    } else if (w) {
        return forgetWatcher(w);
    }
    auto* o = static_cast<Open*>(e.of);
    if (!o) {
        return;
    }
    if (o->channel && --channels == 0) {
        stopServing();
    }
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
    // The file system's socket: at name, one a file system before it left
    // taken away first, or at /dev/<name>, which we register as ours there
    // first.
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    const bool ours = name[0] != '/';
    snprintf(address.sun_path, sizeof address.sun_path, "%s%s", ours ? "/dev/" : "", name);
    int e = 0;
    if (ours) {
        const int registry = open("/dev/init/ctl", O_RDWR | O_CLOEXEC);
        e = registry < 0 ? errno : enroll(registry, name, S_IFSOCK | 0755, uint32_t(geteuid()), uint32_t(getegid()), registered);
        if (registry >= 0) {
            close(registry);
        }
    } else {
        unlink(address.sun_path);
    }
    const int listening = e != 0 ? -1 : socket(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0);
    if (e == 0 && (listening < 0 || bind(listening, reinterpret_cast<const sockaddr*>(&address), sizeof address) != 0 || listen(listening, 16) != 0)) {
        e = errno;
    }
    if (e != 0) {
        dprintf(STDERR_FILENO, "fuse: %s: %s\n", address.sun_path, strerror(e));
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
    if (!ours) {
        unlink(address.sun_path);
    }
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
