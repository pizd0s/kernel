#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <limits.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
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
    Header head{op, arg, 0, 0, 0, 0};
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
    const u32 known = h.op == opWrite ? ioAppend | ioNonblock | ioAt : ioNonblock | ioAt;
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

int tools::memfd() {
    return packetFdCount >= 2 ? packetFds[0] : -1;
}

void tools::Assembly::add(const void* data, usize n) {
    memcpy(out + size, data, n);
    size += n;
}

// One packet into packet, its descriptors into packetFds: the length
// of its body, after the header, or -1 at the end of the endpoint; with
// MSG_DONTWAIT in flags, -2 when there is none now.
static i64 receive(int fd, Header& h, int flags) {
    const ssize_t n = receivePacket(fd, packet, sizeof packet, packetFds, packetFdCount, flags);
    if (n < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
        return -2;
    }
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

Endpoint* tools::openFile(int fd, u32 flags, void* node, u64 dev, u64 ino, bool reads, bool writes) {
    const Opened what{openedFile, flags & (O_ACCMODE | O_APPEND | O_NONBLOCK), dev, ino, 0, POLLIN | POLLOUT};
    int pair[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        error(fd, errno);
        return nullptr;
    }
    Endpoint* ep = listen(pair[0], Role::File, node, reads, writes);
    const iovec said = part(&what, sizeof what);
    reply(fd, opOk, 0, &said, 1, &pair[1], 1);
    close(pair[1]);
    return ep;
}

// An endpoint that closes: what is the file system's of it goes first,
// an open file's.
static void forget(Endpoint* ep, Closing closing) {
    ep->dead = true;
    closing(ep);
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
    Header h;
    const i64 n = receive(ep->fd, h, 0);
    if (n == -2) {
        return;
    }
    if (n < 0) {
        closed(ep, closing);
        return;
    }
    const Body body{packet + sizeof(Header), packet + sizeof(Header) + n};
    if (packetFdCount == 0) {
        // A question of rootfs's, answered on its channel, in order; an
        // open's file goes on an endpoint, which a question has none of.
        // An open file's request with nowhere to answer is nobody's.
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
    // The last descriptor is where the request is answered, and its close
    // the end of the answer: of a call rootfs handed on, and of a request
    // of an open file's, which comes on the file's own endpoint.
    const int fd = packetFds[packetFdCount - 1];
    if (ep->role == Role::File) {
        fileCall(ep, h, body, fd);
    } else {
        fsCall(h, body, fd);
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
