// rootfs: the system, the first process, and its VFS. Every call with a
// path of the system's processes comes here whole from the kernel, and
// rootfs walks it: names, "." and "..", symbolic links, mounts, the root
// of the process that asks, and the last step of the call as Linux makes
// it (lib/protocol/protocol.h is the protocol, the VFS's calls on handles,
// a file system's calls by node, an open file's). Every file system is
// another process's, mounted on the VFS's tree, which rootfs asks by node:
// the image at the root, which erofs serves (bin/tools),
// /run and /dev, which it starts itself (tmpfs and devfs), and those a
// MOUNT names. A walk waits for the file systems' answers, as a coroutine,
// while the loop goes on with other calls, and rootfs keeps what they
// answer, a cache of names as Linux's dcache, for the walks after. The
// watchers of directories are its too (WATCH): it makes the calls that
// change the file systems' names, and a file system that changes by
// itself tells it its changes.
//
// It starts erofs first, from the image itself: the image's descriptor,
// a memfd of erofs's program in it, which it executes (fexecve), and the
// channel of the root, with no root of rootfs's, as erofs walks no path.
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
// process are handles, and so is what O_PATH opens. A call comes on the
// handle's endpoint with where it is answered, the last of its
// descriptors, by rootfs or by the file system it hands the call on to,
// that descriptor with it. An open file is its file system's endpoint: the
// kernel reads and writes it there, and the VFS is out of the way.
//
// A walk goes through a directory who asks may search, as the stat its
// file system gave says; a file system checks the rest of what it does
// itself.
//
// A file system of another process's is a socket it binds and listens on:
// mount(2) connects to it, and the connection is the file system's channel
// rootfs asks it on. The file systems and services rootfs starts get an
// end they close once they are up, and hand on to the services they
// start: when the last of them has closed it, the system is up.
//
// The image is bin/rootfs/image.py's layout, and a disk to rootfs, the
// one vm gives it named image: what follows rootfs in its own file, a
// system of one file, which vm gives as a memfd of the file's pages, or
// the disk vm was told to give by that name. Its header says where
// erofs's program is among its files, and rootfs reads nothing else of
// it.
//
// Memory. A request is received into one buffer, packet, and a call that
// never waits is answered before the next: its names are read where they
// are in it. A call that waits keeps its path, its own copy, in its
// coroutine, whose frames the loop keeps for the next. What a reply
// assembles, a path, goes into a buffer of its own.

#include <lib/protocol/protocol.h>

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
#include <sys/inotify.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
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
using i32 = int32_t;
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

constexpr usize maxPacket = 65536;

constexpr u32 maxLinks = 40; // the symbolic links one path goes through, Linux's

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
    // Bytes of a packet or of a call's, where they are.
    struct Name {
        const char* at = nullptr;
        usize length = 0;

        bool empty() const;
        bool is(const char* text) const;
        bool has(char c) const;
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

static Name literal(const char* text) {
    return {text, strlen(text)};
}

namespace {
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

    // ---- file systems, mounts and places --------------------------------------------

    struct Endpoint;
    struct Fs;
    struct Mount;
    struct Ask;
    struct Watcher;
    struct Events;
    struct Mark;

    // What an endpoint is to the loop.
    enum class Role : u8 {
        Handle,  // a handle of the VFS's, at a place: its calls come on it, and walk from the place
        Fs,      // ours of the channel of a file system: the answers to its questions come on it
        Watcher, // a watcher's open file: its READs come on it
        Kept,    // where a READ a watcher or an events file keeps is answered: its CANCEL comes on it
        Events,  // an events file's, inotify's: its READs come on it
    };

    // A file system of the VFS's, another process's behind the channel
    // rootfs asks it on.
    struct Fs {
        Endpoint* channel = nullptr; // none once it is gone
        u32 mounts = 0;              // it goes with its last
        bool cached = false;         // what it answers is kept: its ROOT did not say fsNocache
        u64 changes = 0;             // the calls handed on to it that may change it
        Ask* waiting = nullptr;      // the questions asked on its channel, to be answered in order
        Ask* lastWaiting = nullptr;
        Watcher* watchers = nullptr; // of its directories
        Mark* marks = nullptr;       // events files' of its directories
    };

    // A mount: a directory of a file system, its root, as a directory of
    // the VFS's tree on a node of another mount, the point, a name of a
    // directory there; the VFS's own root is on none. The point is the
    // VFS's: a walk takes its name into the root, and ".." out of the root
    // to the point's directory, without asking the file system under it.
    // The stats of the root, the point and its directory are as they were
    // when it was mounted.
    struct Mount {
        Fs* fs = nullptr;
        u64 root = 0;
        Stat rootStat{};
        Mount* parent = nullptr;
        u64 point = 0;
        Stat pointStat{};
        u64 pointDir = 0; // the directory of the point, and its name there
        Stat pointDirStat{};
        u8 pointLength = 0;
        char pointName[NAME_MAX];
        u32 refs = 1;          // the mount itself, and the handles and calls at it
        bool gone = false;     // unmounted: no walk goes into it
        Mount* next = nullptr; // among the mounts
    };

    // A place of the VFS's tree: a mount and a node of its file system, its
    // stat as the file system gave it, fresh when it came in an answer to
    // the call.
    struct Place {
        Mount* m = nullptr;
        u64 node = 0;
        Stat st{};
        bool fresh = false;
    };

    struct Endpoint {
        int fd;
        bool reads; // what its open lets it do
        bool writes;
        Role role;
        bool dead;       // closed, while a batch of events may still name it
        Place at;        // a Handle's
        u64 id;          // a Handle's
        u64 pos;         // a Handle's: where its READDIR goes on, an entry's pos
        bool append;     // a Handle's O_APPEND and O_NONBLOCK, the open's, then SETFL's
        bool nonblock;
        Fs* fs;           // a Fs's
        Watcher* watcher; // a Watcher's and a Kept's
        Events* events;   // an Events's and a Kept's
        Endpoint* next;   // among the free ones
        Endpoint* after;  // among the live ones
        Endpoint* before;
    };

    // A watcher of a directory (WATCH), the VFS's: the directory, a node
    // of a file system, and its mount, which it holds; its open file's
    // endpoint, none while it lists the directory; the records it has yet
    // to tell, kind, name, 0, in their order; whether it told watchIdle;
    // and a READ it keeps, where that is answered, its memfd and length.
    struct Watcher {
        Fs* fs = nullptr;
        u64 node = 0;
        Mount* m = nullptr;
        Endpoint* end = nullptr;
        Buffer log;
        Buffer added; // the names added while it lists, records as the log's
        bool listing = true;
        bool idle = false;
        bool gone = false; // the directory
        Endpoint* kept = nullptr;
        int memfd = -1;
        u32 length = 0;
        Watcher* next = nullptr; // among its file system's
    };

    // An events file, inotify's instance (WATCH of watchEventsNew): its
    // endpoint and id; the events it queued, struct inotify_event's, how
    // many, and where the last is; its marks and the number of the next;
    // a READ it keeps, as a watcher's; who made it and when, its FSTAT's.
    struct Events {
        Endpoint* end = nullptr;
        u64 id = 0;
        Buffer queue;
        u32 queued = 0;
        usize last = SIZE_MAX;
        Mark* marks = nullptr;
        i32 nextWd = 1;
        Endpoint* kept = nullptr;
        int memfd = -1;
        u32 length = 0;
        u32 uid = 0;
        u32 gid = 0;
        u64 made = 0;
        Events* next = nullptr; // among all
    };

    // A mark of an events file's: a directory, a node of a file system,
    // and its mount, which it holds; its number and mask, inotify's.
    struct Mark {
        Events* in = nullptr;
        i32 wd = 0;
        u32 mask = 0;
        Fs* fs = nullptr;
        u64 node = 0;
        Mount* m = nullptr;
        Mark* next = nullptr;    // among its file system's
        Mark* sibling = nullptr; // among its events file's
    };
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

// ---- packets ---------------------------------------------------------------------

// The packet recv takes and the descriptors that come with it: one of
// each for the process, whose loop takes a packet and is done with it
// before the next.
alignas(Header) static u8 packet[maxPacket];
static int packetFds[maxFds];
static usize packetFdCount;

// A reply: the header, then the parts where they are.
static void reply(int fd, u32 op, u32 arg, const iovec* parts, usize count, const int* fds = nullptr, usize fdCount = 0) {
    Header head{op, arg, 0, 0, 0, 0};
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

// A reply of an error: the call is done with its connection.
static bool error(int fd, int e) {
    reply(fd, opErr, u32(e), nullptr, 0);
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

// ---- the loop's endpoints -------------------------------------------------------
//
// One epoll of the process watches every endpoint: an endpoint joins it
// when it is listened on, leaves it before it closes. An event carries
// its endpoint.

static int watcher = -1;
static Endpoint* freeEndpoints;
static Endpoint* buried; // closed in this batch of events: free after it
static Endpoint* live;   // every endpoint not closed
static usize endpointCount;

// The signal mask of the wait, once reapOnChild blocked SIGCHLD
// everywhere else: a child's end that comes while the loop works waits
// for the wait, which it interrupts at once, instead of coming before
// it, unseen. The handler notes it: the loop reaps after a wait a
// child's end interrupted.
static sigset_t waitMask;
static bool waitMasked;
static volatile sig_atomic_t childEnded = 1;

// The handles, by id: their pairs', the kernel's, which no one else
// gives a descriptor of.
static OpenTable<Endpoint> handles;

static u64 pairId(int fd) {
    unsigned long long id = 0;
    if (fdid(fd, &id) != 0) {
        fatal("rootfs: fdid: errno %d", errno);
    }
    return id;
}

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

static void hold(const Place& p);
static void letGo(Mount* m);

// A handle of id at a place, on our end of its pair; O_PATH's reads
// nothing and changes nothing.
static Endpoint* listenHandle(int fd, const Place& p, int flags, u64 id) {
    const bool opath = (flags & O_PATH) != 0;
    Endpoint* ep = listen(fd, Role::Handle);
    ep->at = p;
    ep->at.fresh = false; // the stat is the open's, not a later call's
    ep->reads = !opath;
    ep->writes = !opath;
    ep->id = id;
    ep->append = (flags & O_APPEND) != 0;
    ep->nonblock = (flags & O_NONBLOCK) != 0;
    ++p.m->refs;
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

static void gone(Fs* fs);
static void forgetWatcher(Watcher* w);
static void forgetEvents(Events* in);

// An endpoint that closes: a handle lets go of its place, a file
// system's channel of the file system, a watcher's open file of the
// watcher, an events file's of the events file, and where a watcher's or
// an events file's READ is kept of the READ.
static void forget(Endpoint* ep) {
    ep->dead = true;
    switch (ep->role) {
        case Role::Handle:
            handles.erase(hashId(ep->id), ep);
            letGo(ep->at.m);
            break;
        case Role::Fs:
            if (ep->fs) {
                gone(ep->fs);
            }
            break;
        case Role::Watcher:
            ep->watcher->end = nullptr;
            forgetWatcher(ep->watcher);
            break;
        case Role::Kept:
            if (Watcher* w = ep->watcher; w && w->kept == ep) {
                w->kept = nullptr;
                close(w->memfd);
                w->memfd = -1;
            }
            if (Events* in = ep->events; in && in->kept == ep) {
                in->kept = nullptr;
                close(in->memfd);
                in->memfd = -1;
            }
            break;
        case Role::Events:
            ep->events->end = nullptr;
            forgetEvents(ep->events);
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

static Mount* mounts; // every mount, the VFS's root among them
static Mount* rootMount;

static bool isDir(const Place& p) {
    return S_ISDIR(p.st.mode);
}

static bool isLink(const Place& p) {
    return S_ISLNK(p.st.mode);
}

static bool same(const Place& a, const Place& b) {
    return a.m == b.m && a.node == b.node;
}

// Whether who asks may read (4), write (2), and search or execute (1)
// what a stat says: root everything but executing what nobody may, the
// owner what the owner's bits say, the group of gid the group's,
// everyone else the others'.
static bool mayStat(const Stat& st, const Header& h, u32 want) {
    if (h.uid == 0) {
        return (want & 1) == 0 || S_ISDIR(st.mode) || (st.mode & 0111) != 0;
    }
    const u32 bits = h.uid == st.uid ? st.mode >> 6 : h.gid == st.gid ? st.mode >> 3 : st.mode;
    return (bits & want) == want;
}

static bool mayAt(const Place& p, const Header& h, u32 want) {
    return mayStat(p.st, h, want);
}

// A place of a file system, as a stat of an answer says.
static Place theirPlace(Mount* m, const Stat& st) {
    Place p;
    p.m = m;
    p.node = st.ino;
    p.st = st;
    p.fresh = true;
    return p;
}

static Place rootOf(Mount* m) {
    Place p;
    p.m = m;
    p.node = m->root;
    p.st = m->rootStat;
    return p;
}

// Where a mount is mounted: its point, in its parent.
static Place pointOf(Mount* m) {
    Place p;
    p.m = m->parent;
    p.node = m->point;
    p.st = m->pointStat;
    return p;
}

// The directory of a mount's point, in its parent: where ".." goes out
// of the mount's root.
static Place dirOf(Mount* m) {
    Place p;
    p.m = m->parent;
    p.node = m->pointDir;
    p.st = m->pointDirStat;
    return p;
}

static bool isMountRoot(const Place& p) {
    return p.node == p.m->root;
}

// What is mounted on a place, the last mounted on it, or none.
static Mount* mountedOn(const Place& p) {
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

// The mount whose point is a name of a directory of a mount's, or none:
// a walk goes into its root, and a call on the name, which would take
// it away, is EBUSY.
static Mount* mountNamed(const Place& dir, Name name) {
    for (Mount* m = mounts; m; m = m->next) {
        if (!m->gone && m->parent == dir.m && m->pointDir == dir.node && m->pointLength == name.length && memcmp(m->pointName, name.at, name.length) == 0) {
            return m;
        }
    }
    return nullptr;
}

// A call or a handle at a place keeps its mount.
static void hold(const Place& p) {
    ++p.m->refs;
}

static void unhold(const Place& p) {
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

static void forgetAll(const Fs* fs);

// A mount nothing is at any more, unmounted: its memory goes, and its
// file system's with its last mount, which closes its channel, and what
// rootfs kept of it.
static void letGo(Mount* m) {
    if (--m->refs != 0) {
        return;
    }
    Fs* fs = m->fs;
    free(m);
    if (--fs->mounts == 0) {
        if (fs->channel) {
            closed(fs->channel);
        }
        forgetAll(fs);
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
    if (m->parent) {
        letGo(m->parent);
    }
    letGo(m);
}

// Mounts a directory of a file system, its root, on a place of the
// VFS's, point, the name pointName of the directory dir.
static Mount* mountAt(Fs* fs, const Place& root, const Place& point, const Place& dir, Name pointName) {
    auto* m = new (allocate(sizeof(Mount))) Mount();
    m->fs = fs;
    ++fs->mounts;
    m->root = root.node;
    m->rootStat = root.st;
    m->parent = point.m;
    ++point.m->refs;
    m->point = point.node;
    m->pointStat = point.st;
    m->pointDir = dir.node;
    m->pointDirStat = dir.st;
    m->pointLength = u8(pointName.length);
    memcpy(m->pointName, pointName.at, pointName.length);
    m->next = mounts;
    mounts = m;
    return m;
}

static void watchersGone(Fs* fs);

// A file system is gone, its channel closed: it is unmounted wherever
// it is mounted, the root's too, whose walks are EIO from then on,
// nothing of it is kept, and its directories' watchers end.
static void gone(Fs* fs) {
    fs->channel = nullptr;
    forgetAll(fs);
    watchersGone(fs);
    for (Mount* m = mounts; m;) {
        Mount* next = m->next;
        if (m->fs == fs && !m->gone) {
            unmount(m);
            next = mounts;
        }
        m = next;
    }
}

// ---- watchers ---------------------------------------------------------------------
//
// A directory's watchers are the VFS's (WATCH), as <sys/watchdir.h> says:
// a watcher tells the names there as it starts, watchIdle, then what was
// added and removed since its last READ, a name that came and went
// between two READs not at all; a READ with nothing to tell waits, kept,
// until there is, or until its CANCEL; its directory gone, its READs give
// what is left, then 0. rootfs makes the calls that change a file
// system's names, and asks one of a watched directory where it would
// hand it on, and every rmdir and rename of a file system it watches a
// directory of, which may take a watched directory away: the answer says
// what changed, and rootfs answers with it then. A file system that
// changes by itself tells it its changes instead (NOTICE), in order with
// its answers. A watcher starts by listing its directory, READDIR after
// READDIR, and what changes meanwhile, heard in the order of the answers,
// makes the names it starts with; then WATCH is answered.

// A record of a log: kind, the name, 0.
static void put(Buffer& log, u8 kind, Name name) {
    log.append(&kind, 1);
    log.append(name.at, name.length);
    log.append("", 1);
}

static Name recordName(const Buffer& log, usize at) {
    const char* name = static_cast<const char*>(log.data()) + at + 1;
    return {name, strlen(name)};
}

// Where the last record of a name is in a log, or its size: none.
static usize lastOf(const Buffer& log, Name name) {
    usize found = log.size();
    for (usize at = 0; at < log.size();) {
        const Name n = recordName(log, at);
        if (n.length == name.length && memcmp(n.at, name.at, name.length) == 0) {
            found = at;
        }
        at += n.length + 2;
    }
    return found;
}

static void cutRecord(Buffer& log, usize at) {
    char* bytes = static_cast<char*>(log.data());
    const usize length = recordName(log, at).length + 2;
    memmove(bytes + at, bytes + at + length, log.size() - at - length);
    log.resize(log.size() - length);
}

// Whether a watcher has something to tell: watchIdle, a change, or that
// its directory is gone.
static bool pending(const Watcher& w) {
    return !w.listing && (!w.idle || w.log.size() > 0 || w.gone);
}

static bool record(u8* to, usize room, usize& size, u8 kind, Name name) {
    if (room - size < name.length + 2) {
        return false;
    }
    to[size] = kind;
    if (name.length > 0) {
        memcpy(to + size + 1, name.at, name.length);
    }
    to[size + 1 + name.length] = 0;
    size += name.length + 2;
    return true;
}

// The records a watcher tells now, as many as room holds, which it then
// has told: its log in order, watchIdle after the names there as it
// started.
static usize records(Watcher& w, u8* to, usize room) {
    usize size = 0;
    usize told = 0;
    while (told < w.log.size()) {
        const u8 kind = static_cast<const u8*>(w.log.data())[told];
        const Name name = recordName(w.log, told);
        if (!w.idle && kind != watchExisting) {
            if (!record(to, room, size, watchIdle, {})) {
                break;
            }
            w.idle = true;
        }
        if (!record(to, room, size, kind, name)) {
            break;
        }
        told += name.length + 2;
    }
    if (told == w.log.size() && !w.idle && record(to, room, size, watchIdle, {})) {
        w.idle = true;
    }
    if (told > 0) {
        char* bytes = static_cast<char*>(w.log.data());
        memmove(bytes, bytes + told, w.log.size() - told);
        w.log.resize(w.log.size() - told);
    }
    return size;
}

// The records of a READ into its memfd, its count answered on fd: 0 of a
// directory gone, once all is told; none of a READ too short for the
// next record, EINVAL.
static void answerRecords(Watcher& w, int memfd, u32 length, int fd) {
    static u8 out[maxPacket - sizeof(Header)];
    const usize size = records(w, out, MIN(usize(length), sizeof out));
    if (size == 0 && (w.log.size() > 0 || !w.idle)) {
        error(fd, EINVAL);
        return;
    }
    if (size > 0 && pwrite(memfd, out, size, 0) != ssize_t(size)) {
        error(fd, EIO);
        return;
    }
    const u32 count = u32(size);
    ok(fd, &count, sizeof count);
}

// Tells the kernel whether a watcher is readable (READY).
static void ready(const Watcher& w) {
    if (w.end) {
        reply(w.end->fd, opReady, pending(w) ? POLLIN | (w.gone ? POLLHUP : 0) : 0, nullptr, 0);
    }
}

// What a watcher has to tell changed: a READ it keeps is answered.
static void told(Watcher& w) {
    if (w.kept && pending(w)) {
        answerRecords(w, w.memfd, w.length, w.kept->fd);
        closed(w.kept);
    }
    ready(w);
}

// A change a watcher hears, a name added or removed. While it lists its
// directory, what it starts with: what is there in the end, the names a
// READDIR finds after one was added not found again. Then a record, but
// for a name that came and went untold, whose record goes.
static void note(Watcher& w, u8 kind, Name name) {
    if (name.empty() || name.length > NAME_MAX) {
        return;
    }
    const usize at = lastOf(w.log, name);
    if (w.listing) {
        if (kind == watchAdded) {
            if (at == w.log.size()) {
                put(w.log, watchExisting, name);
            }
            put(w.added, watchAdded, name);
            return;
        }
        if (at < w.log.size()) {
            cutRecord(w.log, at);
        }
        if (const usize was = lastOf(w.added, name); was < w.added.size()) {
            cutRecord(w.added, was);
        }
        return;
    }
    if (kind == watchRemoved && at < w.log.size() && static_cast<const u8*>(w.log.data())[at] != watchRemoved) {
        cutRecord(w.log, at);
    } else {
        put(w.log, kind, name);
    }
    told(w);
}

// A name a watcher's listing found.
static void listed(Watcher& w, Name name) {
    if (!name.empty() && !name.is(".") && !name.is("..") && lastOf(w.added, name) == w.added.size()) {
        put(w.log, watchExisting, name);
    }
}

// ---- inotify's events -------------------------------------------------------------
//
// An events file is inotify's instance, the VFS's (WATCH of
// watchEventsNew), its marks of directories by number, wd: it queues the
// events of a mark's directory as struct inotify_event's, IN_CREATE and
// IN_DELETE of the names its watchers hear, IN_ISDIR of a directory's,
// IN_DELETE_SELF and then IN_IGNORED of the directory gone, IN_IGNORED of
// a mark taken away, as IN_ONESHOT's is after its event. An event the
// same as the last queued is not queued again, and of 16384 the last is
// IN_Q_OVERFLOW. A READ gives as many whole events as fit, EINVAL of
// none, or waits as a watcher's does; INQ is the bytes queued. A mark of
// a directory the file marks already changes its mask: EEXIST with
// IN_MASK_CREATE, more of it with IN_MASK_ADD. The file is named by its
// id, random, its FSTAT's ino.

constexpr u32 eventsMax = 16384;

static Events* allEvents;

static Events* eventsOf(u64 id) {
    for (Events* in = allEvents; in; in = in->next) {
        if (in->id == id) {
            return in;
        }
    }
    return nullptr;
}

static u32 eventField(const Events& in, usize at, usize field) {
    u32 value;
    memcpy(&value, static_cast<const u8*>(in.queue.data()) + at + field, sizeof value);
    return value;
}

static usize eventSize(const Events& in, usize at) {
    return 16 + eventField(in, at, 12);
}

static void queueEvent(Events& in, i32 wd, u32 mask, Name name) {
    u32 size = name.empty() ? 0 : u32((name.length + 1 + 15) & ~usize(15));
    if (in.last < in.queue.size()) {
        const char* last = static_cast<const char*>(in.queue.data()) + in.last + 16;
        if (eventField(in, in.last, 0) == u32(wd) && eventField(in, in.last, 4) == mask && eventField(in, in.last, 12) == size &&
            strnlen(last, size) == name.length && memcmp(last, name.at, name.length) == 0) {
            return;
        }
    }
    if (in.queued >= eventsMax) {
        return;
    }
    if (++in.queued == eventsMax) {
        wd = -1;
        mask = IN_Q_OVERFLOW;
        name = {};
        size = 0;
    }
    static const char zeros[16] = {};
    const u32 head[4] = {u32(wd), mask, 0, size};
    in.last = in.queue.size();
    in.queue.append(head, sizeof head);
    if (size > 0) {
        in.queue.append(name.at, name.length);
        in.queue.append(zeros, size - name.length);
    }
}

// Tells the kernel whether an events file is readable (READY).
static void eventsReady(const Events& in) {
    if (in.end) {
        reply(in.end->fd, opReady, in.queue.size() > 0 ? POLLIN : 0, nullptr, 0);
    }
}

// The events of a READ into its memfd, as many whole ones as length
// holds, its count answered on fd: none, EINVAL.
static void answerEvents(Events& in, int memfd, u32 length, int fd) {
    usize size = 0;
    u32 count = 0;
    while (size < in.queue.size() && size + eventSize(in, size) <= length) {
        size += eventSize(in, size);
        ++count;
    }
    if (size == 0) {
        error(fd, EINVAL);
        return;
    }
    if (pwrite(memfd, in.queue.data(), size, 0) != ssize_t(size)) {
        error(fd, EIO);
        return;
    }
    char* bytes = static_cast<char*>(in.queue.data());
    memmove(bytes, bytes + size, in.queue.size() - size);
    in.queue.resize(in.queue.size() - size);
    in.queued -= count;
    in.last = SIZE_MAX;
    for (usize at = 0; at < in.queue.size(); at += eventSize(in, at)) {
        in.last = at;
    }
    const u32 n = u32(size);
    ok(fd, &n, sizeof n);
}

// What an events file queued changed: a READ it keeps is answered.
static void eventsTold(Events& in) {
    if (in.kept && in.queue.size() > 0) {
        answerEvents(in, in.memfd, in.length, in.kept->fd);
        closed(in.kept);
    }
    eventsReady(in);
}

static void dropMark(Mark* k) {
    for (Mark** at = &k->fs->marks; *at; at = &(*at)->next) {
        if (*at == k) {
            *at = k->next;
            break;
        }
    }
    for (Mark** at = &k->in->marks; *at; at = &(*at)->sibling) {
        if (*at == k) {
            *at = k->sibling;
            break;
        }
    }
    Mount* m = k->m;
    k->~Mark();
    free(k);
    letGo(m);
}

// An event of a mark's directory, queued where its mask has it, and of
// IN_ONESHOT's the mark then taken away.
static void tellMark(Mark* k, u32 mask, Name name) {
    Events& in = *k->in;
    if ((k->mask & mask & IN_ALL_EVENTS) == 0) {
        return;
    }
    queueEvent(in, k->wd, mask, name);
    if ((k->mask & IN_ONESHOT) != 0) {
        queueEvent(in, k->wd, IN_IGNORED, {});
        dropMark(k);
    }
    eventsTold(in);
}

// A mark's directory gone, or its file system: IN_DELETE_SELF where its
// mask has it, IN_IGNORED, and the mark taken away.
static void markGone(Mark* k) {
    Events& in = *k->in;
    if ((k->mask & IN_DELETE_SELF) != 0) {
        queueEvent(in, k->wd, IN_DELETE_SELF, {});
    }
    queueEvent(in, k->wd, IN_IGNORED, {});
    dropMark(k);
    eventsTold(in);
}

static void marksGone(Fs* fs, u64 node, bool all) {
    for (Mark *k = fs->marks, *next; k; k = next) {
        next = k->next;
        if (all || k->node == node) {
            markGone(k);
        }
    }
}

// An events file whose endpoint closed goes, its marks with it.
static void forgetEvents(Events* in) {
    while (in->marks) {
        dropMark(in->marks);
    }
    if (in->kept) {
        closed(in->kept);
    }
    for (Events** at = &allEvents; *at; at = &(*at)->next) {
        if (*at == in) {
            *at = in->next;
            break;
        }
    }
    in->~Events();
    free(in);
}

// A WATCH of inotify's of a handle's directory, at, answered on fd.
static bool eventsCall(const Header& h, Body body, int fd, const Place& at) {
    if (body.size() < 4) {
        return error(fd, EINVAL);
    }
    const u32 how = body.take<u32>();
    if (how == watchEventsNew) {
        int pair[2];
        if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
            return error(fd, errno);
        }
        auto* in = new (allocate(sizeof(Events))) Events();
        while (in->id == 0 || eventsOf(in->id)) {
            getentropy(&in->id, sizeof in->id);
        }
        timespec now{};
        clock_gettime(CLOCK_REALTIME, &now);
        in->made = u64(now.tv_sec) * 1000000000 + u64(now.tv_nsec);
        in->uid = h.uid;
        in->gid = h.gid;
        in->next = allEvents;
        allEvents = in;
        in->end = listen(pair[0], Role::Events);
        in->end->events = in;
        in->end->reads = true;
        in->end->nonblock = (h.arg & O_NONBLOCK) != 0;
        const Opened what{openedFile, O_RDONLY, 0, in->id, 0, 0};
        const iovec said = part(&what, sizeof what);
        reply(fd, opOk, 0, &said, 1, &pair[1], 1);
        close(pair[1]);
        return false;
    }
    if ((how != watchEventsAdd && how != watchEventsRemove) || body.size() != 12) {
        return error(fd, EINVAL);
    }
    const u32 value = body.take<u32>();
    Events* in = eventsOf(body.take<u64>());
    if (!in) {
        return error(fd, EINVAL);
    }
    Fs* fs = at.m->fs;
    for (Mark* k = in->marks; k; k = k->sibling) {
        if (how == watchEventsRemove && k->wd == i32(value)) {
            queueEvent(*in, k->wd, IN_IGNORED, {});
            dropMark(k);
            eventsTold(*in);
            ok(fd);
            return false;
        }
        if (how == watchEventsAdd && k->fs == fs && k->node == at.node) {
            if ((value & IN_MASK_CREATE) != 0) {
                return error(fd, EEXIST);
            }
            k->mask = (value & IN_MASK_ADD) != 0 ? k->mask | (value & ~u32(IN_MASK_ADD)) : value;
            const u32 wd = u32(k->wd);
            ok(fd, &wd, sizeof wd);
            return false;
        }
    }
    if (how == watchEventsRemove) {
        return error(fd, EINVAL);
    }
    auto* k = new (allocate(sizeof(Mark))) Mark();
    k->in = in;
    k->wd = in->nextWd++;
    k->mask = value & ~u32(IN_MASK_ADD | IN_MASK_CREATE);
    k->fs = fs;
    k->node = at.node;
    k->m = at.m;
    hold(at);
    k->next = fs->marks;
    fs->marks = k;
    k->sibling = in->marks;
    in->marks = k;
    const u32 wd = u32(k->wd);
    ok(fd, &wd, sizeof wd);
    return false;
}

// A name of a directory of a file system's added or removed, a
// directory's or not, for the directory's watchers and marks.
static void heard(Fs* fs, u64 dir, u8 kind, Name name, bool directory) {
    for (Watcher* w = fs->watchers; w; w = w->next) {
        if (w->node == dir && !w->gone) {
            note(*w, kind, name);
        }
    }
    if (name.empty() || name.length > NAME_MAX) {
        return;
    }
    for (Mark *k = fs->marks, *next; k; k = next) {
        next = k->next;
        if (k->node == dir) {
            tellMark(k, (kind == watchAdded ? IN_CREATE : IN_DELETE) | (directory ? IN_ISDIR : 0), name);
        }
    }
}

// A directory gone: its watchers tell what is left, then their end, and
// its marks go. One of the number later is another.
static void dirGone(Fs* fs, u64 node) {
    for (Watcher* w = fs->watchers; w; w = w->next) {
        if (w->node == node && !w->gone) {
            w->gone = true;
            told(*w);
        }
    }
    marksGone(fs, node, false);
}

static void watchersGone(Fs* fs) {
    for (Watcher* w = fs->watchers; w; w = w->next) {
        if (!w->gone) {
            w->gone = true;
            told(*w);
        }
    }
    marksGone(fs, 0, true);
}

// A file system's NOTICE: a change of its names, for its watchers and
// marks.
static void noticed(Fs* fs, u32 kind, Body body) {
    if (body.size() < 8) {
        return;
    }
    const u64 node = body.take<u64>();
    const bool directory = (kind & noticeDirectory) != 0;
    kind &= ~u32(noticeDirectory);
    if (kind == noticeGone) {
        dirGone(fs, node);
    } else if (kind == watchAdded || kind == watchRemoved) {
        heard(fs, node, u8(kind), body.rest(), directory);
    }
}

// Whether rootfs asks the changes of a file system it watches or marks a
// directory of, to see their answers: none of one that tells its own.
static bool watching(const Fs* fs) {
    for (const Watcher* w = fs->watchers; w && fs->cached; w = w->next) {
        if (!w->gone) {
            return true;
        }
    }
    return fs->cached && fs->marks;
}

// Whether a directory's changes are asked, for its watchers and marks.
static bool watched(const Place& dir) {
    const Fs* fs = dir.m->fs;
    for (const Watcher* w = fs->watchers; w && fs->cached; w = w->next) {
        if (w->node == dir.node && !w->gone) {
            return true;
        }
    }
    for (const Mark* k = fs->marks; k && fs->cached; k = k->next) {
        if (k->node == dir.node) {
            return true;
        }
    }
    return false;
}

// A watcher whose open file closed, or whose WATCH failed, goes, and
// lets go of its mount.
static void forgetWatcher(Watcher* w) {
    if (w->kept) {
        closed(w->kept);
    }
    for (Watcher** at = &w->fs->watchers; *at; at = &(*at)->next) {
        if (*at == w) {
            *at = w->next;
            break;
        }
    }
    Mount* m = w->m;
    w->~Watcher();
    free(w);
    letGo(m);
}

// ---- the cache --------------------------------------------------------------------
//
// What the file systems answered, kept, as Linux's dcache keeps it: the
// names of directories, what each names or that it is not there
// (ENOENT), and of a node its stat and a link's target. A file system
// changes by the calls rootfs hands on to it, and by its open files,
// which the kernel calls itself; one that changes otherwise says
// fsNocache in its ROOT, and nothing of it is kept. So a name holds until
// rootfs hands on a call that may change it, and so does the stat of a
// directory or a link, which no open file changes: a directory's
// descriptor is a handle of rootfs's, a link opens as none. Of another
// node only the type holds, its open files change the rest. atime is
// none of rootfs's: the system is noatime.
//
// rootfs sees no reply to a call it hands on, only that the call may
// change something: it forgets what may change before it hands the call
// on, and keeps no answer to a question asked before a call handed on
// since, as a file system answers its channel in order. A change it asks
// instead, for watchers, it forgets of so too. Of errors only
// ENOENT is kept: the others are who asks's, and rootfs checks the
// search of a directory itself. A node's number may be its file
// system's again for another node once the node is gone, as ext4's are:
// what is kept of a directory goes with it, and rootfs learns what a
// call removes before it hands it on. The names kept are the newest
// maxEntries, and a node is kept while a name of it or in it is.

constexpr usize maxEntries = usize(1) << 15;

namespace {
    struct Known;

    // A name of a directory kept: what it names, none when it is not there.
    // Its bytes follow it.
    struct Entry {
        Known* dir;
        Known* node;
        Entry* next; // among the names of dir kept
        Entry* previous;
        Entry* newer; // among all the names kept, by when they were last used
        Entry* older;
        u32 hash;
        u32 length;
    };

    // A node kept: its stat as its file system last told it and what of it
    // holds, a link's target, the names kept of a directory.
    struct Known {
        Fs* fs = nullptr;
        u64 ino = 0;
        Stat st{};
        bool modes = false;  // its mode and owners hold
        bool whole = false;  // all of it holds, but atime
        bool linked = false; // target is the link's
        Buffer target;
        Entry* inside = nullptr;
        u32 refs = 0; // the names kept of it and in it
    };
}

static OpenTable<Known> knowns;  // by file system and node
static OpenTable<Entry> entries; // by directory and name
static Entry* newest;
static Entry* oldest;
static usize entryCount;

static u32 hashKnown(const Fs* fs, u64 ino) {
    return finish(u64(uintptr_t(fs)) * 0x9e3779b97f4a7c15ull ^ ino);
}

static u32 hashEntry(const Known* dir, Name name) {
    u64 h = u64(uintptr_t(dir)) * 0x9e3779b97f4a7c15ull;
    for (usize i = 0; i < name.length; ++i) {
        h = (h ^ u8(name.at[i])) * 0x100000001b3ull;
    }
    return finish(h);
}

// Whether a stat holds while rootfs hands on nothing that changes it:
// a directory's and a link's.
static bool lasting(const Stat& st) {
    return S_ISDIR(st.mode) || S_ISLNK(st.mode);
}

static Known* knownOf(const Fs* fs, u64 ino) {
    return knowns.find(hashKnown(fs, ino), [fs, ino](const Known* k) {
        return k->fs == fs && k->ino == ino;
    });
}

// The node of a place, kept, or none.
static Known* knownAt(const Place& p) {
    const Fs* fs = p.m->fs;
    return fs->cached ? knownOf(fs, p.node) : nullptr;
}

static Known* knownFor(Fs* fs, u64 ino) {
    Known* k = knownOf(fs, ino);
    if (!k) {
        k = new (allocate(sizeof(Known))) Known();
        k->fs = fs;
        k->ino = ino;
        knowns.insert(hashKnown(fs, ino), k);
    }
    return k;
}

static void release(Known* k) {
    if (--k->refs == 0) {
        knowns.erase(hashKnown(k->fs, k->ino), k);
        k->~Known();
        free(k);
    }
}

// A name kept goes, and a node kept for it alone.
static void drop(Entry* e) {
    entries.erase(e->hash, e);
    (e->previous ? e->previous->next : e->dir->inside) = e->next;
    if (e->next) {
        e->next->previous = e->previous;
    }
    (e->newer ? e->newer->older : newest) = e->older;
    (e->older ? e->older->newer : oldest) = e->newer;
    --entryCount;
    Known* dir = e->dir;
    Known* node = e->node;
    free(e);
    if (node) {
        release(node);
    }
    release(dir);
}

// A name of a directory kept, the newest used now.
static Entry* entryOf(Known* dir, Name name) {
    Entry* e = entries.find(hashEntry(dir, name), [dir, name](const Entry* x) {
        return x->dir == dir && x->length == name.length && memcmp(x + 1, name.at, name.length) == 0;
    });
    if (e && e != newest) {
        e->newer->older = e->older;
        (e->older ? e->older->newer : oldest) = e->newer;
        e->older = newest;
        e->newer = nullptr;
        newest->newer = e;
        newest = e;
    }
    return e;
}

// A name of a place's directory kept, or none.
static Entry* keptAt(const Place& dir, Name name) {
    Known* d = knownAt(dir);
    return d ? entryOf(d, name) : nullptr;
}

// A place of a node kept.
static Place keptPlace(Mount* m, const Known* k) {
    Place p;
    p.m = m;
    p.node = k->ino;
    p.st = k->st;
    return p;
}

// The names kept of a directory go: it goes, or another node may have
// its number.
static void forgetInside(Known* k) {
    ++k->refs;
    while (k->inside) {
        drop(k->inside);
    }
    release(k);
}

// A node's stat as an answer told it. A node of another kind than the
// one kept is another of the number: what was kept of the old goes.
static void learnInto(Known* k, const Stat& st) {
    if ((k->st.mode & S_IFMT) != 0 && (k->st.mode & S_IFMT) != (st.mode & S_IFMT)) {
        forgetInside(k);
        k->linked = false;
        k->target.clear();
    }
    k->st = st;
    k->modes = k->whole = lasting(st);
}

// The stat of a place's node, as an answer to a question asked since
// the last call handed on, current, told it.
static void learn(const Place& p, const Stat& st, bool current) {
    Known* k = current ? knownAt(p) : nullptr;
    if (k) {
        ++k->refs;
        learnInto(k, st);
        release(k);
    }
}

// Keeps what a name of a directory names, as an answer to a question
// asked since the last call handed on, current, told: its stat, or none
// when the name is not there.
static void remember(const Place& dir, Name name, const Stat* st, bool current) {
    Fs* fs = dir.m->fs;
    if (!fs->cached || !current || name.length > NAME_MAX) {
        return;
    }
    Known* d = knownFor(fs, dir.node);
    if ((d->st.mode & S_IFMT) == 0) {
        d->st = dir.st; // its kind, and nothing of it that holds yet
    }
    ++d->refs;
    Known* n = nullptr;
    if (st) {
        n = knownFor(fs, st->ino);
        ++n->refs;
        learnInto(n, *st);
    }
    if (Entry* old = entryOf(d, name)) {
        drop(old);
    }
    auto* e = static_cast<Entry*>(allocate(sizeof(Entry) + name.length));
    memcpy(e + 1, name.at, name.length);
    e->dir = d;
    e->node = n;
    e->hash = hashEntry(d, name);
    e->length = u32(name.length);
    e->previous = nullptr;
    e->next = d->inside;
    if (d->inside) {
        d->inside->previous = e;
    }
    d->inside = e;
    e->newer = nullptr;
    e->older = newest;
    (newest ? newest->newer : oldest) = e;
    newest = e;
    entries.insert(e->hash, e);
    if (++entryCount > maxEntries) {
        drop(oldest);
    }
}

// A link's target, as an answer to a question asked since the last
// call handed on, current, told it.
static void rememberTarget(const Place& link, Name target, bool current) {
    Known* k = current ? knownAt(link) : nullptr;
    if (k && S_ISLNK(k->st.mode)) {
        k->target.clear();
        k->target.append(target.at, target.length);
        k->linked = true;
    }
}

// A call handed on to a place's file system may change it: no answer
// to a question asked before is kept.
static void changing(const Place& p) {
    ++p.m->fs->changes;
}

// A name kept, forgotten.
static void forgetName(const Place& dir, Name name) {
    if (Entry* e = keptAt(dir, name)) {
        drop(e);
    }
}

// What of a node's stat a call may change: all but its mode and
// owners, or with modes those too.
static void forgetStat(Known* k, bool modes) {
    if (k) {
        k->whole = false;
        k->modes = k->modes && !modes;
    }
}

// A file system's names kept go: it is gone, or let go of.
static void forgetAll(const Fs* fs) {
    for (Entry* e = oldest; e;) {
        Entry* next = e->newer;
        if (e->dir->fs == fs) {
            drop(e);
        }
        e = next;
    }
}

// ---- coroutines ------------------------------------------------------------------

namespace {
    // A call of the VFS's that may wait for a file system: a
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
}

static Frames frames[32];

namespace {
    // What a file system answered: OK with its body, which is the packet's
    // until the call waits again, or the errno.
    struct Answer {
        int error = 0;
        Body body{nullptr, nullptr};
        bool current = false; // no call that may change the file system was handed on after the question
        int fd = -1;          // the open file it brings, of a question that takes it: the call's to close
    };

    // A question of a call's to a file system, which the call waits for: it
    // goes on the file system's channel with no endpoint, and the file
    // system answers it there, after the questions before it.
    struct Ask {
        bool await_ready() const noexcept;
        bool await_suspend(std::coroutine_handle<void> h);
        Answer await_resume() const noexcept;

        Fs* fs = nullptr; // whose channel a question goes on
        Header head{};
        u8 body[8 + NAME_MAX];
        usize size = 0;
        Name tail;          // what follows the body, the caller's while the question goes
        bool takes = false; // the answer's open file is the call's
        Answer answer;
        u64 changes = 0; // the file system's changes when the question went
        std::coroutine_handle<void> call;
        Ask* next = nullptr; // among the file system's questions
    };
}

// A question by node of a place's file system, with a name.
static Ask asking(const Place& p, const Header& h, u32 op, Name name) {
    Ask a;
    a.fs = p.m->fs;
    a.head = {op, 0, h.uid, h.gid, h.pgrp, h.sid};
    memcpy(a.body, &p.node, 8);
    memcpy(a.body + 8, name.at, MIN(name.length, usize(NAME_MAX)));
    a.size = 8 + MIN(name.length, usize(NAME_MAX));
    return a;
}

// A question of a change by node, which rootfs asks to see its answer:
// what follows the node, args, then names, which stay the caller's until
// the question goes.
static Ask changeAsk(const Place& p, const Header& h, u32 op, u32 arg, const void* args, usize size, Name names) {
    Ask a;
    a.fs = p.m->fs;
    a.head = {op, arg, h.uid, h.gid, h.pgrp, h.sid};
    memcpy(a.body, &p.node, 8);
    if (size > 0) {
        memcpy(a.body + 8, args, size);
    }
    a.size = 8 + size;
    a.tail = names;
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

// Whether a ROOT's answer says its file system is not to be kept.
static bool nocacheIn(const Answer& a) {
    u32 flags = 0;
    if (a.body.size() >= sizeof(Stat) + sizeof flags) {
        memcpy(&flags, a.body.at + sizeof(Stat), sizeof flags);
    }
    return (flags & fsNocache) != 0;
}

// What a name of a directory names as rootfs keeps it: whether it
// keeps the name, and then whether the name is there, and its stat.
static bool keptStat(const Place& dir, Name name, Stat& st, bool& there) {
    const Entry* e = keptAt(dir, name);
    if (!e) {
        return false;
    }
    there = e->node != nullptr;
    if (there) {
        st = e->node->st;
    }
    return true;
}

// A node of a stat a call removes or moves, forgotten before the call
// goes: its stat, and when it goes, a link's target, a directory's
// names kept, or a directory's ".." when it moves.
static void forgetNode(Fs* fs, const Stat& st, bool moves) {
    Known* k = fs->cached ? knownOf(fs, st.ino) : nullptr;
    if (!k) {
        return;
    }
    forgetStat(k, false);
    if (!S_ISDIR(st.mode)) {
        k->linked = k->linked && moves;
        return;
    }
    if (!moves) {
        forgetInside(k);
    } else if (Entry* e = entryOf(k, literal(".."))) {
        drop(e);
    }
}

// Whether a STATFS answer says its file system is read-only.
static bool readOnlyIn(const Answer& a) {
    Statfs st;
    if (a.error != 0 || a.body.size() < sizeof st) {
        return false;
    }
    memcpy(&st, a.body.at, sizeof st);
    return (st.flags & ST_RDONLY) != 0;
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
    const iovec parts[] = {part(&head, sizeof head), part(body, size), part(tail.at, tail.length)};
    Endpoint* channel = fs ? fs->channel : nullptr;
    call = h;
    if (!channel || sendPacket(channel->fd, parts, 3, nullptr, 0, MSG_DONTWAIT) < 0) {
        answer.error = channel && errno == EAGAIN ? EAGAIN : EIO;
        return false;
    }
    changes = fs->changes;
    if (fs->lastWaiting) {
        fs->lastWaiting->next = this;
    } else {
        fs->waiting = this;
    }
    fs->lastWaiting = this;
    return true;
}

Answer Ask::await_resume() const noexcept {
    return answer;
}

// ---- opens and calls handed on ---------------------------------------------------

// A handle's open, answered on fd: a new handle at the place, a pair
// whose first end is the handle's endpoint and whose second goes with the
// OK to who asked, a directory's or of what O_PATH opened that is no
// directory, its id its pair's, the kernel's. The place's dev and ino go
// with it, its file system's, which the kernel keeps a directory's locks
// by: none of a place whose file system has not said its stat yet.
static void openHandle(int fd, const Place& at, int flags) {
    const u64 ino = at.st.dev != 0 ? at.st.ino : 0;
    int pair[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        error(fd, errno);
        return;
    }
    const u64 id = pairId(pair[0]);
    const Opened what{u32(isDir(at) ? openedDir : openedPath), u32(flags) & (O_ACCMODE | O_PATH), at.st.dev, ino, POLLIN | POLLOUT};
    listenHandle(pair[0], at, flags, id);
    const iovec said = part(&what, sizeof what);
    reply(fd, opOk, 0, &said, 1, &pair[1], 1);
    close(pair[1]);
}

// A call handed on to a file system, with the connection it came on,
// which the file system answers on, the call in hand its own from then
// on: the node of a place, then what follows it in the call. EIO when
// the file system is gone, EAGAIN when it does not keep up.
static bool hand(const Place& p, const Header& h, u32 op, u32 arg, const void* args, usize size, Name name, int fd, int extra = -1) {
    const Header head{op, arg, h.uid, h.gid, h.pgrp, h.sid};
    const iovec parts[] = {part(&head, sizeof head), part(&p.node, 8), part(args, size), part(name.at, name.length)};
    const int fds[] = {extra, fd};
    Endpoint* channel = p.m->fs->channel;
    if (!channel || sendPacket(channel->fd, parts, 4, extra >= 0 ? fds : fds + 1, extra >= 0 ? 2 : 1, MSG_DONTWAIT) < 0) {
        return error(fd, channel && errno == EAGAIN ? EAGAIN : EIO);
    }
    return false;
}

// An open of a place, answered on the kernel's endpoint: a directory, and
// what O_PATH opens, is a handle of ours, who asks may read it; a socket
// opens nothing, ENXIO; else the file system opens it, answering on the
// kernel's endpoint itself.
static void openAt(const Place& at, int flags, const Header& h, int fd) {
    const bool opath = (flags & O_PATH) != 0;
    const u32 access = flags & O_ACCMODE;
    if (!opath && S_ISSOCK(at.st.mode)) {
        error(fd, ENXIO);
    } else if ((flags & O_DIRECTORY) != 0 && !isDir(at)) {
        error(fd, ENOTDIR);
    } else if (!opath && isDir(at) && access != O_WRONLY && !mayStat(at.st, h, 4)) {
        error(fd, EACCES);
    } else if (!opath && isDir(at) && (access != O_RDONLY || (flags & O_CREAT) != 0)) {
        error(fd, EISDIR);
    } else if (opath || isDir(at)) {
        openHandle(fd, at, flags);
    } else {
        hand(at, h, fsOpen, u32(flags), nullptr, 0, {}, fd);
    }
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

namespace {
    // A walk of a path as Linux walks one, to the directory of its last
    // name, here: every name but the last a directory who asks may search,
    // or a symbolic link, whose target takes its place, from the root of
    // the process when absolute, no more than maxLinks of them in all; "."
    // and ".."; the root of what is mounted on a name for the name, which
    // the mount knows by its name. With look, the last name is looked at
    // too, target, a link there followed when follow says or a slash ends
    // the path, which then wants a directory; with missing, a last name
    // that is not there is no error: found says. The target of ".", ".."
    // and the root alone is the directory itself. A path of "." alone
    // names where it starts, which nobody is asked about. A step its file
    // system answers is a question (Step::Ask), which the call asks, and
    // the walk goes on with the answer, which rootfs keeps; a step it kept
    // the answer of asks nobody.
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
            Here,       // here's stat, which a call handed on may have changed since it was kept
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
        Step current();
        Step through(const Known* link, bool lastOne);
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
}

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
    root.fresh = here.fresh = false; // their stats are older calls'
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
// root to where it is mounted, and on from there to the point's
// directory, which the mount keeps; else the parent, which the file
// system tells.
Walk::Step Walk::up() {
    Place out; // the directory of the point the walk went up to
    for (;;) {
        if (same(here, root)) {
            return Step::Go;
        }
        if (!isMountRoot(here) || !here.m->parent) {
            break;
        }
        out = dirOf(here.m);
        moveTo(here, pointOf(here.m));
    }
    if (isMountRoot(here)) {
        return Step::Go;
    }
    if (out.m) {
        moveTo(here, out);
        return Step::Go;
    }
    if (const Entry* e = keptAt(here, literal("..")); e && e->node) {
        moveTo(here, keptPlace(here.m, e->node));
        return Step::Go;
    }
    ask(fsLookup, here, literal(".."), Wait::Parent);
    return Step::Ask;
}

// A link the walk goes through, its target kept: the target takes its
// place.
Walk::Step Walk::through(const Known* link, bool lastOne) {
    const Name to{static_cast<const char*>(link->target.data()), link->target.size()};
    if (to.empty()) {
        return fail(ENOENT);
    }
    ++links;
    if (!follow(to, lastOne)) {
        return fail(ENAMETOOLONG);
    }
    return lastOne ? run() : Step::Go;
}

// Here as its file system has it: a directory's stat kept holds until a
// call handed on may have changed its mode or owners, then it is asked
// again. A directory nothing is kept of has nothing kept in it, and its
// file system answers every name of it, as it checks the search itself.
Walk::Step Walk::current() {
    if (here.fresh || !S_ISDIR(here.st.mode)) {
        return Step::Go;
    }
    const Known* k = knownAt(here);
    if (!k) {
        return Step::Go;
    }
    if (k->modes) {
        here.st = k->st;
        return Step::Go;
    }
    ask(fsGetattr, here, {}, Wait::Here);
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
    if (const Known* k = knownAt(next); k && k->linked) {
        return through(k, false);
    }
    ask(fsReadlink, next, {}, Wait::Target);
    return Step::Ask;
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
    if (Mount* m = mountNamed(here, last)) {
        return settleLast(rootOf(m));
    }
    if (const Entry* e = keptAt(here, last)) {
        if (e->node) {
            return settleLast(keptPlace(here.m, e->node));
        }
        if (!missing) {
            return fail(ENOENT);
        }
        found = false;
        return Step::Done;
    }
    ask(fsLookup, here, last, Wait::Last);
    return Step::Ask;
}

// What the last name names: what is mounted on it, and a link followed
// when the walk follows it.
Walk::Step Walk::settleLast(Place t) {
    t = down(t);
    if (isLink(t) && (followLast || slash)) {
        if (links >= maxLinks) {
            return fail(ELOOP);
        }
        if (const Known* k = knownAt(t); k && k->linked) {
            return through(k, true);
        }
        ask(fsReadlink, t, {}, Wait::LastTarget);
        return Step::Ask;
    }
    if (slash && !isDir(t)) {
        return fail(ENOTDIR);
    }
    moveTo(target, t);
    found = true;
    return Step::Done;
}

Walk::Step Walk::run() {
    if (const Step s = current(); s != Step::Go) {
        return s;
    }
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
        if (const Step s = current(); s != Step::Go) {
            return s;
        }
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
        if (Mount* m = mountNamed(here, n)) {
            if (const Step s = settle(rootOf(m)); s != Step::Go) {
                return s;
            }
            continue;
        }
        if (const Entry* e = keptAt(here, n)) {
            if (!e->node) {
                return fail(ENOENT);
            }
            if (const Step s = settle(keptPlace(here.m, e->node)); s != Step::Go) {
                return s;
            }
            continue;
        }
        ask(fsLookup, here, n, Wait::Child);
        return Step::Ask;
    }
}

// The answer to the walk's question, and the walk on from there.
Walk::Step Walk::answered(const Answer& a) {
    const Wait w = wait;
    wait = Wait::None;
    if (a.error != 0) {
        if (a.error == ENOENT && (w == Wait::Child || w == Wait::Last)) {
            remember(here, name, nullptr, a.current);
        }
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
            remember(here, name, &st, a.current);
            if (const Step s = settle(theirPlace(here.m, st)); s != Step::Go) {
                return s;
            }
            return run();
        case Wait::Parent:
            if (!statIn(a.body, st)) {
                return fail(EIO);
            }
            remember(here, literal(".."), &st, a.current);
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
            remember(here, name, &st, a.current);
            return settleLast(theirPlace(here.m, st));
        case Wait::Target:
        case Wait::LastTarget: {
            Body body = a.body;
            const Name to = body.rest();
            if (to.empty()) {
                return fail(ENOENT);
            }
            rememberTarget(asked, to, a.current);
            ++links;
            if (!follow(to, w == Wait::LastTarget)) {
                return fail(ENAMETOOLONG);
            }
            return run();
        }
        case Wait::Here:
            if (!statIn(a.body, st)) {
                return fail(EIO);
            }
            learn(here, st, a.current);
            here.st = st;
            here.fresh = true;
            return run();
        case Wait::None:
            break;
    }
    return fail(EIO);
}

// ---- the VFS's calls ---------------------------------------------------------------
//
// Each is a coroutine that takes the kernel's endpoint, which it closes
// when it is done: walks the path as a walk does, waiting for the file
// systems, and makes the last step as Linux makes it, by the file system
// the place is of, which answers the kernel itself. What a call needs of
// the request it copies before it first waits.

namespace {
    // Where a call is answered, the call's until it is done: its close is
    // the end of the answer.
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
}

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

namespace {
    // A place of a call's, held while the call holds it.
    struct Held {
        Held(const Place& p);
        ~Held();
        Held(const Held&) = delete;
        Held& operator=(const Held&) = delete;
        void become(const Place& p);

        Place at;
    };
}

// The answers of the changes rootfs asks for watchers, which it answers
// with: what changed, as the answer says, heard by the watchers first.

// CREATE's: the name added when the file system made the file, and the
// open file on to who asked.
static void created(const Place& dir, Name name, const Answer& a, int fd) {
    if (a.error != 0) {
        error(fd, a.error);
        return;
    }
    Opened what{};
    if (a.body.size() == sizeof what) {
        memcpy(&what, a.body.at, sizeof what);
        if (what.made != 0) {
            heard(dir.m->fs, dir.node, watchAdded, name, false);
        }
    }
    const iovec body = part(a.body.at, a.body.size());
    reply(fd, opOk, 0, &body, 1, a.fd >= 0 ? &a.fd : nullptr, a.fd >= 0 ? 1 : 0);
    if (a.fd >= 0) {
        close(a.fd);
    }
}

// UNLINK's: the name removed, a directory's of AT_REMOVEDIR, and a
// directory it was gone.
static void unlinked(const Place& dir, Name name, bool directory, const Answer& a) {
    if (a.error != 0) {
        return;
    }
    heard(dir.m->fs, dir.node, watchRemoved, name, directory);
    u64 removed = 0;
    if (a.body.size() >= sizeof removed) {
        memcpy(&removed, a.body.at, sizeof removed);
    }
    if (removed != 0) {
        dirGone(dir.m->fs, removed);
    }
}

// RENAME's: the old name removed and the new one added, a name it
// replaced removed first, and a directory it replaced gone; nothing of
// two names of one node. moved and replaced say which are directories.
static void renamed(const Place& from, Name old, const Place& to, Name name, bool moved, bool replaced, const Answer& a) {
    if (a.error != 0) {
        return;
    }
    u64 nodes[2] = {0, 0};
    if (a.body.size() >= sizeof nodes) {
        memcpy(nodes, a.body.at, sizeof nodes);
    }
    if (nodes[0] != 0 && nodes[0] == nodes[1]) {
        return;
    }
    Fs* fs = from.m->fs;
    heard(fs, from.node, watchRemoved, old, moved);
    if (nodes[1] != 0) {
        heard(fs, to.node, watchRemoved, name, replaced);
        dirGone(fs, nodes[1]);
    }
    heard(fs, to.node, watchAdded, name, moved);
}

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
        } else {
            openAt(w.target, flags, h, fd);
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
        changing(w.here);
        forgetName(w.here, w.last);
        forgetStat(knownAt(w.here), false);
        if (watched(w.here)) {
            Ask ask = changeAsk(w.here, h, fsCreate, u32(fl), &mode, sizeof mode, w.last);
            ask.takes = true;
            created(w.here, w.last, co_await ask, fd);
            co_return;
        }
        hand(w.here, h, fsCreate, u32(fl), &mode, sizeof mode, w.last, fd);
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
    openAt(w.target, fl, h, fd);
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
    if (t.fresh) {
        ok(fd, &t.st, sizeof t.st);
        co_return;
    }
    if (const Known* k = knownAt(t); k && k->whole) {
        ok(fd, &k->st, sizeof k->st);
        co_return;
    }
    const Answer a = co_await asking(t, h, fsGetattr, {});
    Stat st;
    if (a.error != 0) {
        error(fd, a.error);
    } else {
        if (statIn(a.body, st)) {
            learn(t, st, a.current);
        }
        ok(fd, a.body.at, a.body.size());
    }
}

// The target of the link t: known, or its file system's.
static void linkTarget(Header h, int fd, const Place& t) {
    if (const Known* k = knownAt(t); k && k->linked) {
        ok(fd, k->target.data(), k->target.size());
    } else {
        hand(t, h, fsReadlink, 0, nullptr, 0, {}, fd);
    }
}

// READLINK: a last link's target; what is no link is EINVAL, a slash
// after one follows it. With no path, the target of the handle's own
// link, as readlinkat reads a descriptor's: what is no link is ENOENT.
static Task readlinkCall(Header h, int fd, Place root, Place from, Name path) {
    Reply r{fd};
    if (path.empty()) {
        if (!isLink(from)) {
            error(fd, ENOENT);
        } else {
            linkTarget(h, fd, from);
        }
        co_return;
    }
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
    } else {
        linkTarget(h, fd, t);
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
    Stat st;
    bool there = false;
    const bool kept = keptStat(w.here, w.last, st, there);
    if (there) {
        error(fd, EEXIST);
        co_return;
    }
    if (w.slash && h.op != vfsMkdir) {
        if (kept) {
            error(fd, ENOENT);
            co_return;
        }
        const Answer a = co_await asking(w.here, h, fsLookup, w.last);
        error(fd, a.error == 0 ? EEXIST : ENOENT);
        co_return;
    }
    const Name to{static_cast<const char*>(link.data()), link.size()};
    changing(w.here);
    forgetName(w.here, w.last);
    forgetStat(knownAt(w.here), false);
    Buffer names;
    Name made = w.last;
    if (h.op == vfsSymlink) {
        names.append(to.at, to.length);
        names.append("", 1);
        names.append(w.last.at, w.last.length);
        made = {static_cast<const char*>(names.data()), names.size()};
    }
    const u32 op = h.op == vfsSymlink ? fsSymlink : h.op == vfsMkdir ? fsMkdir : fsMknod;
    const u32 arg = h.op == vfsSymlink ? 0 : h.arg;
    if (watched(w.here)) {
        const Answer a = co_await changeAsk(w.here, h, op, arg, nullptr, 0, made);
        if (a.error == 0) {
            heard(w.here.m->fs, w.here.node, watchAdded, w.last, op == fsMkdir);
        }
        answer(fd, a.error);
        co_return;
    }
    hand(w.here, h, op, arg, nullptr, 0, made, fd);
}

// UNLINK: the last name taken away, a directory's with AT_REMOVEDIR.
// "." is EINVAL to rmdir, ".." ENOTEMPTY, the root alone EBUSY, and all
// three EISDIR to unlink; a slash after a name that is no directory is
// ENOTDIR to unlink, EISDIR after one that is; a mount point is EBUSY,
// but on a read-only file system, whose EROFS Linux finds first.
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
        const Answer a = co_await asking(w.here, h, fsLookup, w.last);
        Stat st;
        error(fd, a.error != 0 ? a.error : !statIn(a.body, st) ? EIO : S_ISDIR(st.mode) ? EISDIR : ENOTDIR);
        co_return;
    }
    if (dir && mountNamed(w.here, w.last) && !readOnlyIn(co_await asking(w.here, h, fsStatfs, {}))) {
        error(fd, EBUSY);
        co_return;
    }
    // What goes, which rootfs forgets first: kept, or asked.
    Stat st;
    bool there = false;
    if (w.here.m->fs->cached && !keptStat(w.here, w.last, st, there)) {
        const Answer got = co_await asking(w.here, h, fsLookup, w.last);
        there = got.error == 0 && statIn(got.body, st);
    }
    changing(w.here);
    if (there) {
        forgetNode(w.here.m->fs, st, false);
    }
    forgetName(w.here, w.last);
    forgetStat(knownAt(w.here), false);
    if (watched(w.here) || (dir && watching(w.here.m->fs))) {
        const Answer a = co_await changeAsk(w.here, h, fsUnlink, h.arg, nullptr, 0, w.last);
        unlinked(w.here, w.last, (h.arg & AT_REMOVEDIR) != 0, a);
        answer(fd, a.error);
        co_return;
    }
    hand(w.here, h, fsUnlink, h.arg, nullptr, 0, w.last, fd);
}

// RENAME: the last name of one path moved to the last name of another,
// from the handle to: ".", ".." and the root alone are EBUSY, a slash
// after either is for a directory only, two mounts are EXDEV and a
// mount point EBUSY, but on a read-only file system, whose EROFS Linux
// finds first.
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
        const Answer got = co_await asking(a.here, h, fsLookup, a.last);
        Stat st;
        if (got.error != 0 || !statIn(got.body, st)) {
            error(fd, got.error != 0 ? got.error : EIO);
            co_return;
        }
        if (!S_ISDIR(st.mode)) {
            error(fd, ENOTDIR);
            co_return;
        }
    }
    if (a.here.m != b.here.m) {
        error(fd, EXDEV);
        co_return;
    }
    if ((mountNamed(a.here, a.last) || mountNamed(b.here, b.last)) && !readOnlyIn(co_await asking(a.here, h, fsStatfs, {}))) {
        error(fd, EBUSY);
        co_return;
    }
    // What moves and what it replaces, which rootfs forgets first: kept,
    // or asked.
    Stat st[2];
    bool there[2] = {false, false};
    const Place* dirs[] = {&a.here, &b.here};
    const Name lasts[] = {a.last, b.last};
    for (int i = 0; i < 2 && a.here.m->fs->cached; ++i) {
        if (!keptStat(*dirs[i], lasts[i], st[i], there[i])) {
            const Answer got = co_await asking(*dirs[i], h, fsLookup, lasts[i]);
            there[i] = got.error == 0 && statIn(got.body, st[i]);
        }
    }
    changing(a.here);
    for (int i = 0; i < 2; ++i) {
        if (there[i]) {
            forgetNode(a.here.m->fs, st[i], i == 0);
        }
        forgetName(*dirs[i], lasts[i]);
        forgetStat(knownAt(*dirs[i]), false);
    }
    Buffer names;
    names.append(a.last.at, a.last.length);
    names.append("", 1);
    names.append(b.last.at, b.last.length);
    const Name both{static_cast<const char*>(names.data()), names.size()};
    if (watching(a.here.m->fs)) {
        const Answer got = co_await changeAsk(a.here, h, fsRename, 0, &b.here.node, 8, both);
        renamed(a.here, a.last, b.here, b.last, there[0] && S_ISDIR(st[0].mode), there[1] && S_ISDIR(st[1].mode), got);
        answer(fd, got.error);
        co_return;
    }
    hand(a.here, h, fsRename, 0, &b.here.node, 8, both, fd);
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
        const Answer got = co_await asking(b.here, h, fsLookup, b.last);
        error(fd, got.error == 0 ? EEXIST : ENOENT);
        co_return;
    }
    if (a.target.m != b.here.m) {
        error(fd, readOnlyIn(co_await asking(b.here, h, fsStatfs, {})) ? EROFS : EXDEV);
        co_return;
    }
    changing(b.here);
    forgetName(b.here, b.last);
    forgetStat(knownAt(b.here), false);
    forgetStat(knownAt(a.target), false);
    if (watched(b.here)) {
        const Answer got = co_await changeAsk(a.target, h, fsLink, 0, &b.here.node, 8, b.last);
        if (got.error == 0) {
            heard(b.here.m->fs, b.here.node, watchAdded, b.last, false);
        }
        answer(fd, got.error);
        co_return;
    }
    hand(a.target, h, fsLink, 0, &b.here.node, 8, b.last, fd);
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
        changing(w.target);
        forgetStat(knownAt(w.target), op == fsChmod || op == fsChown);
        hand(w.target, h, op, h.arg, args.bytes, args.size, {}, fd);
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
    hand(w.target, h, fsStatfs, 0, nullptr, 0, {}, fd);
}

// PATH and REALPATH: the path from the root of a place, up through the
// places ".." goes to: in its file system as the file system says,
// then where its mount is mounted. A removed directory has none, nor one the root is
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
        // The file system says the place's path in it, and that of the
        // mount's root, or of the root of the process in this mount.
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
// with a connection the kernel made to a file system's socket, the
// root of that file system, which the connection's first question
// asks: the connection is its channel from then on. A target that is a
// mount's root, of what is mounted on it or of the VFS's, is EBUSY.
static Task mountCall(Header h, int fd, Place root, Place from, int conn, Name target, Name source) {
    Reply r{fd};
    Reply connection{conn};
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
    if (conn >= 0) {
        auto* fs = new (allocate(sizeof(Fs))) Fs();
        fs->channel = listen(conn, Role::Fs);
        fs->channel->fs = fs;
        connection.fd = -1; // the channel's now
        Ask ask;
        ask.fs = fs;
        ask.head = {fsRoot, 0, h.uid, h.gid, h.pgrp, h.sid};
        const Answer got = co_await ask;
        Stat st;
        if (got.error != 0 || !statIn(got.body, st)) {
            if (fs->channel) {
                closed(fs->channel);
            }
            free(fs);
            error(fd, got.error != 0 ? got.error : EIO);
            co_return;
        }
        fs->cached = !nocacheIn(got);
        Place top;
        top.node = st.ino;
        top.st = st;
        mountAt(fs, top, t.target, t.here, t.last);
        ok(fd);
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
    if (!isDir(src)) {
        error(fd, ENOTDIR);
        co_return;
    }
    mountAt(src.m->fs, src, t.target, t.here, t.last);
    ok(fd);
}

// FSTAT of a handle: the stat of its place, which its file system
// tells.
static Task fstatCall(Header h, int fd, Place at) {
    Reply r{fd};
    Held here(at);
    if (const Known* k = knownAt(here.at); k && k->whole) {
        ok(fd, &k->st, sizeof k->st);
        co_return;
    }
    const Answer a = co_await asking(here.at, h, fsGetattr, {});
    Stat st;
    if (a.error != 0 || !statIn(a.body, st)) {
        error(fd, a.error != 0 ? a.error : EIO);
    } else {
        learn(here.at, st, a.current);
        ok(fd, &st, sizeof st);
    }
}

// READDIR of a directory's handle of id: the file system's entries after
// the handle's position, after, as many as the room holds; the position
// moves to the last one's.
static Task readdirCall(Header h, int fd, Place at, u64 id, u64 after) {
    Reply r{fd};
    Held here(at);
    Ask ask = asking(here.at, h, fsReaddir, {reinterpret_cast<const char*>(&after), sizeof after});
    ask.head.arg = h.arg;
    const Answer a = co_await ask;
    if (a.error != 0) {
        error(fd, a.error);
        co_return;
    }
    u64 last = after;
    for (const u8* entry = a.body.at; a.body.end - entry > 17;) {
        const void* nul = memchr(entry + 17, 0, usize(a.body.end - entry - 17));
        if (!nul) {
            break;
        }
        memcpy(&last, entry + 8, sizeof last);
        entry = static_cast<const u8*>(nul) + 1;
    }
    if (Endpoint* ep = handleOf(id)) {
        ep->pos = last;
    }
    ok(fd, a.body.at, a.body.size());
}

// WATCH of a handle's directory: a watcher, which lists the directory
// first, READDIR after READDIR, a change that comes meanwhile heard in
// the order of the answers, and is then answered: an open file of the
// flags asked, O_NONBLOCK of them, whose READs give its records. A
// directory gone while it lists ends it at once.
static Task watchCall(Header h, int fd, Place at) {
    Reply r{fd};
    Fs* fs = at.m->fs;
    auto* w = new (allocate(sizeof(Watcher))) Watcher();
    w->fs = fs;
    w->node = at.node;
    w->m = at.m;
    hold(at);
    w->next = fs->watchers;
    fs->watchers = w;
    for (u64 after = 0;;) {
        Ask ask = asking(at, h, fsReaddir, {reinterpret_cast<const char*>(&after), sizeof after});
        ask.head.arg = u32(maxPacket);
        const Answer a = co_await ask;
        if (a.error != 0 && !w->gone) {
            forgetWatcher(w);
            error(fd, a.error);
            co_return;
        }
        bool any = false;
        for (const u8* entry = a.body.at; a.error == 0 && a.body.end - entry > 17;) {
            const auto* nul = static_cast<const u8*>(memchr(entry + 17, 0, usize(a.body.end - entry - 17)));
            if (!nul) {
                break;
            }
            memcpy(&after, entry + 8, sizeof after);
            listed(*w, {reinterpret_cast<const char*>(entry + 17), usize(nul - entry - 17)});
            entry = nul + 1;
            any = true;
        }
        if (!any) {
            break;
        }
    }
    w->listing = false;
    w->added.clear();
    int pair[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        const int e = errno;
        forgetWatcher(w);
        error(fd, e);
        co_return;
    }
    w->end = listen(pair[0], Role::Watcher);
    w->end->watcher = w;
    w->end->reads = true;
    w->end->nonblock = (h.arg & O_NONBLOCK) != 0;
    const Opened what{openedFile, O_RDONLY, at.st.dev, 0, u32(POLLIN | (w->gone ? POLLHUP : 0)), 0};
    const iovec said = part(&what, sizeof what);
    reply(fd, opOk, 0, &said, 1, &pair[1], 1);
    close(pair[1]);
}

// SEEK of a handle: a directory's position is an entry's, READDIR's; its
// end the size its stat has. What O_PATH opens has none.
static bool seekCall(Endpoint* ep, const Header& h, Body body, int fd) {
    if (!ep->reads) {
        return error(fd, EBADF);
    }
    if (body.size() != 8) {
        return error(fd, EINVAL);
    }
    const i64 off = body.take<i64>();
    const i64 from = h.arg == SEEK_SET ? 0 : h.arg == SEEK_CUR ? i64(ep->pos) : h.arg == SEEK_END ? i64(ep->at.st.size) : -1;
    if (from < 0) {
        return error(fd, EINVAL);
    }
    if (off > 0 && from > INT64_MAX - off) {
        return error(fd, EOVERFLOW);
    }
    if (from + off < 0) {
        return error(fd, EINVAL);
    }
    ep->pos = u64(from + off);
    ok(fd, &ep->pos, sizeof ep->pos);
    return false;
}

// GETFL and SETFL of a handle: O_RDONLY, or O_PATH, and what it keeps.
static bool flagsCall(Endpoint* ep, const Header& h, int fd) {
    if (h.op == opSetfl) {
        ep->append = (h.arg & O_APPEND) != 0;
        ep->nonblock = (h.arg & O_NONBLOCK) != 0;
        return answer(fd, 0);
    }
    const u32 flags = (ep->reads ? O_RDONLY : O_PATH) | (ep->append ? O_APPEND : 0) | (ep->nonblock ? O_NONBLOCK : 0);
    ok(fd, &flags, sizeof flags);
    return false;
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

// ---- the loop ---------------------------------------------------------------

// An open file's call on a handle: FSTAT rootfs answers, with the
// handle's id, and SEEK, GETFL and SETFL of the position and the flags
// the handle keeps; READDIR asks the file system for the entries after
// the position; WATCH makes a watcher of rootfs's; the others are handed
// on to the file system its place is of. O_PATH's handle reads, changes and locks nothing: EBADF; a
// handle locks nothing, EINVAL; a directory reads by READDIR, not READ,
// EISDIR; a call no handle serves fails as the protocol has it.
static bool onPlace(Endpoint* ep, const Header& h, Body body, int fd) {
    const Place& at = ep->at;
    const bool opath = !ep->reads;
    switch (h.op) {
        case opFstat:
            fstatCall(h, fd, at);
            return true;
        case opFstatfs:
            return hand(at, h, fsStatfs, 0, nullptr, 0, {}, fd);
        case opReaddir:
            if (opath) {
                return error(fd, EBADF);
            }
            if (!isDir(at)) {
                return error(fd, ENOTDIR);
            }
            readdirCall(h, fd, at, ep->id, ep->pos);
            return true;
        case opWatch:
            if (opath) {
                return error(fd, EBADF);
            }
            if (!isDir(at)) {
                return error(fd, ENOTDIR);
            }
            if (body.size() > 0) {
                return eventsCall(h, body, fd, at);
            }
            watchCall(h, fd, at);
            return true;
        case opSeek:
            return seekCall(ep, h, body, fd);
        case opGetfl:
        case opSetfl:
            return flagsCall(ep, h, fd);
        case opFsync:
            return opath ? error(fd, EBADF) : answer(fd, 0);
        case opFtruncate:
            return error(fd, opath ? EBADF : EINVAL);
        case opRead:
            return error(fd, opath ? EBADF : EISDIR);
        case opFchmod:
        case opFchown:
        case opFutimes:
            if (opath) {
                return error(fd, EBADF);
            }
            changing(at);
            forgetStat(knownAt(at), h.op != opFutimes);
            return hand(at, h, h.op == opFchmod ? fsChmod : h.op == opFchown ? fsChown : fsUtimes, h.arg, body.at, body.size(), {}, fd);
        default:
            return error(fd, unserved(h.op));
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
            const int conn = packetFdCount == 2 ? packetFds[0] : -1;
            if (conn >= 0) {
                packetFds[0] = -1; // the call's now
            }
            mountCall(h, fd, root, from, conn, target, body.rest());
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

// A file system's channel closes, the file system gone or let go of:
// its questions are EIO, and their calls go on once it is closed.
static void ended(Endpoint* ep) {
    Ask* left = nullptr;
    if (Fs* fs = ep->fs) {
        left = fs->waiting;
        fs->waiting = fs->lastWaiting = nullptr;
    }
    closed(ep);
    while (Ask* a = left) {
        left = a->next;
        a->answer.error = EIO;
        a->call.resume();
    }
}

// The answer to a question on a file system's channel, its first, the
// packet of n bytes a turn took: the call goes on, the open file it
// brings the call's when it takes one. What nobody asked is dropped. A
// NOTICE answers nothing: its change goes to the watchers.
static void answeredOn(Endpoint* ep, ssize_t n) {
    Fs* fs = ep->fs;
    Header h{};
    if (n >= ssize_t(sizeof h)) {
        memcpy(&h, packet, sizeof h);
    }
    const bool notice = n >= ssize_t(sizeof h) && h.op == fsNotice;
    Ask* a = fs && n > 0 && !notice ? fs->waiting : nullptr;
    int taken = -1;
    if (a && a->takes && h.op == opOk && packetFdCount > 0) {
        taken = packetFds[0];
        packetFds[0] = -1;
    }
    for (usize i = 0; i < packetFdCount; ++i) {
        if (packetFds[i] >= 0) {
            close(packetFds[i]);
        }
    }
    if (n <= 0) {
        ended(ep);
        return;
    }
    if (notice && fs) {
        noticed(fs, h.arg, {packet + sizeof h, packet + n});
    }
    if (!a) {
        return;
    }
    fs->waiting = a->next;
    if (!fs->waiting) {
        fs->lastWaiting = nullptr;
    }
    a->answer.fd = taken;
    a->answer.current = fs->changes == a->changes;
    if (n < ssize_t(sizeof h) || (h.op != opOk && h.op != opErr)) {
        a->answer.error = EIO;
    } else if (h.op == opErr) {
        a->answer.error = h.arg != 0 ? int(h.arg) : EIO;
    } else {
        a->answer.body = {packet + sizeof h, packet + n};
    }
    a->call.resume();
}

// A request on a watcher's open file, answered on fd: READ, its records,
// or, with none, the READ kept until there are, but of O_NONBLOCK, one at
// a time; GETFL and SETFL of its flags; what else an open file takes,
// as the protocol has it of what is none of it. Whether fd is kept.
static bool onWatcher(Endpoint* ep, const Header& h, Body body, int fd) {
    Watcher& w = *ep->watcher;
    switch (h.op) {
        case opRead: {
            Io io;
            if (packetFdCount < 2 || body.size() != sizeof io) {
                return error(fd, EINVAL);
            }
            memcpy(&io, body.at, sizeof io);
            if ((io.flags & ioAt) != 0) {
                return error(fd, ESPIPE);
            }
            if (pending(w)) {
                answerRecords(w, packetFds[0], h.arg, fd);
                ready(w);
                return false;
            }
            if (ep->nonblock) {
                return error(fd, EAGAIN);
            }
            if (w.kept) {
                return error(fd, EBUSY);
            }
            w.kept = listen(fd, Role::Kept);
            w.kept->watcher = &w;
            w.memfd = packetFds[0];
            packetFds[0] = -1;
            w.length = h.arg;
            return true;
        }
        case opGetfl: {
            const u32 flags = O_RDONLY | (ep->nonblock ? O_NONBLOCK : 0);
            ok(fd, &flags, sizeof flags);
            return false;
        }
        case opSetfl:
            ep->nonblock = (h.arg & O_NONBLOCK) != 0;
            ok(fd);
            return false;
        default:
            return error(fd, unserved(h.op));
    }
}

// A request on an events file, answered on fd: READ, its events, or,
// with none, the READ kept until there are, but of O_NONBLOCK, one at a
// time; INQ, the bytes queued; FSTAT, of its id and who made it when;
// GETFL and SETFL of its flags; WRITE, EBADF; what else an open file
// takes, as the protocol has it of what is none of it. Whether fd is
// kept.
static bool onEvents(Endpoint* ep, const Header& h, Body body, int fd) {
    Events& in = *ep->events;
    switch (h.op) {
        case opRead: {
            Io io;
            if (packetFdCount < 2 || body.size() != sizeof io) {
                return error(fd, EINVAL);
            }
            memcpy(&io, body.at, sizeof io);
            if ((io.flags & ioAt) != 0) {
                return error(fd, ESPIPE);
            }
            if (in.queue.size() > 0) {
                answerEvents(in, packetFds[0], h.arg, fd);
                eventsReady(in);
                return false;
            }
            if (ep->nonblock) {
                return error(fd, EAGAIN);
            }
            if (in.kept) {
                return error(fd, EBUSY);
            }
            in.kept = listen(fd, Role::Kept);
            in.kept->events = &in;
            in.memfd = packetFds[0];
            packetFds[0] = -1;
            in.length = h.arg;
            return true;
        }
        case opWrite:
            return error(fd, EBADF);
        case opInq: {
            const u32 n = u32(in.queue.size());
            ok(fd, &n, sizeof n);
            return false;
        }
        case opFstat: {
            const Stat st{in.id, 0, in.made, 0600, 1, in.uid, in.gid, in.made, 0, in.made};
            ok(fd, &st, sizeof st);
            return false;
        }
        case opGetfl: {
            const u32 flags = O_RDONLY | (ep->nonblock ? O_NONBLOCK : 0);
            ok(fd, &flags, sizeof flags);
            return false;
        }
        case opSetfl:
            ep->nonblock = (h.arg & O_NONBLOCK) != 0;
            ok(fd);
            return false;
        default:
            return error(fd, unserved(h.op));
    }
}

// What comes where a watcher's or an events file's READ is kept: its
// CANCEL, answered EINTR, or the end of who asked; the READ is done.
static void keptHeard(Endpoint* ep, ssize_t n) {
    for (usize i = 0; i < packetFdCount; ++i) {
        close(packetFds[i]);
    }
    Header h{};
    if (n >= ssize_t(sizeof h)) {
        memcpy(&h, packet, sizeof h);
    }
    if (n >= ssize_t(sizeof h) && h.op == opCancel) {
        error(ep->fd, EINTR);
    }
    closed(ep);
}

// What a turn took on an endpoint: a packet of n bytes in packet, its
// descriptors in packetFds, or the end of the endpoint, n 0.
static void handle(Endpoint* ep, ssize_t n) {
    if (ep->dead) {
        for (usize i = 0; i < packetFdCount; ++i) {
            close(packetFds[i]);
        }
        return;
    }
    if (ep->role == Role::Fs) {
        answeredOn(ep, n);
        return;
    }
    if (ep->role == Role::Kept) {
        keptHeard(ep, n);
        return;
    }
    if (n <= 0) {
        closed(ep);
        return;
    }
    if (usize(n) < sizeof(Header)) {
        fatal("a packet without a header");
    }
    Header h;
    memcpy(&h, packet, sizeof h);
    if (h.op != opOk && h.op != opErr && packetFdCount > 0) {
        // A call of the handle's, answered where the last of its
        // descriptors says, by us or by the file system the call is
        // handed on to: the call's until it is done, at once or when what
        // it waits for came, and closed then, which ends the answer.
        const Body body{packet + sizeof(Header), packet + n};
        const usize last = packetFdCount - 1;
        const bool kept = ep->role == Role::Watcher  ? onWatcher(ep, h, body, packetFds[last])
                          : ep->role == Role::Events ? onEvents(ep, h, body, packetFds[last])
                                                     : onHandle(ep, h, body, packetFds[last]);
        if (kept) {
            packetFds[last] = -1;
        }
    }
    // The request's descriptors are done with once it is answered; one a
    // call kept is -1.
    for (usize i = 0; i < packetFdCount; ++i) {
        if (packetFds[i] >= 0) {
            close(packetFds[i]);
        }
    }
}

static pid_t shellPid;
static int shellStatus = -1;

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

static int bootEnd = -1;    // ours of the pair the services close once they are up
static Endpoint bootMarker; // what the events of bootEnd carry

static void booted();

// A turn of the loop: the next packet of an endpoint comes into packet,
// its descriptors into packetFds, the endpoint's event into event. The
// packet's length, 0 for an endpoint that ended; -1 when none came, for a
// signal or the time.
static ssize_t turn(epoll_event& event, int timeout) {
    alignas(cmsghdr) static unsigned char control[CMSG_SPACE(maxFds * sizeof(int))];
    packetFdCount = 0;
    if (epoll_pwait(watcher, &event, 1, timeout, waitMasked ? &waitMask : nullptr) <= 0) {
        return -1;
    }
    const Endpoint* ep = static_cast<const Endpoint*>(event.data.ptr);
    if (ep != &bootMarker && ep->dead) {
        return -1;
    }
    iovec in{packet, sizeof packet};
    msghdr request{};
    request.msg_iov = &in;
    request.msg_iovlen = 1;
    request.msg_control = control;
    request.msg_controllen = sizeof control;
    const ssize_t n = recvmsg(ep == &bootMarker ? bootEnd : ep->fd, &request, MSG_DONTWAIT | MSG_CMSG_CLOEXEC);
    if (n < 0) {
        return n;
    }
    for (cmsghdr* c = CMSG_FIRSTHDR(&request); c; c = CMSG_NXTHDR(&request, c)) {
        if (c->cmsg_level != SOL_SOCKET || c->cmsg_type != SCM_RIGHTS || c->cmsg_len < CMSG_LEN(0)) {
            continue;
        }
        const usize count = (c->cmsg_len - CMSG_LEN(0)) / sizeof(int);
        for (usize i = 0; i < count && packetFdCount < maxFds; ++i) {
            memcpy(&packetFds[packetFdCount++], CMSG_DATA(c) + i * sizeof(int), sizeof(int));
        }
    }
    return n;
}

// Serves the VFS until the last endpoint closes, when nobody can reach
// it, or the shell ends: then the system goes down, every other process,
// the services' too, killed. The end of bootEnd, no packet on it, is the
// services' being up. An endpoint closed while a batch of events is
// handled stays dead to the rest of the batch.
static void serve() {
    while (endpointCount > 0) {
        epoll_event event{};
        const ssize_t n = turn(event, -1);
        if (!reap()) {
            kill(-1, SIGKILL);
            break;
        }
        if (n >= 0 && event.data.ptr == &bootMarker) {
            if (n == 0) {
                booted();
            }
            for (usize i = 0; i < packetFdCount; ++i) {
                close(packetFds[i]);
            }
        } else if (n >= 0) {
            handle(static_cast<Endpoint*>(event.data.ptr), n);
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

// The most descriptors a child gets: the end of boot's pair, a file
// system's channel, and the disks.
constexpr int maxKept = 2 + 32;

// A program run in a child whose root and working directory are root,
// a directory over an end of a pair of ours (attach): the child asks us,
// who serve when it does;
// with no root, rootfs's own, which has no path. The descriptors kept
// are the child's 3 and on, in order. The program is the file path
// names, or the one of the descriptor program. A session's leads one of
// its own, as the shell does, whose signals to its group are none of
// the system's, its group the console's foreground one.
static pid_t spawn(const char* path, char* const* argv, char* const* env, int root, const int* kept = nullptr, int count = 0, int program = -1,
                   bool session = false) {
    const pid_t pid = fork();
    if (pid == 0) {
        if (waitMasked) {
            sigprocmask(SIG_SETMASK, &waitMask, nullptr);
        }
        if (session && setsid() < 0) {
            dprintf(STDERR_FILENO, "rootfs: setsid: errno %d\n", errno);
            _exit(127);
        }
        if (session) {
            // The console's foreground group: what the host's terminal
            // makes of a ^C goes to the session's.
            tcsetpgrp(STDIN_FILENO, getpid());
        }
        if (root >= 0 && (fchdir(root) != 0 || chroot(".") != 0)) {
            dprintf(STDERR_FILENO, "rootfs: chroot: errno %d\n", errno);
            _exit(127);
        }
        if (root >= 0) {
            close(root);
        }
        int high[maxKept];
        for (int i = 0; i < count; ++i) {
            high[i] = fcntl(kept[i], F_DUPFD_CLOEXEC, 64);
        }
        const int file = program >= 0 ? fcntl(program, F_DUPFD_CLOEXEC, 64) : -1;
        for (int i = 0; i < count; ++i) {
            if (high[i] < 0 || dup2(high[i], 3 + i) < 0) {
                dprintf(STDERR_FILENO, "rootfs: descriptor %d: errno %d\n", 3 + i, errno);
                _exit(127);
            }
        }
        if (program >= 0) {
            fexecve(file, argv, env);
        } else {
            execve(path, argv, env);
        }
        dprintf(STDERR_FILENO, "rootfs: %s: errno %d\n", path, errno);
        _exit(127);
    }
    if (pid < 0) {
        fatal("rootfs: fork: errno %d", errno);
    }
    return pid;
}

// What rootfs starts runs with a clean environment, its root a directory
// over the far end of our own pair, never ours: a server does not walk its own tree
// by path, and could not ask itself before it serves. erofs has none.
// HOME and TMPDIR are /run, where a program may write what it keeps.
// TERM is the console's type (consoleType).
static char pathVariable[] = "PATH=/bin";
static char homeVariable[] = "HOME=/run";
static char tmpVariable[] = "TMPDIR=/run";
static char* environment[] = {pathVariable, homeVariable, tmpVariable, nullptr, nullptr};
static int rootEnd = -1;

// The console is a terminal of the host's, and of what type only the
// host says: TERM as vm gave it to us is TERM for what we start, and
// with none given there is none.
static void consoleType() {
    const char* type = getenv("TERM");
    if (type == nullptr) {
        return;
    }
    const usize size = strlen(type) + sizeof "TERM=";
    char* variable = static_cast<char*>(allocate(size));
    snprintf(variable, size, "TERM=%s", type);
    environment[3] = variable;
}

// A file system rootfs starts: its channel, ours, on the loop.
static Fs* started(int channel) {
    auto* fs = new (allocate(sizeof(Fs))) Fs();
    fs->channel = listen(channel, Role::Fs);
    fs->channel->fs = fs;
    return fs;
}

// Mounts the root of a file system rootfs started on a directory of the
// root's, point, named name there: the file system's channel ours, its
// root its node 1, a directory of root's of mode perm as it starts,
// until its own stat says more.
static Mount* mountStarted(int channel, const Stat& point, Name name, u32 perm) {
    Place top;
    top.node = 1;
    top.st.ino = 1;
    top.st.mode = S_IFDIR | perm;
    top.st.nlink = 2;
    return mountAt(started(channel), top, theirPlace(rootMount, point), rootOf(rootMount), name);
}

// The image's header, of size bytes: where erofs's program is in it, the
// file of /bin/tools, its offset and size.
static void serverOf(const u32* header, u32 imageSize, u32& offset, u32& size) {
    if (memcmp(header, "PZIM", 4) != 0 || header[1] != 2 || header[8] != imageSize || header[9] > imageSize || header[10] == 0 || header[10] > imageSize - header[9]) {
        fatal("rootfs: the image is none of image.py's");
    }
    offset = header[9];
    size = header[10];
}

// A memfd of erofs's program in the image, disk, the disk vm gave us
// named image: the program read into memory that stays, as a memfd is of
// the memory itself.
static int serverIn(int disk) {
    if (disk < 0) {
        fatal("rootfs: no image: vm gave no disk named image, and nothing follows rootfs in its file");
    }
    u32 header[11];
    u32 offset;
    u32 size;
    struct stat st;
    if (fstat(disk, &st) != 0 || pread(disk, header, sizeof header, 0) != ssize_t(sizeof header)) {
        fatal("rootfs: the image disk reads not: errno %d", errno);
    }
    serverOf(header, u32(st.st_size), offset, size);
    u8* bytes = static_cast<u8*>(allocate(size));
    if (pread(disk, bytes, size, off_t(offset)) != ssize_t(size)) {
        fatal("rootfs: the image disk reads not: errno %d", errno);
    }
    const int program = memfile(bytes, size, O_RDONLY | O_CLOEXEC);
    if (program < 0) {
        fatal("rootfs: memfile: errno %d", errno);
    }
    return program;
}

// Starts the system. erofs serves the image, its descriptor 3 the disk
// named image, its 4 the channel of the root, its program a memfd of its
// bytes there (serverIn); it has no root and walks no path. erofs tells
// the root's stat, then where /run and /dev are. The file systems and the services of
// the system start next, the far end of a pair of ours their descriptor
// 3: they close it once they are up, after the services they start
// have it, and when the last one has, the shell starts (booted).
// /bin/tmpfs is /run, its channel its descriptor 3. /bin/devfs is /dev,
// its channel its descriptor 4, and it gets the disks vm gave us but the
// image, our descriptors after 2, in order, named by DISKS, and the cards
// of the network after them, named by NETS: its 5 and on, and it starts
// their services and every system's devices. Each is
// mounted before it starts: what it starts finds it there.
static Task boot() {
    int ends[2];
    int run[2];
    int dev[2];
    int image[2];
    int* pairs[] = {ends, run, dev, image};
    for (int* pair : pairs) {
        if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
            fatal("rootfs: socketpair: errno %d", errno);
        }
    }
    // The disks are no one's but devfs's, and so are the network's cards,
    // after them, which it names net/<name>.
    char disks[256] = {};
    if (const char* given = getenv("DISKS")) {
        strncpy(disks, given, sizeof disks - 1);
    }
    char nets[256] = {};
    if (const char* given = getenv("NETS")) {
        strncpy(nets, given, sizeof nets - 1);
    }
    char cards[maxKept][NAME_MAX + 5];
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
    int imageDisk = -1;
    int disk = 3;
    for (char* name = strtok(disks, " "); name && count < maxKept; name = strtok(nullptr, " "), ++disk) {
        fcntl(disk, F_SETFD, FD_CLOEXEC);
        if (strcmp(name, "image") == 0) {
            imageDisk = disk;
            continue;
        }
        kept[count] = disk;
        snprintf(numbers[count], sizeof numbers[count], "%d", 3 + count);
        argv[args++] = name;
        argv[args++] = numbers[count];
        ++count;
    }
    for (char* name = strtok(nets, " "); name && count < maxKept; name = strtok(nullptr, " "), ++disk) {
        fcntl(disk, F_SETFD, FD_CLOEXEC);
        kept[count] = disk;
        snprintf(numbers[count], sizeof numbers[count], "%d", 3 + count);
        snprintf(cards[count], sizeof cards[count], "net/%s", name);
        argv[args++] = cards[count];
        argv[args++] = numbers[count];
        ++count;
    }
    argv[args] = nullptr;
    const int served[] = {imageDisk, image[1]};
    const int program = serverIn(imageDisk);
    char erofsName[] = "erofs";
    char* const erofsArgv[] = {erofsName, three, four, nullptr};
    spawn(erofsName, erofsArgv, environment, -1, served, 2, program);
    close(served[0]);
    close(program);
    close(image[1]);
    rootMount = new (allocate(sizeof(Mount))) Mount();
    rootMount->fs = started(image[0]);
    rootMount->fs->mounts = 1;
    rootMount->root = 1;
    mounts = rootMount;
    const Header h{};
    {
        Ask ask;
        ask.fs = rootMount->fs;
        ask.head = {fsRoot, 0, 0, 0, 0, 0};
        const Answer a = co_await ask;
        if (a.error != 0 || !statIn(a.body, rootMount->rootStat)) {
            fatal("rootfs: erofs serves no root: errno %d", a.error);
        }
        rootMount->fs->cached = !nocacheIn(a);
    }
    // The root of the processes rootfs starts: a pair, whose first end is
    // the handle at the root, and a directory over whose second (attach)
    // each of them makes its working directory and its root (spawn).
    int root[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, root) != 0) {
        fatal("rootfs: a pair for the root: errno %d", errno);
    }
    listenHandle(root[0], rootOf(rootMount), O_RDONLY, pairId(root[0]));
    rootEnd = attach(root[1]);
    if (rootEnd < 0 || fcntl(rootEnd, F_SETFD, FD_CLOEXEC) != 0) {
        fatal("rootfs: attach: errno %d", errno);
    }
    close(root[1]);

    const struct {
        const char* name;
        int channel;
        u32 perm;
    } points[] = {{"run", run[0], 01777}, {"dev", dev[0], 0755}};

    Mount* begun[2];
    int begins = 0;
    for (const auto& p : points) {
        const Answer a = co_await asking(rootOf(rootMount), h, fsLookup, literal(p.name));
        Stat st;
        if (a.error != 0 || !statIn(a.body, st) || !S_ISDIR(st.mode)) {
            fatal("rootfs: mount on /%s: errno %d", p.name, a.error);
        }
        begun[begins++] = mountStarted(p.channel, st, literal(p.name), p.perm);
    }
    char tmpfsName[] = "tmpfs";
    char* const tmpfsArgv[] = {tmpfsName, three, nullptr};
    spawn("/bin/tmpfs", tmpfsArgv, environment, rootEnd, &run[1], 1);
    close(run[1]);
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
    // Each file system started tells its root once it serves, and
    // whether rootfs keeps its answers: until then it keeps none.
    for (Mount* m : begun) {
        Held held(rootOf(m));
        Ask ask;
        ask.fs = m->fs;
        ask.head = {fsRoot, 0, 0, 0, 0, 0};
        const Answer a = co_await ask;
        Stat st;
        if (!m->gone && a.error == 0 && statIn(a.body, st)) {
            m->rootStat = st;
            m->fs->cached = !nocacheIn(a);
        }
    }
}

// command is what vm gave us to run, the shell's arguments: none for the
// shell on the terminal.
static char** command;
static int commandCount;

// The services are up: the shell starts, with the command vm gave us, the
// leader of a session of its own, on a pseudoterminal joined to the
// console (bin/tools/console.cpp), whose discipline is the system's,
// whatever the console is, a host's terminal or pipes.
static void booted() {
    epoll_ctl(watcher, EPOLL_CTL_DEL, bootEnd, nullptr);
    close(bootEnd);
    bootEnd = -1;
    char console[] = "console";
    char path[] = "/bin/sh";
    char sh[] = "sh";
    char** argv = static_cast<char**>(allocate(sizeof(char*) * (commandCount + 4)));
    int at = 0;
    argv[at++] = console;
    argv[at++] = path;
    argv[at++] = sh;
    for (int i = 0; i < commandCount; ++i) {
        argv[at++] = command[i];
    }
    argv[at] = nullptr;
    shellPid = spawn("/bin/console", argv, environment, rootEnd, nullptr, 0, -1, true);
    free(argv);
    close(rootEnd);
    rootEnd = -1;
}

int main(int argc, char** argv) {
    command = argv + 1;
    commandCount = argc - 1;
    consoleType();
    reapOnChild();
    watcher = epoll_create1(EPOLL_CLOEXEC);
    if (watcher < 0) {
        fatal("rootfs: epoll_create1: errno %d", errno);
    }
    boot();
    serve();
    return MAX(shellStatus, 0);
}
