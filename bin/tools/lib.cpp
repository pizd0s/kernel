#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <limits.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <sys/epoll.h>
#include <sys/param.h>
#include <sys/socket.h>
#include <bin/tools/lib.h>

using namespace tools;

void tools::fatal(const char* format, ...) {
    va_list args;
    va_start(args, format);
    vdprintf(STDERR_FILENO, format, args);
    va_end(args);
    dprintf(STDERR_FILENO, "\n");
    exit(1);
}

usize tools::checkedAdd(usize a, usize b) {
    if (b > SIZE_MAX - a) {
        fatal("%s: size overflow", program_invocation_short_name);
    }
    return a + b;
}

usize tools::checkedMultiply(usize a, usize b) {
    if (a != 0 && b > SIZE_MAX / a) {
        fatal("%s: size overflow", program_invocation_short_name);
    }
    return a * b;
}

void* tools::allocate(usize size) {
    void* p = malloc(size);
    if (!p) {
        fatal("%s: out of memory", program_invocation_short_name);
    }
    return p;
}

void* tools::reallocate(void* p, usize size) {
    void* q = realloc(p, size);
    if (!q) {
        fatal("%s: out of memory", program_invocation_short_name);
    }
    return q;
}

// ---- containers --------------------------------------------------------------------

// An allocation's bytes: a power of two, from objectAlignment, as
// aot.cpp's.
static usize allocationBytes(usize size) {
    usize bytes = objectAlignment;
    while (bytes < size) {
        bytes = checkedMultiply(bytes, 2);
    }
    return bytes;
}

alignas(objectAlignment) constinit const Buffer::Header tools::emptyBuffer{};

tools::Buffer::~Buffer() {
    if (header() != &emptyBuffer) {
        free(header());
    }
}

usize tools::Buffer::capacity() const {
    return header()->allocation == 0 ? 0 : header()->allocation - sizeof(Header);
}

// The capacity test stays small; growth itself is out of line.
void tools::Buffer::reserve(usize size) {
    if (size > capacity()) {
        grow(size);
    }
}

[[gnu::noinline]] void tools::Buffer::grow(usize size) {
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

void tools::Buffer::resize(usize size) {
    reserve(size);
    if (size != 0 || header() != &emptyBuffer) {
        header()->used = size;
    }
}

void tools::Buffer::append(const void* data, usize size) {
    const usize before = this->size();
    resize(checkedAdd(before, size));
    memcpy(static_cast<u8*>(storage) + before, data, size);
}

// Back to no bytes and no memory.
void tools::Buffer::clear() {
    if (header() != &emptyBuffer) {
        free(header());
    }
    storage = const_cast<Header*>(&emptyBuffer) + 1;
}

void tools::Buffer::swap(Buffer& other) {
    auto* temporary = storage;
    storage = other.storage;
    other.storage = temporary;
}

// ---- names and bodies ------------------------------------------------------------

bool tools::Name::is(const char* text) const {
    return length == strlen(text) && memcmp(at, text, length) == 0;
}

bool tools::Name::has(char c) const {
    return length != 0 && memchr(at, c, length) != nullptr;
}

Name tools::Body::string() {
    const auto* nul = static_cast<const u8*>(memchr(at, 0, size()));
    const Name name{reinterpret_cast<const char*>(at), usize((nul ? nul : end) - at)};
    at = nul ? nul + 1 : end;
    return name;
}

Name tools::Body::rest() {
    const Name name{reinterpret_cast<const char*>(at), size()};
    at = end;
    return name;
}

int tools::badName(Name name) {
    if (name.is(".") || name.is("..") || name.has('/')) {
        return EINVAL;
    }
    if (name.length > NAME_MAX) {
        return ENAMETOOLONG;
    }
    return 0;
}

// ---- replies ------------------------------------------------------------------------

// The packet recv takes.
alignas(Header) static u8 packet[maxPacket];
int tools::packetFds[maxFds];
usize tools::packetFdCount;
u8 tools::out[maxPacket - sizeof(Header)];

void tools::reply(int fd, u32 op, u32 arg, const iovec* parts, usize count, const int* fds, usize fdCount) {
    Header head{op, arg, 0, 0};
    iovec iov[3] = {{&head, sizeof head}};
    if (count > 2 || fdCount > maxFds) {
        fatal("%s: a reply of %zu parts and %zu descriptors", program_invocation_short_name, count, fdCount);
    }
    for (usize i = 0; i < count; ++i) {
        iov[1 + i] = parts[i];
    }
    sendPacket(fd, iov, 1 + count, fds, fdCount); // a gone reader is its own loss
}

iovec tools::part(const void* data, usize size) {
    return {const_cast<void*>(data), size};
}

void tools::ok(int fd, const void* data, usize size) {
    const iovec body = part(data, size);
    reply(fd, opOk, 0, &body, size ? 1 : 0);
}

void tools::error(int fd, int e) {
    reply(fd, opErr, u32(e), nullptr, 0);
}

bool tools::ioOf(const Header& h, Body body, Io& out) {
    if (body.size() != sizeof out) {
        return false;
    }
    memcpy(&out, body.at, sizeof out);
    const u32 known = h.op == opWrite ? ioAppend | ioNonblock : ioNonblock;
    return (out.flags & ~known) == 0;
}

void tools::readCount(int fd, usize count) {
    const u32 n = u32(count);
    ok(fd, &n, sizeof n);
}

void tools::answer(int fd, int e) {
    if (e != 0) {
        return error(fd, e);
    }
    ok(fd);
}

int tools::window() {
    return packetFdCount >= 2 ? packetFds[0] : -1;
}

void tools::Assembly::add(const void* data, usize n) {
    memcpy(out + size, data, n);
    size += n;
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

// ---- what the file systems' calls share ----------------------------------------

u64 tools::device() {
    int ends[2];
    struct stat st;
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, ends) != 0 || fstat(ends[0], &st) != 0) {
        fatal("%s: a pair for a device: errno %d", program_invocation_short_name, errno);
    }
    close(ends[0]);
    close(ends[1]);
    return u64(st.st_ino);
}

static void closed(Endpoint* ep, Closing closing);

static void unshare(Locks& locks, int me) {
    locks.shared.keepIf([me](int holder) {
        return holder != me;
    });
}

// What an endpoint holds of flock's lock goes.
static void flockDrop(Locks& locks, int me) {
    unshare(locks, me);
    if (locks.exclusive == me) {
        locks.exclusive = -1;
    }
}

// Whether flock's lock of a kind, LOCK_SH or LOCK_EX, is free for an
// endpoint: no other endpoint's lock is in its way.
static bool flockFree(const Locks& locks, int me, u32 kind) {
    if (locks.exclusive >= 0 && locks.exclusive != me) {
        return false;
    }
    for (usize i = 0; kind == LOCK_EX && i < locks.shared.size(); ++i) {
        if (locks.shared[i] != me) {
            return false;
        }
    }
    return true;
}

// flock's lock of a kind, free for the endpoint, becomes its, in place of
// what it held.
static void flockTake(Locks& locks, int me, u32 kind) {
    flockDrop(locks, me);
    if (kind == LOCK_EX) {
        locks.exclusive = me;
    } else {
        locks.shared.pushBack(me);
    }
}

// The lock of another endpoint in the way of one of a type on a range.
static const Range* inWay(const Locks& locks, int me, u32 type, u64 start, u64 stop) {
    for (usize i = 0; i < locks.ranges.size(); ++i) {
        const Range& l = locks.ranges[i];
        if (l.owner != me && l.start < stop && start < l.stop && (l.write || type == F_WRLCK)) {
            return &l;
        }
    }
    return nullptr;
}

// The ranges a LOCK keeps, made here and swapped in: one of each for
// the process.
static SmallVec<Range> keptRanges;

// A lock of a type on a range becomes the endpoint's, in place of what
// it held of the range; F_UNLCK lets the range go.
static void rangeTake(Locks& locks, int me, u32 type, u64 start, u64 stop) {
    keptRanges.clear();
    for (usize i = 0; i < locks.ranges.size(); ++i) {
        const Range l = locks.ranges[i];
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
    locks.ranges.buffer.swap(keptRanges.buffer);
}

// Whether two that wait are in each other's way: the later waits behind
// the earlier then, though the lock is free for it.
static bool crossing(const Waiter& a, const Waiter& b) {
    if (a.owner == b.owner || a.ranged != b.ranged) {
        return false;
    }
    if (!a.ranged) {
        return a.type == LOCK_EX || b.type == LOCK_EX;
    }
    return a.start < b.stop && b.start < a.stop && (a.type == F_WRLCK || b.type == F_WRLCK);
}

// Whether the lock asked is its endpoint's to take now: free, and none of
// the first count that wait in its way.
static bool ready(const Locks& locks, const Waiter& w, usize count) {
    if (w.ranged ? inWay(locks, w.owner, w.type, w.start, w.stop) != nullptr : !flockFree(locks, w.owner, w.type)) {
        return false;
    }
    for (usize i = 0; i < count; ++i) {
        if (crossing(locks.waiting[i], w)) {
            return false;
        }
    }
    return true;
}

static void take(Locks& locks, const Waiter& w) {
    if (w.ranged) {
        rangeTake(locks, w.owner, w.type, w.start, w.stop);
    } else {
        flockTake(locks, w.owner, w.type);
    }
}

// A waiter is one no more: its request's endpoint goes out of the loop.
static void settle(Locks& locks, Endpoint* reply) {
    locks.waiting.keepIf([reply](const Waiter& w) {
        return w.reply != reply;
    });
    closed(reply, nullptr);
}

// Gives the locks that wait to those they are free for now, the first to
// come first, each answered OK on its request's endpoint. One that takes
// a range in place of what it held may free an earlier one's: from the
// first again then.
static void grant(Locks& locks) {
    for (usize i = 0; i < locks.waiting.size();) {
        const Waiter w = locks.waiting[i];
        if (!ready(locks, w, i)) {
            ++i;
            continue;
        }
        take(locks, w);
        ok(w.reply->fd);
        settle(locks, w.reply);
        i = 0;
    }
}

// The lock a request asks, answered on fd: taken when it is the
// endpoint's to take, behind all that wait; else EAGAIN, or the request
// waits, fd kept as an endpoint of the loop's, which its CANCEL comes on.
static void lock(Locks& locks, Waiter w, bool waits, int fd) {
    if (ready(locks, w, locks.waiting.size())) {
        take(locks, w);
        answer(fd, 0);
        return grant(locks);
    }
    if (!waits) {
        return error(fd, EAGAIN);
    }
    if (!w.ranged) {
        flockDrop(locks, w.owner);
    }
    w.reply = listen(fd, Role::Lock, &locks, false, false);
    packetFds[packetFdCount - 1] = -1;
    locks.waiting.pushBack(w);
    grant(locks);
}

void tools::flockNode(Locks& locks, int me, const Header& h, int fd) {
    const u32 kind = h.arg & ~u32(LOCK_NB);
    switch (kind) {
        case LOCK_UN:
            flockDrop(locks, me);
            answer(fd, 0);
            return grant(locks);
        case LOCK_SH:
        case LOCK_EX:
            return lock(locks, {me, nullptr, false, kind, 0, 0}, (h.arg & LOCK_NB) == 0, fd);
        default:
            return error(fd, EINVAL);
    }
}

void tools::lockNode(Locks& locks, int me, const Header& h, Body body, int fd) {
    const u32 type = body.take<u32>();
    const u64 start = body.take<u64>();
    const u64 length = body.take<u64>();
    const u64 stop = length == 0 || start > past - length ? past : start + length;
    if (type > F_UNLCK || h.arg > lockWait) {
        return error(fd, EINVAL);
    }
    if (h.arg == lockGet) {
        const Range* way = inWay(locks, me, type, start, stop);
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
        return;
    }
    if (type != F_UNLCK) {
        return lock(locks, {me, nullptr, true, type, start, stop}, h.arg == lockWait, fd);
    }
    rangeTake(locks, me, type, start, stop);
    answer(fd, 0);
    grant(locks);
}

void tools::unlock(Locks& locks, int me) {
    flockDrop(locks, me);
    locks.ranges.keepIf([me](const Range& l) {
        return l.owner != me;
    });
    for (usize i = 0; i < locks.waiting.size();) {
        if (locks.waiting[i].owner == me) {
            settle(locks, locks.waiting[i].reply);
        } else {
            ++i;
        }
    }
    grant(locks);
}

// A lock's request that waits hears on its endpoint: a CANCEL takes it
// back, EINTR, and the end of who asked forgets it; those behind it may
// have theirs then.
static void lockEvent(Endpoint* ep) {
    Header h;
    const i64 n = receive(ep->fd, h);
    for (usize i = 0; i < packetFdCount; ++i) {
        close(packetFds[i]);
    }
    if (n >= 0 && h.op != opCancel) {
        return;
    }
    Locks& locks = *static_cast<Locks*>(ep->node);
    if (n >= 0) {
        error(ep->fd, EINTR);
    }
    settle(locks, ep);
    grant(locks);
}

// ---- the loop's endpoints ----------------------------------------------------------

static int watcher = -1;
static Endpoint* freeEndpoints;
static Endpoint* buried; // closed in this batch of events: free after it
static Endpoint* live;   // every endpoint not closed
static usize endpointCount;

Endpoint* tools::listen(int fd, Role role, void* node, bool reads, bool writes) {
    if (watcher < 0) {
        watcher = epoll_create1(EPOLL_CLOEXEC);
        if (watcher < 0) {
            fatal("%s: epoll_create1: errno %d", program_invocation_short_name, errno);
        }
    }
    Endpoint* ep = freeEndpoints;
    if (ep) {
        freeEndpoints = ep->next;
    } else {
        ep = static_cast<Endpoint*>(allocate(sizeof(Endpoint)));
    }
    *ep = {fd, reads, writes, role, false, node, nullptr, live, nullptr};
    if (live) {
        live->before = ep;
    }
    live = ep;
    ++endpointCount;
    epoll_event ev{};
    ev.events = EPOLLIN;
    ev.data.ptr = ep;
    if (epoll_ctl(watcher, EPOLL_CTL_ADD, fd, &ev) != 0) {
        fatal("%s: epoll_ctl: errno %d", program_invocation_short_name, errno);
    }
    return ep;
}

Endpoint* tools::openFile(int fd, u32 flags, void* node, bool reads, bool writes) {
    const served what{SERVED_FILE, flags & (O_ACCMODE | O_APPEND | O_NONBLOCK), 0, 0, 0};
    int pair[2];
    if (servedpair(&what, pair) != 0) {
        error(fd, errno);
        return nullptr;
    }
    Endpoint* ep = listen(pair[0], Role::File, node, reads, writes);
    reply(fd, opOk, 0, nullptr, 0, &pair[1], 1);
    close(pair[1]);
    return ep;
}

// An endpoint that closes: what is the file system's of it goes first; a
// lock's request's is none of the file system's.
static void forget(Endpoint* ep, Closing closing) {
    ep->dead = true;
    if (closing) {
        closing(ep);
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

static void closed(Endpoint* ep, Closing closing) {
    const int fd = ep->fd;
    epoll_ctl(watcher, EPOLL_CTL_DEL, fd, nullptr);
    forget(ep, closing);
    close(fd);
}

static void bury() {
    while (Endpoint* ep = buried) {
        buried = ep->next;
        ep->next = freeEndpoints;
        freeEndpoints = ep;
    }
}

static void handle(Endpoint* ep, FsCall fsCall, FileCall fileCall, Closing closing) {
    if (ep->dead) {
        return;
    }
    if (ep->role == Role::Lock) {
        return lockEvent(ep);
    }
    Header h;
    const i64 n = receive(ep->fd, h);
    if (n < 0) {
        closed(ep, closing);
        return;
    }
    const Body body{packet + sizeof(Header), packet + sizeof(Header) + n};
    if (packetFdCount == 0) {
        // A question of rootfs's, answered on its channel, in order; an
        // open's file goes on an endpoint, which a question has none of.
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
    if (ep->role == Role::Channel) {
        fsCall(h, body, fd);
    } else {
        fileCall(ep, h, body, fd);
    }
    for (usize i = 0; i < packetFdCount; ++i) {
        if (packetFds[i] >= 0) {
            close(packetFds[i]);
        }
    }
}

void tools::serve(FsCall fsCall, FileCall fileCall, Closing closing) {
    epoll_event events[64];
    while (endpointCount > 0) {
        const int n = epoll_wait(watcher, events, 64, -1);
        if (n < 0 && errno != EINTR) {
            fatal("%s: epoll_wait: errno %d", program_invocation_short_name, errno);
        }
        for (int i = 0; i < n; ++i) {
            handle(static_cast<Endpoint*>(events[i].data.ptr), fsCall, fileCall, closing);
        }
        bury();
    }
}
