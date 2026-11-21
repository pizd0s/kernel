// devfs: /dev, a file system of the VFS's (lib/protocol/protocol.h), which
// rootfs starts and mounts on /dev with the channel it gives it: rootfs
// asks it by node, its root node 1. It holds tty, whose open is of the
// opener's controlling terminal, which the kernel opens instead; init,
// where the services of the system register their devices (REGISTER,
// lib/protocol/service.h); those devices, each a service's: an open of
// one goes on to its service, where it is answered with it, and the
// service serves the open file from then on; and the sockets the file
// systems of the system bind there, for mount(2) to connect to. devfs
// keeps a device's name, its mode and its owner, and lets the name go when
// the service's end of the pair closes; a socket's goes when it is
// unlinked. A device's name may be in a directory of /dev, pts/0: the
// directory comes with the first device in it and goes with the last.
// devfs starts the services of the system's devices, /bin/devices,
// /bin/pty and /bin/blk for each disk vm gave the system, and ends when
// rootfs lets go of its channel.
//
//     devfs [-r fd] <channel> [<disk> <fd>]...
//
// -r: a descriptor to close once the devices are there, which the
// services get too: rootfs starts the shell when the last holder has.

#include <bin/tools/tools.h>
#include <lib/protocol/service.h>

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <sys/uio.h>
#include <time.h>
#include <unistd.h>

namespace {
    enum class Kind {
        Dir,
        Controlling, // tty: the opener's controlling terminal
        Registry,
        Device, // a service's: a character or block device
        Socket, // a socket's name: it holds the socket's listener, which CONNECT gives
    };

    // A node of /dev: its root or a directory of devices, tty, init, or a
    // service's device; pos its place among its directory's entries.
    struct Node {
        uint64_t ino;
        uint64_t pos;
        Kind kind;
        uint32_t mode;
        uint32_t uid;
        uint32_t gid;
        uint64_t atime;
        uint64_t mtime;
        uint64_t ctime;
        int fd; // a device's end of its service's pair; a socket's listener
        char name[NAME_MAX + 1];
        Node* parent;  // the directory it is in; the root's is the root
        Node* entries; // a directory's, in their order
        Node* next;    // among its directory's entries
    };

    // A name a watcher told of: its inode, which is its identity, as a
    // name gone and back is another.
    struct Seen {
        uint64_t ino;
        char name[NAME_MAX + 1];
    };

    // A watcher of a directory (WATCH): its open file's endpoint, the
    // names it told of, whether it told them all once (watchIdle), and a
    // READ it keeps, where it is answered, its memfd and length.
    struct Watcher {
        Node* dir; // none once the directory is gone
        Endpoint* end;
        Seen* seen;
        size_t count;
        size_t room;
        bool idle;
        Endpoint* kept;
        int memfd;
        uint32_t length;
        int keptTag; // what the kept READ's endpoint is of
        Watcher* next;
    };
}

static Node root;
static uint64_t inodes;
static uint64_t positions;
static uint64_t dev;
static int epoll = -1;

// What an endpoint is, its of: rootfs's channel or an open of init; a
// device's end is the device's node.
static int channelTag;
static int registryTag;

static uint64_t now() {
    timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return uint64_t(ts.tv_sec) * 1000000000 + uint64_t(ts.tv_nsec);
}

// A device for the nodes: the number of a pair, which no other pair in
// the kernel has.
static uint64_t deviceNumber() {
    int ends[2];
    struct stat st;
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, ends) != 0 || fstat(ends[0], &st) != 0) {
        fatal("devfs: a pair for a device: errno %d", errno);
    }
    close(ends[0]);
    close(ends[1]);
    return uint64_t(st.st_ino);
}

static Stat statOf(const Node& n) {
    Stat st{};
    st.ino = n.ino;
    st.mode = n.mode;
    st.nlink = n.kind == Kind::Dir ? 2 : 1;
    st.uid = n.uid;
    st.gid = n.gid;
    st.atime = n.atime;
    st.mtime = n.mtime;
    st.ctime = n.ctime;
    st.dev = dev;
    return st;
}

static void answerStat(int fd, const Node& n) {
    const Stat st = statOf(n);
    answer(fd, opOk, 0, &st, sizeof st);
}

// Whether who asks may read (4), write (2), and search (1) a node.
static bool may(const Node& n, const Header& h, uint32_t want) {
    if (h.uid == 0) {
        return true;
    }
    const uint32_t bits = h.uid == n.uid ? n.mode >> 6 : h.gid == n.gid ? n.mode >> 3 : n.mode;
    return (bits & want) == want;
}

// A new entry of a directory, after every other.
static Node* add(Node& dir, const char* name, size_t length, Kind kind, uint32_t mode, uint32_t uid, uint32_t gid) {
    auto* n = static_cast<Node*>(calloc(1, sizeof(Node)));
    if (!n) {
        fatal("devfs: out of memory");
    }
    n->ino = ++inodes;
    n->pos = ++positions;
    n->kind = kind;
    n->mode = mode;
    n->uid = uid;
    n->gid = gid;
    n->atime = n->mtime = n->ctime = now();
    n->fd = -1;
    memcpy(n->name, name, length);
    n->parent = &dir;
    Node** at = &dir.entries;
    while (*at) {
        at = &(*at)->next;
    }
    *at = n;
    dir.mtime = dir.ctime = n->ctime;
    return n;
}

static Node* named(const Node& dir, const char* name, size_t length) {
    for (Node* n = dir.entries; n; n = n->next) {
        if (strlen(n->name) == length && memcmp(n->name, name, length) == 0) {
            return n;
        }
    }
    return nullptr;
}

// The node of an inode number, the root or one in a directory under it.
static Node* nodeIn(Node& dir, uint64_t ino) {
    if (dir.ino == ino) {
        return &dir;
    }
    for (Node* n = dir.entries; n; n = n->next) {
        if (Node* found = nodeIn(*n, ino)) {
            return found;
        }
    }
    return nullptr;
}

static Node* nodeOf(uint64_t ino) {
    return nodeIn(root, ino);
}

// The nodes under a directory, it not counted.
static size_t nodesUnder(const Node& dir) {
    size_t count = 0;
    for (const Node* n = dir.entries; n; n = n->next) {
        count += 1 + nodesUnder(*n);
    }
    return count;
}

// ---- watchers -----------------------------------------------------------------

static Watcher* watchers;

static bool knows(const Watcher& w, uint64_t ino) {
    for (size_t i = 0; i < w.count; ++i) {
        if (w.seen[i].ino == ino) {
            return true;
        }
    }
    return false;
}

static bool there(const Node& dir, uint64_t ino) {
    for (const Node* n = dir.entries; n; n = n->next) {
        if (n->ino == ino) {
            return true;
        }
    }
    return false;
}

// Whether a watcher has something to tell: its first names, a change
// since, or that its directory is gone.
static bool pending(const Watcher& w) {
    if (!w.dir || !w.idle) {
        return true;
    }
    for (size_t i = 0; i < w.count; ++i) {
        if (!there(*w.dir, w.seen[i].ino)) {
            return true;
        }
    }
    for (const Node* n = w.dir->entries; n; n = n->next) {
        if (!knows(w, n->ino)) {
            return true;
        }
    }
    return false;
}

static bool put(unsigned char* out, size_t room, size_t& size, uint8_t kind, const char* name) {
    const size_t length = strlen(name);
    if (room - size < length + 2) {
        return false;
    }
    out[size] = kind;
    memcpy(out + size + 1, name, length + 1);
    size += length + 2;
    return true;
}

// The records a watcher tells now, as many as room holds, which it then
// knows of: the names there not told yet, as watchExisting and then
// watchIdle the first time, as watchAdded after it; the names told and
// gone, watchRemoved, all of them once its directory is gone.
static size_t records(Watcher& w, unsigned char* out, size_t room) {
    size_t size = 0;
    if (!w.dir) {
        while (w.count > 0 && put(out, room, size, watchRemoved, w.seen[w.count - 1].name)) {
            --w.count;
        }
        return size;
    }
    if (w.idle) {
        for (size_t i = 0; i < w.count;) {
            if (there(*w.dir, w.seen[i].ino)) {
                ++i;
            } else if (put(out, room, size, watchRemoved, w.seen[i].name)) {
                w.seen[i] = w.seen[--w.count];
            } else {
                return size;
            }
        }
    }
    for (const Node* n = w.dir->entries; n; n = n->next) {
        if (knows(w, n->ino)) {
            continue;
        }
        if (!put(out, room, size, w.idle ? watchAdded : watchExisting, n->name)) {
            return size;
        }
        if (w.count == w.room) {
            w.room = w.room ? 2 * w.room : 8;
            w.seen = static_cast<Seen*>(realloc(w.seen, w.room * sizeof(Seen)));
            if (!w.seen) {
                fatal("devfs: out of memory");
            }
        }
        w.seen[w.count].ino = n->ino;
        memcpy(w.seen[w.count].name, n->name, sizeof n->name);
        ++w.count;
    }
    if (!w.idle && put(out, room, size, watchIdle, "")) {
        w.idle = true;
    }
    return size;
}

// Tells the kernel whether a watcher is readable.
static void ready(const Watcher& w) {
    tell(w.end->fd, pending(w) ? POLLIN | (w.dir ? 0 : POLLHUP) : 0);
}

// The records of a READ into its memfd, its count answered on fd: 0 of a
// directory gone, once its names are told gone; none of a READ too short
// for the next record, EINVAL.
static void answerRecords(Watcher& w, int memfd, uint32_t length, int fd) {
    static unsigned char out[65536 - sizeof(Header)];
    const size_t size = records(w, out, length < sizeof out ? size_t(length) : sizeof out);
    if (size == 0 && (w.dir || w.count > 0)) {
        return fail(fd, EINVAL);
    }
    if (size > 0 && pwrite(memfd, out, size, 0) != ssize_t(size)) {
        return fail(fd, EIO);
    }
    const uint32_t count = uint32_t(size);
    answer(fd, opOk, 0, &count, sizeof count);
}

static void letKeptGo(Watcher& w) {
    close(w.memfd);
    unkeep(epoll, w.kept);
    w.kept = nullptr;
}

// A watcher's directory changed, or is gone: a READ it keeps is answered.
static void told(Watcher& w) {
    if (w.kept && pending(w)) {
        answerRecords(w, w.memfd, w.length, w.kept->fd);
        letKeptGo(w);
    }
    ready(w);
}

static void changed(Node& dir) {
    for (Watcher* w = watchers; w; w = w->next) {
        if (w->dir == &dir) {
            told(*w);
        }
    }
}

static void dirGone(Node& dir) {
    for (Watcher* w = watchers; w; w = w->next) {
        if (w->dir == &dir) {
            w->dir = nullptr;
            told(*w);
        }
    }
}

// WATCH of a directory: a watcher, an open file of the flags asked,
// O_NONBLOCK of them, whose READs give its records.
static void watchNode(Node& n, Request& r) {
    if (n.kind != Kind::Dir) {
        return fail(r.reply(), ENOTDIR);
    }
    if (!may(n, r.h, 4)) {
        return fail(r.reply(), EACCES);
    }
    auto* w = static_cast<Watcher*>(calloc(1, sizeof(Watcher)));
    if (!w) {
        fatal("devfs: out of memory");
    }
    w->dir = &n;
    w->memfd = -1;
    r.h.arg &= O_NONBLOCK;
    w->end = answerOpen(epoll, r, {openedFile, 0, dev, 0, 0, POLLIN}, w);
    if (!w->end) {
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
    free(w->seen);
    free(w);
}

// A service's end of its pair closed: the service is gone, and its
// device's name with it, and the directory it was in when that is left
// empty, but the root.
static void unregister(Node* n) {
    Node* dir = n->parent;
    for (Node** at = &dir->entries; *at; at = &(*at)->next) {
        if (*at == n) {
            *at = n->next;
            break;
        }
    }
    dir->mtime = dir->ctime = now();
    if (n->kind == Kind::Socket && n->fd >= 0) {
        close(n->fd);
    }
    if (n->kind == Kind::Dir) {
        dirGone(*n);
    }
    free(n);
    changed(*dir);
    if (dir != &root && !dir->entries) {
        unregister(dir);
    }
}

// A name a call takes: not empty, no "." or "..", no "/", NAME_MAX at
// most.
static int badName(const char* name, size_t length) {
    if (length == 0 || (length == 1 && name[0] == '.') || (length == 2 && name[0] == '.' && name[1] == '.') ||
        memchr(name, '/', length)) {
        return EINVAL;
    }
    return length > NAME_MAX ? ENAMETOOLONG : 0;
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

// An open of a node: tty's the opener's controlling terminal, which the
// kernel opens; init's a plain socket, an end of a pair of ours, which
// the opener speaks the protocol on itself, REGISTER; a device's open
// handed on to its service with where it is answered, which the service
// answers on, ENXIO when the service is gone.
static void openNode(Node& n, Request& r) {
    const uint32_t access = r.h.arg & O_ACCMODE;
    const bool reads = access != O_WRONLY;
    const bool writes = access != O_RDONLY;
    if (n.kind == Kind::Dir) {
        return fail(r.reply(), EISDIR);
    }
    if (n.kind == Kind::Socket) {
        return fail(r.reply(), ENXIO);
    }
    if (!may(n, r.h, (reads ? 4 : 0) | (writes || (r.h.arg & O_TRUNC) != 0 ? 2 : 0))) {
        return fail(r.reply(), EACCES);
    }
    switch (n.kind) {
        case Kind::Controlling:
            return answer(r.reply(), opOk, openControlling);
        case Kind::Registry: {
            int pair[2];
            if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
                return fail(r.reply(), errno);
            }
            watch(epoll, {pair[0], false, reads, writes, &registryTag, false, true});
            const Header h{opOk, 0, 0, 0, 0, 0};
            const iovec part{const_cast<Header*>(&h), sizeof h};
            sendPacket(r.reply(), &part, 1, &pair[1], 1);
            close(pair[1]);
            return;
        }
        default: {
            const Header h{opOpen, r.h.arg, r.h.uid, r.h.gid, r.h.pgrp, r.h.sid};
            const uint32_t mode = 0;
            const iovec parts[] = {{const_cast<Header*>(&h), sizeof h}, {const_cast<uint32_t*>(&mode), sizeof mode}};
            const int reply = r.reply();
            if (n.fd < 0 || sendPacket(n.fd, parts, 2, &reply, 1, MSG_DONTWAIT) < 0) {
                fail(reply, n.fd >= 0 && errno == EAGAIN ? EAGAIN : ENXIO);
            }
            return;
        }
    }
}

// The entries of a directory after a position, as many as one reply holds.
static void readDir(const Node& dir, const Request& r) {
    uint64_t after;
    if (!r.at(8, after)) {
        return fail(r.reply(), EINVAL);
    }
    static unsigned char out[65536 - sizeof(Header)];
    size_t size = 0;
    size_t room = r.h.arg;
    for (const Node* n = dir.entries; n; n = n->next) {
        const size_t length = strlen(n->name);
        if (n->pos <= after) {
            continue;
        }
        if (size + 17 + length + 1 > sizeof out || direntRoom(length) > room) {
            break;
        }
        room -= direntRoom(length);
        const uint8_t type = uint8_t((n->mode >> 12) & 017);
        memcpy(out + size, &n->ino, 8);
        memcpy(out + size + 8, &n->pos, 8);
        out[size + 16] = type;
        memcpy(out + size + 17, n->name, length + 1);
        size += 17 + length + 1;
    }
    answer(r.reply(), opOk, 0, out, size);
}

// A call of rootfs's by node. /dev makes and changes nothing but by
// REGISTER: a name that is there is EEXIST to the calls that make one,
// as Linux finds it first, else EROFS.
static void fsCall(Request& r) {
    uint64_t ino;
    if (r.h.op == fsRoot) {
        // Names come and go here by REGISTER, which rootfs does not see:
        // it keeps nothing of /dev's.
        struct [[gnu::packed]] {
            Stat st;
            uint32_t flags;
        } reply{statOf(root), fsNocache};

        return answer(r.reply(), opOk, 0, &reply, sizeof reply);
    }
    if (!r.at(0, ino)) {
        return fail(r.reply(), EINVAL);
    }
    Node* n = nodeOf(ino);
    if (!n) {
        return fail(r.reply(), ENOENT);
    }
    const char* name = nullptr;
    size_t length = 0;
    switch (r.h.op) {
        case fsLookup: {
            if (!nameAt(r, 8, name, length)) {
                return fail(r.reply(), EINVAL);
            }
            if (n->kind != Kind::Dir) {
                return fail(r.reply(), ENOTDIR);
            }
            if (!may(*n, r.h, 1)) {
                return fail(r.reply(), EACCES);
            }
            const Node* found = length == 2 && memcmp(name, "..", 2) == 0 ? n->parent : named(*n, name, length);
            return found ? answerStat(r.reply(), *found) : fail(r.reply(), ENOENT);
        }
        case fsGetattr:
            return answerStat(r.reply(), *n);
        case fsReadlink:
            return fail(r.reply(), EINVAL);
        case fsReaddir:
            return n->kind == Kind::Dir ? readDir(*n, r) : fail(r.reply(), ENOTDIR);
        case fsOpen:
            return openNode(*n, r);
        case fsCreate:
        case fsMkdir:
        case fsMknod:
        case fsSymlink:
        case fsLink: {
            const size_t at = r.h.op == fsCreate ? 12 : r.h.op == fsLink ? 16 : 8;
            if (r.h.op == fsSymlink && nameAt(r, 8, name, length)) {
                nameAt(r, 8 + length + 1, name, length);
            } else if (!nameAt(r, at, name, length)) {
                return fail(r.reply(), EINVAL);
            }
            uint64_t to = ino;
            if (r.h.op == fsLink && !r.at(8, to)) {
                return fail(r.reply(), EINVAL);
            }
            const Node* dir = nodeOf(to);
            if (!dir) {
                return fail(r.reply(), ENOENT);
            }
            if (const int e = badName(name, length)) {
                return fail(r.reply(), e);
            }
            if (dir->kind != Kind::Dir) {
                return fail(r.reply(), ENOTDIR);
            }
            if (!may(*dir, r.h, 1)) {
                return fail(r.reply(), EACCES);
            }
            return fail(r.reply(), named(*dir, name, length) ? EEXIST : EROFS);
        }
        case fsUnlink: {
            // A socket's name goes as a writer of /dev takes it away;
            // nothing else of /dev does.
            if (!nameAt(r, 8, name, length)) {
                return fail(r.reply(), EINVAL);
            }
            Node* found = named(*n, name, length);
            if (!found) {
                return fail(r.reply(), ENOENT);
            }
            if (found->kind != Kind::Socket || (r.h.arg & AT_REMOVEDIR) != 0) {
                return fail(r.reply(), EROFS);
            }
            if (!may(*n, r.h, 3)) {
                return fail(r.reply(), EACCES);
            }
            unregister(found);
            return answer(r.reply(), opOk, 0);
        }
        case fsBind: {
            if (!nameAt(r, 8, name, length) || r.count != 2) {
                return fail(r.reply(), EINVAL);
            }
            if (const int e = badName(name, length)) {
                return fail(r.reply(), e);
            }
            if (n->kind != Kind::Dir) {
                return fail(r.reply(), ENOTDIR);
            }
            if (named(*n, name, length)) {
                return fail(r.reply(), EEXIST);
            }
            if (!may(*n, r.h, 3)) {
                return fail(r.reply(), EACCES);
            }
            Node* made = add(*n, name, length, Kind::Socket, S_IFSOCK | (r.h.arg & 07777), r.h.uid, r.h.gid);
            made->fd = r.fds[0];
            r.fds[0] = -1; // the node's now
            return answer(r.reply(), opOk, 0);
        }
        case fsConnect: {
            if (n->kind != Kind::Socket) {
                return fail(r.reply(), ECONNREFUSED);
            }
            if (!may(*n, r.h, 2)) {
                return fail(r.reply(), EACCES);
            }
            const Header ok{opOk, 0, 0, 0, 0, 0};
            const iovec parts[] = {{const_cast<Header*>(&ok), sizeof ok}};
            sendPacket(r.reply(), parts, 1, &n->fd, 1);
            return;
        }
        case fsRename:
        case fsChmod:
        case fsChown:
        case fsUtimes:
        case fsTruncate:
            return fail(r.reply(), EROFS);
        case fsStatfs: {
            const Statfs st{0, 0, 1 + nodesUnder(root), 0, dev, 4096, NAME_MAX, ST_RDONLY};
            return answer(r.reply(), opOk, 0, &st, sizeof st);
        }
        case fsPath: {
            // Made from its end, a name and its "/" before it at a time.
            static char path[PATH_MAX];
            size_t start = sizeof path;
            for (const Node* at = n; at != &root; at = at->parent) {
                const size_t length = strlen(at->name);
                if (start < length + 1) {
                    return fail(r.reply(), ENAMETOOLONG);
                }
                start -= length;
                memcpy(path + start, at->name, length);
                path[--start] = '/';
            }
            if (start == sizeof path) {
                path[--start] = '/';
            }
            return answer(r.reply(), opOk, 0, path + start, sizeof path - start);
        }
        case fsWatch:
            return watchNode(*n, r);
        default:
            return fail(r.reply(), ENOSYS);
    }
}

// A service's device: a name in /dev of the mode asked for, S_IFCHR or
// S_IFBLK, whose opens go on to the end of a pair the request brings,
// the service's the other; its stat is the reply. A service registers
// files alone: a name with "/" is one in directories of /dev, which come
// with the first name in them and go with the last. Only on an endpoint
// of init open to write.
static void registerName(const Endpoint& e, Request& r) {
    const char* name;
    size_t length;
    const uint32_t type = r.h.arg & S_IFMT;
    if (!e.writes) {
        return fail(r.reply(), EBADF);
    }
    if (r.count != 2 || (type != S_IFCHR && type != S_IFBLK) || !nameAt(r, 0, name, length)) {
        return fail(r.reply(), EINVAL);
    }
    for (const char *at = name, *end = name + length;;) {
        const char* slash = static_cast<const char*>(memchr(at, '/', size_t(end - at)));
        if (const int bad = badName(at, size_t((slash ? slash : end) - at))) {
            return fail(r.reply(), bad);
        }
        if (!slash) {
            break;
        }
        at = slash + 1;
    }
    // Every directory of the path, there or made, a file where it is not.
    Node* dir = &root;
    for (const char* slash; (slash = static_cast<const char*>(memchr(name, '/', length)));) {
        const size_t part = size_t(slash - name);
        Node* in = named(*dir, name, part);
        if (in && in->kind != Kind::Dir) {
            return fail(r.reply(), ENOTDIR);
        }
        if (!in) {
            in = add(*dir, name, part, Kind::Dir, S_IFDIR | 0755, 0, 0);
            changed(*dir);
        }
        dir = in;
        length -= part + 1;
        name = slash + 1;
    }
    if (named(*dir, name, length)) {
        return fail(r.reply(), EEXIST);
    }
    Node* n = add(*dir, name, length, Kind::Device, r.h.arg & (S_IFMT | 07777), r.h.uid, r.h.gid);
    n->fd = r.fds[0];
    r.fds[0] = -1; // the device's now
    watch(epoll, {n->fd, false, false, false, n});
    changed(*dir);
    answerStat(r.reply(), *n);
}

static void handle(Endpoint& e, Request& r) {
    bool kept = false;
    if (e.of == &channelTag) {
        fsCall(r);
    } else if (e.of == &registryTag && r.h.op == opRegister) {
        registerName(e, r);
    } else if (Watcher* w = watcherOf(e.of, kept); w && kept) {
        if (r.h.op == opCancel) {
            fail(w->kept->fd, EINTR);
            letKeptGo(*w);
        }
    } else if (w && r.h.op == opRead) {
        readWatcher(*w, e, r);
    } else if (w) {
        fail(r.reply(), unserved(r.h.op));
    } else {
        fail(r.reply(), EINVAL);
    }
}

// An endpoint closed: rootfs's channel, which ends devfs; a device's
// end, whose name goes.
static void gone(Endpoint& e) {
    bool kept = false;
    if (e.of == &channelTag) {
        stopServing();
    } else if (Watcher* w = watcherOf(e.of, kept); w && kept) {
        // Who asked went: serve closes the endpoint itself.
        close(w->memfd);
        w->kept = nullptr;
    } else if (w) {
        forgetWatcher(w);
    } else if (e.of != &registryTag) {
        unregister(static_cast<Node*>(e.of));
    }
}

// Starts a service of the system's: its argv, a disk's descriptor for
// it alone, not closed on exec in the child.
static void start(const char* path, char* const* argv, int disk) {
    const pid_t pid = fork();
    if (pid == 0) {
        if (disk >= 0) {
            fcntl(disk, F_SETFD, 0);
        }
        execv(path, argv);
        dprintf(STDERR_FILENO, "devfs: %s: %s\n", path, strerror(errno));
        _exit(127);
    }
    if (pid < 0) {
        dprintf(STDERR_FILENO, "devfs: fork: %s\n", strerror(errno));
    }
}

int devfsMain(int argc, char** argv) {
    int at;
    const int ready = readyDescriptor(argc, argv, at);
    if (argc - at < 1 || (argc - at - 1) % 2 != 0) {
        dprintf(STDERR_FILENO, "usage: devfs [-r fd] <channel> [<disk> <fd>]...\n");
        return 2;
    }
    const int channel = atoi(argv[at]);
    dev = deviceNumber();
    root.ino = ++inodes;
    root.kind = Kind::Dir;
    root.mode = S_IFDIR | 0755;
    root.uid = uint32_t(geteuid());
    root.gid = uint32_t(getegid());
    root.atime = root.mtime = root.ctime = now();
    root.parent = &root;
    add(root, "tty", 3, Kind::Controlling, S_IFCHR | 0666, 0, 0);
    add(root, "init", 4, Kind::Registry, S_IFCHR | 0600, uint32_t(geteuid()), uint32_t(getegid()));
    epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0 || fcntl(channel, F_SETFD, FD_CLOEXEC) != 0) {
        fatal("devfs: errno %d", errno);
    }
    watch(epoll, {channel, false, true, true, &channelTag});
    signal(SIGCHLD, SIG_IGN);
    for (int i = at + 2; i < argc; i += 2) {
        fcntl(atoi(argv[i]), F_SETFD, FD_CLOEXEC);
    }
    char readyText[12];
    snprintf(readyText, sizeof readyText, "%d", ready);
    char devicesName[] = "devices";
    char blkName[] = "blk";
    char option[] = "-r";
    char* const devicesArgv[] = {devicesName, option, readyText, nullptr};
    char* const bareDevices[] = {devicesName, nullptr};
    start("/bin/devices", ready >= 0 ? devicesArgv : bareDevices, -1);
    char ptyName[] = "pty";
    char* const ptyArgv[] = {ptyName, option, readyText, nullptr};
    char* const barePty[] = {ptyName, nullptr};
    start("/bin/pty", ready >= 0 ? ptyArgv : barePty, -1);
    for (int i = at + 1; i < argc; i += 2) {
        char* const blkArgv[] = {blkName, option, readyText, argv[i], argv[i + 1], nullptr};
        char* const bareBlk[] = {blkName, argv[i], argv[i + 1], nullptr};
        start("/bin/blk", ready >= 0 ? blkArgv : bareBlk, atoi(argv[i + 1]));
        close(atoi(argv[i + 1]));
    }
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle, gone);
    return 0;
}
