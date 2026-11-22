// tmpfs: /run, a tree in the memory of its own process, of at most
// tmpCapacity bytes, a quarter of what a memory of wasm32 holds: a file
// system of the VFS's (lib/protocol/protocol.h), which rootfs starts and
// mounts on /run with the channel it gives it. rootfs walks the VFS's
// paths and asks it by node, its root node 1, one name at a time; an open
// file is an endpoint of tmpfs's, which the kernel reads and writes. It
// ends when rootfs has let go of its channel and the last open file is
// closed.
//
//     tmpfs <channel>
//
// The tree makes what it creates theirs who ask, and lets them do what its
// modes let them: root everything but executing what nobody may, the owner
// what the owner's bits say, the group of gid the group's, everyone else
// the others'. A name is looked up in a directory who asks may search,
// made or removed in one they may write, and in a sticky one removed only
// by the owner of the name's node or of the directory.
//
// A directory's entries have positions, each after those before it: a
// READDIR gives the entries after one, so a reading that removes what it
// read misses nothing; "." is at 1 and ".." at 2, the root's its own, the
// names from 3 on. A node has the names that are links to it, a
// directory one. dev is the tree's: the number of a pair it makes as it
// makes its root, which no other pair in the kernel has. A node no name
// and no open file has is gone, and a call by its node ENOENT.
//
// A WRITE with append writes at the end the file has when the tree takes
// it, which no other write comes between. The locks of flock and fcntl
// are the kernel's, kept by the dev and ino an open file is made with. A
// FIFO is a node and nothing more: the kernel makes the pipe its openers
// share; so is a socket, the kernel keeping what is bound to it. The tree holds what its capacity lets it, in bytes and in nodes,
// a node a block's worth: past that, ENOSPC.
//
// Memory. A request is received into one buffer, packet, and answered
// before the next: its names are read where they are in it. A file's bytes
// go straight to the memory of the process that reads them, or come
// straight from the memory of the one that writes them, through the memfd
// of it a READ or WRITE brings: one copy. What a reply assembles goes into
// one buffer of its own, out. The names of every directory are one
// open-addressing table, by directory and name; a directory keeps its
// entries in the order of their positions.

#include <bin/tools/lib.h>
#include <bin/tools/tools.h>
#include <lib/protocol/protocol.h>

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/param.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <time.h>
#include <unistd.h>

using namespace tools;

constexpr u64 tmpCapacity = u64(1) << 30;

namespace {
    // An open-addressing table of pointers, linear probing, removal by
    // shifting back what follows: each slot keeps the hash of its item,
    // which a probe compares first.
    template <typename T>
    struct OpenTable {
        struct Slot {
            u32 hash;
            T* item; // none: the slot is empty
        };

        template <typename Same>
        T* find(u32 hash, Same same) const;
        void insert(u32 hash, T* item);
        void erase(u32 hash, const T* item);
        void put(u32 hash, T* item);
        void grow();

        Slot* slots = nullptr;
        u32 mask = 0;
        u32 count = 0;
    };
}

template <typename T>
template <typename Same>
inline T* OpenTable<T>::find(u32 hash, Same same) const {
    if (!slots) {
        return nullptr;
    }
    for (u32 i = hash & mask;; i = (i + 1) & mask) {
        if (!slots[i].item) {
            return nullptr;
        }
        if (slots[i].hash == hash && same(slots[i].item)) {
            return slots[i].item;
        }
    }
}

template <typename T>
inline void OpenTable<T>::insert(u32 hash, T* item) {
    if (!slots || (count + 1) * 2 > mask + 1) {
        grow();
    }
    put(hash, item);
    ++count;
}

template <typename T>
inline void OpenTable<T>::erase(u32 hash, const T* item) {
    u32 i = hash & mask;
    while (slots[i].item != item) {
        if (!slots[i].item) {
            fatal("tmpfs: no such item in a table");
        }
        i = (i + 1) & mask;
    }
    // What follows moves back into the hole, unless that would put
    // it before its home: its home is in (i, j], cyclically.
    for (u32 j = (i + 1) & mask; slots[j].item; j = (j + 1) & mask) {
        const u32 home = slots[j].hash & mask;
        const bool stays = i <= j ? i < home && home <= j : i < home || home <= j;
        if (!stays) {
            slots[i] = slots[j];
            i = j;
        }
    }
    slots[i] = {0, nullptr};
    --count;
}

template <typename T>
inline void OpenTable<T>::put(u32 hash, T* item) {
    u32 i = hash & mask;
    while (slots[i].item) {
        i = (i + 1) & mask;
    }
    slots[i] = {hash, item};
}

template <typename T>
inline void OpenTable<T>::grow() {
    Slot* old = slots;
    const usize before = old ? usize(mask) + 1 : 0;
    const usize capacity = old ? checkedMultiply(before, 2) : 16;
    slots = static_cast<Slot*>(calloc(capacity, sizeof(Slot)));
    if (!slots) {
        fatal("tmpfs: out of memory");
    }
    mask = u32(capacity - 1);
    for (usize i = 0; i < before; ++i) {
        if (old[i].item) {
            put(old[i].hash, old[i].item);
        }
    }
    free(old);
}

// The bits of a 64-bit hash folded to 32, each output bit depending on
// all (murmur3's finalizer).
static u32 finish(u64 h) {
    h ^= h >> 33;
    h *= 0xff51afd7ed558ccdull;
    h ^= h >> 33;
    h *= 0xc4ceb9fe1a85ec53ull;
    h ^= h >> 33;
    return u32(h);
}

// ---- the tree --------------------------------------------------------------------

namespace {
    struct Node;
    struct Server;

    enum class Kind : u8 {
        Dir,
        File,
        Link,
        Fifo,   // a named pipe: the kernel makes the pipe
        Socket, // a socket's name: the kernel keeps what is bound to it
    };

    // A name of a directory: its bytes are the image's or follow the entry.
    struct Entry {
        Node* dir;
        Node* node;
        u64 pos; // where the name is among the directory's entries
        Name name;
        u32 hash;
    };

    // A directory's entries in the order of their positions; a removed
    // one leaves a hole, until holes are half of them.
    struct Slot {
        u64 pos;
        Entry* entry; // none: removed
    };

    // A file's bytes or a link's target: the image's, or the node's own.
    struct Content {
        const u8* view = nullptr;
        usize viewSize = 0;
        Buffer own;

        const u8* data() const;
        usize size() const;
        Name name() const;
    };

    struct Node {
        u64 ino = 0;
        u32 perm = 0; // the permission bits
        u32 uid = 0;
        u32 gid = 0;
        u64 atime = 0; // nanoseconds
        u64 mtime = 0;
        u64 ctime = 0;
        u32 links = 0;         // the names that are links to it
        u32 opens = 0;         // the endpoints open on it
        u32 holds = 0;         // what else points at it: directories under it, files opened in it by name
        bool released = false; // gone from its server, its memory kept for what holds it
        Kind kind = Kind::Dir;
        Content content; // a file's, a link's
        // a directory's
        Node* parent = nullptr; // where ".." goes; none for the root
        Entry* entry = nullptr; // its name in parent
        u64 entries = 2;        // the last position given out, after "." and ".."
        u32 names = 0;          // the names in it
        u32 subdirs = 0;        // the directories among them
        u32 holes = 0;
        SmallVec<Slot> order;
    };

    struct Server {
        Node* root = nullptr;
        bool writable = false;
        u64 capacity = 0;      // the bytes a writable server's files may hold
        u64 inodes = 0;        // the inode numbers given out
        u64 nodes = 0;         // the nodes there are
        u64 used = 0;          // the bytes the files hold
        u64 dev = 0;           // the device of the server's files
        OpenTable<Node> byIno; // the nodes there are, by inode

        Node* find(u64 ino) const;
    };
}

inline const u8* Content::data() const {
    return view ? view : static_cast<const u8*>(own.data());
}

inline usize Content::size() const {
    return view ? viewSize : own.size();
}

inline Name Content::name() const {
    return {reinterpret_cast<const char*>(data()), size()};
}

static Server tree; // the one of the process

static u32 hashIno(u64 ino) {
    return finish(ino);
}

Node* Server::find(u64 ino) const {
    return byIno.find(hashIno(ino), [ino](const Node* n) {
        return n->ino == ino;
    });
}

// FNV-1a over the directory and the name.
static u32 hashName(const Node* dir, Name name) {
    u64 h = 0xcbf29ce484222325ull ^ u64(uintptr_t(dir));
    for (usize i = 0; i < name.length; ++i) {
        h = (h ^ u8(name.at[i])) * 0x100000001b3ull;
    }
    return finish(h);
}

// The names of all directories, by directory and name.
static OpenTable<Entry> names;

static Entry* lookup(const Node* dir, Name name) {
    return names.find(hashName(dir, name), [dir, name](const Entry* e) {
        return e->dir == dir && e->name.length == name.length && memcmp(e->name.at, name.at, name.length) == 0;
    });
}

static u64 now() {
    timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return u64(ts.tv_sec) * 1000000000 + u64(ts.tv_nsec);
}

static void touch(Node* n) {
    n->mtime = n->ctime = now();
}

static bool dead(const Node* n) {
    return n->released && n->links == 0 && n->opens == 0 && n->holds == 0;
}

// A released node nothing points at any more: its memory goes, and
// with it its hold on its parent.
static void destroy(Node* n) {
    while (n) {
        Node* parent = n->kind == Kind::Dir ? n->parent : nullptr;
        n->~Node();
        free(n);
        n = parent && --parent->holds == 0 && dead(parent) ? parent : nullptr;
    }
}

static void drop(Node* n) {
    if (--n->holds == 0 && dead(n)) {
        destroy(n);
    }
}

// Puts a name for a node in a directory, after every entry there: at
// the next position. copy: the name's bytes go with the entry, else
// they are the image's.
static void place(Node* dir, Name name, Node* n, u64 t, bool copy = true) {
    auto* e = static_cast<Entry*>(allocate(checkedAdd(sizeof(Entry), copy ? name.length : 0)));
    if (copy) {
        char* bytes = reinterpret_cast<char*>(e + 1);
        memcpy(bytes, name.at, name.length);
        name.at = bytes;
    }
    *e = {dir, n, ++dir->entries, name, hashName(dir, name)};
    names.insert(e->hash, e);
    dir->order.pushBack({e->pos, e});
    ++dir->names;
    ++n->links;
    if (n->kind == Kind::Dir) {
        ++dir->subdirs;
        n->entry = e;
        if (n->parent != dir) {
            ++dir->holds;
            if (n->parent) {
                drop(n->parent);
            }
            n->parent = dir;
        }
    }
    dir->mtime = dir->ctime = n->ctime = t;
}

// Takes a name out of a directory: the node it named, one link fewer.
static Node* unplace(Node* dir, Entry* e, u64 t) {
    Node* n = e->node;
    names.erase(e->hash, e);
    Slot* slots = dir->order.data();
    const usize count = dir->order.size();
    usize lo = 0;
    usize hi = count;
    while (lo < hi) {
        const usize mid = lo + (hi - lo) / 2;
        if (slots[mid].pos < e->pos) {
            lo = mid + 1;
        } else {
            hi = mid;
        }
    }
    if (lo == count || slots[lo].entry != e) {
        fatal("tmpfs: an entry out of its directory's order");
    }
    slots[lo].entry = nullptr;
    if (++dir->holes * 2 > count) {
        dir->order.keepIf([](const Slot& slot) {
            return slot.entry != nullptr;
        });
        dir->holes = 0;
    }
    --dir->names;
    if (n->kind == Kind::Dir) {
        --dir->subdirs;
        n->entry = nullptr;
    }
    --n->links;
    free(e);
    dir->mtime = dir->ctime = n->ctime = t;
    return n;
}

// A new node, with the permissions of its kind until they are set; the
// first gives the server its device.
static Node* makeNode(Server& s, Node* parent, Name name, Kind kind, bool copy = true) {
    if (s.dev == 0) {
        s.dev = device();
    }
    ++s.nodes;
    const u64 t = now();
    auto* n = new (allocate(sizeof(Node))) Node();
    n->ino = ++s.inodes;
    n->kind = kind;
    n->atime = n->mtime = n->ctime = t;
    switch (kind) {
        case Kind::Dir:
            n->perm = 0755;
            break;
        case Kind::File:
        case Kind::Fifo:
            n->perm = 0644;
            break;
        case Kind::Link:
            n->perm = 0777;
            break;
        default:
            n->perm = 0666;
    }
    s.byIno.insert(hashIno(n->ino), n);
    if (parent) {
        place(parent, name, n, t, copy);
    }
    return n;
}

// A node with no name and no endpoint is gone: what it held is the
// server's again. The root has no name, and stays.
static void release(Server& s, Node* n) {
    if (n->links != 0 || n->opens != 0 || n == s.root) {
        return;
    }
    if (!n->released) {
        if (n->kind == Kind::File) {
            s.used -= n->content.size();
            n->content.own.clear();
            n->content.view = nullptr;
            n->content.viewSize = 0;
        }
        s.byIno.erase(hashIno(n->ino), n);
        --s.nodes;
        n->released = true;
    }
    if (n->holds == 0) {
        destroy(n);
    }
}

// Whether a writable server has no node left to make.
static bool full(const Server& s) {
    return s.capacity > 0 && s.nodes >= s.capacity / blockSize;
}

// A file's size set, its bytes counted against the capacity, those past
// what it had zeros: false when they do not fit, ENOSPC.
static bool resize(Server& s, Node* n, u64 size) {
    Buffer& own = n->content.own;
    const u64 old = own.size();
    if ((size > old && s.capacity > 0 && s.used + (size - old) > s.capacity) || size > SIZE_MAX) {
        return false;
    }
    if (size == 0) {
        own.clear();
    } else {
        own.resize(usize(size));
        if (size > old) {
            memset(static_cast<u8*>(own.data()) + old, 0, usize(size - old));
        }
    }
    s.used = s.used + size - old;
    return true;
}

// Whether who asks may read (4), write (2), and search or execute (1)
// the node.
static bool may(const Node* n, const Header& h, u32 want) {
    if (h.uid == 0) {
        return (want & 1) == 0 || n->kind == Kind::Dir || (n->perm & 0111) != 0;
    }
    const u32 bits = h.uid == n->uid ? n->perm >> 6 : h.gid == n->gid ? n->perm >> 3 : n->perm;
    return (bits & want) == want;
}

static bool owns(const Node* n, const Header& h) {
    return h.uid == 0 || h.uid == n->uid;
}

// Whether a sticky directory keeps a name from who asks: neither the
// node it names nor the directory is theirs.
static bool keeps(const Node* dir, const Node* n, const Header& h) {
    return (dir->perm & S_ISVTX) != 0 && !owns(n, h) && h.uid != dir->uid;
}

static u32 modeOf(const Node* n) {
    switch (n->kind) {
        case Kind::Dir:
            return S_IFDIR | n->perm;
        case Kind::File:
            return S_IFREG | n->perm;
        case Kind::Link:
            return S_IFLNK | n->perm;
        case Kind::Socket:
            return S_IFSOCK | n->perm;
        default:
            return S_IFIFO | n->perm;
    }
}

static u8 dtype(const Node* n) {
    switch (n->kind) {
        case Kind::Dir:
            return DT_DIR;
        case Kind::File:
            return DT_REG;
        case Kind::Link:
            return DT_LNK;
        case Kind::Socket:
            return DT_SOCK;
        default:
            return DT_FIFO;
    }
}

// Whether a directory was removed: it takes no name.
static bool gone(const Server* s, const Node* c) {
    return c->kind == Kind::Dir && c->links == 0 && c != s->root;
}

// Why a name cannot be made in a directory, 0 when it can. A name that
// is there is EEXIST before anything else, on a server that writes
// nothing too: mkdir -p asks for each directory.
static int creates(const Server* s, Node* c, Name name, const Header& h) {
    if (const int e = badName(name)) {
        return e;
    }
    if (c->kind != Kind::Dir) {
        return ENOTDIR;
    }
    if (!may(c, h, 1)) {
        return EACCES;
    }
    if (lookup(c, name)) {
        return EEXIST;
    }
    if (gone(s, c)) {
        return ENOENT;
    }
    if (!s->writable) {
        return EROFS;
    }
    if (!may(c, h, 3)) {
        return EACCES;
    }
    if (full(*s)) {
        return ENOSPC;
    }
    return 0;
}

// A directory's links: its name, its ".", and the ".." of each
// directory in it; another's: its names.
static Stat statOf(const Server& s, const Node* n) {
    Stat st{};
    st.ino = n->ino;
    st.mode = modeOf(n);
    st.nlink = n->kind == Kind::Dir ? 2 + n->subdirs : n->links;
    st.uid = n->uid;
    st.gid = n->gid;
    st.atime = n->atime;
    st.mtime = n->mtime;
    st.dev = s.dev;
    st.ctime = n->ctime;
    if (n->kind == Kind::File || n->kind == Kind::Link) {
        st.size = n->content.size();
    }
    return st;
}

// A writable server's capacity, in blocks and in nodes, and what is
// left of it; a read-only one's what it holds, nothing left.
static Statfs statfsOf(const Server& s) {
    Statfs st{};
    st.bsize = u32(blockSize);
    st.namemax = NAME_MAX;
    st.dev = s.dev;
    const u64 used = (s.used + blockSize - 1) / blockSize;
    if (s.writable && s.capacity > 0) {
        st.blocks = st.files = s.capacity / blockSize;
        st.bfree = st.blocks - MIN(used, st.blocks);
        st.ffree = st.files - MIN(s.nodes, st.files);
    } else {
        st.blocks = used;
        st.files = s.nodes;
    }
    if (!s.writable) {
        st.flags = ST_RDONLY;
    }
    return st;
}

// ---- the calls of a tree of ours, by node --------------------------------------

// mkdir, mknod and symlink: a new name in the directory, who asks may
// write, theirs, with the mode asked for. mknod makes FIFOs, sockets and
// regular files; a device is EPERM.
static int makeOurs(Server& s, Node* dir, u32 op, u32 mode, Name name, Name target, const Header& h) {
    if (const int e = creates(&s, dir, name, h)) {
        return e;
    }
    Kind kind = Kind::Dir;
    if (op == vfsSymlink) {
        kind = Kind::Link;
    } else if (op == vfsMknod) {
        switch (mode & S_IFMT) {
            case S_IFIFO:
                kind = Kind::Fifo;
                break;
            case S_IFSOCK:
                kind = Kind::Socket;
                break;
            case 0:
            case S_IFREG:
                kind = Kind::File;
                break;
            default:
                return EPERM;
        }
    }
    Node* n = makeNode(s, dir, name, kind);
    n->uid = h.uid;
    n->gid = h.gid;
    if (kind == Kind::Link) {
        n->content.own.append(target.at, target.length);
    } else {
        n->perm = mode & 07777;
    }
    return 0;
}

static int unlinkError(Server* s, Node* c, Name name, bool removeDir, const Header& h, u64& removed) {
    if (const int e = badName(name)) {
        return e;
    }
    if (c->kind != Kind::Dir) {
        return ENOTDIR;
    }
    if (!may(c, h, 1)) {
        return EACCES;
    }
    if (!s->writable) {
        return EROFS;
    }
    Entry* entry = lookup(c, name);
    if (!entry) {
        return ENOENT;
    }
    const Node* n = entry->node;
    if (removeDir && n->kind != Kind::Dir) {
        return ENOTDIR;
    }
    if (!removeDir && n->kind == Kind::Dir) {
        return EISDIR;
    }
    if (!may(c, h, 3)) {
        return EACCES;
    }
    if (keeps(c, n, h)) {
        return EPERM;
    }
    if (removeDir && n->names > 0) {
        return ENOTEMPTY;
    }
    removed = n->ino;
    release(*s, unplace(c, entry, now()));
    return 0;
}

// Whether dir is n or under it.
static bool within(const Node* n, const Node* dir) {
    for (; dir; dir = dir->parent) {
        if (dir == n) {
            return true;
        }
    }
    return false;
}

// A name of a directory moved to a name of another of the tree's:
// what the new name names goes, when it is of the old one's kind, and
// empty if a directory; a directory does not go under itself, nor a
// mount point anywhere, and one that goes to another directory must be
// one who asks may write, for its "..".
static int renameOurs(Server* s, Node* src, Name oldName, Node* dst, Name newName, const Header& h, u64 nodes[2]) {
    if (const int e = badName(oldName)) {
        return e;
    }
    if (const int e = badName(newName)) {
        return e;
    }
    if (dst->kind != Kind::Dir || gone(s, dst)) {
        return ENOENT;
    }
    if (src->kind != Kind::Dir) {
        return ENOTDIR;
    }
    if (!may(src, h, 1) || !may(dst, h, 1)) {
        return EACCES;
    }
    if (!s->writable) {
        return EROFS;
    }
    Entry* entry = lookup(src, oldName);
    if (!entry) {
        return ENOENT;
    }
    Node* n = entry->node;
    Entry* replaced = lookup(dst, newName);
    const Node* old = replaced ? replaced->node : nullptr;
    nodes[0] = n->ino;
    nodes[1] = old ? old->ino : 0;
    if (old == n) {
        return 0;
    }
    if (n->kind == Kind::Dir && within(n, dst)) {
        return EINVAL;
    }
    if (!may(src, h, 3) || !may(dst, h, 3)) {
        return EACCES;
    }
    if (keeps(src, n, h) || (old && keeps(dst, old, h))) {
        return EPERM;
    }
    if (n->kind == Kind::Dir && src != dst && !may(n, h, 2)) {
        return EACCES;
    }
    if (old && n->kind == Kind::Dir && old->kind != Kind::Dir) {
        return ENOTDIR;
    }
    if (old && n->kind != Kind::Dir && old->kind == Kind::Dir) {
        return EISDIR;
    }
    if (old && old->kind == Kind::Dir && old->names > 0) {
        return ENOTEMPTY;
    }
    const u64 t = now();
    unplace(src, entry, t);
    if (replaced) {
        release(*s, unplace(dst, replaced, t));
    }
    place(dst, newName, n, t);
    return 0;
}

// A new name, in a directory of the tree's, for a node of it, which is
// no directory.
static int linkOurs(Server* s, Node* node, Node* dst, Name newName, const Header& h) {
    if (dst->kind != Kind::Dir || gone(s, dst)) {
        return ENOENT;
    }
    if (const int e = creates(s, dst, newName, h)) {
        return e;
    }
    if (node->kind == Kind::Dir) {
        return EPERM;
    }
    place(dst, newName, node, now());
    return 0;
}

// The attributes of a node: set when the tree is writable, by its owner
// or root, a group given away by the owner only to its own, the times set
// to now by who may write it too, a size by who may write it.

// chmod, chown, utimes or truncate of a node, by name or by endpoint,
// their arguments in args: 0, or the error.
static int change(Server& s, Node* node, u32 op, const Header& h, Body args) {
    const u64 t = now();
    switch (op) {
        case fsChmod:
        case opFchmod:
            if (!owns(node, h)) {
                return EPERM;
            }
            node->perm = h.arg & 07777;
            break;
        case fsChown:
        case opFchown: {
            const u32 uid = args.take<u32>();
            const u32 gid = args.take<u32>();
            const u32 toUid = uid == noId ? node->uid : uid;
            const u32 toGid = gid == noId ? node->gid : gid;
            if (h.uid != 0 && (h.uid != node->uid || toUid != node->uid || (toGid != node->gid && toGid != h.gid))) {
                return EPERM;
            }
            if ((toUid != node->uid || toGid != node->gid) && node->kind != Kind::Dir) {
                node->perm &= ~u32(S_ISUID | S_ISGID);
            }
            node->uid = toUid;
            node->gid = toGid;
            break;
        }
        case fsUtimes:
        case opFutimes: {
            const u64 atime = args.take<u64>();
            const u64 mtime = args.take<u64>();
            const bool toNow = (h.arg & whenAtimeNow) != 0 && (h.arg & whenMtimeNow) != 0;
            if (toNow && !owns(node, h) && !may(node, h, 2)) {
                return EACCES;
            }
            if (!toNow && !owns(node, h)) {
                return EPERM;
            }
            if ((h.arg & whenAtimeKept) == 0) {
                node->atime = (h.arg & whenAtimeNow) != 0 ? t : atime;
            }
            if ((h.arg & whenMtimeKept) == 0) {
                node->mtime = (h.arg & whenMtimeNow) != 0 ? t : mtime;
            }
            break;
        }
        case fsTruncate:
        case opFtruncate: {
            const u64 size = args.take<u64>();
            if (node->kind == Kind::Dir) {
                return op == fsTruncate ? EISDIR : EINVAL;
            }
            if (node->kind != Kind::File || size > u64(INT32_MAX) * 1024) {
                return EINVAL;
            }
            if (op == fsTruncate && !may(node, h, 2)) {
                return EACCES;
            }
            if (!resize(s, node, size)) {
                return ENOSPC;
            }
            node->mtime = t;
            break;
        }
    }
    node->ctime = t;
    return 0;
}

// ---- calls on a node ---------------------------------------------------------

// Puts count bytes into a READ's memfd: false when they do not all go.
static bool put(int w, const void* from, usize count) {
    return count == 0 || pwrite(w, from, count, 0) == ssize_t(count);
}

static void readNode(Endpoint* ep, const Header& h, Body body, int fd) {
    Io io;
    if (!ioOf(body, io) || io.off > INT64_MAX) {
        return error(fd, EINVAL);
    }
    const u64 off = ioOffset(ep, io, false, 0);
    const auto* node = static_cast<const Node*>(ep->node);
    const usize n = h.arg;
    if (node->kind != Kind::Dir && !ep->reads) {
        return error(fd, EBADF);
    }
    const int w = memfd();
    if (w < 0 && node->kind != Kind::Dir) {
        return error(fd, EINVAL);
    }
    switch (node->kind) {
        case Kind::File: {
            const usize size = node->content.size();
            const usize count = off >= size ? 0 : MIN(n, size - usize(off));
            if (!put(w, node->content.data() + (count ? usize(off) : 0), count)) {
                return error(fd, EIO);
            }
            if ((io.flags & ioAt) == 0) {
                ep->pos = off + count;
            }
            return readCount(fd, count);
        }
        default:
            return error(fd, node->kind == Kind::Dir ? EISDIR : EINVAL);
    }
}

// The data at the open file's position or the offset, or at the end
// when it appends, as much as the capacity takes: the count; the position
// after it.
static void writeNode(Endpoint* ep, const Header& h, Body body, int fd) {
    Io io;
    if (!ioOf(body, io) || io.off > INT64_MAX) {
        return error(fd, EINVAL);
    }
    u64 off = io.off;
    const bool positioned = (io.flags & ioAt) == 0;
    const usize length = h.arg;
    Server& s = tree;
    auto* node = static_cast<Node*>(ep->node);
    const int w = memfd();
    if (node->kind != Kind::Dir && !ep->writes) {
        return error(fd, EBADF);
    }
    if (w < 0 && node->kind != Kind::Dir) {
        return error(fd, EINVAL);
    }
    u64 count = length;
    switch (node->kind) {
        case Kind::File: {
            if (!s.writable) {
                return error(fd, EROFS);
            }
            const u64 size = node->content.size();
            off = ioOffset(ep, io, true, size);
            const u64 grows = off + count > size ? off + count - size : 0;
            if (s.capacity > 0 && grows > s.capacity - s.used) {
                const u64 over = grows - (s.capacity - s.used);
                count = count > over ? count - over : 0;
            }
            if (count == 0 && length > 0) {
                return error(fd, ENOSPC);
            }
            if (size < off + count && !resize(s, node, off + count)) {
                return error(fd, ENOSPC);
            }
            if (count && pread(w, static_cast<u8*>(node->content.own.data()) + usize(off), usize(count), 0) != ssize_t(count)) {
                return error(fd, EIO);
            }
            touch(node);
            break;
        }
        default:
            return error(fd, node->kind == Kind::Dir ? EISDIR : EINVAL);
    }
    if (positioned) {
        ep->pos = off + count;
    }
    const u32 wrote = u32(count);
    ok(fd, &wrote, sizeof wrote);
}

// An entry of a READDIR's reply, at a position.
static bool addEntry(Assembly& a, u64& room, const Node* n, u64 pos, const char* name, usize length) {
    const usize need = direntRoom(length);
    if (need > room || !a.fits(17 + length + 1)) {
        return false;
    }
    room -= need;
    a.add(n->ino);
    a.add(pos);
    a.add(dtype(n));
    a.add(name, length);
    a.add(u8(0));
    return true;
}

// The entries after a position, in order, as many as one reply holds.
static void readdirNode(const Node* node, const Header& h, Body body, int fd) {
    if (node->kind != Kind::Dir) {
        return error(fd, ENOTDIR);
    }
    const u64 after = body.take<u64>();
    u64 room = h.arg;
    Assembly a;
    if (after < 1) {
        addEntry(a, room, node, 1, ".", 1);
    }
    if (after < 2) {
        addEntry(a, room, node->parent ? node->parent : node, 2, "..", 2);
    }
    const Slot* slots = node->order.data();
    const usize count = node->order.size();
    usize lo = 0;
    usize hi = count;
    while (lo < hi) {
        const usize mid = lo + (hi - lo) / 2;
        if (slots[mid].pos <= after) {
            lo = mid + 1;
        } else {
            hi = mid;
        }
    }
    for (usize i = lo; i < count; ++i) {
        const Entry* e = slots[i].entry;
        if (e && !addEntry(a, room, e->node, e->pos, e->name.at, e->name.length)) {
            break;
        }
    }
    ok(fd, out, a.size);
}

// ---- rootfs's calls, by node ------------------------------------------------------

// The path of a directory from the tree's root, made from its end, at
// the end of out: none for a removed one, or for what is no directory.
static void pathOf(const Node* n, int fd) {
    usize start = sizeof out;
    while (n != tree.root) {
        const Entry* entry = n->entry;
        if (n->kind != Kind::Dir || gone(&tree, n) || !entry) {
            return error(fd, ENOENT);
        }
        if (start < entry->name.length + 1) {
            return error(fd, ENAMETOOLONG);
        }
        start -= entry->name.length;
        memcpy(out + start, entry->name.at, entry->name.length);
        out[--start] = '/';
        n = n->parent;
    }
    if (start == sizeof out) {
        ok(fd, "/", 1);
    } else {
        ok(fd, out + start, sizeof out - start);
    }
}

// An open of a node, answered where its request says with a new open
// file: read or written as its access mode says and who asks may,
// emptied when O_TRUNC asks. A link is ELOOP, ENOTDIR with O_DIRECTORY; a
// directory is rootfs's to open, EISDIR here; a FIFO is its stat, the
// kernel makes its pipe. made: who asks made it now.
static void openNode(Node* n, u32 flags, bool made, const Header& h, int fd) {
    const u32 access = flags & O_ACCMODE;
    const bool reads = access != O_WRONLY;
    const bool writes = access != O_RDONLY;
    const bool truncates = (flags & O_TRUNC) != 0;
    if (n->kind == Kind::Link) {
        return error(fd, (flags & O_DIRECTORY) != 0 ? ENOTDIR : ELOOP);
    }
    if ((flags & O_DIRECTORY) != 0 && n->kind != Kind::Dir) {
        return error(fd, ENOTDIR);
    }
    if (!made && !may(n, h, (reads ? 4 : 0) | (writes || truncates ? 2 : 0))) {
        return error(fd, EACCES);
    }
    if (n->kind == Kind::Dir) {
        return error(fd, EISDIR);
    }
    const Stat st = statOf(tree, n);
    if (n->kind == Kind::Fifo) {
        ok(fd, &st, sizeof st);
        return;
    }
    if (truncates && n->content.size() > 0) {
        resize(tree, n, 0);
        touch(n);
    }
    if (openFile(fd, flags, n, tree.dev, n->ino, reads, writes, made)) {
        ++n->opens;
    }
}

// A call of rootfs's on its channel, by node, answered on fd.
static void fsCall(const Header& h, Body body, int fd) {
    if (h.op == fsRoot) {
        const Stat st = statOf(tree, tree.root);
        ok(fd, &st, sizeof st);
        return;
    }
    if (body.size() < 8) {
        return error(fd, EINVAL);
    }
    Node* n = tree.find(body.take<u64>());
    if (!n) {
        return error(fd, ENOENT);
    }
    switch (h.op) {
        case fsLookup: {
            const Name name = body.rest();
            if (n->kind != Kind::Dir) {
                return error(fd, ENOTDIR);
            }
            if (!may(n, h, 1)) {
                return error(fd, EACCES);
            }
            const Node* found = nullptr;
            if (name.is("..")) {
                found = n->parent ? n->parent : n;
            } else if (const Entry* entry = lookup(n, name)) {
                found = entry->node;
            }
            if (!found) {
                return error(fd, ENOENT);
            }
            const Stat st = statOf(tree, found);
            ok(fd, &st, sizeof st);
            return;
        }
        case fsGetattr: {
            const Stat st = statOf(tree, n);
            ok(fd, &st, sizeof st);
            return;
        }
        case fsReadlink:
            if (n->kind != Kind::Link) {
                return error(fd, EINVAL);
            }
            ok(fd, n->content.data(), n->content.size());
            return;
        case fsReaddir:
            return readdirNode(n, h, body, fd);
        case fsOpen:
            return openNode(n, h.arg, false, h, fd);
        case fsCreate: {
            const u32 mode = body.take<u32>();
            const Name name = body.rest();
            if (const Entry* entry = n->kind == Kind::Dir ? lookup(n, name) : nullptr) {
                if ((h.arg & O_EXCL) != 0) {
                    return error(fd, EEXIST);
                }
                return openNode(entry->node, h.arg, false, h, fd);
            }
            if (const int e = creates(&tree, n, name, h)) {
                return error(fd, e);
            }
            Node* made = makeNode(tree, n, name, Kind::File);
            made->perm = mode & 07777;
            made->uid = h.uid;
            made->gid = h.gid;
            return openNode(made, h.arg, true, h, fd);
        }
        case fsMkdir:
        case fsMknod:
            return answer(fd, makeOurs(tree, n, h.op == fsMkdir ? vfsMkdir : vfsMknod, h.arg, body.rest(), {}, h));
        case fsSymlink: {
            const Name target = body.string();
            return answer(fd, makeOurs(tree, n, vfsSymlink, 0, body.rest(), target, h));
        }
        case fsLink:
        case fsRename: {
            Node* to = tree.find(body.take<u64>());
            if (!to) {
                return error(fd, ENOENT);
            }
            if (h.op == fsLink) {
                return answer(fd, linkOurs(&tree, n, to, body.rest(), h));
            }
            // What moved and what it replaced, for rootfs's watchers.
            const Name old = body.string();
            u64 nodes[2] = {0, 0};
            if (const int e = renameOurs(&tree, n, old, to, body.rest(), h, nodes)) {
                return error(fd, e);
            }
            return ok(fd, nodes, sizeof nodes);
        }
        case fsUnlink: {
            u64 removed = 0;
            if (const int e = unlinkError(&tree, n, body.rest(), (h.arg & AT_REMOVEDIR) != 0, h, removed)) {
                return error(fd, e);
            }
            return ok(fd, &removed, sizeof removed);
        }
        case fsChmod:
        case fsChown:
        case fsUtimes:
        case fsTruncate:
            return answer(fd, change(tree, n, h.op, h, body));
        case fsStatfs: {
            const Statfs st = statfsOf(tree);
            ok(fd, &st, sizeof st);
            return;
        }
        case fsPath:
            return pathOf(n, fd);
        default:
            return error(fd, ENOSYS);
    }
}

// A call on an open file, answered on fd.
static void fileCall(Endpoint* ep, const Header& h, Body body, int fd) {
    auto* node = static_cast<Node*>(ep->node);
    switch (h.op) {
        case opRead:
            return readNode(ep, h, body, fd);
        case opWrite:
            return writeNode(ep, h, body, fd);
        case opReaddir:
            return error(fd, ENOTDIR);
        case opSeek: {
            u64 pos;
            if (const int e = seekTo(ep, h, body, node->kind == Kind::File ? node->content.size() : 0, pos); e != 0) {
                return error(fd, e);
            }
            return ok(fd, &pos, sizeof pos);
        }
        case opFstat: {
            const Stat st = statOf(tree, node);
            ok(fd, &st, sizeof st);
            return;
        }
        case opFstatfs: {
            const Statfs st = statfsOf(tree);
            ok(fd, &st, sizeof st);
            return;
        }
        case opFsync:
            return answer(fd, 0);
        case opFtruncate:
            if (!ep->writes) {
                return error(fd, EINVAL);
            }
            return answer(fd, change(tree, node, h.op, h, body));
        case opFchmod:
        case opFchown:
        case opFutimes:
            return answer(fd, change(tree, node, h.op, h, body));
        default:
            return error(fd, unserved(h.op));
    }
}

// An endpoint that closes: its node goes with it when no name is left
// of it.
static void closing(Endpoint* ep) {
    if (auto* n = static_cast<Node*>(ep->node)) {
        --n->opens;
        release(tree, n);
    }
}

int tmpfsMain(int argc, char** argv) {
    if (argc != 2) {
        dprintf(STDERR_FILENO, "usage: tmpfs <channel>\n");
        return 2;
    }
    const int channel = atoi(argv[1]);
    tree.writable = true;
    tree.capacity = tmpCapacity;
    tree.root = makeNode(tree, nullptr, {}, Kind::Dir);
    tree.root->perm = 01777;
    if (fcntl(channel, F_SETFD, FD_CLOEXEC) != 0) {
        fatal("tmpfs: errno %d", errno);
    }
    listen(channel, Role::Channel, nullptr, true, true);
    serve(fsCall, fileCall, closing);
    return 0;
}
