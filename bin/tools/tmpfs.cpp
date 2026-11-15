// tmpfs: /tmp, a tree in the memory of its own process, of at most
// tmpCapacity bytes, a quarter of what a memory of wasm32 holds: a file
// system of the VFS's (lib/protocol/protocol.h), which rootfs starts and
// mounts on /tmp with the channel it gives it. rootfs walks the VFS's
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
// read misses nothing. A node has the names that are links to it, a
// directory one. dev is the tree's: the number of a pair it makes as it
// makes its root, which no other pair in the kernel has. A node no name
// and no open file has is gone, and a call by its node ENOENT.
//
// A WRITE with append writes at the end the file has when the tree takes
// it, which no other write comes between. A lock is an endpoint's, an open
// file's, as Linux's open file description locks are: flock's on the
// whole file, fcntl's on a range of its bytes, refused with EAGAIN when
// another endpoint holds one in the way, and gone when the endpoint
// closes. A FIFO is a node and nothing more: the kernel makes the pipe its
// openers share. The tree holds what its capacity lets it, in bytes and in
// nodes, a node a block's worth: past that, ENOSPC.
//
// Memory. A request is received into one buffer, packet, and answered
// before the next: its names are read where they are in it. A file's bytes
// go straight to the memory of the process that reads them, or come
// straight from the memory of the one that writes them, through the memfd
// of it a READ or WRITE brings: one copy. What a reply assembles goes into
// one buffer of its own, out. The names of every directory are one
// open-addressing table, by directory and name; a directory keeps its
// entries in the order of their positions.

#include <bin/tools/tools.h>
#include <lib/protocol/protocol.h>

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdarg.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/file.h>
#include <sys/param.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <sys/uio.h>
#include <time.h>
#include <unistd.h>

using u8 = uint8_t;
using u32 = uint32_t;
using u64 = uint64_t;
using i64 = int64_t;
using usize = size_t;

// Placement new expressions are lowered directly by Clang; no runtime
// call uses this overload. Its declaration supplies the freestanding ABI.
void* operator new(size_t, void*) noexcept;

constexpr usize maxPacket = 65536;

// The servers'.
constexpr u64 blockSize = 4096;
constexpr u64 past = UINT64_MAX; // the stop of a lock to the end of a file and past it
constexpr u64 tmpCapacity = u64(1) << 30;

[[noreturn, gnu::format(printf, 1, 2)]] static void fatal(const char* format, ...) {
    va_list args;
    va_start(args, format);
    vdprintf(STDERR_FILENO, format, args);
    va_end(args);
    dprintf(STDERR_FILENO, "\n");
    exit(1);
}

static usize checkedAdd(usize a, usize b) {
    if (b > SIZE_MAX - a) {
        fatal("tmpfs: size overflow");
    }
    return a + b;
}

static usize checkedMultiply(usize a, usize b) {
    if (a != 0 && b > SIZE_MAX / a) {
        fatal("tmpfs: size overflow");
    }
    return a * b;
}

static void* allocate(usize size) {
    void* p = malloc(size);
    if (!p) {
        fatal("tmpfs: out of memory");
    }
    return p;
}

static void* reallocate(void* p, usize size) {
    void* q = realloc(p, size);
    if (!q) {
        fatal("tmpfs: out of memory");
    }
    return q;
}

// ---- containers, bin/aot/aot.cpp's over malloc ------------------------------

constexpr usize objectAlignment = 16;

// An allocation's bytes: a power of two, from objectAlignment, as
// aot.cpp's.
static usize allocationBytes(usize size) {
    usize bytes = objectAlignment;
    while (bytes < size) {
        bytes = checkedMultiply(bytes, 2);
    }
    return bytes;
}

namespace {
    struct Buffer {
        struct alignas(objectAlignment) Header {
            usize used;
            usize allocation;
        };

        constexpr Buffer();
        ~Buffer();
        Buffer(const Buffer&) = delete;
        Buffer& operator=(const Buffer&) = delete;
        void* data();
        const void* data() const;
        usize size() const;
        usize capacity() const;
        void reserve(usize size);
        void grow(usize size);
        void resize(usize size);
        void append(const void* data, usize size);
        void clear();
        void swap(Buffer& other);

        Header* header() const;
        void* storage;
    };
}

alignas(objectAlignment) constinit const Buffer::Header emptyBuffer{};

constexpr Buffer::Buffer()
    : storage(const_cast<Header*>(&emptyBuffer) + 1)
{
}

Buffer::~Buffer() {
    if (header() != &emptyBuffer) {
        free(header());
    }
}

Buffer::Header* Buffer::header() const {
    return static_cast<Header*>(storage) - 1;
}

void* Buffer::data() {
    return storage;
}

const void* Buffer::data() const {
    return storage;
}

usize Buffer::size() const {
    return header()->used;
}

usize Buffer::capacity() const {
    return header()->allocation == 0 ? 0 : header()->allocation - sizeof(Header);
}

// The capacity test stays small enough to be inlined where buffers grow
// element by element; growth itself is out of line.
inline void Buffer::reserve(usize size) {
    if (size > capacity()) {
        grow(size);
    }
}

[[gnu::noinline]] void Buffer::grow(usize size) {
    auto* before = header();
    const usize bytes = allocationBytes(checkedAdd(size, sizeof(Header)));
    Header* next;
    if (before == &emptyBuffer) {
        next = static_cast<Header*>(allocate(bytes));
        next->used = 0;
    } else {
        next = static_cast<Header*>(reallocate(before, bytes));
    }
    next->allocation = bytes;
    storage = next + 1;
}

inline void Buffer::resize(usize size) {
    reserve(size);
    if (size != 0 || header() != &emptyBuffer) {
        header()->used = size;
    }
}

void Buffer::append(const void* data, usize size) {
    const usize before = this->size();
    resize(checkedAdd(before, size));
    memcpy(static_cast<u8*>(storage) + before, data, size);
}

// Back to no bytes and no memory.
void Buffer::clear() {
    if (header() != &emptyBuffer) {
        free(header());
    }
    storage = const_cast<Header*>(&emptyBuffer) + 1;
}

void Buffer::swap(Buffer& other) {
    auto* temporary = storage;
    storage = other.storage;
    other.storage = temporary;
}

namespace {
    template <typename T>
    struct SmallVec {
        static_assert(__is_trivially_copyable(T));
        static_assert(__is_trivially_destructible(T));
        static_assert(alignof(T) <= objectAlignment);

        constexpr SmallVec() = default;
        SmallVec(const SmallVec&) = delete;
        SmallVec& operator=(const SmallVec&) = delete;

        T* data();
        const T* data() const;
        usize size() const;
        T& operator[](usize index);
        const T& operator[](usize index) const;
        void resize(usize count);
        void clear();
        void pushBack(T value);

        // Keeps the elements keep takes, in their order.
        template <typename F>
        void keepIf(F keep);

        Buffer buffer;
    };

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
inline T* SmallVec<T>::data() {
    return static_cast<T*>(buffer.data());
}

template <typename T>
inline const T* SmallVec<T>::data() const {
    return static_cast<const T*>(buffer.data());
}

template <typename T>
inline usize SmallVec<T>::size() const {
    return buffer.size() / sizeof(T);
}

template <typename T>
inline T& SmallVec<T>::operator[](usize index) {
    return data()[index];
}

template <typename T>
inline const T& SmallVec<T>::operator[](usize index) const {
    return data()[index];
}

template <typename T>
inline void SmallVec<T>::resize(usize count) {
    buffer.resize(checkedMultiply(count, sizeof(T)));
}

template <typename T>
inline void SmallVec<T>::clear() {
    buffer.resize(0);
}

template <typename T>
inline void SmallVec<T>::pushBack(T value) {
    const usize at = size();
    resize(checkedAdd(at, 1));
    data()[at] = value;
}

template <typename T>
template <typename F>
inline void SmallVec<T>::keepIf(F keep) {
    usize kept = 0;
    for (usize i = 0; i < size(); ++i) {
        if (keep(data()[i])) {
            data()[kept++] = data()[i];
        }
    }
    resize(kept);
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

// ---- names and bodies ----------------------------------------------------------

namespace {
    // Bytes of a packet, of the image or of a node, where they are.
    struct Name {
        const char* at = nullptr;
        usize length = 0;

        bool empty() const;
        bool is(const char* text) const;
        bool has(char c) const;
    };

    // A request's body, read from its start.
    struct Body {
        const u8* at;
        const u8* end;

        usize size() const;
        template <typename T>
        T take();

        // A string up to its NUL, or the end, and past the NUL.
        Name string();
        Name rest();
    };

    // ---- the tree --------------------------------------------------------------------

    struct Node;
    struct Server;
    struct Endpoint;

    enum class Kind : u8 {
        Dir,
        File,
        Link,
        Fifo,   // a named pipe: the kernel makes the pipe
        Socket, // a socket's name: it holds the socket's listener, which CONNECT gives
    };

    // What an endpoint is to the loop.
    enum class Role : u8 {
        File,    // an open file: requests come on it
        Channel, // rootfs's channel: its calls by node come on it
    };

    // A lock of fcntl: an endpoint's, on the bytes [start, stop), to read
    // or to write.
    struct Range {
        int owner;
        bool write;
        u64 start;
        u64 stop;
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
        int fd = -1;          // a socket's listener
        int exclusive = -1;   // the endpoint that holds flock's exclusive lock, or -1
        SmallVec<int> shared; // the endpoints that hold flock's shared lock
        SmallVec<Range> ranges;
        Content content; // a file's, a link's
        // a directory's
        Node* parent = nullptr; // where ".." goes; none for the root
        Entry* entry = nullptr; // its name in parent
        u64 entries = 0;        // the last position given out
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

    struct Endpoint {
        int fd;
        bool reads; // what its open lets it do
        bool writes;
        Role role;
        bool dead;       // closed, while a batch of events may still name it
        Node* node;      // what a File is open on
        Endpoint* next;  // among the free ones
        Endpoint* after; // among the live ones
        Endpoint* before;
    };
}

inline bool Name::empty() const {
    return length == 0;
}

inline bool Name::is(const char* text) const {
    return length == strlen(text) && memcmp(at, text, length) == 0;
}

inline bool Name::has(char c) const {
    return length != 0 && memchr(at, c, length) != nullptr;
}

inline usize Body::size() const {
    return usize(end - at);
}

template <typename T>
inline T Body::take() {
    if (size() < sizeof(T)) {
        fatal("a short packet");
    }
    T value;
    memcpy(&value, at, sizeof value);
    at += sizeof value;
    return value;
}

inline Name Body::string() {
    const auto* nul = static_cast<const u8*>(memchr(at, 0, size()));
    const Name name{reinterpret_cast<const char*>(at), usize((nul ? nul : end) - at)};
    at = nul ? nul + 1 : end;
    return name;
}

inline Name Body::rest() {
    const Name name{reinterpret_cast<const char*>(at), size()};
    at = end;
    return name;
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
        fatal("rootfs: an entry out of its directory's order");
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

// A device for a server's files: the number of a pair, which no other
// pair in the kernel has, as it never gives a number twice.
static u64 device() {
    int ends[2];
    struct stat st;
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, ends) != 0 || fstat(ends[0], &st) != 0) {
        fatal("rootfs: a pair for a device: errno %d", errno);
    }
    close(ends[0]);
    close(ends[1]);
    return u64(st.st_ino);
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
        if (n->fd >= 0) {
            close(n->fd);
            n->fd = -1;
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

// Why a name is none a call takes, 0 for one it does: "." and ".." and
// names with "/" are a walk's; a name past NAME_MAX is too long.
static int badName(Name name) {
    if (name.is(".") || name.is("..") || name.has('/')) {
        return EINVAL;
    }
    if (name.length > NAME_MAX) {
        return ENAMETOOLONG;
    }
    return 0;
}

// What a name of a directory is to a call that looks at it: the
// directory itself for "", else the name's node, or the root of a

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

// ---- packets ---------------------------------------------------------------------

// The packet recv takes and the descriptors that come with it, and the
// bytes a reply assembles: one of each for the process, whose loop
// takes a packet and is done with it before the next, and sends each
// reply whole.
alignas(Header) static u8 packet[maxPacket];
static int packetFds[maxFds];
static usize packetFdCount;
static u8 out[maxPacket - sizeof(Header)];

// A reply: the header, then the parts where they are.
static void reply(int fd, u32 op, u32 arg, const iovec* parts, usize count, const int* fds = nullptr, usize fdCount = 0) {
    Header head{op, arg, 0, 0};
    iovec iov[3] = {{&head, sizeof head}};
    if (count > 2 || fdCount > maxFds) {
        fatal("rootfs: a reply of %zu parts and %zu descriptors", count, fdCount);
    }
    for (usize i = 0; i < count; ++i) {
        iov[1 + i] = parts[i];
    }
    sendPacket(fd, iov, 1 + count, fds, fdCount); // a gone reader is its own loss
}

static iovec part(const void* data, usize size) {
    return {const_cast<void*>(data), size};
}

static void ok(int fd, const void* data = nullptr, usize size = 0) {
    const iovec body = part(data, size);
    reply(fd, opOk, 0, &body, size ? 1 : 0);
}

// A reply of an error: the reply's endpoint is not kept.
static bool error(int fd, int e) {
    reply(fd, opErr, u32(e), nullptr, 0);
    return false;
}

// The memfd of the memory a READ fills or a WRITE takes from: the
// first of the request's descriptors, before its reply endpoint; -1
// when it brought none.
static int window() {
    return packetFdCount >= 2 ? packetFds[0] : -1;
}

// Puts count bytes into a READ's window at at: false when they do not
// all go.
static bool put(int w, const void* from, usize count, usize at) {
    return count == 0 || pwrite(w, from, count, off_t(at)) == ssize_t(count);
}

// The reply of a READ: how many bytes went into its window.
static bool readCount(int fd, usize count) {
    const u32 n = u32(count);
    ok(fd, &n, sizeof n);
    return false;
}

// The reply of a call that changes something: OK, or the error.
static bool answer(int fd, int e) {
    if (e != 0) {
        return error(fd, e);
    }
    ok(fd);
    return false;
}

// One packet into packet, its descriptors into packetFds: the length
// of its body, after the header, or -1 at the end of the endpoint.
static i64 receive(int fd, Header& h) {
    const ssize_t n = receivePacket(fd, packet, sizeof packet, packetFds, packetFdCount);
    if (n <= 0) {
        return -1;
    }
    if (usize(n) < sizeof(Header)) {
        fatal("a packet without a header");
    }
    memcpy(&h, packet, sizeof h);
    return i64(n) - i64(sizeof(Header));
}

namespace {
    // What a reply assembles in out.
    struct Assembly {
        usize size = 0;

        bool fits(usize n) const;
        void add(const void* data, usize n);
        template <typename T>
        void add(T value);
    };
}

inline bool Assembly::fits(usize n) const {
    return n <= sizeof out - size;
}

inline void Assembly::add(const void* data, usize n) {
    memcpy(out + size, data, n);
    size += n;
}

template <typename T>
inline void Assembly::add(T value) {
    add(&value, sizeof value);
}

// ---- the calls of a tree of ours, by node --------------------------------------

// mkdir, mknod and symlink: a new name in the directory, who asks may
// write, theirs, with the mode asked for. mknod makes FIFOs and regular
// files; a device is EPERM.
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

static int unlinkError(Server* s, Node* c, Name name, bool removeDir, const Header& h) {
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
static int renameOurs(Server* s, Node* src, Name oldName, Node* dst, Name newName, const Header& h) {
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

static bool readNode(Endpoint* ep, const Header& h, Body body, int fd) {
    const u64 off = body.take<u64>();
    const u32 woff = body.take<u32>();
    const Node* node = ep->node;
    const usize n = h.arg;
    if (off > INT64_MAX) {
        return error(fd, EINVAL);
    }
    if (node->kind != Kind::Dir && !ep->reads) {
        return error(fd, EBADF);
    }
    const int w = window();
    if (w < 0 && node->kind != Kind::Dir) {
        return error(fd, EINVAL);
    }
    switch (node->kind) {
        case Kind::File: {
            const usize size = node->content.size();
            const usize count = off >= size ? 0 : MIN(n, size - usize(off));
            if (!put(w, node->content.data() + (count ? usize(off) : 0), count, woff)) {
                return error(fd, EIO);
            }
            return readCount(fd, count);
        }
        default:
            return error(fd, node->kind == Kind::Dir ? EISDIR : EINVAL);
    }
}

// The data at the offset, or at the end with append, as much as the
// capacity takes: the count and where it went.
static bool writeNode(Endpoint* ep, const Header& h, Body body, int fd) {
    u64 off = body.take<u64>();
    const usize length = body.take<u32>();
    const u32 woff = body.take<u32>();
    Server& s = tree;
    Node* node = ep->node;
    const int w = window();
    if (off > INT64_MAX) {
        return error(fd, EINVAL);
    }
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
            if ((h.arg & 1) != 0) {
                off = size;
            }
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
            if (count && pread(w, static_cast<u8*>(node->content.own.data()) + usize(off), usize(count), woff) != ssize_t(count)) {
                return error(fd, EIO);
            }
            touch(node);
            break;
        }
        default:
            return error(fd, node->kind == Kind::Dir ? EISDIR : EINVAL);
    }
    Assembly a;
    a.add(u32(count));
    a.add(off);
    ok(fd, out, a.size);
    return false;
}

// The entries after a position, in order, as many as one reply holds.
static bool readdirNode(const Node* node, Body body, int fd) {
    if (node->kind != Kind::Dir) {
        return error(fd, ENOTDIR);
    }
    const u64 after = body.take<u64>();
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
    Assembly a;
    for (usize i = lo; i < count; ++i) {
        const Entry* e = slots[i].entry;
        if (!e) {
            continue;
        }
        if (!a.fits(17 + e->name.length + 1)) {
            break;
        }
        a.add(e->node->ino);
        a.add(e->pos);
        a.add(dtype(e->node));
        a.add(e->name.at, e->name.length);
        a.add(u8(0));
    }
    ok(fd, out, a.size);
    return false;
}

// flock's lock of the endpoint's node: shared, which no other
// endpoint's exclusive one is in the way of, exclusive, which no other
// endpoint's lock is, or none.
static bool flockNode(Endpoint* ep, const Header& h, int fd) {
    Node* n = ep->node;
    const int me = ep->fd;
    const bool other = n->exclusive >= 0 && n->exclusive != me;
    usize others = 0;
    for (usize i = 0; i < n->shared.size(); ++i) {
        others += n->shared[i] != me;
    }
    auto unshare = [n, me] {
        n->shared.keepIf([me](int holder) {
            return holder != me;
        });
    };
    switch (h.arg & ~u32(LOCK_NB)) {
        case LOCK_UN:
            unshare();
            if (n->exclusive == me) {
                n->exclusive = -1;
            }
            break;
        case LOCK_SH:
            if (other) {
                return error(fd, EAGAIN);
            }
            n->exclusive = -1;
            unshare();
            n->shared.pushBack(me);
            break;
        case LOCK_EX:
            if (other || others > 0) {
                return error(fd, EAGAIN);
            }
            unshare();
            n->exclusive = me;
            break;
        default:
            return error(fd, EINVAL);
    }
    return answer(fd, 0);
}

// The ranges a LOCK keeps, made here and swapped in: one of each for
// the process.
static SmallVec<Range> keptRanges;

// fcntl's locks of the endpoint's node, on a range, to the end and past
// with a length of 0. With get, the first lock of another endpoint in
// the way of the one described, else F_UNLCK. Else that lock is set in
// place of what the endpoint held of the range, unless another
// endpoint's lock is in the way: EAGAIN; F_UNLCK lets the range go.
static bool lockNode(Endpoint* ep, const Header& h, Body body, int fd) {
    const u32 type = body.take<u32>();
    const u64 start = body.take<u64>();
    const u64 length = body.take<u64>();
    const u64 stop = length == 0 || start > past - length ? past : start + length;
    if (type > F_UNLCK) {
        return error(fd, EINVAL);
    }
    Node* n = ep->node;
    const int me = ep->fd;
    const Range* way = nullptr;
    for (usize i = 0; i < n->ranges.size() && !way; ++i) {
        const Range& l = n->ranges[i];
        if (l.owner != me && l.start < stop && start < l.stop && (l.write || type == F_WRLCK)) {
            way = &l;
        }
    }
    if (h.arg == 1) {
        Assembly a;
        if (way) {
            a.add(u32(way->write ? F_WRLCK : F_RDLCK));
            a.add(way->start);
            a.add(way->stop == past ? 0 : way->stop - way->start);
        } else {
            a.add(u32(F_UNLCK));
            a.add(start);
            a.add(length);
        }
        ok(fd, out, a.size);
        return false;
    }
    if (type != F_UNLCK && way) {
        return error(fd, EAGAIN);
    }
    keptRanges.clear();
    for (usize i = 0; i < n->ranges.size(); ++i) {
        const Range l = n->ranges[i];
        if (l.owner != me || l.stop <= start || l.start >= stop) {
            keptRanges.pushBack(l);
            continue;
        }
        if (l.start < start) {
            keptRanges.pushBack({l.owner, l.write, l.start, start});
        }
        if (l.stop > stop) {
            keptRanges.pushBack({l.owner, l.write, stop, l.stop});
        }
    }
    if (type != F_UNLCK) {
        keptRanges.pushBack({me, type == F_WRLCK, start, stop});
    }
    n->ranges.buffer.swap(keptRanges.buffer);
    return answer(fd, 0);
}

// ---- the loop's endpoints -------------------------------------------------------

static int watcher = -1;
static Endpoint* freeEndpoints;
static Endpoint* buried; // closed in this batch of events: free after it
static Endpoint* live;   // every endpoint not closed
static usize endpointCount;

// An endpoint of a role on a descriptor of ours, an open file's open on
// a node to read, to write or both.
static Endpoint* listen(int fd, Role role, Node* n, bool reads, bool writes) {
    Endpoint* ep = freeEndpoints;
    if (ep) {
        freeEndpoints = ep->next;
    } else {
        ep = static_cast<Endpoint*>(allocate(sizeof(Endpoint)));
    }
    *ep = {fd, reads, writes, role, false, n, nullptr, live, nullptr};
    if (live) {
        live->before = ep;
    }
    live = ep;
    if (n) {
        ++n->opens;
    }
    ++endpointCount;
    epoll_event ev{};
    ev.events = EPOLLIN;
    ev.data.ptr = ep;
    if (epoll_ctl(watcher, EPOLL_CTL_ADD, fd, &ev) != 0) {
        fatal("tmpfs: epoll_ctl: errno %d", errno);
    }
    return ep;
}

// An endpoint that closes: its locks go with it, and its node when no
// name is left of it.
static void forget(Endpoint* ep) {
    ep->dead = true;
    if (Node* n = ep->node) {
        const int fd = ep->fd;
        n->shared.keepIf([fd](int holder) {
            return holder != fd;
        });
        if (n->exclusive == fd) {
            n->exclusive = -1;
        }
        n->ranges.keepIf([fd](const Range& l) {
            return l.owner != fd;
        });
        --n->opens;
        release(tree, n);
    }
    if (ep->before) {
        ep->before->after = ep->after;
    } else {
        live = ep->after;
    }
    if (ep->after) {
        ep->after->before = ep->before;
    }
    ep->next = buried;
    buried = ep;
    --endpointCount;
}

static void closed(Endpoint* ep) {
    const int fd = ep->fd;
    epoll_ctl(watcher, EPOLL_CTL_DEL, fd, nullptr);
    forget(ep);
    close(fd);
}

static void bury() {
    while (Endpoint* ep = buried) {
        buried = ep->next;
        ep->next = freeEndpoints;
        freeEndpoints = ep;
    }
}

// ---- rootfs's calls, by node ------------------------------------------------------

// The path of a directory from the tree's root, made from its end, at
// the end of out: none for a removed one, or for what is no directory.
static bool pathOf(const Node* n, int fd) {
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
    return false;
}

// An open of a node, on the reply's endpoint: read or written as its
// access mode says and who asks may, emptied when O_TRUNC asks. A link
// is ELOOP, ENOTDIR with O_DIRECTORY; a directory is rootfs's to open,
// EISDIR here; a FIFO is its stat, the kernel makes its pipe. made: who
// asks made it now. Whether fd is kept.
static bool openNode(Node* n, u32 flags, bool made, const Header& h, int fd) {
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
        return false;
    }
    if (truncates && n->content.size() > 0) {
        resize(tree, n, 0);
        touch(n);
    }
    const Stat now = statOf(tree, n);
    listen(fd, Role::File, n, reads, writes);
    ok(fd, &now, sizeof now);
    return true;
}

// A call of rootfs's on its channel, by node, answered on fd: whether
// fd is kept.
static bool fsCall(const Header& h, Body body, int fd) {
    if (h.op == fsRoot) {
        const Stat st = statOf(tree, tree.root);
        ok(fd, &st, sizeof st);
        return false;
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
            return false;
        }
        case fsGetattr: {
            const Stat st = statOf(tree, n);
            ok(fd, &st, sizeof st);
            return false;
        }
        case fsReadlink:
            if (n->kind != Kind::Link) {
                return error(fd, EINVAL);
            }
            ok(fd, n->content.data(), n->content.size());
            return false;
        case fsReaddir:
            return readdirNode(n, body, fd);
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
            const Name old = body.string();
            return answer(fd, renameOurs(&tree, n, old, to, body.rest(), h));
        }
        case fsUnlink:
            return answer(fd, unlinkError(&tree, n, body.rest(), (h.arg & AT_REMOVEDIR) != 0, h));
        case fsChmod:
        case fsChown:
        case fsUtimes:
        case fsTruncate:
            return answer(fd, change(tree, n, h.op, h, body));
        case fsStatfs: {
            const Statfs st = statfsOf(tree);
            ok(fd, &st, sizeof st);
            return false;
        }
        case fsPath:
            return pathOf(n, fd);
        case fsBind: {
            // A socket's name, which holds the listener the call brings.
            if (packetFdCount != 2) {
                return error(fd, EINVAL);
            }
            const Name name = body.rest();
            if (const int e = creates(&tree, n, name, h)) {
                return error(fd, e);
            }
            Node* made = makeNode(tree, n, name, Kind::Socket);
            made->perm = h.arg & 07777;
            made->uid = h.uid;
            made->gid = h.gid;
            made->fd = packetFds[0];
            packetFds[0] = -1; // the node's now
            return answer(fd, 0);
        }
        case fsConnect:
            if (n->kind != Kind::Socket) {
                return error(fd, ECONNREFUSED);
            }
            if (!may(n, h, 2)) {
                return error(fd, EACCES);
            }
            reply(fd, opOk, 0, nullptr, 0, &n->fd, 1);
            return false;
        default:
            return error(fd, ENOSYS);
    }
}

// A call on an open file, answered on fd: whether fd is kept.
static bool fileCall(Endpoint* ep, const Header& h, Body body, int fd) {
    switch (h.op) {
        case opRead:
            return readNode(ep, h, body, fd);
        case opWrite:
            return writeNode(ep, h, body, fd);
        case opReaddir:
            return error(fd, ENOTDIR);
        case opFstat: {
            const Stat st = statOf(tree, ep->node);
            ok(fd, &st, sizeof st);
            return false;
        }
        case opFstatfs: {
            const Statfs st = statfsOf(tree);
            ok(fd, &st, sizeof st);
            return false;
        }
        case opFsync:
            return answer(fd, 0);
        case opFtruncate:
            if (!ep->writes) {
                return error(fd, EINVAL);
            }
            return answer(fd, change(tree, ep->node, h.op, h, body));
        case opFchmod:
        case opFchown:
        case opFutimes:
            return answer(fd, change(tree, ep->node, h.op, h, body));
        case opFlock:
            return flockNode(ep, h, fd);
        case opLock:
            return lockNode(ep, h, body, fd);
        default:
            return error(fd, ENOSYS);
    }
}

static void handle(Endpoint* ep) {
    if (ep->dead) {
        return;
    }
    Header h;
    const i64 n = receive(ep->fd, h);
    if (n < 0) {
        closed(ep);
        return;
    }
    const Body body{packet + sizeof(Header), packet + sizeof(Header) + n};
    if (packetFdCount == 0) {
        // A question of rootfs's, answered on its channel, in order; an
        // open keeps an endpoint, which a question has none of.
        if (ep->role == Role::Channel) {
            if (h.op == fsOpen || h.op == fsCreate) {
                error(ep->fd, EINVAL);
            } else {
                fsCall(h, body, ep->fd);
            }
        }
        return;
    }
    if (h.op == opOk || h.op == opErr) {
        for (usize i = 0; i < packetFdCount; ++i) {
            close(packetFds[i]);
        }
        return;
    }
    const int fd = packetFds[packetFdCount - 1];
    const bool kept = ep->role == Role::Channel ? fsCall(h, body, fd) : fileCall(ep, h, body, fd);
    if (!kept) {
        close(fd);
    }
    for (usize i = 0; i + 1 < packetFdCount; ++i) {
        if (packetFds[i] >= 0) {
            close(packetFds[i]);
        }
    }
}

// Serves the tree until its last endpoint closes: rootfs's channel, and
// the open files after it.
static void serve() {
    epoll_event events[64];
    while (endpointCount > 0) {
        const int n = epoll_wait(watcher, events, 64, -1);
        if (n < 0 && errno != EINTR) {
            fatal("tmpfs: epoll_wait: errno %d", errno);
        }
        for (int i = 0; i < n; ++i) {
            handle(static_cast<Endpoint*>(events[i].data.ptr));
        }
        bury();
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
    watcher = epoll_create1(EPOLL_CLOEXEC);
    if (watcher < 0 || fcntl(channel, F_SETFD, FD_CLOEXEC) != 0) {
        fatal("tmpfs: errno %d", errno);
    }
    listen(channel, Role::Channel, nullptr, true, true);
    serve();
    return 0;
}
