// rootfs: the system, the first process, and its VFS. Every call with a
// path of the system's processes comes here whole from the kernel, and
// rootfs walks it: names, "." and "..", symbolic links, mounts, the root
// of the process that asks, and the last step of the call as Linux makes
// it (lib/protocol/protocol.h is the protocol, the VFS's calls on handles,
// a file system's calls by node, an open file's). It serves the image
// linked into it, read-only, at the root, a tree of its own, from one
// loop, and the file systems of other processes are mounted on it, which
// it asks by node: /tmp and /dev, which it starts itself (bin/tools tmpfs
// and devfs), and those a MOUNT names. A walk in its own tree never waits;
// one in another's waits for that file system's answers, as a coroutine,
// while the loop goes on with other calls.
//
// It makes itself the root of everything else with what POSIX has: one
// seqpacket pair, chroot to one end in each process it starts. It starts
// tmpfs, devfs, which starts the services of the system's devices and gets
// the disks vm gave the system, and the shell on the terminal once they
// are up, each with a clean environment, reaps what ends under it, and
// ends with the shell's status, as a system does.
//
// The VFS's tree is mounts: a directory of a file system, the mount's
// root, mounted on a node of another mount; the VFS's root is on none. A
// walk goes into the root of what is mounted on a name, by ".." out of a
// mount's root to where it is mounted, and never above the root of the
// process that asks. A place is a mount and a node of its file system. A
// handle is an endpoint of rootfs's at a place, which a path walks from:
// the root, the working directory and the directory descriptors of a
// process are handles, and so is what O_PATH opens. An open file is its
// file system's endpoint: the kernel reads and writes it there, the VFS
// is out of the way, and rootfs serves only the open files of its own
// trees.
//
// The image lets who asks do what its modes let them: root everything
// but executing what nobody may, the owner what the owner's bits say, the
// group of gid the group's, everyone else the others'; a name is looked up
// in a directory who asks may search. A file system of another's checks
// what it does itself.
//
// A file system's service registers a socket at /dev/init (devfs): MOUNT
// connects to it, and the end of the connection is the file system's
// channel. The file systems and services rootfs starts get an end they
// close once they are up, and hand on to the services they start: when
// the last of them has closed it, the system is up.
//
// A directory's entries have positions, each after those before it: a
// READDIR gives the entries after one, so a reading that removes what it
// read misses nothing. A node has the names that are links to it, a
// directory one. dev is a tree's: the number of a pair it makes as it
// makes its root, which no other pair in the kernel has.
//
// A WRITE with append writes at the end the file has when the tree takes
// it, which no other write comes between. A lock is an endpoint's, an open
// file's, as Linux's open file description locks are: flock's on the
// whole file, fcntl's on a range of its bytes, refused with EAGAIN when
// another endpoint holds one in the way, and gone when the endpoint
// closes. A FIFO is a node and nothing more: the kernel makes the pipe its
// openers share. A writable tree holds what its capacity lets it, in bytes
// and in nodes, a node a block's worth: past that, ENOSPC.
//
// Memory. A request is received into one buffer, packet, and a call that
// never waits is answered before the next: its names are read where they
// are in it. A call that waits keeps its path, its own copy, in its
// coroutine, whose frames the loop keeps for the next. A file's bytes go
// straight to the memory of the process that reads them, or come straight
// from the memory of the one that writes them, through the memfd of it a
// READ or WRITE brings: one copy. What a reply assembles, the entries of a
// directory, a path, goes into one buffer of its own, out. The image's
// files and names are its own bytes, never copied. The names of every
// directory are one open-addressing table, by directory and name; a
// directory keeps its entries in the order of their positions.

#include <lib/protocol/protocol.h>

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <signal.h>
#include <stdarg.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/file.h>
#include <sys/mount.h>
#include <sys/param.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <sys/uio.h>
#include <sys/wait.h>
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

// The names of C++20's coroutines that clang asks the library for, with
// no library: a coroutine's handle over its frame, and the awaitable that
// never waits. They are core-language support, which has no other
// spelling.
namespace std {
    template <typename R, typename... Args>
    struct coroutine_traits {
        using promise_type = typename R::promise_type;
    };

    template <typename P = void>
    struct coroutine_handle;

    template <>
    struct coroutine_handle<void> {
        static coroutine_handle from_address(void* frame) noexcept;
        void resume() const;

        void* frame = nullptr;
    };

    template <typename P>
    struct coroutine_handle: public coroutine_handle<void> {
        static coroutine_handle from_address(void* frame) noexcept;
    };

    struct suspend_never {
        bool await_ready() const noexcept;
        void await_suspend(coroutine_handle<void> h) const noexcept;
        void await_resume() const noexcept;
    };
}

std::coroutine_handle<void> std::coroutine_handle<void>::from_address(void* frame) noexcept {
    coroutine_handle h;
    h.frame = frame;
    return h;
}

void std::coroutine_handle<void>::resume() const {
    __builtin_coro_resume(frame);
}

template <typename P>
std::coroutine_handle<P> std::coroutine_handle<P>::from_address(void* frame) noexcept {
    coroutine_handle h;
    h.frame = frame;
    return h;
}

bool std::suspend_never::await_ready() const noexcept {
    return true;
}

void std::suspend_never::await_suspend(coroutine_handle<void>) const noexcept {
}

void std::suspend_never::await_resume() const noexcept {
}

// The image: a ustar archive (bin/rootfs/image.py) linked in as data
// (bin/rootfs/make.py).
extern "C" const u8 rootfs_image[];
extern "C" const u32 rootfs_image_size;

namespace {
    constexpr usize maxPacket = 65536;

    // The servers'.
    constexpr u64 blockSize = 4096;
    constexpr u32 maxLinks = 40;     // the symbolic links one path goes through, Linux's
    constexpr u64 past = UINT64_MAX; // the stop of a lock to the end of a file and past it

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
            fatal("rootfs: size overflow");
        }
        return a + b;
    }

    static usize checkedMultiply(usize a, usize b) {
        if (a != 0 && b > SIZE_MAX / a) {
            fatal("rootfs: size overflow");
        }
        return a * b;
    }

    static void* allocate(usize size) {
        void* p = malloc(size);
        if (!p) {
            fatal("rootfs: out of memory");
        }
        return p;
    }

    static void* reallocate(void* p, usize size) {
        void* q = realloc(p, size);
        if (!q) {
            fatal("rootfs: out of memory");
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

    alignas(objectAlignment) constinit const Buffer::Header emptyBuffer{};

    constexpr Buffer::Buffer()
        : storage(const_cast<Header*>(&emptyBuffer) + 1)
    {
    }
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

        T* data() {
            return static_cast<T*>(buffer.data());
        }

        const T* data() const {
            return static_cast<const T*>(buffer.data());
        }

        usize size() const {
            return buffer.size() / sizeof(T);
        }

        T& operator[](usize index) {
            return data()[index];
        }

        const T& operator[](usize index) const {
            return data()[index];
        }

        void resize(usize count) {
            buffer.resize(checkedMultiply(count, sizeof(T)));
        }

        void clear() {
            buffer.resize(0);
        }

        void pushBack(T value) {
            const usize at = size();
            resize(checkedAdd(at, 1));
            data()[at] = value;
        }

        // Keeps the elements keep takes, in their order.
        template <typename F>
        void keepIf(F keep) {
            usize kept = 0;
            for (usize i = 0; i < size(); ++i) {
                if (keep(data()[i])) {
                    data()[kept++] = data()[i];
                }
            }
            resize(kept);
        }

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
        T* find(u32 hash, Same same) const {
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

        void insert(u32 hash, T* item) {
            if (!slots || (count + 1) * 2 > mask + 1) {
                grow();
            }
            put(hash, item);
            ++count;
        }

        void erase(u32 hash, const T* item) {
            u32 i = hash & mask;
            while (slots[i].item != item) {
                if (!slots[i].item) {
                    fatal("rootfs: no such item in a table");
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

        void put(u32 hash, T* item) {
            u32 i = hash & mask;
            while (slots[i].item) {
                i = (i + 1) & mask;
            }
            slots[i] = {hash, item};
        }

        void grow() {
            Slot* old = slots;
            const usize before = old ? usize(mask) + 1 : 0;
            const usize capacity = old ? checkedMultiply(before, 2) : 16;
            slots = static_cast<Slot*>(calloc(capacity, sizeof(Slot)));
            if (!slots) {
                fatal("rootfs: out of memory");
            }
            mask = u32(capacity - 1);
            for (usize i = 0; i < before; ++i) {
                if (old[i].item) {
                    put(old[i].hash, old[i].item);
                }
            }
            free(old);
        }

        Slot* slots = nullptr;
        u32 mask = 0;
        u32 count = 0;
    };

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

    // Bytes of a packet, of the image or of a node, where they are.
    struct Name {
        const char* at = nullptr;
        usize length = 0;

        bool empty() const {
            return length == 0;
        }

        bool is(const char* text) const {
            return length == strlen(text) && memcmp(at, text, length) == 0;
        }

        bool has(char c) const {
            return length != 0 && memchr(at, c, length) != nullptr;
        }
    };

    static Name literal(const char* text) {
        return {text, strlen(text)};
    }

    // A request's body, read from its start.
    struct Body {
        const u8* at;
        const u8* end;

        usize size() const {
            return usize(end - at);
        }

        template <typename T>
        T take() {
            if (size() < sizeof(T)) {
                fatal("a short packet");
            }
            T value;
            memcpy(&value, at, sizeof value);
            at += sizeof value;
            return value;
        }

        // A string up to its NUL, or the end, and past the NUL.
        Name string() {
            const auto* nul = static_cast<const u8*>(memchr(at, 0, size()));
            const Name name{reinterpret_cast<const char*>(at), usize((nul ? nul : end) - at)};
            at = nul ? nul + 1 : end;
            return name;
        }

        Name rest() {
            const Name name{reinterpret_cast<const char*>(at), size()};
            at = end;
            return name;
        }
    };

    // ---- the tree --------------------------------------------------------------------

    struct Node;
    struct Server;
    struct Endpoint;
    struct Fs;
    struct Mount;
    struct Ask;

    enum class Kind : u8 {
        Dir,
        File,
        Link,
        Fifo, // a named pipe: the kernel makes the pipe
    };

    // What an endpoint is to the loop.
    enum class Role : u8 {
        File,    // an open file of a tree of ours: requests come on it
        Handle,  // a handle of the VFS's, at a place: calls with a path walk from there
        Fs,      // ours of the channel of another's file system: only its close comes
        Pending, // ours of a call to a file system: its answer goes on with the call
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

        const u8* data() const {
            return view ? view : static_cast<const u8*>(own.data());
        }

        usize size() const {
            return view ? viewSize : own.size();
        }

        Name name() const {
            return {reinterpret_cast<const char*>(data()), size()};
        }
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
        int exclusive = -1;   // the endpoint that holds flock's exclusive lock, or -1
        int fd = -1;          // an object's; a service's end of its pair
        u32 covered = 0;      // the mounts on it
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

    // A file system of the VFS's: a tree of ours, or another process's
    // behind the channel rootfs connected to it by.
    struct Fs {
        Server* tree = nullptr;      // ours
        Endpoint* channel = nullptr; // ours of another's channel, none once it is gone
        u64 dev = 0;
        u32 mounts = 0; // it goes with its last
    };

    // A mount: a directory of a file system, its root, as a directory of
    // the VFS's tree on a node of another mount, the point; the VFS's own
    // root is on none. The stats of the root and the point are as they
    // were when it was mounted, of a file system of another's.
    struct Mount {
        Fs* fs = nullptr;
        u64 root = 0;
        Node* rootNode = nullptr; // the root, of a tree of ours
        Stat rootStat{};
        Mount* parent = nullptr;
        u64 point = 0;
        Node* pointNode = nullptr; // the point, of a tree of ours
        Stat pointStat{};
        u64 pointDir = 0; // the directory of the point, and its name there
        u8 pointLength = 0;
        char pointName[NAME_MAX];
        u32 refs = 1;          // the mount itself, and the handles and calls at it
        bool gone = false;     // unmounted: no walk goes into it
        Mount* next = nullptr; // among the mounts
    };

    // A place of the VFS's tree: a mount and a node of its file system;
    // the node itself in a tree of ours, else its stat as the file system
    // gave it, fresh when it came in an answer to the call.
    struct Place {
        Mount* m = nullptr;
        u64 node = 0;
        Node* n = nullptr;
        Stat st{};
        bool fresh = false;
    };

    struct Endpoint {
        int fd;
        bool reads; // what its open lets it do
        bool writes;
        Role role;
        bool dead;       // closed, while a batch of events may still name it
        Server* server;  // a File's tree
        Node* node;      // what a File or a Handle is open on, of a tree of ours
        Place at;        // a Handle's
        u64 id;          // a Handle's
        Fs* fs;          // a Fs's
        Ask* ask;        // a Pending's
        Endpoint* next;  // among the free ones
        Endpoint* after; // among the live ones
        Endpoint* before;
    };

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
    OpenTable<Entry> names;

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
    alignas(Header) u8 packet[maxPacket];
    int packetFds[maxFds];
    usize packetFdCount;
    u8 out[maxPacket - sizeof(Header)];

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

    // What a reply assembles in out.
    struct Assembly {
        usize size = 0;

        bool fits(usize n) const {
            return n <= sizeof out - size;
        }

        void add(const void* data, usize n) {
            memcpy(out + size, data, n);
            size += n;
        }

        template <typename T>
        void add(T value) {
            add(&value, sizeof value);
        }
    };

    // ---- the loop's endpoints -------------------------------------------------------
    //
    // One epoll of the process watches every endpoint: an endpoint joins it
    // when it is listened on, leaves it before it closes. An event carries
    // its endpoint.

    int watcher = -1;
    Endpoint* freeEndpoints;
    Endpoint* buried; // closed in this batch of events: free after it
    Endpoint* live;   // every endpoint not closed
    usize endpointCount;

    // The signal mask of the wait, once reapOnChild blocked SIGCHLD
    // everywhere else: a child's end that comes while the loop works waits
    // for the wait, which it interrupts at once, instead of coming before
    // it, unseen. The handler notes it: the loop reaps after a wait a
    // child's end interrupted.
    sigset_t waitMask;
    bool waitMasked;
    volatile sig_atomic_t childEnded = 1;

    // The handles, by id; the ids given out.
    OpenTable<Endpoint> handles;
    u64 handleIds;

    static u32 hashId(u64 id) {
        return finish(id);
    }

    static Endpoint* handleOf(u64 id) {
        return handles.find(hashId(id), [id](const Endpoint* ep) {
            return ep->id == id;
        });
    }

    // An endpoint of a role on a descriptor of ours, which came with
    // MSG_CMSG_CLOEXEC or from a pair of ours made with SOCK_CLOEXEC; the
    // caller fills in what its role has.
    static Endpoint* listen(int fd, Role role) {
        Endpoint* ep = freeEndpoints;
        if (ep) {
            freeEndpoints = ep->next;
        } else {
            ep = static_cast<Endpoint*>(allocate(sizeof(Endpoint)));
        }
        new (ep) Endpoint();
        ep->fd = fd;
        ep->role = role;
        ep->after = live;
        if (live) {
            live->before = ep;
        }
        live = ep;
        ++endpointCount;
        epoll_event ev{};
        ev.events = EPOLLIN;
        ev.data.ptr = ep;
        if (epoll_ctl(watcher, EPOLL_CTL_ADD, fd, &ev) != 0) {
            fatal("rootfs: epoll_ctl: errno %d", errno);
        }
        return ep;
    }

    // An open file of a tree of ours, to read, to write or both.
    static Endpoint* listenFile(Server& s, int fd, Node* n, bool reads, bool writes) {
        Endpoint* ep = listen(fd, Role::File);
        ep->server = &s;
        ep->node = n;
        ep->reads = reads;
        ep->writes = writes;
        ++n->opens;
        return ep;
    }

    static void hold(const Place& p);
    static void letGo(Mount* m);

    // A handle at a place, its id the next; O_PATH's reads nothing and
    // changes nothing.
    static Endpoint* listenHandle(int fd, const Place& p, bool opath) {
        Endpoint* ep = listen(fd, Role::Handle);
        ep->at = p;
        ep->node = p.n;
        ep->reads = !opath;
        ep->writes = !opath;
        ep->id = ++handleIds;
        ++p.m->refs;
        if (p.n) {
            ++p.n->opens;
        }
        handles.insert(hashId(ep->id), ep);
        return ep;
    }

    static void forget(Endpoint* ep);

    // The other end of an endpoint closed, or it is let go of: the file is
    // closed.
    static void closed(Endpoint* ep) {
        const int fd = ep->fd;
        epoll_ctl(watcher, EPOLL_CTL_DEL, fd, nullptr);
        forget(ep);
        close(fd);
    }

    // The endpoints closed in a batch of events are free once it is done.
    static void bury() {
        while (Endpoint* ep = buried) {
            buried = ep->next;
            ep->~Endpoint();
            ep->next = freeEndpoints;
            freeEndpoints = ep;
        }
    }

    // The locks an endpoint holds on a node go with it.
    static void unlock(Node* n, int fd) {
        n->shared.keepIf([fd](int holder) {
            return holder != fd;
        });
        if (n->exclusive == fd) {
            n->exclusive = -1;
        }
        n->ranges.keepIf([fd](const Range& l) {
            return l.owner != fd;
        });
    }

    static void gone(Fs* fs);

    // An endpoint that closes: its locks go with it, and its node when no
    // name is left of it.
    static void forget(Endpoint* ep) {
        ep->dead = true;
        switch (ep->role) {
            case Role::File:
                unlock(ep->node, ep->fd);
                --ep->node->opens;
                release(*ep->server, ep->node);
                break;
            case Role::Handle:
                handles.erase(hashId(ep->id), ep);
                if (Node* n = ep->at.n) {
                    unlock(n, ep->fd);
                    --n->opens;
                    release(*ep->at.m->fs->tree, n);
                }
                letGo(ep->at.m);
                break;
            case Role::Fs:
                gone(ep->fs);
                break;
            case Role::Pending:
                break;
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

    // ---- places and mounts ---------------------------------------------------------

    Mount* mounts; // every mount, the VFS's root among them
    Mount* rootMount;

    // Whether a place is in a tree of ours: its node is there.
    static bool ours(const Place& p) {
        return p.m->fs->tree != nullptr;
    }

    static u32 modeAt(const Place& p) {
        return ours(p) ? modeOf(p.n) : p.st.mode;
    }

    static bool isDir(const Place& p) {
        return S_ISDIR(modeAt(p));
    }

    static bool isLink(const Place& p) {
        return S_ISLNK(modeAt(p));
    }

    static bool same(const Place& a, const Place& b) {
        return a.m == b.m && a.node == b.node;
    }

    // Whether who asks may read (4), write (2), and search or execute (1)
    // what a stat says, as may says of a node.
    static bool mayStat(const Stat& st, const Header& h, u32 want) {
        if (h.uid == 0) {
            return (want & 1) == 0 || S_ISDIR(st.mode) || (st.mode & 0111) != 0;
        }
        const u32 bits = h.uid == st.uid ? st.mode >> 6 : h.gid == st.gid ? st.mode >> 3 : st.mode;
        return (bits & want) == want;
    }

    static bool mayAt(const Place& p, const Header& h, u32 want) {
        return ours(p) ? may(p.n, h, want) : mayStat(p.st, h, want);
    }

    static Place ourPlace(Mount* m, Node* n) {
        Place p;
        p.m = m;
        p.node = n->ino;
        p.n = n;
        return p;
    }

    // A place of another's file system, as a stat of an answer says.
    static Place theirPlace(Mount* m, const Stat& st) {
        Place p;
        p.m = m;
        p.node = st.ino;
        p.st = st;
        p.fresh = true;
        return p;
    }

    static Place rootOf(Mount* m) {
        if (m->rootNode) {
            return ourPlace(m, m->rootNode);
        }
        Place p;
        p.m = m;
        p.node = m->root;
        p.st = m->rootStat;
        return p;
    }

    // Where a mount is mounted: its point, in its parent.
    static Place pointOf(Mount* m) {
        if (m->pointNode) {
            return ourPlace(m->parent, m->pointNode);
        }
        Place p;
        p.m = m->parent;
        p.node = m->point;
        p.st = m->pointStat;
        return p;
    }

    static bool isMountRoot(const Place& p) {
        return p.node == p.m->root;
    }

    // What is mounted on a place, the last mounted on it, or none.
    static Mount* mountedOn(const Place& p) {
        if (p.n && p.n->covered == 0) {
            return nullptr;
        }
        Mount* found = nullptr;
        for (Mount* m = mounts; m; m = m->next) {
            if (!m->gone && m->parent == p.m && m->point == p.node) {
                found = m;
            }
        }
        return found;
    }

    // What a name's place is to a walk: the root of what is mounted on it.
    static Place down(Place p) {
        while (Mount* m = mountedOn(p)) {
            p = rootOf(m);
        }
        return p;
    }

    // Whether a name of a directory of a mount's is a mount point: a call
    // on the name, which would take it away, is EBUSY.
    static bool pointNamed(const Place& dir, Name name) {
        for (Mount* m = mounts; m; m = m->next) {
            if (!m->gone && m->parent == dir.m && m->pointDir == dir.node && m->pointLength == name.length && memcmp(m->pointName, name.at, name.length) == 0) {
                return true;
            }
        }
        return false;
    }

    // A call or a handle at a place keeps its mount, and its node in a
    // tree of ours.
    static void hold(const Place& p) {
        ++p.m->refs;
        if (p.n) {
            ++p.n->holds;
        }
    }

    static void unhold(const Place& p) {
        if (p.n) {
            drop(p.n);
        }
        letGo(p.m);
    }

    // A place of a call's taken for another one: the new one held first.
    static void moveTo(Place& to, const Place& from) {
        hold(from);
        if (to.m) {
            unhold(to);
        }
        to = from;
    }

    // A mount nothing is at any more, unmounted: its memory goes, and its
    // file system's with its last mount, which closes the channel of
    // another's.
    static void letGo(Mount* m) {
        if (--m->refs != 0) {
            return;
        }
        Fs* fs = m->fs;
        free(m);
        if (--fs->mounts == 0 && !fs->tree) {
            if (fs->channel) {
                closed(fs->channel);
            }
            free(fs);
        }
    }

    // Takes a mount, and what is mounted under it, out of the VFS's tree:
    // no walk goes into it; what is at it stays until it lets go.
    static void unmount(Mount* m) {
        for (Mount* sub = mounts; sub;) {
            if (!sub->gone && sub->parent == m) {
                unmount(sub);
                sub = mounts;
                continue;
            }
            sub = sub->next;
        }
        m->gone = true;
        for (Mount** at = &mounts; *at; at = &(*at)->next) {
            if (*at == m) {
                *at = m->next;
                break;
            }
        }
        if (Node* point = m->pointNode) {
            --point->covered;
            drop(point);
        }
        letGo(m->parent);
        letGo(m);
    }

    // Mounts a directory of a file system, its root, on a place of the
    // VFS's: the name of the place in its directory is dirName.
    static void mountAt(Fs* fs, const Place& root, const Place& point, u64 pointDir, Name pointName) {
        auto* m = new (allocate(sizeof(Mount))) Mount();
        m->fs = fs;
        ++fs->mounts;
        m->root = root.node;
        m->rootNode = root.n;
        m->rootStat = root.st;
        m->parent = point.m;
        ++point.m->refs;
        m->point = point.node;
        m->pointNode = point.n;
        m->pointStat = point.st;
        m->pointDir = pointDir;
        m->pointLength = u8(pointName.length);
        memcpy(m->pointName, pointName.at, pointName.length);
        if (Node* n = point.n) {
            ++n->covered;
            ++n->holds;
        }
        m->next = mounts;
        mounts = m;
    }

    // Another's file system is gone, its channel closed: it is unmounted
    // wherever it is mounted.
    static void gone(Fs* fs) {
        fs->channel = nullptr;
        for (Mount* m = mounts; m;) {
            Mount* next = m->next;
            if (m->fs == fs && !m->gone) {
                unmount(m);
                next = mounts;
            }
            m = next;
        }
    }

    // ---- coroutines ------------------------------------------------------------------

    // A call of the VFS's that may wait for another's file system: a
    // coroutine, which runs at once, until it waits, and goes on when the
    // answer comes. Its frame is kept for the next call of the same size.
    struct Task {
        struct promise_type {
            Task get_return_object() noexcept;
            std::suspend_never initial_suspend() noexcept;
            std::suspend_never final_suspend() noexcept;
            void return_void() noexcept;
            void unhandled_exception() noexcept;
            static void* operator new(usize size);
            static void operator delete(void* frame, usize size);
        };
    };

    // The frames of the coroutines that ended, by size: few sizes, one of
    // each kind of call.
    struct Frames {
        usize size;
        void* free;
    };

    Frames frames[32];

    // What a file system answered: OK with its body, which is the packet's
    // until the call waits again, or the errno. A connection's channel
    // comes with it, its endpoint ours from then on.
    struct Answer {
        int error = 0;
        Body body{nullptr, nullptr};
        Endpoint* channel = nullptr;
    };

    // A question of a call's to a file system, which the call waits for:
    // the request goes on to with an endpoint of ours, whose answer goes on
    // with the call (Role::Pending). connects: the endpoint is the channel
    // of a mount, kept when the answer is OK.
    struct Ask {
        bool await_ready() const noexcept;
        bool await_suspend(std::coroutine_handle<void> h);
        Answer await_resume() const noexcept;

        int to = -1;
        Header head{};
        u8 body[8 + NAME_MAX];
        usize size = 0;
        bool connects = false;
        Answer answer;
        std::coroutine_handle<void> call;
    };

    // A question by node of a place's file system, another's, with a name.
    static Ask asking(const Place& p, const Header& h, u32 op, Name name) {
        Ask a;
        a.to = p.m->fs->channel ? p.m->fs->channel->fd : -1;
        a.head = {op, 0, h.uid, h.gid};
        memcpy(a.body, &p.node, 8);
        memcpy(a.body + 8, name.at, MIN(name.length, usize(NAME_MAX)));
        a.size = 8 + MIN(name.length, usize(NAME_MAX));
        return a;
    }

    // A call that never waits takes no frame of its own but its first.
    static void* frameOf(usize size) {
        for (Frames& f : frames) {
            if (f.size == size && f.free) {
                void* p = f.free;
                memcpy(&f.free, p, sizeof(void*));
                return p;
            }
        }
        return allocate(MAX(size, sizeof(void*)));
    }

    static void keepFrame(void* p, usize size) {
        for (Frames& f : frames) {
            if (f.size == size || f.size == 0) {
                f.size = size;
                memcpy(p, &f.free, sizeof(void*));
                f.free = p;
                return;
            }
        }
        free(p);
    }

    static bool statIn(Body b, Stat& st) {
        if (b.size() < sizeof st) {
            return false;
        }
        memcpy(&st, b.at, sizeof st);
        return true;
    }
}

Task Task::promise_type::get_return_object() noexcept {
    return {};
}

std::suspend_never Task::promise_type::initial_suspend() noexcept {
    return {};
}

std::suspend_never Task::promise_type::final_suspend() noexcept {
    return {};
}

void Task::promise_type::return_void() noexcept {
}

void Task::promise_type::unhandled_exception() noexcept {
    fatal("rootfs: an exception in a call");
}

void* Task::promise_type::operator new(usize size) {
    return frameOf(size);
}

void Task::promise_type::operator delete(void* frame, usize size) {
    keepFrame(frame, size);
}

bool Ask::await_ready() const noexcept {
    return false;
}

// The question goes out, or it cannot: then the call goes on at once with
// the error, EIO for a file system that is gone.
bool Ask::await_suspend(std::coroutine_handle<void> h) {
    int ends[2];
    if (to < 0) {
        answer.error = EIO;
        return false;
    }
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, ends) != 0) {
        answer.error = errno;
        return false;
    }
    const iovec parts[] = {part(&head, sizeof head), part(body, size)};
    if (sendPacket(to, parts, 2, &ends[1], 1, MSG_DONTWAIT) < 0) {
        answer.error = errno == EAGAIN ? EAGAIN : EIO;
        close(ends[0]);
        close(ends[1]);
        return false;
    }
    close(ends[1]);
    call = h;
    listen(ends[0], Role::Pending)->ask = this;
    return true;
}

Answer Ask::await_resume() const noexcept {
    return answer;
}

namespace {
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
        if (removeDir && n->covered > 0) {
            return EBUSY;
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
        if (n->covered > 0 || (old && old->covered > 0)) {
            return EBUSY;
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
        Server& s = *ep->server;
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
    SmallVec<Range> keptRanges;

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

    // ---- opens and calls handed on ---------------------------------------------------

    // The reply of a handle's open: its stat and its id.
    static void handleReply(int fd, const Stat& st, u64 id) {
        u8 b[sizeof st + 8];
        memcpy(b, &st, sizeof st);
        memcpy(b + sizeof st, &id, 8);
        ok(fd, b, sizeof b);
    }

    // A call handed on to another's file system, with the kernel's
    // endpoint, which the file system answers on: the node of a place,
    // then what follows it in the call. EIO when the file system is gone,
    // EAGAIN when it does not keep up.
    static bool hand(const Place& p, const Header& h, u32 op, u32 arg, const void* args, usize size, Name name, int fd) {
        const Header head{op, arg, h.uid, h.gid};
        const iovec parts[] = {part(&head, sizeof head), part(&p.node, 8), part(args, size), part(name.at, name.length)};
        Endpoint* channel = p.m->fs->channel;
        if (!channel || sendPacket(channel->fd, parts, 4, &fd, 1, MSG_DONTWAIT) < 0) {
            return error(fd, channel && errno == EAGAIN ? EAGAIN : EIO);
        }
        return false;
    }

    // An open of a node of a tree of ours, on the reply's endpoint: read or
    // written as its access mode says and who asks may, emptied when
    // O_TRUNC asks, with O_PATH only held. A directory, and what O_PATH
    // opens, is a handle; a FIFO is its stat, the kernel makes its pipe; a
    // link is ELOOP but with O_PATH and O_NOFOLLOW, ENOTDIR with
    // O_DIRECTORY. made: who asks made it now. Whether fd is kept.
    static bool openOurs(const Place& at, int flags, bool made, const Header& h, int fd) {
        Node* n = at.n;
        Server& s = *at.m->fs->tree;
        const bool opath = (flags & O_PATH) != 0;
        const u32 access = flags & O_ACCMODE;
        const bool reads = !opath && access != O_WRONLY;
        const bool writes = !opath && access != O_RDONLY;
        const bool truncates = !opath && (flags & O_TRUNC) != 0;
        if (n->kind == Kind::Link && !(opath && (flags & O_NOFOLLOW) != 0)) {
            return error(fd, (flags & O_DIRECTORY) != 0 ? ENOTDIR : ELOOP);
        }
        if ((flags & O_DIRECTORY) != 0 && n->kind != Kind::Dir) {
            return error(fd, ENOTDIR);
        }
        if (!made && !opath) {
            if (!may(n, h, (reads ? 4 : 0) | (writes || truncates ? 2 : 0))) {
                return error(fd, EACCES);
            }
            if (n->kind == Kind::Dir && (writes || (flags & O_CREAT) != 0)) {
                return error(fd, EISDIR);
            }
            if ((writes || truncates) && n->kind == Kind::File && !s.writable) {
                return error(fd, EROFS);
            }
        }
        if (!opath && n->kind == Kind::Fifo) {
            const Stat st = statOf(s, n);
            ok(fd, &st, sizeof st);
            return false;
        }
        if (truncates && n->kind == Kind::File && n->content.size() > 0) {
            resize(s, n, 0);
            touch(n);
        }
        const Stat st = statOf(s, n);
        if (opath || n->kind == Kind::Dir) {
            handleReply(fd, st, listenHandle(fd, at, opath)->id);
            return true;
        }
        listenFile(s, fd, n, reads, writes);
        ok(fd, &st, sizeof st);
        return true;
    }

    // An open of a place of another's file system: a directory, and what
    // O_PATH opens, is a handle of ours, who asks may read it; a socket
    // opens nothing, ENXIO; else the file system opens it on the kernel's
    // endpoint.
    static bool openTheirs(const Place& at, int flags, const Header& h, int fd) {
        const bool opath = (flags & O_PATH) != 0;
        const u32 access = flags & O_ACCMODE;
        if (!opath && S_ISSOCK(at.st.mode)) {
            return error(fd, ENXIO);
        }
        if ((flags & O_DIRECTORY) != 0 && !isDir(at)) {
            return error(fd, ENOTDIR);
        }
        if (!opath && isDir(at)) {
            if (access != O_WRONLY && !mayStat(at.st, h, 4)) {
                return error(fd, EACCES);
            }
            if (access != O_RDONLY || (flags & O_CREAT) != 0) {
                return error(fd, EISDIR);
            }
        }
        if (opath || isDir(at)) {
            handleReply(fd, at.st, listenHandle(fd, at, opath)->id);
            return true;
        }
        return hand(at, h, fsOpen, u32(flags), nullptr, 0, {}, fd);
    }

    static bool openAt(const Place& at, int flags, const Header& h, int fd) {
        return ours(at) ? openOurs(at, flags, false, h, fd) : openTheirs(at, flags, h, fd);
    }

    // An open that makes the name in a directory, a file of the mode asked
    // for, who asks's.
    static bool createAt(const Place& dir, Name name, int flags, u32 mode, const Header& h, int fd) {
        if (!ours(dir)) {
            return hand(dir, h, fsCreate, u32(flags), &mode, sizeof mode, name, fd);
        }
        Server& s = *dir.m->fs->tree;
        if (const int e = creates(&s, dir.n, name, h)) {
            return error(fd, e);
        }
        Node* n = makeNode(s, dir.n, name, Kind::File);
        n->perm = mode & 07777;
        n->uid = h.uid;
        n->gid = h.gid;
        return openOurs(ourPlace(dir.m, n), flags, true, h, fd);
    }

    // chmod, chown, utimes and truncate of a place, its file system's call
    // op with what goes with it, args: by a tree of ours, which writes
    // nothing on a read-only one, or handed on.
    static bool changeAt(const Place& at, u32 op, const Header& h, const void* args, usize size, int fd) {
        if (!ours(at)) {
            return hand(at, h, op, h.arg, args, size, {}, fd);
        }
        Server& s = *at.m->fs->tree;
        if (!s.writable) {
            return error(fd, EROFS);
        }
        const u8* bytes = static_cast<const u8*>(args);
        return answer(fd, change(s, at.n, op, h, Body{bytes, bytes + size}));
    }

    // ---- the walk ---------------------------------------------------------------------

    // The kinds of a path's last name.
    constexpr u32 lastName = 0;
    constexpr u32 lastDot = 1;
    constexpr u32 lastDotdot = 2;
    constexpr u32 lastNone = 3; // the path is "/" alone

    // The longest a path gets with the targets of the links it goes
    // through in it.
    constexpr usize maxWalk = 65536;

    // A walk of a path as Linux walks one, to the directory of its last
    // name, here: every name but the last a directory who asks may search,
    // or a symbolic link, whose target takes its place, from the root of
    // the process when absolute, no more than maxLinks of them in all; "."
    // and ".."; the root of what is mounted on a name for the name. With
    // look, the last name is looked at too, target, a link there followed
    // when follow says or a slash ends the path, which then wants a
    // directory; with missing, a last name that is not there is no error:
    // found says. The target of ".", ".." and the root alone is the
    // directory itself. A path of "." alone names where it starts, which
    // nobody is asked about. A step another's file system answers is a
    // question (Step::Ask), which the call asks, and the walk goes on with
    // the answer.
    struct Walk {
        enum class Step : u8 {
            Done,
            Ask,
            Failed,
            Go,
        };

        enum class Wait : u8 {
            None,
            Child,      // a name of here, which the walk goes through
            Parent,     // here's parent
            Target,     // the target of a link the walk goes through
            Last,       // the last name
            LastTarget, // the target of a last link
        };

        Walk() = default;
        Walk(const Walk&) = delete;
        Walk& operator=(const Walk&) = delete;
        ~Walk();
        void start(const Header& head, const Place& from, const Place& top, Name path, bool looks, bool follows, bool misses);
        Step run();
        Step answered(const Answer& a);
        Ask question() const;
        Step fail(int e);
        Step up();
        Step settle(Place next);
        Step finish();
        Step settleLast(Place t);
        bool take(Name& name);
        bool more() const;
        bool onlyDots() const;
        bool follow(Name to, bool lastOne);
        void ask(u32 call, const Place& at, Name name, Wait w);

        Header h{};
        Place root;
        Place here;
        Place target;
        bool look = false;
        bool followLast = false;
        bool missing = false;
        bool found = false;
        Buffer path; // the names left, from at on
        usize at = 0;
        bool slash = false;
        bool fresh = false;  // the walk starts on its path: "." alone asks nobody
        bool ending = false; // the ".." asked is the last name
        u32 links = 0;
        Name last; // the last name, in path while the walk is done
        u32 kind = lastNone;
        int error = 0;
        Wait wait = Wait::None;
        u32 op = 0; // the question: its call, whom, the name
        Place asked;
        Name name;
    };

    // The place of a handle a call names by its id: the root of the
    // process, or where a second path starts.
    static bool placeOf(u64 id, Place& out) {
        const Endpoint* ep = handleOf(id);
        if (!ep) {
            return false;
        }
        out = ep->at;
        return true;
    }
}

Walk::~Walk() {
    Place* held[] = {&root, &here, &target};
    for (Place* p : held) {
        if (p->m) {
            unhold(*p);
        }
    }
}

void Walk::start(const Header& head, const Place& from, const Place& top, Name p, bool looks, bool follows, bool misses) {
    h = head;
    look = looks;
    followLast = follows;
    missing = misses;
    moveTo(root, top);
    moveTo(here, p.length != 0 && p.at[0] == '/' ? top : from);
    path.clear();
    path.append(p.at, p.length);
    at = 0;
    slash = p.length != 0 && p.at[p.length - 1] == '/';
    fresh = true;
    links = 0;
}

Walk::Step Walk::fail(int e) {
    error = e;
    return Step::Failed;
}

// The next name, or false at the end.
bool Walk::take(Name& out) {
    const char* text = static_cast<const char*>(path.data());
    const usize size = path.size();
    while (at < size && text[at] == '/') {
        ++at;
    }
    if (at == size) {
        return false;
    }
    const auto* slashAt = static_cast<const char*>(memchr(text + at, '/', size - at));
    const usize end = slashAt ? usize(slashAt - text) : size;
    out = {text + at, end - at};
    at = end;
    return true;
}

// Whether a name is left.
bool Walk::more() const {
    const char* text = static_cast<const char*>(path.data());
    for (usize i = at; i < path.size(); ++i) {
        if (text[i] != '/') {
            return true;
        }
    }
    return false;
}

// Whether the names left are "." alone, or none.
bool Walk::onlyDots() const {
    const char* text = static_cast<const char*>(path.data());
    for (usize i = at; i < path.size(); ++i) {
        const bool dot = text[i] == '.' && (i == 0 || text[i - 1] == '/') && (i + 1 == path.size() || text[i + 1] == '/');
        if (text[i] != '/' && !dot) {
            return false;
        }
    }
    return true;
}

// The target of a link takes the link's place, the names left after it,
// or a slash that ended the path after the last's, and a slash that ends
// the target ends the path then; from the root when it is absolute.
bool Walk::follow(Name to, bool lastOne) {
    Buffer next;
    const char* text = static_cast<const char*>(path.data());
    const usize left = path.size() - at;
    if (to.length + 1 + left > maxWalk) {
        return false;
    }
    next.append(to.at, to.length);
    if (lastOne) {
        if (slash) {
            next.append("/", 1);
        }
    } else {
        next.append("/", 1);
        next.append(text + at, left);
    }
    path.swap(next);
    at = 0;
    if (lastOne) {
        slash = path.size() != 0 && static_cast<const char*>(path.data())[path.size() - 1] == '/';
    }
    if (to.at[0] == '/') {
        moveTo(here, root);
    }
    fresh = lastOne;
    return true;
}

void Walk::ask(u32 call, const Place& p, Name n, Wait w) {
    op = call;
    asked = p;
    name = n;
    wait = w;
}

Ask Walk::question() const {
    return asking(asked, h, op, name);
}

// ".." from here: nowhere from the root of the process; out of a mount's
// root to where it is mounted, and on from there; else the parent, which
// another's file system tells.
Walk::Step Walk::up() {
    for (;;) {
        if (same(here, root)) {
            return Step::Go;
        }
        if (!isMountRoot(here) || !here.m->parent) {
            break;
        }
        moveTo(here, pointOf(here.m));
    }
    if (isMountRoot(here)) {
        return Step::Go;
    }
    if (ours(here)) {
        if (here.n->parent) {
            moveTo(here, ourPlace(here.m, here.n->parent));
        }
        return Step::Go;
    }
    ask(fsLookup, here, literal(".."), Wait::Parent);
    return Step::Ask;
}

// A name the walk goes through: into what is mounted on it, on along a
// link's target, or into it.
Walk::Step Walk::settle(Place next) {
    next = down(next);
    if (!isLink(next)) {
        moveTo(here, next);
        return Step::Go;
    }
    if (links >= maxLinks) {
        return fail(ELOOP);
    }
    if (!ours(next)) {
        ask(fsReadlink, next, {}, Wait::Target);
        return Step::Ask;
    }
    const Name to = next.n->content.name();
    if (to.empty()) {
        return fail(ENOENT);
    }
    ++links;
    return follow(to, false) ? Step::Go : fail(ENAMETOOLONG);
}

// The walk is at the directory of its last name: with look, it looks at
// the name.
Walk::Step Walk::finish() {
    if (!look) {
        return Step::Done;
    }
    if (kind != lastName) {
        moveTo(target, here);
        found = true;
        return Step::Done;
    }
    if (!ours(here)) {
        ask(fsLookup, here, last, Wait::Last);
        return Step::Ask;
    }
    const Entry* entry = lookup(here.n, last);
    if (!entry) {
        if (!missing) {
            return fail(ENOENT);
        }
        found = false;
        return Step::Done;
    }
    return settleLast(ourPlace(here.m, entry->node));
}

// What the last name names: what is mounted on it, and a link followed
// when the walk follows it.
Walk::Step Walk::settleLast(Place t) {
    t = down(t);
    if (isLink(t) && (followLast || slash)) {
        if (links >= maxLinks) {
            return fail(ELOOP);
        }
        if (!ours(t)) {
            ask(fsReadlink, t, {}, Wait::LastTarget);
            return Step::Ask;
        }
        const Name to = t.n->content.name();
        if (to.empty()) {
            return fail(ENOENT);
        }
        ++links;
        if (!follow(to, true)) {
            return fail(ENAMETOOLONG);
        }
        return run();
    }
    if (slash && !isDir(t)) {
        return fail(ENOTDIR);
    }
    moveTo(target, t);
    found = true;
    return Step::Done;
}

Walk::Step Walk::run() {
    if (fresh) {
        fresh = false;
        if (onlyDots()) {
            Name dot;
            kind = take(dot) ? lastDot : lastNone;
            if (kind == lastDot && !isDir(here)) {
                return fail(ENOTDIR);
            }
            return finish();
        }
    }
    for (;;) {
        Name n;
        if (!take(n)) {
            kind = lastNone;
            return finish();
        }
        const bool lastOne = !more();
        if (!isDir(here)) {
            return fail(ENOTDIR);
        }
        if (!mayAt(here, h, 1)) {
            return fail(EACCES);
        }
        if (n.is(".")) {
            if (lastOne) {
                kind = lastDot;
                return finish();
            }
            continue;
        }
        if (n.is("..")) {
            ending = lastOne;
            if (const Step s = up(); s != Step::Go) {
                return s;
            }
            if (lastOne) {
                kind = lastDotdot;
                return finish();
            }
            continue;
        }
        if (n.length > NAME_MAX) {
            return fail(ENAMETOOLONG);
        }
        if (lastOne) {
            kind = lastName;
            last = n;
            return finish();
        }
        if (!ours(here)) {
            ask(fsLookup, here, n, Wait::Child);
            return Step::Ask;
        }
        const Entry* entry = lookup(here.n, n);
        if (!entry) {
            return fail(ENOENT);
        }
        if (const Step s = settle(ourPlace(here.m, entry->node)); s != Step::Go) {
            return s;
        }
    }
}

// The answer to the walk's question, and the walk on from there.
Walk::Step Walk::answered(const Answer& a) {
    const Wait w = wait;
    wait = Wait::None;
    if (a.error != 0) {
        if (w == Wait::Last && a.error == ENOENT && missing) {
            found = false;
            return Step::Done;
        }
        return fail(a.error);
    }
    Stat st;
    switch (w) {
        case Wait::Child:
            if (!statIn(a.body, st)) {
                return fail(EIO);
            }
            if (const Step s = settle(theirPlace(here.m, st)); s != Step::Go) {
                return s;
            }
            return run();
        case Wait::Parent:
            if (!statIn(a.body, st)) {
                return fail(EIO);
            }
            moveTo(here, theirPlace(here.m, st));
            if (ending) {
                kind = lastDotdot;
                return finish();
            }
            return run();
        case Wait::Last:
            if (!statIn(a.body, st)) {
                return fail(EIO);
            }
            return settleLast(theirPlace(here.m, st));
        case Wait::Target:
        case Wait::LastTarget: {
            Body body = a.body;
            const Name to = body.rest();
            if (to.empty()) {
                return fail(ENOENT);
            }
            ++links;
            if (!follow(to, w == Wait::LastTarget)) {
                return fail(ENAMETOOLONG);
            }
            return run();
        }
        case Wait::None:
            break;
    }
    return fail(EIO);
}

namespace {
    // ---- the VFS's calls ---------------------------------------------------------------
    //
    // Each is a coroutine that takes the kernel's endpoint, which it closes
    // when it is done unless an endpoint of ours took it: walks the path as
    // a walk does, waiting for another's file system when it must, and
    // makes the last step as Linux makes it, by a tree of ours or by the
    // file system the place is of, which answers the kernel itself. What a
    // call needs of the request it copies before it first waits.

    // The kernel's endpoint of a call.
    struct Reply {
        int fd;

        ~Reply();
    };

    // What goes with a call of chmod, chown, utimes and truncate.
    struct Args {
        u8 bytes[16];
        usize size;
    };

    // A path made from its end: the names of a place up to the root.
    struct Path {
        void prepend(Name name);
        Name text() const;

        Buffer bytes; // the names, each with a slash before it
    };

    // Whether a path is prefix or under it, and what follows it then.
    static bool under(Name prefix, Name path, Name& rest) {
        if (prefix.is("/")) {
            rest = path.is("/") ? Name{} : path;
            return true;
        }
        if (path.length < prefix.length || memcmp(path.at, prefix.at, prefix.length) != 0) {
            return false;
        }
        if (path.length != prefix.length && path.at[prefix.length] != '/') {
            return false;
        }
        rest = {path.at + prefix.length, path.length - prefix.length};
        return true;
    }

    // A place of a call's, held while the call holds it.
    struct Held {
        Held(const Place& p);
        ~Held();
        Held(const Held&) = delete;
        Held& operator=(const Held&) = delete;
        void become(const Place& p);

        Place at;
    };

    // OPEN: what the path names, opened as open(2) opens it.
    static Task openCall(Header h, int fd, Place root, Place from, u32 mode, Name path) {
        Reply r{fd};
        const int flags = int(h.arg);
        const bool excl = (flags & (O_CREAT | O_EXCL)) == (O_CREAT | O_EXCL);
        Walk w;
        w.start(h, from, root, path, true, (flags & O_NOFOLLOW) == 0 && !excl, (flags & O_CREAT) != 0);
        for (Walk::Step s = w.run(); s != Walk::Step::Done; s = w.answered(co_await w.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, w.error);
                co_return;
            }
        }
        if (w.kind != lastName) {
            if ((flags & O_CREAT) != 0) {
                error(fd, EISDIR);
            } else if (openAt(w.target, flags, h, fd)) {
                r.fd = -1;
            }
            co_return;
        }
        int fl = flags;
        if (w.slash) {
            if ((flags & O_CREAT) != 0) {
                error(fd, EISDIR);
                co_return;
            }
            fl |= O_DIRECTORY;
        }
        if (!w.found) {
            if (createAt(w.here, w.last, fl, mode, h, fd)) {
                r.fd = -1;
            }
            co_return;
        }
        if (excl) {
            error(fd, EEXIST);
            co_return;
        }
        if (isLink(w.target) && !((fl & O_PATH) != 0 && (fl & O_NOFOLLOW) != 0)) {
            error(fd, (fl & O_DIRECTORY) != 0 ? ENOTDIR : ELOOP);
            co_return;
        }
        if (openAt(w.target, fl, h, fd)) {
            r.fd = -1;
        }
    }

    // STAT: the metadata of what the path names, or of a last link itself.
    static Task statCall(Header h, int fd, Place root, Place from, Name path) {
        Reply r{fd};
        Walk w;
        w.start(h, from, root, path, true, h.arg != 0, false);
        for (Walk::Step s = w.run(); s != Walk::Step::Done; s = w.answered(co_await w.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, w.error);
                co_return;
            }
        }
        const Place& t = w.target;
        if (ours(t)) {
            const Stat st = statOf(*t.m->fs->tree, t.n);
            ok(fd, &st, sizeof st);
            co_return;
        }
        if (t.fresh) {
            ok(fd, &t.st, sizeof t.st);
            co_return;
        }
        const Answer a = co_await asking(t, h, fsGetattr, {});
        if (a.error != 0) {
            error(fd, a.error);
        } else {
            ok(fd, a.body.at, a.body.size());
        }
    }

    // READLINK: a last link's target; what is no link is EINVAL, a slash
    // after one follows it.
    static Task readlinkCall(Header h, int fd, Place root, Place from, Name path) {
        Reply r{fd};
        Walk w;
        w.start(h, from, root, path, true, false, false);
        for (Walk::Step s = w.run(); s != Walk::Step::Done; s = w.answered(co_await w.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, w.error);
                co_return;
            }
        }
        const Place& t = w.target;
        if (w.kind != lastName || !isLink(t)) {
            error(fd, EINVAL);
        } else if (ours(t)) {
            ok(fd, t.n->content.data(), t.n->content.size());
        } else {
            hand(t, h, fsReadlink, 0, nullptr, 0, {}, fd);
        }
    }

    // MKDIR, MKNOD and SYMLINK: the last name made. One that is there,
    // ".", ".." and the root alone are EEXIST, and a slash after a name is
    // for a directory only: EEXIST for one that is there, else ENOENT.
    static Task makeCall(Header h, int fd, Place root, Place from, Name target, Name path) {
        Reply r{fd};
        Buffer link;
        link.append(target.at, target.length);
        Walk w;
        w.start(h, from, root, path, false, false, false);
        for (Walk::Step s = w.run(); s != Walk::Step::Done; s = w.answered(co_await w.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, w.error);
                co_return;
            }
        }
        if (w.kind != lastName) {
            error(fd, EEXIST);
            co_return;
        }
        if (w.slash && h.op != vfsMkdir) {
            if (ours(w.here)) {
                error(fd, lookup(w.here.n, w.last) ? EEXIST : ENOENT);
                co_return;
            }
            const Answer a = co_await asking(w.here, h, fsLookup, w.last);
            error(fd, a.error == 0 ? EEXIST : ENOENT);
            co_return;
        }
        const Name to{static_cast<const char*>(link.data()), link.size()};
        if (ours(w.here)) {
            answer(fd, makeOurs(*w.here.m->fs->tree, w.here.n, h.op, h.arg, w.last, to, h));
            co_return;
        }
        if (h.op == vfsSymlink) {
            Buffer names;
            names.append(to.at, to.length);
            names.append("", 1);
            names.append(w.last.at, w.last.length);
            hand(w.here, h, fsSymlink, 0, nullptr, 0, {static_cast<const char*>(names.data()), names.size()}, fd);
            co_return;
        }
        hand(w.here, h, h.op == vfsMkdir ? fsMkdir : fsMknod, h.arg, nullptr, 0, w.last, fd);
    }

    // UNLINK: the last name taken away, a directory's with AT_REMOVEDIR.
    // "." is EINVAL to rmdir, ".." ENOTEMPTY, the root alone EBUSY, and all
    // three EISDIR to unlink; a slash after a name that is no directory is
    // ENOTDIR to unlink, EISDIR after one that is; a mount point is EBUSY.
    static Task unlinkCall(Header h, int fd, Place root, Place from, Name path) {
        Reply r{fd};
        const bool dir = (h.arg & AT_REMOVEDIR) != 0;
        Walk w;
        w.start(h, from, root, path, false, false, false);
        for (Walk::Step s = w.run(); s != Walk::Step::Done; s = w.answered(co_await w.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, w.error);
                co_return;
            }
        }
        if (w.kind != lastName) {
            error(fd, !dir ? EISDIR : w.kind == lastDot ? EINVAL : w.kind == lastDotdot ? ENOTEMPTY : EBUSY);
            co_return;
        }
        if (w.slash && !dir) {
            if (ours(w.here)) {
                const Entry* entry = lookup(w.here.n, w.last);
                error(fd, !entry ? ENOENT : entry->node->kind == Kind::Dir ? EISDIR : ENOTDIR);
                co_return;
            }
            const Answer a = co_await asking(w.here, h, fsLookup, w.last);
            Stat st;
            error(fd, a.error != 0 ? a.error : !statIn(a.body, st) ? EIO : S_ISDIR(st.mode) ? EISDIR : ENOTDIR);
            co_return;
        }
        if (ours(w.here)) {
            answer(fd, unlinkError(w.here.m->fs->tree, w.here.n, w.last, dir, h));
        } else if (dir && pointNamed(w.here, w.last)) {
            error(fd, EBUSY);
        } else {
            hand(w.here, h, fsUnlink, h.arg, nullptr, 0, w.last, fd);
        }
    }

    // RENAME: the last name of one path moved to the last name of another,
    // from the handle to: ".", ".." and the root alone are EBUSY, a slash
    // after either is for a directory only, two mounts are EXDEV and a
    // mount point EBUSY.
    static Task renameCall(Header h, int fd, Place root, Place from, Place to, Name oldPath, Name newPath) {
        Reply r{fd};
        Buffer second;
        second.append(newPath.at, newPath.length);
        Held toward(to);
        Walk a;
        a.start(h, from, root, oldPath, false, false, false);
        for (Walk::Step s = a.run(); s != Walk::Step::Done; s = a.answered(co_await a.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, a.error);
                co_return;
            }
        }
        Walk b;
        b.start(h, toward.at, root, {static_cast<const char*>(second.data()), second.size()}, false, false, false);
        for (Walk::Step s = b.run(); s != Walk::Step::Done; s = b.answered(co_await b.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, b.error);
                co_return;
            }
        }
        if (a.kind != lastName || b.kind != lastName) {
            error(fd, EBUSY);
            co_return;
        }
        if (a.slash || b.slash) {
            bool directory = false;
            if (ours(a.here)) {
                const Entry* entry = lookup(a.here.n, a.last);
                if (!entry) {
                    error(fd, ENOENT);
                    co_return;
                }
                directory = entry->node->kind == Kind::Dir;
            } else {
                const Answer got = co_await asking(a.here, h, fsLookup, a.last);
                Stat st;
                if (got.error != 0 || !statIn(got.body, st)) {
                    error(fd, got.error != 0 ? got.error : EIO);
                    co_return;
                }
                directory = S_ISDIR(st.mode);
            }
            if (!directory) {
                error(fd, ENOTDIR);
                co_return;
            }
        }
        if (a.here.m != b.here.m) {
            error(fd, EXDEV);
            co_return;
        }
        if (ours(a.here)) {
            answer(fd, renameOurs(a.here.m->fs->tree, a.here.n, a.last, b.here.n, b.last, h));
            co_return;
        }
        if (pointNamed(a.here, a.last) || pointNamed(b.here, b.last)) {
            error(fd, EBUSY);
            co_return;
        }
        Buffer names;
        names.append(a.last.at, a.last.length);
        names.append("", 1);
        names.append(b.last.at, b.last.length);
        hand(a.here, h, fsRename, 0, &b.here.node, 8, {static_cast<const char*>(names.data()), names.size()}, fd);
    }

    // LINK: the last name of a new path, from the handle to, made a name of
    // what an old path names, a last link of it followed when follow says:
    // a directory is EPERM, the new name is made as MKDIR makes one, and
    // two mounts are EXDEV, but EROFS when the new name's file system is
    // read-only, which Linux finds first.
    static Task linkCall(Header h, int fd, Place root, Place from, Place to, Name oldPath, Name newPath) {
        Reply r{fd};
        Buffer second;
        second.append(newPath.at, newPath.length);
        Held toward(to);
        Walk a;
        a.start(h, from, root, oldPath, true, h.arg != 0, false);
        for (Walk::Step s = a.run(); s != Walk::Step::Done; s = a.answered(co_await a.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, a.error);
                co_return;
            }
        }
        if (a.kind != lastName || isDir(a.target)) {
            error(fd, EPERM);
            co_return;
        }
        Walk b;
        b.start(h, toward.at, root, {static_cast<const char*>(second.data()), second.size()}, false, false, false);
        for (Walk::Step s = b.run(); s != Walk::Step::Done; s = b.answered(co_await b.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, b.error);
                co_return;
            }
        }
        if (b.kind != lastName) {
            error(fd, EEXIST);
            co_return;
        }
        if (b.slash) {
            if (ours(b.here)) {
                error(fd, lookup(b.here.n, b.last) ? EEXIST : ENOENT);
                co_return;
            }
            const Answer got = co_await asking(b.here, h, fsLookup, b.last);
            error(fd, got.error == 0 ? EEXIST : ENOENT);
            co_return;
        }
        if (a.target.m != b.here.m) {
            bool readOnly = false;
            if (ours(b.here)) {
                readOnly = !b.here.m->fs->tree->writable;
            } else {
                const Answer got = co_await asking(b.here, h, fsStatfs, {});
                Statfs st;
                readOnly = got.error == 0 && got.body.size() >= sizeof st && (memcpy(&st, got.body.at, sizeof st), (st.flags & ST_RDONLY) != 0);
            }
            error(fd, readOnly ? EROFS : EXDEV);
            co_return;
        }
        if (ours(b.here)) {
            answer(fd, linkOurs(b.here.m->fs->tree, a.target.n, b.here.n, b.last, h));
        } else {
            hand(a.target, h, fsLink, 0, &b.here.node, 8, b.last, fd);
        }
    }

    // CHMOD, CHOWN, UTIMES and TRUNCATE: the file system's call op on what
    // the path names, a last link followed when follows says; a link has no
    // mode to change, EOPNOTSUPP as Linux's fchmodat says.
    static Task changeCall(Header h, int fd, Place root, Place from, u32 op, bool follows, Args args, Name path) {
        Reply r{fd};
        Walk w;
        w.start(h, from, root, path, true, follows, false);
        for (Walk::Step s = w.run(); s != Walk::Step::Done; s = w.answered(co_await w.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, w.error);
                co_return;
            }
        }
        if (op == fsChmod && isLink(w.target)) {
            error(fd, EOPNOTSUPP);
        } else {
            changeAt(w.target, op, h, args.bytes, args.size, fd);
        }
    }

    // STATFS: the file system of what the path names.
    static Task statfsCall(Header h, int fd, Place root, Place from, Name path) {
        Reply r{fd};
        Walk w;
        w.start(h, from, root, path, true, true, false);
        for (Walk::Step s = w.run(); s != Walk::Step::Done; s = w.answered(co_await w.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, w.error);
                co_return;
            }
        }
        if (ours(w.target)) {
            const Statfs st = statfsOf(*w.target.m->fs->tree);
            ok(fd, &st, sizeof st);
        } else {
            hand(w.target, h, fsStatfs, 0, nullptr, 0, {}, fd);
        }
    }

    // PATH and REALPATH: the path from the root of a place, up through the
    // places ".." goes to: in a tree of ours by the names of its
    // directories, in another's file system as it says, then where its
    // mount is mounted. A removed directory has none, nor one the root is
    // not above: ENOENT. PATH's place is a handle's; REALPATH's is what a
    // path names, a last link followed: its directory's path and its name,
    // or a directory's own, with no ".", ".." or link in it.
    static Task pathCall(Header h, int fd, Place root, Place from, bool walks, Name path) {
        Reply r{fd};
        Held top(root);
        Held up(from);
        Path out;
        if (walks) {
            Walk w;
            w.start(h, from, root, path, true, true, false);
            for (Walk::Step s = w.run(); s != Walk::Step::Done; s = w.answered(co_await w.question())) {
                if (s == Walk::Step::Failed) {
                    error(fd, w.error);
                    co_return;
                }
            }
            if (isDir(w.target)) {
                up.become(w.target);
            } else {
                out.prepend(w.last);
                up.become(w.here);
            }
        }
        while (!same(up.at, top.at)) {
            if (isMountRoot(up.at)) {
                if (!up.at.m->parent) {
                    error(fd, ENOENT);
                    co_return;
                }
                up.become(pointOf(up.at.m));
                continue;
            }
            if (ours(up.at)) {
                const Entry* entry = up.at.n->entry;
                if (gone(up.at.m->fs->tree, up.at.n) || !entry || entry->name.empty()) {
                    error(fd, ENOENT);
                    co_return;
                }
                out.prepend(entry->name);
                up.become(ourPlace(up.at.m, up.at.n->parent));
                continue;
            }
            // Another's file system says the place's path in it, and that of
            // the mount's root, or of the root of the process in this mount.
            Buffer own;
            {
                const Answer got = co_await asking(up.at, h, fsPath, {});
                if (got.error != 0) {
                    error(fd, got.error);
                    co_return;
                }
                own.append(got.body.at, got.body.size());
            }
            const bool rooted = top.at.m == up.at.m;
            Buffer base;
            {
                const Answer got = co_await asking(rooted ? top.at : rootOf(up.at.m), h, fsPath, {});
                if (got.error != 0) {
                    error(fd, got.error);
                    co_return;
                }
                base.append(got.body.at, got.body.size());
            }
            Name rest;
            if (!under({static_cast<const char*>(base.data()), base.size()}, {static_cast<const char*>(own.data()), own.size()}, rest)) {
                error(fd, ENOENT);
                co_return;
            }
            out.prepend(rest);
            if (rooted) {
                break;
            }
            up.become(rootOf(up.at.m));
        }
        const Name text = out.text();
        ok(fd, text.at, text.length);
    }

    // MOUNT: on the directory target names, a directory source names, or
    // the root of the file system whose socket of /dev it names, which
    // rootfs connects to: the file system's end of the connection is its
    // channel from then on. A target that is a mount's root, of what is
    // mounted on it or of the VFS's, is EBUSY.
    static Task mountCall(Header h, int fd, Place root, Place from, Name target, Name source) {
        Reply r{fd};
        Buffer second;
        second.append(source.at, source.length);
        Walk t;
        t.start(h, from, root, target, true, true, false);
        for (Walk::Step s = t.run(); s != Walk::Step::Done; s = t.answered(co_await t.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, t.error);
                co_return;
            }
        }
        if (t.kind != lastName) {
            error(fd, EINVAL);
            co_return;
        }
        if (!isDir(t.target)) {
            error(fd, ENOTDIR);
            co_return;
        }
        if (isMountRoot(t.target)) {
            error(fd, EBUSY);
            co_return;
        }
        Walk ws;
        ws.start(h, from, root, {static_cast<const char*>(second.data()), second.size()}, true, true, false);
        for (Walk::Step s = ws.run(); s != Walk::Step::Done; s = ws.answered(co_await ws.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, ws.error);
                co_return;
            }
        }
        const Place& src = ws.target;
        if (isDir(src)) {
            mountAt(src.m->fs, src, t.target, t.here.node, t.last);
            ok(fd);
            co_return;
        }
        if (ours(src) || !S_ISSOCK(src.st.mode)) {
            error(fd, ENOTDIR);
            co_return;
        }
        // The socket's file system opens it, its service: the endpoint of
        // ours is the service's channel from then on.
        Ask connect = asking(src, h, fsOpen, {});
        connect.head.arg = O_RDWR;
        connect.connects = true;
        const Answer got = co_await connect;
        Stat st;
        if (got.error != 0 || !statIn(got.body, st)) {
            if (got.channel) {
                closed(got.channel);
            }
            error(fd, got.error != 0 ? got.error : EIO);
            co_return;
        }
        auto* fs = new (allocate(sizeof(Fs))) Fs();
        fs->channel = got.channel;
        fs->dev = st.dev;
        got.channel->fs = fs;
        Place top;
        top.node = st.ino;
        top.st = st;
        mountAt(fs, top, t.target, t.here.node, t.last);
        ok(fd);
    }

    // UNMOUNT: what is mounted where a path names, a last link followed
    // unless UMOUNT_NOFOLLOW says; EINVAL for what is no mount's root, EBUSY
    // for one with mounts on it.
    static Task unmountCall(Header h, int fd, Place root, Place from, Name path) {
        Reply r{fd};
        Walk w;
        w.start(h, from, root, path, true, (h.arg & UMOUNT_NOFOLLOW) == 0, false);
        for (Walk::Step s = w.run(); s != Walk::Step::Done; s = w.answered(co_await w.question())) {
            if (s == Walk::Step::Failed) {
                error(fd, w.error);
                co_return;
            }
        }
        Mount* m = w.target.m;
        if (w.kind != lastName || !isMountRoot(w.target) || !m->parent) {
            error(fd, EINVAL);
            co_return;
        }
        for (const Mount* sub = mounts; sub; sub = sub->next) {
            if (sub->parent == m) {
                error(fd, EBUSY);
                co_return;
            }
        }
        unmount(m);
        ok(fd);
    }
}

Reply::~Reply() {
    if (fd >= 0) {
        close(fd);
    }
}

Held::Held(const Place& p)
    : at(p)
{
    hold(at);
}

Held::~Held() {
    unhold(at);
}

void Held::become(const Place& p) {
    hold(p);
    unhold(at);
    at = p;
}

void Path::prepend(Name name) {
    Buffer next;
    const char* rest = static_cast<const char*>(bytes.data());
    if (name.empty()) {
        return;
    }
    if (name.at[0] != '/') {
        next.append("/", 1);
    }
    next.append(name.at, name.length);
    next.append(rest, bytes.size());
    bytes.swap(next);
}

Name Path::text() const {
    if (bytes.size() == 0) {
        return literal("/");
    }
    return {static_cast<const char*>(bytes.data()), bytes.size()};
}

namespace {
    // ---- the loop ---------------------------------------------------------------

    // fchmod, fchown, futimes, ftruncate and fsync of an open file of a
    // tree of ours; only an endpoint open to write truncates.
    static bool fattr(Endpoint* ep, Server& s, Node* n, const Header& h, Body body, int fd) {
        if (h.op == opFsync) {
            return answer(fd, 0);
        }
        if (!s.writable) {
            return error(fd, EROFS);
        }
        if (h.op == opFtruncate && !ep->writes) {
            return error(fd, EINVAL);
        }
        return answer(fd, change(s, n, h.op, h, body));
    }

    // A call on an open file of a tree of ours, answered on fd: whether fd
    // is kept.
    static bool onFile(Endpoint* ep, const Header& h, Body body, int fd) {
        switch (h.op) {
            case opRead:
                return readNode(ep, h, body, fd);
            case opWrite:
                return writeNode(ep, h, body, fd);
            case opReaddir:
                return error(fd, ENOTDIR);
            case opFstat: {
                const Stat st = statOf(*ep->server, ep->node);
                ok(fd, &st, sizeof st);
                return false;
            }
            case opFstatfs: {
                const Statfs st = statfsOf(*ep->server);
                ok(fd, &st, sizeof st);
                return false;
            }
            case opFchmod:
            case opFchown:
            case opFutimes:
            case opFtruncate:
            case opFsync:
                return fattr(ep, *ep->server, ep->node, h, body, fd);
            case opFlock:
                return flockNode(ep, h, fd);
            case opLock:
                return lockNode(ep, h, body, fd);
            default:
                return error(fd, ENOSYS);
        }
    }

    // An open file's call on a handle: by a tree of ours, or handed on to
    // the file system its place is of. O_PATH's handle reads, changes and
    // locks nothing: EBADF.
    static bool onPlace(Endpoint* ep, const Header& h, Body body, int fd) {
        const Place& at = ep->at;
        const bool opath = !ep->reads;
        switch (h.op) {
            case opFstat:
                if (ours(at)) {
                    handleReply(fd, statOf(*at.m->fs->tree, at.n), ep->id);
                    return false;
                }
                return hand(at, h, fsGetattr, 0, nullptr, 0, {}, fd);
            case opFstatfs:
                if (ours(at)) {
                    const Statfs st = statfsOf(*at.m->fs->tree);
                    ok(fd, &st, sizeof st);
                    return false;
                }
                return hand(at, h, fsStatfs, 0, nullptr, 0, {}, fd);
            case opReaddir:
                if (opath) {
                    return error(fd, EBADF);
                }
                if (!isDir(at)) {
                    return error(fd, ENOTDIR);
                }
                if (ours(at)) {
                    return readdirNode(at.n, body, fd);
                }
                return hand(at, h, fsReaddir, 0, body.at, body.size(), {}, fd);
            case opFsync:
                return opath ? error(fd, EBADF) : answer(fd, 0);
            case opFtruncate:
                return error(fd, opath ? EBADF : EINVAL);
            case opFchmod:
            case opFchown:
            case opFutimes:
                if (opath) {
                    return error(fd, EBADF);
                }
                if (ours(at)) {
                    return fattr(ep, *at.m->fs->tree, at.n, h, body, fd);
                }
                return hand(at, h, h.op == opFchmod ? fsChmod : h.op == opFchown ? fsChown : fsUtimes, h.arg, body.at, body.size(), {}, fd);
            case opFlock:
            case opLock:
                if (opath) {
                    return error(fd, EBADF);
                }
                if (!ours(at)) {
                    return error(fd, EINVAL);
                }
                return h.op == opFlock ? flockNode(ep, h, fd) : lockNode(ep, h, body, fd);
            default:
                return error(fd, ENOSYS);
        }
    }

    // A call on a handle, answered on fd: one of the VFS's, which names
    // the process's root by its handle's id and walks from this one; or an
    // open file's. Whether fd is kept: a call of the VFS's keeps it, and
    // closes it when it is done.
    static bool onHandle(Endpoint* ep, const Header& h, Body body, int fd) {
        if (h.op < vfsOpen || h.op > vfsPath) {
            return onPlace(ep, h, body, fd);
        }
        Place root;
        if (body.size() < 8 || !placeOf(body.take<u64>(), root)) {
            return error(fd, EIO);
        }
        const Place from = ep->at;
        switch (h.op) {
            case vfsOpen: {
                const u32 mode = body.take<u32>();
                openCall(h, fd, root, from, mode, body.rest());
                break;
            }
            case vfsStat:
                statCall(h, fd, root, from, body.rest());
                break;
            case vfsReadlink:
                readlinkCall(h, fd, root, from, body.rest());
                break;
            case vfsMkdir:
            case vfsMknod:
                makeCall(h, fd, root, from, {}, body.rest());
                break;
            case vfsSymlink: {
                const Name target = body.string();
                makeCall(h, fd, root, from, target, body.rest());
                break;
            }
            case vfsUnlink:
                unlinkCall(h, fd, root, from, body.rest());
                break;
            case vfsRename:
            case vfsLink: {
                Place to;
                if (!placeOf(body.take<u64>(), to)) {
                    return error(fd, EIO);
                }
                const Name old = body.string();
                if (h.op == vfsRename) {
                    renameCall(h, fd, root, from, to, old, body.rest());
                } else {
                    linkCall(h, fd, root, from, to, old, body.rest());
                }
                break;
            }
            case vfsChmod:
            case vfsChown:
            case vfsUtimes:
            case vfsTruncate: {
                const u32 flags = h.op == vfsTruncate ? 0 : body.take<u32>();
                const usize size = h.op == vfsChown || h.op == vfsTruncate ? 8 : h.op == vfsUtimes ? 16 : 0;
                Args args{};
                if (body.size() < size) {
                    return error(fd, EINVAL);
                }
                memcpy(args.bytes, body.at, size);
                args.size = size;
                body.at += size;
                const u32 op = h.op == vfsChmod ? fsChmod : h.op == vfsChown ? fsChown : h.op == vfsUtimes ? fsUtimes : fsTruncate;
                changeCall(h, fd, root, from, op, (flags & AT_SYMLINK_NOFOLLOW) == 0, args, body.rest());
                break;
            }
            case vfsStatfs:
                statfsCall(h, fd, root, from, body.rest());
                break;
            case vfsRealpath:
                pathCall(h, fd, root, from, true, body.rest());
                break;
            case vfsMount: {
                const Name target = body.string();
                mountCall(h, fd, root, from, target, body.rest());
                break;
            }
            case vfsUnmount:
                unmountCall(h, fd, root, from, body.rest());
                break;
            case vfsPath:
                pathCall(h, fd, root, from, false, {});
                break;
        }
        return true;
    }

    // The answer to a call's question to a file system: the call goes on.
    // A connection's endpoint stays, the channel from then on; any other
    // is done with. An answer that is no OK or ERR, or none, is EIO.
    static void answered(Endpoint* ep) {
        Ask* a = ep->ask;
        int fds[maxFds];
        usize count = 0;
        const ssize_t n = receivePacket(ep->fd, packet, sizeof packet, fds, count);
        for (usize i = 0; i < count; ++i) {
            close(fds[i]);
        }
        Header h{};
        if (n >= ssize_t(sizeof h)) {
            memcpy(&h, packet, sizeof h);
        }
        if (n < ssize_t(sizeof h) || (h.op != opOk && h.op != opErr)) {
            a->answer.error = EIO;
        } else if (h.op == opErr) {
            a->answer.error = h.arg != 0 ? int(h.arg) : EIO;
        } else {
            a->answer.body = {packet + sizeof h, packet + n};
        }
        if (a->connects && a->answer.error == 0) {
            ep->role = Role::Fs;
            ep->ask = nullptr;
            a->answer.channel = ep;
        } else {
            closed(ep);
        }
        a->call.resume();
    }

    static void handle(Endpoint* ep) {
        if (ep->dead) {
            return;
        }
        if (ep->role == Role::Pending) {
            answered(ep);
            return;
        }
        if (ep->role == Role::Fs) {
            // Nothing comes on a file system's channel but its close; what
            // else does is dropped.
            const ssize_t got = receivePacket(ep->fd, packet, sizeof packet, packetFds, packetFdCount);
            for (usize i = 0; i < packetFdCount; ++i) {
                close(packetFds[i]);
            }
            if (got <= 0) {
                closed(ep);
            }
            return;
        }
        Header h;
        const i64 n = receive(ep->fd, h);
        if (n < 0) {
            closed(ep);
            return;
        }
        if (packetFdCount == 0) {
            return; // a request without its reply endpoint: nobody to answer
        }
        if (h.op == opOk || h.op == opErr) {
            for (usize i = 0; i < packetFdCount; ++i) {
                close(packetFds[i]);
            }
            return;
        }
        const int fd = packetFds[packetFdCount - 1];
        const Body body{packet + sizeof(Header), packet + sizeof(Header) + n};
        const bool kept = ep->role == Role::Handle ? onHandle(ep, h, body, fd) : onFile(ep, h, body, fd);
        if (!kept) {
            close(fd);
        }
        // The request's other descriptors, a READ's or WRITE's window, are
        // done with once it is answered; one a call kept is -1.
        for (usize i = 0; i + 1 < packetFdCount; ++i) {
            if (packetFds[i] >= 0) {
                close(packetFds[i]);
            }
        }
    }

    pid_t shellPid;
    int shellStatus = -1;

    // After a wait a child's end interrupted: what ended is reaped; the
    // shell's end is the system's.
    static bool reap() {
        if (!childEnded) {
            return true;
        }
        childEnded = 0;
        int status;
        for (pid_t pid; (pid = waitpid(-1, &status, WNOHANG)) > 0;) {
            if (pid == shellPid) {
                shellStatus = WIFEXITED(status) ? WEXITSTATUS(status) : 128 + WTERMSIG(status);
            }
        }
        return shellStatus < 0;
    }

    int bootEnd = -1;    // ours of the pair the services close once they are up
    Endpoint bootMarker; // what the events of bootEnd carry

    static void booted();

    // The shell ended: the system goes down. The services' devices lose
    // their names and the file systems of others their channels; a service
    // ends once what is open of it is closed, a file system once it has
    // written what it holds, the file systems first, as the devices under
    // them serve them until then.
    static void shutdown() {
        for (Endpoint* ep = live; ep;) {
            Endpoint* after = ep->after;
            if (!ep->dead && ep->role == Role::Fs) {
                closed(ep);
            }
            ep = after;
        }
    }

    static i64 monotonicMilliseconds() {
        timespec ts;
        clock_gettime(CLOCK_MONOTONIC, &ts);
        return i64(ts.tv_sec) * 1000 + ts.tv_nsec / 1000000;
    }

    // Serves the VFS until the last endpoint closes: then nobody can reach
    // it. When the shell ends, the system goes down (shutdown), and it is
    // served until its processes are gone, the last endpoint closed, or for
    // 10 seconds at most. The end of bootEnd, no packet on it, is the
    // services' being up. An endpoint closed while a batch of events is
    // handled stays dead to the rest of the batch.
    static void serve() {
        epoll_event events[64];
        i64 deadline = -1;
        while (endpointCount > 0) {
            const i64 left = deadline < 0 ? -1 : deadline - monotonicMilliseconds();
            if (deadline >= 0 && left <= 0) {
                break;
            }
            const int n = epoll_pwait(watcher, events, 64, int(left), waitMasked ? &waitMask : nullptr);
            if (!reap() && deadline < 0) {
                deadline = monotonicMilliseconds() + 10000;
                shutdown();
                bury();
                continue;
            }
            for (int i = 0; i < n; ++i) {
                if (events[i].data.ptr == &bootMarker) {
                    if (recv(bootEnd, packet, sizeof packet, MSG_DONTWAIT) == 0) {
                        booted();
                    }
                    continue;
                }
                handle(static_cast<Endpoint*>(events[i].data.ptr));
            }
            bury();
        }
        close(watcher);
        watcher = -1;
    }

    // ---- the system ----------------------------------------------------------------

    static void onChild(int) {
        childEnded = 1;
    }

    // Has a child's end interrupt the server's wait, and nothing else: the
    // loop then reaps. SIGCHLD is blocked but in the wait, and the mask
    // before is the wait's and, through spawn, a child's.
    static void reapOnChild() {
        sigset_t chld;
        sigemptyset(&chld);
        sigaddset(&chld, SIGCHLD);
        signal(SIGCHLD, onChild);
        if (sigprocmask(SIG_BLOCK, &chld, &waitMask) != 0) {
            fatal("rootfs: sigprocmask: errno %d", errno);
        }
        waitMasked = true;
    }

    // A field of a ustar header: its bytes up to a NUL.
    static Name field(const u8* at, usize size) {
        const char* text = reinterpret_cast<const char*>(at);
        return {text, strnlen(text, size)};
    }

    // A number field of a ustar header: octal.
    static u64 octal(const u8* at, usize size) {
        char text[16];
        const Name digits = field(at, MIN(size, sizeof text - 1));
        memcpy(text, digits.at, digits.length);
        text[digits.length] = 0;
        return strtoull(text, nullptr, 8);
    }

    // The directory of a path, made on the way.
    static Node* dirOf(Server& s, Name path) {
        Node* n = s.root;
        for (usize i = 0; i < path.length;) {
            usize j = i;
            while (j < path.length && path.at[j] != '/') {
                ++j;
            }
            if (j > i) {
                const Name part{path.at + i, j - i};
                const Entry* entry = lookup(n, part);
                n = entry ? entry->node : makeNode(s, n, part, Kind::Dir, false);
            }
            i = j + 1;
        }
        return n;
    }

    // A name of the image made again: what it named goes.
    static void unname(Server& s, Node* dir, Name name) {
        if (Entry* entry = lookup(dir, name)) {
            release(s, unplace(dir, entry, now()));
        }
    }

    // The entries of the archive, a header block of 512 bytes each, the
    // data of a file in the blocks after it. Names, targets and data are
    // the image's bytes.
    static void load(Server& s) {
        const usize total = rootfs_image_size;
        for (usize at = 0; at + 512 <= total && rootfs_image[at] != 0;) {
            const u8* block = rootfs_image + at;
            Name path = field(block, 100);
            const u32 perm = u32(octal(block + 100, 8)) & 0777;
            const u64 size = octal(block + 124, 12);
            const char kind = char(block[156]);
            const Name target = field(block + 157, 100);
            if (size > total - at - 512) {
                fatal("rootfs: the image ends in an entry");
            }
            while (!path.empty() && path.at[path.length - 1] == '/') {
                --path.length;
            }
            const char* slash = static_cast<const char*>(memrchr(path.at, '/', path.length));
            Node* dir = dirOf(s, {path.at, slash ? usize(slash - path.at) : 0});
            const Name base = slash ? Name{slash + 1, path.length - usize(slash + 1 - path.at)} : path;
            switch (kind) {
                case '5':
                    if (!base.empty()) {
                        const Entry* entry = lookup(dir, base);
                        (entry ? entry->node : makeNode(s, dir, base, Kind::Dir, false))->perm = perm;
                    }
                    break;
                case '2': {
                    unname(s, dir, base);
                    Node* n = makeNode(s, dir, base, Kind::Link, false);
                    n->content.view = reinterpret_cast<const u8*>(target.at);
                    n->content.viewSize = target.length;
                    break;
                }
                default: {
                    unname(s, dir, base);
                    Node* n = makeNode(s, dir, base, Kind::File, false);
                    n->content.view = block + 512;
                    n->content.viewSize = usize(size);
                    n->perm = perm;
                    s.used += size;
                }
            }
            at += 512 + usize((size + 511) / 512 * 512);
        }
    }

    // The most descriptors a child gets: the end of boot's pair, a file
    // system's channel, and the disks.
    constexpr int maxKept = 2 + 32;

    // A program run in a child whose root and working directory are root,
    // an end of a pair of ours: the child asks us, who serve when it does.
    // The descriptors kept are the child's 3 and on, in order.
    static pid_t spawn(const char* path, char* const* argv, char* const* env, int root, const int* kept = nullptr, int count = 0) {
        const pid_t pid = fork();
        if (pid == 0) {
            if (waitMasked) {
                sigprocmask(SIG_SETMASK, &waitMask, nullptr);
            }
            if (fchdir(root) != 0 || chroot(".") != 0) {
                dprintf(STDERR_FILENO, "rootfs: chroot: errno %d\n", errno);
                _exit(127);
            }
            close(root);
            int high[maxKept];
            for (int i = 0; i < count; ++i) {
                high[i] = fcntl(kept[i], F_DUPFD_CLOEXEC, 64);
            }
            for (int i = 0; i < count; ++i) {
                if (high[i] < 0 || dup2(high[i], 3 + i) < 0) {
                    dprintf(STDERR_FILENO, "rootfs: descriptor %d: errno %d\n", 3 + i, errno);
                    _exit(127);
                }
            }
            execve(path, argv, env);
            dprintf(STDERR_FILENO, "rootfs: %s: errno %d\n", path, errno);
            _exit(127);
        }
        if (pid < 0) {
            fatal("rootfs: fork: errno %d", errno);
        }
        return pid;
    }

    // What rootfs starts runs with a clean environment, its root the far
    // end of our own pair, never ours: a server does not walk its own tree
    // by path, and could not ask itself before it serves.
    char pathVariable[] = "PATH=/bin";
    char homeVariable[] = "HOME=/";
    char termVariable[] = "TERM=vt100";
    char* const environment[] = {pathVariable, homeVariable, termVariable, nullptr};
    int rootEnd = -1;

    Server image; // the root
    Fs imageFs;

    // Mounts the root of a file system rootfs started, on a directory of
    // the image's root: the file system's channel ours, its root its node
    // 1, a directory of root's of mode perm as it starts, until its own
    // stat says more.
    static void mountStarted(int channel, const char* name, u32 perm) {
        const Entry* entry = lookup(image.root, literal(name));
        if (!entry || entry->node->kind != Kind::Dir) {
            fatal("rootfs: mount on /%s", name);
        }
        auto* fs = new (allocate(sizeof(Fs))) Fs();
        fs->channel = listen(channel, Role::Fs);
        fs->channel->fs = fs;
        Place top;
        top.node = 1;
        top.st.ino = 1;
        top.st.mode = S_IFDIR | perm;
        top.st.nlink = 2;
        mountAt(fs, top, ourPlace(rootMount, entry->node), image.root->ino, literal(name));
    }

    // Starts the file systems and the services of the system, the far end
    // of a pair of ours their descriptor 3: they close it once they are up,
    // after the services they start have it, and when the last one has, the
    // shell starts (booted). /bin/tmpfs is /tmp, its channel its descriptor
    // 3. /bin/devfs is /dev, its channel its descriptor 4, and it gets the
    // disks vm gave us, our descriptors after 2, in order, named by DISKS:
    // its 5 and on, and it starts their services and every system's
    // devices.
    static void boot() {
        int ends[2];
        int tmp[2];
        int dev[2];
        int* pairs[] = {ends, tmp, dev};
        for (int* pair : pairs) {
            if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
                fatal("rootfs: socketpair: errno %d", errno);
            }
        }
        // The disks are no one's but devfs's.
        char disks[256] = {};
        if (const char* given = getenv("DISKS")) {
            strncpy(disks, given, sizeof disks - 1);
        }
        int kept[maxKept] = {ends[1], dev[1]};
        char numbers[maxKept][12];
        char* argv[3 + 2 * maxKept];
        char devfsName[] = "devfs";
        char option[] = "-r";
        char three[] = "3";
        char four[] = "4";
        int count = 2;
        int args = 0;
        argv[args++] = devfsName;
        argv[args++] = option;
        argv[args++] = three;
        argv[args++] = four;
        for (char* name = strtok(disks, " "); name && count < maxKept; name = strtok(nullptr, " ")) {
            fcntl(3 + count - 2, F_SETFD, FD_CLOEXEC);
            kept[count] = 3 + count - 2;
            snprintf(numbers[count], sizeof numbers[count], "%d", 3 + count);
            argv[args++] = name;
            argv[args++] = numbers[count];
            ++count;
        }
        argv[args] = nullptr;
        char tmpfsName[] = "tmpfs";
        char* const tmpfsArgv[] = {tmpfsName, three, nullptr};
        spawn("/bin/tmpfs", tmpfsArgv, environment, rootEnd, &tmp[1], 1);
        close(tmp[1]);
        mountStarted(tmp[0], "tmp", 01777);
        mountStarted(dev[0], "dev", 0755);
        spawn("/bin/devfs", argv, environment, rootEnd, kept, count);
        for (int i = 2; i < count; ++i) {
            close(kept[i]);
        }
        close(dev[1]);
        close(ends[1]);
        bootEnd = ends[0];
        epoll_event ev{};
        ev.events = EPOLLIN;
        ev.data.ptr = &bootMarker;
        if (epoll_ctl(watcher, EPOLL_CTL_ADD, bootEnd, &ev) != 0) {
            fatal("rootfs: epoll_ctl: errno %d", errno);
        }
    }

    // The services are up: the shell starts on the terminal.
    static void booted() {
        epoll_ctl(watcher, EPOLL_CTL_DEL, bootEnd, nullptr);
        close(bootEnd);
        bootEnd = -1;
        char sh[] = "sh";
        char* const argv[] = {sh, nullptr};
        shellPid = spawn("/bin/sh", argv, environment, rootEnd);
        close(rootEnd);
        rootEnd = -1;
    }

    // The image as a file system of the VFS's.
    static void tree(Server& s, Fs& fs) {
        s.root = makeNode(s, nullptr, {}, Kind::Dir);
        fs.tree = &s;
        fs.dev = s.dev;
    }
}

int main() {
    tree(image, imageFs);
    load(image);
    rootMount = new (allocate(sizeof(Mount))) Mount();
    rootMount->fs = &imageFs;
    imageFs.mounts = 1;
    rootMount->root = image.root->ino;
    rootMount->rootNode = image.root;
    mounts = rootMount;
    int ends[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, ends) != 0) {
        fatal("rootfs: socketpair: errno %d", errno);
    }
    reapOnChild();
    watcher = epoll_create1(EPOLL_CLOEXEC);
    if (watcher < 0) {
        fatal("rootfs: epoll_create1: errno %d", errno);
    }
    listenHandle(ends[0], rootOf(rootMount), false);
    rootEnd = ends[1];
    boot();
    serve();
    return MAX(shellStatus, 0);
}
