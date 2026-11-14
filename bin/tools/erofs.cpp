// erofs: the image of the system, read-only: a file system of the VFS's
// (lib/protocol/protocol.h), which rootfs starts first, from the image
// itself, and mounts on the root, with a memfd of the image, rootfs's
// memory, and the channel it gives it. rootfs walks the VFS's paths and
// asks it by node, its root node 1, one name at a time; an open file is an
// endpoint of erofs's, which the kernel reads. It ends when rootfs has let
// go of its channel and the last open file is closed.
//
//     erofs <image> <channel>
//
// The image is bin/rootfs/image.py's layout: a header, a node table, a
// table of the directories' entries, sorted by name in each, their names,
// then the bytes of the files and links. A node's inode is its place in
// the table plus one, a name is found by a binary search, and an entry's
// position, which a READDIR gives the entries after, is its place among
// its directory's. erofs reads what is before the bytes into its memory as
// it starts, checked to be where the header says, and nothing else of the
// image: a file's bytes go from there straight to the memory of the
// process that reads them, through the memfd of it a READ brings, one copy
// (copy_file_range), and a link's target is read when it is asked for. dev
// is the image's: the number of a pair erofs makes as it starts, which no
// other pair in the kernel has.
//
// The image lets who asks do what its modes let them: root everything but
// executing what nobody may, the owner what the owner's bits say, the
// group of gid the group's, everyone else the others'; a name is looked up
// in a directory who asks may search. Nothing of it changes: a call that
// would is EROFS, after what Linux finds first. A lock is an endpoint's,
// an open file's, as Linux's open file description locks are: flock's on
// the whole file, fcntl's on a range of its bytes, refused with EAGAIN when
// another endpoint holds one in the way, and gone when the endpoint closes.
//
// Memory. A request is received into one buffer, packet, and answered
// before the next: its names are read where they are in it. What a reply
// assembles, the entries of a directory, a path, a link's target, goes
// into one buffer of its own, out. What erofs keeps of a node is its
// locks, once one is taken.

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
#include <unistd.h>

using u8 = uint8_t;
using u32 = uint32_t;
using u64 = uint64_t;
using i64 = int64_t;
using usize = size_t;

// Placement new expressions are lowered directly by Clang; no runtime
// call uses this overload. Its declaration supplies the freestanding ABI.
void* operator new(size_t, void*) noexcept;

namespace {
    constexpr usize maxPacket = 65536;

    // The servers'.
    constexpr u64 blockSize = 4096;
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
            fatal("erofs: size overflow");
        }
        return a + b;
    }

    static usize checkedMultiply(usize a, usize b) {
        if (a != 0 && b > SIZE_MAX / a) {
            fatal("erofs: size overflow");
        }
        return a * b;
    }

    static void* allocate(usize size) {
        void* p = malloc(size);
        if (!p) {
            fatal("erofs: out of memory");
        }
        return p;
    }

    static void* reallocate(void* p, usize size) {
        void* q = realloc(p, size);
        if (!q) {
            fatal("erofs: out of memory");
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

    // ---- names and bodies ----------------------------------------------------------

    // Bytes of a packet or of the image, where they are.
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

    // ---- the image ------------------------------------------------------------------

    // A node of the image.
    struct Node {
        u32 mode;
        u32 nlink;
        u32 uid;
        u32 gid;
        u32 size;   // a file's bytes, a link's target's, a directory's entries
        u32 data;   // where a file's or a link's bytes are, a directory's first entry
        u32 parent; // a directory's parent, the root's itself
        u32 entry;  // a directory's entry in its parent's, the root's none
    };

    // An entry of a directory: its name, in the names, and its node.
    struct Entry {
        u32 name;
        u32 length;
        u32 node;
    };

    // A lock of fcntl: an endpoint's, on the bytes [start, stop), to read
    // or to write.
    struct Range {
        int owner;
        bool write;
        u64 start;
        u64 stop;
    };

    // The locks of a node, once one is taken: flock's, exclusive or
    // shared, and fcntl's.
    struct Locks {
        int exclusive = -1;   // the endpoint that holds flock's exclusive lock, or -1
        SmallVec<int> shared; // the endpoints that hold flock's shared lock
        SmallVec<Range> ranges;
    };

    // The image's header and tables, in erofs's memory, and the memfd of
    // the whole.
    struct Image {
        int fd = -1;
        const Node* nodes = nullptr;
        u32 count = 0;
        const Entry* entries = nullptr;
        u32 entryCount = 0;
        const char* names = nullptr;
        u64 dev = 0;
        u64 used = 0;            // the bytes of its files
        Locks** locks = nullptr; // a node's, by its number
    };

    constexpr u32 noEntry = 0xffffffff;

    // What an endpoint is to the loop.
    enum class Role : u8 {
        File,    // an open file: requests come on it
        Channel, // rootfs's channel: its calls by node come on it
    };

    struct Endpoint {
        int fd;
        Role role;
        bool dead;        // closed, while a batch of events may still name it
        const Node* node; // what a File is open on
        Endpoint* next;   // among the free ones
        Endpoint* after;  // among the live ones
        Endpoint* before;
    };

    Image image;

    static u64 inoOf(const Node* n) {
        return u64(n - image.nodes) + 1;
    }

    // The node of an inode, or none.
    static const Node* nodeOf(u64 ino) {
        return ino >= 1 && ino <= image.count ? &image.nodes[ino - 1] : nullptr;
    }

    static bool isDirNode(const Node* n) {
        return S_ISDIR(n->mode);
    }

    static Name nameOf(const Entry& e) {
        return {image.names + e.name, e.length};
    }

    static const Node* parentOf(const Node* n) {
        return &image.nodes[n->parent];
    }

    static int compare(Name a, Name b) {
        const int c = memcmp(a.at, b.at, MIN(a.length, b.length));
        if (c != 0) {
            return c;
        }
        return a.length < b.length ? -1 : a.length > b.length ? 1 : 0;
    }

    // The node a name of a directory names, or none: its entries are
    // sorted, a binary search finds it.
    static const Node* childOf(const Node* dir, Name name) {
        u32 lo = dir->data;
        u32 hi = dir->data + dir->size;
        while (lo < hi) {
            const u32 mid = lo + (hi - lo) / 2;
            const int c = compare(nameOf(image.entries[mid]), name);
            if (c == 0) {
                return &image.nodes[image.entries[mid].node];
            }
            if (c < 0) {
                lo = mid + 1;
            } else {
                hi = mid;
            }
        }
        return nullptr;
    }

    // A device for a file system's files: the number of a pair, which no
    // other pair in the kernel has, as it never gives a number twice.
    static u64 device() {
        int ends[2];
        struct stat st;
        if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, ends) != 0 || fstat(ends[0], &st) != 0) {
            fatal("erofs: a pair for a device: errno %d", errno);
        }
        close(ends[0]);
        close(ends[1]);
        return u64(st.st_ino);
    }

    // Whether who asks may read (4), write (2), and search or execute (1)
    // the node.
    static bool may(const Node* n, const Header& h, u32 want) {
        if (h.uid == 0) {
            return (want & 1) == 0 || isDirNode(n) || (n->mode & 0111) != 0;
        }
        const u32 bits = h.uid == n->uid ? n->mode >> 6 : h.gid == n->gid ? n->mode >> 3 : n->mode;
        return (bits & want) == want;
    }

    static u8 dtype(const Node* n) {
        return u8((n->mode >> 12) & 017);
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

    // Why a name cannot be made in a directory of the image: a name that
    // is there is EEXIST before anything else, as mkdir -p asks for each
    // directory, and the image makes nothing, EROFS.
    static int creates(const Node* c, Name name, const Header& h) {
        if (const int e = badName(name)) {
            return e;
        }
        if (!isDirNode(c)) {
            return ENOTDIR;
        }
        if (!may(c, h, 1)) {
            return EACCES;
        }
        return childOf(c, name) ? EEXIST : EROFS;
    }

    // A directory's links: its name, its ".", and the ".." of each
    // directory in it; another's: its names.
    static Stat statOf(const Node* n) {
        Stat st{};
        st.ino = inoOf(n);
        st.mode = n->mode;
        st.nlink = n->nlink;
        st.uid = n->uid;
        st.gid = n->gid;
        st.dev = image.dev;
        if (!isDirNode(n)) {
            st.size = n->size;
        }
        return st;
    }

    // What the image holds, nothing left: it is read-only.
    static Statfs statfsOf() {
        Statfs st{};
        st.bsize = u32(blockSize);
        st.namemax = NAME_MAX;
        st.dev = image.dev;
        st.blocks = (image.used + blockSize - 1) / blockSize;
        st.files = image.count;
        st.flags = ST_RDONLY;
        return st;
    }

    // The locks of a node, made when there are none yet.
    static Locks* locksOf(const Node* n) {
        Locks*& l = image.locks[n - image.nodes];
        if (!l) {
            l = new (allocate(sizeof(Locks))) Locks();
        }
        return l;
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
    static void reply(int fd, u32 op, u32 arg, const iovec* parts, usize count) {
        Header head{op, arg, 0, 0};
        iovec iov[3] = {{&head, sizeof head}};
        if (count > 2) {
            fatal("erofs: a reply of %zu parts", count);
        }
        for (usize i = 0; i < count; ++i) {
            iov[1 + i] = parts[i];
        }
        sendPacket(fd, iov, 1 + count, nullptr, 0); // a gone reader is its own loss
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

    // The memfd of the memory a READ fills: the first of the request's
    // descriptors, before its reply endpoint; -1 when it brought none.
    static int window() {
        return packetFdCount >= 2 ? packetFds[0] : -1;
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

    // ---- the calls of the image, by node ------------------------------------------
    //
    // The image changes nothing: a call that would is EROFS, after what
    // Linux finds before it.

    // UNLINK and RMDIR of a name.
    static int unlinkError(const Node* c, Name name, const Header& h) {
        if (const int e = badName(name)) {
            return e;
        }
        if (!isDirNode(c)) {
            return ENOTDIR;
        }
        if (!may(c, h, 1)) {
            return EACCES;
        }
        return EROFS;
    }

    // RENAME of a name of a directory to a name of another.
    static int renameError(const Node* src, Name oldName, const Node* dst, Name newName, const Header& h) {
        if (const int e = badName(oldName)) {
            return e;
        }
        if (const int e = badName(newName)) {
            return e;
        }
        if (!isDirNode(dst)) {
            return ENOENT;
        }
        if (!isDirNode(src)) {
            return ENOTDIR;
        }
        if (!may(src, h, 1) || !may(dst, h, 1)) {
            return EACCES;
        }
        return EROFS;
    }

    // LINK: a new name in a directory.
    static int linkError(const Node* dst, Name newName, const Header& h) {
        if (!isDirNode(dst)) {
            return ENOENT;
        }
        return creates(dst, newName, h);
    }

    // ---- calls on a node ---------------------------------------------------------

    // A READ of a file of the image: its bytes at the offset, as many as
    // asked up to its end, from the image into the window.
    static bool readNode(Endpoint* ep, const Header& h, Body body, int fd) {
        const u64 off = body.take<u64>();
        const u32 woff = body.take<u32>();
        const Node* node = ep->node;
        if (off > INT64_MAX) {
            return error(fd, EINVAL);
        }
        const int w = window();
        if (w < 0) {
            return error(fd, EINVAL);
        }
        if (!S_ISREG(node->mode)) {
            return error(fd, EINVAL);
        }
        const usize count = off >= node->size ? 0 : MIN(usize(h.arg), usize(node->size - off));
        off_t from = off_t(node->data + off);
        off_t to = off_t(woff);
        for (usize left = count; left > 0;) {
            const ssize_t n = copy_file_range(image.fd, &from, w, &to, left, 0);
            if (n <= 0) {
                return error(fd, EIO);
            }
            left -= usize(n);
        }
        return readCount(fd, count);
    }

    // A link's target, read from the image.
    static bool readlinkNode(const Node* n, int fd) {
        if (!S_ISLNK(n->mode)) {
            return error(fd, EINVAL);
        }
        if (n->size > sizeof out || pread(image.fd, out, n->size, off_t(n->data)) != ssize_t(n->size)) {
            return error(fd, EIO);
        }
        ok(fd, out, n->size);
        return false;
    }

    // The entries after a position, in order, as many as one reply holds:
    // an entry's position is its place among its directory's plus one.
    static bool readdirNode(const Node* node, Body body, int fd) {
        if (!isDirNode(node)) {
            return error(fd, ENOTDIR);
        }
        const u64 after = body.take<u64>();
        Assembly a;
        for (u64 i = MIN(after, u64(node->size)); i < node->size; ++i) {
            const Entry& e = image.entries[node->data + i];
            const Node* n = &image.nodes[e.node];
            if (!a.fits(17 + e.length + 1)) {
                break;
            }
            a.add(inoOf(n));
            a.add(i + 1);
            a.add(dtype(n));
            a.add(image.names + e.name, e.length);
            a.add(u8(0));
        }
        ok(fd, out, a.size);
        return false;
    }

    // The path of a directory from the image's root, made from its end, at
    // the end of out: none for what is no directory.
    static bool pathOf(const Node* n, int fd) {
        if (!isDirNode(n)) {
            return error(fd, ENOENT);
        }
        usize start = sizeof out;
        for (; n->entry != noEntry; n = parentOf(n)) {
            const Name name = nameOf(image.entries[n->entry]);
            if (start < name.length + 1) {
                return error(fd, ENAMETOOLONG);
            }
            start -= name.length;
            memcpy(out + start, name.at, name.length);
            out[--start] = '/';
        }
        if (start == sizeof out) {
            ok(fd, "/", 1);
        } else {
            ok(fd, out + start, sizeof out - start);
        }
        return false;
    }

    // flock's lock of the endpoint's node: shared, which no other
    // endpoint's exclusive one is in the way of, exclusive, which no other
    // endpoint's lock is, or none.
    static bool flockNode(Endpoint* ep, const Header& h, int fd) {
        Locks* n = locksOf(ep->node);
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
        Locks* n = locksOf(ep->node);
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

    int watcher = -1;
    Endpoint* freeEndpoints;
    Endpoint* buried; // closed in this batch of events: free after it
    Endpoint* live;   // every endpoint not closed
    usize endpointCount;

    // An endpoint of a role on a descriptor of ours, an open file's open on
    // a node.
    static Endpoint* listen(int fd, Role role, const Node* n) {
        Endpoint* ep = freeEndpoints;
        if (ep) {
            freeEndpoints = ep->next;
        } else {
            ep = static_cast<Endpoint*>(allocate(sizeof(Endpoint)));
        }
        *ep = {fd, role, false, n, nullptr, live, nullptr};
        if (live) {
            live->before = ep;
        }
        live = ep;
        ++endpointCount;
        epoll_event ev{};
        ev.events = EPOLLIN;
        ev.data.ptr = ep;
        if (epoll_ctl(watcher, EPOLL_CTL_ADD, fd, &ev) != 0) {
            fatal("erofs: epoll_ctl: errno %d", errno);
        }
        return ep;
    }

    // An endpoint that closes: its locks go with it.
    static void forget(Endpoint* ep) {
        ep->dead = true;
        if (const Node* n = ep->node) {
            if (Locks* l = image.locks[n - image.nodes]) {
                const int fd = ep->fd;
                l->shared.keepIf([fd](int holder) {
                    return holder != fd;
                });
                if (l->exclusive == fd) {
                    l->exclusive = -1;
                }
                l->ranges.keepIf([fd](const Range& r) {
                    return r.owner != fd;
                });
            }
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

    // An open of a node, on the reply's endpoint: read as its access mode
    // says and who asks may, never written, EROFS. A link is ELOOP, ENOTDIR
    // with O_DIRECTORY; a directory is rootfs's to open, EISDIR here.
    // Whether fd is kept.
    static bool openNode(const Node* n, u32 flags, const Header& h, int fd) {
        const u32 access = flags & O_ACCMODE;
        const bool reads = access != O_WRONLY;
        const bool writes = access != O_RDONLY;
        const bool truncates = (flags & O_TRUNC) != 0;
        if (S_ISLNK(n->mode)) {
            return error(fd, (flags & O_DIRECTORY) != 0 ? ENOTDIR : ELOOP);
        }
        if ((flags & O_DIRECTORY) != 0 && !isDirNode(n)) {
            return error(fd, ENOTDIR);
        }
        if (!may(n, h, (reads ? 4 : 0) | (writes || truncates ? 2 : 0))) {
            return error(fd, EACCES);
        }
        if (isDirNode(n)) {
            return error(fd, EISDIR);
        }
        if (writes || truncates) {
            return error(fd, EROFS);
        }
        listen(fd, Role::File, n);
        const Stat st = statOf(n);
        ok(fd, &st, sizeof st);
        return true;
    }

    // A call of rootfs's on its channel, by node, answered on fd: whether
    // fd is kept.
    static bool fsCall(const Header& h, Body body, int fd) {
        if (h.op == fsRoot) {
            const Stat st = statOf(&image.nodes[0]);
            ok(fd, &st, sizeof st);
            return false;
        }
        if (body.size() < 8) {
            return error(fd, EINVAL);
        }
        const Node* n = nodeOf(body.take<u64>());
        if (!n) {
            return error(fd, ENOENT);
        }
        switch (h.op) {
            case fsLookup: {
                const Name name = body.rest();
                if (!isDirNode(n)) {
                    return error(fd, ENOTDIR);
                }
                if (!may(n, h, 1)) {
                    return error(fd, EACCES);
                }
                const Node* found = name.is("..") ? parentOf(n) : childOf(n, name);
                if (!found) {
                    return error(fd, ENOENT);
                }
                const Stat st = statOf(found);
                ok(fd, &st, sizeof st);
                return false;
            }
            case fsGetattr: {
                const Stat st = statOf(n);
                ok(fd, &st, sizeof st);
                return false;
            }
            case fsReadlink:
                return readlinkNode(n, fd);
            case fsReaddir:
                return readdirNode(n, body, fd);
            case fsOpen:
                return openNode(n, h.arg, h, fd);
            case fsCreate: {
                body.take<u32>(); // the mode of what it would make
                const Name name = body.rest();
                if (const Node* c = isDirNode(n) ? childOf(n, name) : nullptr) {
                    if ((h.arg & O_EXCL) != 0) {
                        return error(fd, EEXIST);
                    }
                    return openNode(c, h.arg, h, fd);
                }
                return error(fd, creates(n, name, h));
            }
            case fsMkdir:
            case fsMknod:
            case fsBind:
                return error(fd, creates(n, body.rest(), h));
            case fsSymlink:
                body.string(); // the target
                return error(fd, creates(n, body.rest(), h));
            case fsLink:
            case fsRename: {
                const Node* to = nodeOf(body.take<u64>());
                if (!to) {
                    return error(fd, ENOENT);
                }
                if (h.op == fsLink) {
                    return error(fd, linkError(to, body.rest(), h));
                }
                const Name old = body.string();
                return error(fd, renameError(n, old, to, body.rest(), h));
            }
            case fsUnlink:
                return error(fd, unlinkError(n, body.rest(), h));
            case fsChmod:
            case fsChown:
            case fsUtimes:
            case fsTruncate:
                return error(fd, EROFS);
            case fsStatfs: {
                const Statfs st = statfsOf();
                ok(fd, &st, sizeof st);
                return false;
            }
            case fsPath:
                return pathOf(n, fd);
            case fsConnect:
                return error(fd, ECONNREFUSED); // the image holds no socket
            default:
                return error(fd, ENOSYS);
        }
    }

    // A call on an open file, answered on fd: whether fd is kept. What the
    // image opens is open to read only, and changes nothing.
    static bool fileCall(Endpoint* ep, const Header& h, Body body, int fd) {
        switch (h.op) {
            case opRead:
                return readNode(ep, h, body, fd);
            case opWrite:
                return error(fd, EBADF);
            case opReaddir:
                return error(fd, ENOTDIR);
            case opFstat: {
                const Stat st = statOf(ep->node);
                ok(fd, &st, sizeof st);
                return false;
            }
            case opFstatfs: {
                const Statfs st = statfsOf();
                ok(fd, &st, sizeof st);
                return false;
            }
            case opFsync:
                return answer(fd, 0);
            case opFchmod:
            case opFchown:
            case opFutimes:
            case opFtruncate:
                return error(fd, EROFS);
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

    // Serves the image until its last endpoint closes: rootfs's channel,
    // and the open files after it.
    static void serve() {
        epoll_event events[64];
        while (endpointCount > 0) {
            const int n = epoll_wait(watcher, events, 64, -1);
            if (n < 0 && errno != EINTR) {
                fatal("erofs: epoll_wait: errno %d", errno);
            }
            for (int i = 0; i < n; ++i) {
                handle(static_cast<Endpoint*>(events[i].data.ptr));
            }
            bury();
        }
    }

    // What is before the image's bytes, its header and tables, read from
    // the memfd into memory and checked to be where the header says,
    // inside what was read: a node's entries and parent, an entry's name
    // and node; a file's and a link's bytes, inside the image.
    static void openImage(int fd) {
        u32 header[11];
        struct stat st;
        if (fstat(fd, &st) != 0 || pread(fd, header, sizeof header, 0) != ssize_t(sizeof header)) {
            fatal("erofs: no image: errno %d", errno);
        }
        const u32 count = header[2];
        const u32 entries = header[3];
        const u64 tables = header[7];
        const u64 size = header[8];
        if (memcmp(header, "PZIM", 4) != 0 || header[1] != 2 || count == 0 || size != u64(st.st_size) || tables > size || tables < sizeof header || (header[4] | header[5]) % 4 != 0 || u64(header[4]) + u64(count) * sizeof(Node) > tables || u64(header[5]) + u64(entries) * sizeof(Entry) > tables || header[6] > tables) {
            fatal("erofs: the image is none of image.py's");
        }
        auto* bytes = static_cast<u8*>(allocate(tables));
        if (pread(fd, bytes, tables, 0) != ssize_t(tables)) {
            fatal("erofs: the image's tables: errno %d", errno);
        }
        image.fd = fd;
        image.nodes = reinterpret_cast<const Node*>(bytes + header[4]);
        image.count = count;
        image.entries = reinterpret_cast<const Entry*>(bytes + header[5]);
        image.entryCount = entries;
        image.names = reinterpret_cast<const char*>(bytes + header[6]);
        if (!isDirNode(&image.nodes[0])) {
            fatal("erofs: the image's root is no directory");
        }
        for (u32 i = 0; i < count; ++i) {
            const Node& n = image.nodes[i];
            const bool dir = S_ISDIR(n.mode);
            const bool fits = dir ? u64(n.data) + n.size <= entries && n.parent < count : u64(n.data) + n.size <= size;
            if (!fits || (dir && n.entry != noEntry && n.entry >= entries)) {
                fatal("erofs: node %u of the image is out of it", i);
            }
            if (S_ISREG(n.mode)) {
                image.used += n.size;
            }
        }
        for (u32 i = 0; i < entries; ++i) {
            const Entry& e = image.entries[i];
            if (e.node >= count || u64(header[6]) + e.name + e.length > tables) {
                fatal("erofs: entry %u of the image is out of it", i);
            }
        }
        image.locks = static_cast<Locks**>(allocate(sizeof(Locks*) * count));
        memset(image.locks, 0, sizeof(Locks*) * count);
        image.dev = device();
    }
}

int erofsMain(int argc, char** argv) {
    if (argc != 3) {
        dprintf(STDERR_FILENO, "usage: erofs <image> <channel>\n");
        return 2;
    }
    const int fd = atoi(argv[1]);
    const int channel = atoi(argv[2]);
    watcher = epoll_create1(EPOLL_CLOEXEC);
    if (watcher < 0 || fcntl(fd, F_SETFD, FD_CLOEXEC) != 0 || fcntl(channel, F_SETFD, FD_CLOEXEC) != 0) {
        fatal("erofs: errno %d", errno);
    }
    openImage(fd);
    listen(channel, Role::Channel, nullptr);
    serve();
    return 0;
}
