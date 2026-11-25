// sockmgr: the sockets of the network's families, as rootfs is the
// files': a socket of AF_INET is sockmgr's, a socket on no stack yet,
// until its call says which card's it is, and the stack of that card has
// it from then on, the kernel's calls going to the stack and not here
// (lib/protocol/protocol.h, a socket's calls and a stack's and
// sockmgr's). Every card has its own stack (bin/inet), isolated: an
// address is one card's, and so is a port.
//
// Its files, which it registers at /dev/init/ctl:
//
//     sockmgr/socket   an open is a socket on no stack (socket(2) opens it)
//     sockmgr/ctl      commands, a line each: "attach <card>" starts the
//                      card's stack, "auto <pattern>" every card's whose
//                      name matches (*, the start's); read, the stacks
//
// A socket on no stack keeps what is asked of it: its options, the card
// SO_BINDTODEVICE names, a TCP socket's bind. Its BIND of UDP, CONNECT,
// and SEND to an address, sockmgr hands to the stack of the card: the
// card SO_BINDTODEVICE names, the card of the address bound, the card on
// whose network the address is, or the one card with a router; none or
// more than one, ENETUNREACH. A socket that listens is sockmgr's for good:
// it listens on the card of its address, or on every card for the
// wildcard, those that come later too, and its ACCEPT goes to a card
// with a connection, which answers the kernel with the connection's open
// file, the stack's.
//
// sockmgr watches /dev/net for the cards (watchdir), starts the stack of
// each card the pattern names, and connects to each card's stack's socket
// once it is there (its directory watched, then its iface), and again as
// iface/status comes, which the stack makes once its socket listens: on
// the connection the stack tells its state (STACK) and takes the sockets
// sockmgr hands it (HAND). sockmgr asks the cards, and they ask nothing
// of it.
//
//     sockmgr [-r fd]

#include <bin/tools/tools.h>
#include <lib/protocol/service.h>

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <fnmatch.h>
#include <limits.h>
#include <netinet/in.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/uio.h>
#include <sys/un.h>
#include <sys/watchdir.h>
#include <unistd.h>

namespace {
    constexpr uint32_t bindToDevice = 25; // SO_BINDTODEVICE

    // A card's stack: its end of ours, its card, its address, the mask
    // of its network, its router and name server, in network order.
    struct Stack {
        int fd;
        char card[NAME_MAX + 1];
        uint32_t address;
        uint32_t mask;
        uint32_t router;
        uint32_t dns;
        bool known; // it told its state
        Stack* next;
    };

    struct Option {
        uint32_t level;
        uint32_t name;
        uint32_t length;
        unsigned char value[64];
    };

    struct Socket;

    // A listener of a socket's on a stack: the open file of its, sockmgr's,
    // and whether it has a connection to take.
    struct Listener {
        Socket* of;
        Stack* stack;
        Endpoint* end;
        bool ready;
        Listener* next;
    };

    // A socket on no stack, or one that listens.
    struct Socket {
        Endpoint* end;
        uint32_t domain;
        uint32_t type;
        uint32_t protocol;
        bool made;
        Option options[16];
        size_t optionCount;
        char device[NAME_MAX + 1];
        sockaddr_in bound;
        bool hasBound;
        bool listening;
        uint32_t backlog;
        Listener* listeners;
        Endpoint* accepting; // an ACCEPT kept, where it is answered
        uint32_t acceptFlags;
        int acceptTag; // what accepting is of
        Socket* next;
    };

    enum class File {
        Socket,
        Ctl,
    };

    struct Entry {
        File file;
        Device device;
    };

    // A card's watches: of its directory, for iface, and of its iface, for
    // the stack's file.
    struct Card {
        char name[NAME_MAX + 1];
        Endpoint* dir;
        Endpoint* iface;
        int ifaceTag; // what iface's watch is of
        Card* next;
    };

    int epoll = -1;
    Entry entries[2];
    Stack* stacks;
    Socket* handles;
    char pattern[NAME_MAX + 1] = "*";
    int ctlTag;
    int devTag;
    int netTag;
    Endpoint* netWatch;
    Card* cards;
}

// ---- the stacks ------------------------------------------------------------

static Stack* stackOf(const void* of) {
    for (Stack* s = stacks; s; s = s->next) {
        if (s == of) {
            return s;
        }
    }
    return nullptr;
}

static Stack* cardNamed(const char* card) {
    for (Stack* s = stacks; s; s = s->next) {
        if (s->known && strcmp(s->card, card) == 0) {
            return s;
        }
    }
    return nullptr;
}

// The stack of an address of its own.
static Stack* owner(uint32_t address) {
    for (Stack* s = stacks; s; s = s->next) {
        if (s->known && s->address == address) {
            return s;
        }
    }
    return nullptr;
}

// The stack a socket to an address goes by: its card, SO_BINDTODEVICE's;
// the card of its bound address; the card on whose network it is; the
// one card with a router.
static Stack* route(const Socket& h, uint32_t to) {
    if (h.device[0]) {
        return cardNamed(h.device);
    }
    if (h.hasBound && h.bound.sin_addr.s_addr != 0) {
        return owner(h.bound.sin_addr.s_addr);
    }
    for (Stack* s = stacks; s; s = s->next) {
        if (s->known && s->mask != 0 && (s->address & s->mask) == (to & s->mask)) {
            return s;
        }
    }
    Stack* routed = nullptr;
    for (Stack* s = stacks; s; s = s->next) {
        if (s->known && s->router != 0) {
            if (routed) {
                return nullptr;
            }
            routed = s;
        }
    }
    return routed;
}

static uint32_t addressOf(const char* text) {
    char a[INET_ADDRSTRLEN];
    snprintf(a, sizeof a, "%.*s", int(strcspn(text, "/\n")), text);
    uint32_t out = 0;
    inet_pton(AF_INET, a, &out);
    return out;
}

// ---- handing a socket to a stack ---------------------------------------------

// A call of a socket on no stack, handed to a stack with what sockmgr kept
// of the socket, answered where reply is, the rights before it. A SEND's
// datagram that leaves the HAND no room in a packet is EMSGSIZE.
static int hand(const Stack& s, const Socket& h, const Header& call, const void* body, size_t size, const int* fds, size_t count) {
    unsigned char head[6 * 4 + sizeof(sockaddr_in)];
    size_t n = 0;
    const uint32_t nonblock = h.end->nonblock ? 1 : 0;
    const uint32_t boundLength = h.hasBound ? sizeof h.bound : 0;
    const uint32_t values[] = {h.domain, h.type, h.protocol, nonblock, call.arg, boundLength};
    for (const uint32_t v : values) {
        memcpy(head + n, &v, 4);
        n += 4;
    }
    if (h.hasBound) {
        memcpy(head + n, &h.bound, sizeof h.bound);
        n += sizeof h.bound;
    }
    unsigned char options[4 + 16 * (12 + 64)];
    size_t m = 0;
    const uint32_t optionCount = uint32_t(h.optionCount);
    memcpy(options, &optionCount, 4);
    m += 4;
    for (size_t i = 0; i < h.optionCount; ++i) {
        memcpy(options + m, &h.options[i], 12);
        memcpy(options + m + 12, h.options[i].value, h.options[i].length);
        m += 12 + h.options[i].length;
    }
    const Header hh{opHand, call.op, call.uid, call.gid, call.pgrp, call.sid};
    const iovec parts[] = {{const_cast<Header*>(&hh), sizeof hh}, {head, n}, {options, m}, {const_cast<void*>(body), size}};
    if (sendPacket(s.fd, parts, 4, fds, count, MSG_DONTWAIT) < 0) {
        return errno == EMSGSIZE ? EMSGSIZE : EIO;
    }
    return 0;
}

// A listener of a socket's on a stack: LISTEN handed to it, its answer
// the listener's open file, waited for.
static int listenOn(Socket& h, Stack& s) {
    int pair[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        return errno;
    }
    Header call{opListen, h.backlog, 0, 0, 0, 0};
    const int sent = hand(s, h, call, nullptr, 0, &pair[1], 1);
    close(pair[1]);
    if (sent != 0) {
        close(pair[0]);
        return sent;
    }
    alignas(Header) unsigned char got[sizeof(Header) + sizeof(Opened)];
    int fds[maxFds];
    size_t count = 0;
    const ssize_t n = receivePacket(pair[0], got, sizeof got, fds, count);
    close(pair[0]);
    Header answered{};
    if (n >= ssize_t(sizeof(Header))) {
        memcpy(&answered, got, sizeof answered);
    }
    if (n < ssize_t(sizeof(Header)) || answered.op != opOk || count == 0) {
        for (size_t i = 0; i < count; ++i) {
            close(fds[i]);
        }
        return n >= ssize_t(sizeof(Header)) && answered.op == opErr ? int(answered.arg) : EIO;
    }
    auto* l = static_cast<Listener*>(calloc(1, sizeof(Listener)));
    if (!l) {
        fatal("sockmgr: out of memory");
    }
    l->of = &h;
    l->stack = &s;
    l->end = watch(epoll, {fds[0], false, true, true, l});
    l->next = h.listeners;
    h.listeners = l;
    return 0;
}

// The port of a listener, its stack's for port 0, which the socket's
// listeners on the other cards then take.
static void portOf(Socket& h) {
    if (h.bound.sin_port != 0 || !h.listeners) {
        return;
    }
    const Header ask{opSockname, 0, 0, 0, 0, 0};
    int pair[2];
    if (socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        return;
    }
    const iovec part{const_cast<Header*>(&ask), sizeof ask};
    sendPacket(h.listeners->end->fd, &part, 1, &pair[1], 1);
    close(pair[1]);
    alignas(Header) unsigned char got[sizeof(Header) + 128];
    int fds[maxFds];
    size_t count = 0;
    const ssize_t n = receivePacket(pair[0], got, sizeof got, fds, count);
    close(pair[0]);
    if (n >= ssize_t(sizeof(Header) + sizeof(sockaddr_in))) {
        sockaddr_in name;
        memcpy(&name, got + sizeof(Header), sizeof name);
        h.bound.sin_port = name.sin_port;
    }
}

static bool anyReady(const Socket& h) {
    for (const Listener* l = h.listeners; l; l = l->next) {
        if (l->ready) {
            return true;
        }
    }
    return false;
}

// An ACCEPT on a listener with a connection: the stack answers it, where
// it is answered; its READY says what it has after.
static bool accepted(Socket& h, int reply, uint32_t flags) {
    for (Listener* l = h.listeners; l; l = l->next) {
        if (l->ready) {
            const Header call{opAccept, flags, 0, 0, 0, 0};
            const iovec part{const_cast<Header*>(&call), sizeof call};
            sendPacket(l->end->fd, &part, 1, &reply, 1);
            return true;
        }
    }
    return false;
}

static void toldOf(Socket& h) {
    tell(h.end->fd, anyReady(h) ? POLLIN : 0);
}

// ---- a socket's calls -------------------------------------------------------------

static bool sockaddrOf(const Request& r, size_t at, sockaddr_in& out, size_t& end) {
    uint32_t length;
    if (!r.at(at, length) || r.size < at + 4 + length) {
        return false;
    }
    memset(&out, 0, sizeof out);
    memcpy(&out, r.body + at + 4, length < sizeof out ? length : sizeof out);
    end = at + 4 + length;
    return length >= sizeof out && out.sin_family == AF_INET;
}

static void setOption(Socket& h, const Request& r) {
    uint32_t level, name;
    if (!r.at(0, level) || !r.at(4, name)) {
        return fail(r.reply(), EINVAL);
    }
    const size_t length = r.size - 8;
    if (level == SOL_SOCKET && name == bindToDevice) {
        snprintf(h.device, sizeof h.device, "%.*s", int(strnlen(reinterpret_cast<const char*>(r.body + 8), length)), r.body + 8);
        return answer(r.reply(), opOk, 0);
    }
    if (length > sizeof h.options[0].value) {
        return fail(r.reply(), EINVAL);
    }
    Option* o = nullptr;
    for (size_t i = 0; i < h.optionCount; ++i) {
        if (h.options[i].level == level && h.options[i].name == name) {
            o = &h.options[i];
        }
    }
    if (!o && h.optionCount == sizeof h.options / sizeof h.options[0]) {
        return fail(r.reply(), ENOBUFS);
    }
    if (!o) {
        o = &h.options[h.optionCount++];
    }
    *o = {level, name, uint32_t(length), {}};
    memcpy(o->value, r.body + 8, length);
    answer(r.reply(), opOk, 0);
}

// An option of the socket's, as a call set it, else as a stack has it of a
// new socket, FreeBSD's defaults: off, no linger, no timeout, the buffers
// of TCP's and UDP's sizes, a low-water mark of a byte to read and of a
// cluster, 2048 bytes, to write.
static void getOption(const Socket& h, const Request& r) {
    uint32_t level, name;
    if (!r.at(0, level) || !r.at(4, name)) {
        return fail(r.reply(), EINVAL);
    }
    for (size_t i = 0; i < h.optionCount; ++i) {
        if (h.options[i].level == level && h.options[i].name == name) {
            return answer(r.reply(), opOk, 0, h.options[i].value, h.options[i].length);
        }
    }
    if (level != SOL_SOCKET) {
        return fail(r.reply(), ENOPROTOOPT);
    }
    const bool stream = h.type == SOCK_STREAM;
    int value = 0;
    switch (name) {
        case SO_TYPE:
            value = int(h.type);
            break;
        case SO_ACCEPTCONN:
            value = h.listening;
            break;
        case SO_SNDBUF:
            value = stream ? 32768 : 9216;
            break;
        case SO_RCVBUF:
            value = stream ? 65536 : 42080;
            break;
        case SO_RCVLOWAT:
            value = 1;
            break;
        case SO_SNDLOWAT:
            value = 2048;
            break;
        case SO_ERROR:
        case SO_DEBUG:
        case SO_REUSEADDR:
        case SO_REUSEPORT:
        case SO_DONTROUTE:
        case SO_BROADCAST:
        case SO_KEEPALIVE:
        case SO_OOBINLINE:
            break;
        case SO_LINGER: {
            const linger none{};
            return answer(r.reply(), opOk, 0, &none, sizeof none);
        }
        case SO_RCVTIMEO:
        case SO_SNDTIMEO: {
            const timeval none{};
            return answer(r.reply(), opOk, 0, &none, sizeof none);
        }
        default:
            return fail(r.reply(), ENOPROTOOPT);
    }
    answer(r.reply(), opOk, 0, &value, sizeof value);
}

static void handOn(Socket& h, Request& r, uint32_t to) {
    Stack* s = route(h, to);
    if (!s) {
        return fail(r.reply(), ENETUNREACH);
    }
    if (const int e = hand(*s, h, r.h, r.body, r.size, r.fds, r.count)) {
        fail(r.reply(), e);
    }
}

// A port of TCP's for a bind to port 0, which the socket's name says before
// it listens or connects, as a stack's bind has it: the next of IANA's
// dynamic ports, 49152 to 65535, no socket of sockmgr's is bound to.
static uint16_t ephemeral() {
    static uint32_t next = 49152;
    for (uint32_t tries = 0; tries <= 65535 - 49152; ++tries) {
        const uint16_t port = htons(uint16_t(next));
        next = next == 65535 ? 49152 : next + 1;
        bool held = false;
        for (const Socket* h = handles; h && !held; h = h->next) {
            held = h->hasBound && h->type == SOCK_STREAM && h->bound.sin_port == port;
        }
        if (!held) {
            return port;
        }
    }
    return 0;
}

static void bindCall(Socket& h, Request& r) {
    sockaddr_in a;
    size_t end;
    if (!sockaddrOf(r, 0, a, end)) {
        return fail(r.reply(), EINVAL);
    }
    if (a.sin_addr.s_addr != 0 && !owner(a.sin_addr.s_addr)) {
        return fail(r.reply(), EADDRNOTAVAIL);
    }
    if (h.type == SOCK_DGRAM) {
        // A datagram socket's bind is its stack's, which receives on it: a
        // wildcard's, the stack with a router's, else the loopback's.
        Stack* s = a.sin_addr.s_addr != 0 ? owner(a.sin_addr.s_addr) : route(h, 0);
        if (!s && a.sin_addr.s_addr == 0 && !h.device[0]) {
            s = owner(htonl(INADDR_LOOPBACK));
        }
        if (!s) {
            return fail(r.reply(), EADDRNOTAVAIL);
        }
        if (const int e = hand(*s, h, r.h, r.body, r.size, r.fds, r.count)) {
            fail(r.reply(), e);
        }
        return;
    }
    if (a.sin_port == 0) {
        a.sin_port = ephemeral();
    }
    h.bound = a;
    h.hasBound = true;
    answer(r.reply(), opOk, 0);
}

static void listenCall(Socket& h, const Request& r) {
    if (h.type != SOCK_STREAM) {
        return fail(r.reply(), EOPNOTSUPP);
    }
    if (!h.hasBound) {
        h.bound = {};
        h.bound.sin_family = AF_INET;
        h.hasBound = true;
    }
    h.backlog = r.h.arg;
    if (!h.listening) {
        for (Stack* s = stacks; s; s = s->next) {
            if (!s->known || (h.bound.sin_addr.s_addr != 0 && s->address != h.bound.sin_addr.s_addr)) {
                continue;
            }
            if (const int e = listenOn(h, *s); e != 0 && h.bound.sin_addr.s_addr != 0) {
                return fail(r.reply(), e);
            }
            portOf(h);
        }
        h.listening = true;
    }
    answer(r.reply(), opOk, 0);
}

static void acceptCall(Socket& h, Request& r) {
    if (!h.listening) {
        return fail(r.reply(), EINVAL);
    }
    if (accepted(h, r.reply(), r.h.arg)) {
        return toldOf(h);
    }
    if (h.end->nonblock) {
        return fail(r.reply(), EAGAIN);
    }
    if (h.accepting) {
        return fail(r.reply(), EBUSY);
    }
    h.accepting = keep(epoll, r, &h.acceptTag);
    h.acceptFlags = r.h.arg;
}

static void nameCall(const Socket& h, const Request& r) {
    if (r.h.arg) {
        return fail(r.reply(), ENOTCONN);
    }
    sockaddr_in name{};
    name.sin_family = AF_INET;
    if (h.hasBound) {
        name = h.bound;
    }
    answer(r.reply(), opOk, 0, &name, sizeof name);
}

static void socketCall(Socket& h, Request& r) {
    if (r.h.op == opSocket) {
        uint32_t domain, protocol;
        const uint32_t type = r.h.arg & 0xf;
        if (h.made || !r.at(0, domain) || !r.at(4, protocol)) {
            return fail(r.reply(), EINVAL);
        }
        if (domain != AF_INET) {
            return fail(r.reply(), EAFNOSUPPORT);
        }
        if (type != SOCK_STREAM && type != SOCK_DGRAM) {
            return fail(r.reply(), ESOCKTNOSUPPORT);
        }
        h = {h.end, domain, type, protocol, true, {}, 0, {}, {}, false, false, 0, nullptr, nullptr, 0, 0, h.next};
        return answer(r.reply(), opOk, 0);
    }
    if (!h.made) {
        return fail(r.reply(), EINVAL);
    }
    sockaddr_in to;
    size_t end;
    switch (r.h.op) {
        case opBind:
            return bindCall(h, r);
        case opConnect:
            if (h.listening) {
                return fail(r.reply(), EINVAL);
            }
            if (!sockaddrOf(r, 0, to, end)) {
                return fail(r.reply(), EINVAL);
            }
            return handOn(h, r, to.sin_addr.s_addr);
        case opSend:
            if (!sockaddrOf(r, 0, to, end)) {
                return fail(r.reply(), h.type == SOCK_DGRAM ? EDESTADDRREQ : ENOTCONN);
            }
            return handOn(h, r, to.sin_addr.s_addr);
        case opListen:
            return listenCall(h, r);
        case opAccept:
            return acceptCall(h, r);
        case opSetsockopt:
            return setOption(h, r);
        case opGetsockopt:
            return getOption(h, r);
        case opSockname:
            return nameCall(h, r);
        case opSocktype:
            return answer(r.reply(), opOk, 0, &h.type, sizeof h.type);
        case opFstat: {
            Stat st{};
            st.mode = S_IFSOCK | 0777;
            st.nlink = 1;
            return answer(r.reply(), opOk, 0, &st, sizeof st);
        }
        case opRead:
        case opWrite:
        case opRecv:
        case opShutdown:
            return fail(r.reply(), ENOTCONN);
        default:
            return fail(r.reply(), unserved(r.h.op));
    }
}

static void forgetHandle(Socket* h) {
    for (Listener* l = h->listeners, *next; l; l = next) {
        next = l->next;
        unwatch(epoll, l->end);
        free(l);
    }
    if (h->accepting) {
        unkeep(epoll, h->accepting);
    }
    for (Socket** at = &handles; *at; at = &(*at)->next) {
        if (*at == h) {
            *at = h->next;
            break;
        }
    }
    free(h);
}

// ---- a stack's state, the cards ------------------------------------------------------

static void stated(Stack& s, const Request& r) {
    char text[512];
    const size_t size = r.size < sizeof text - 1 ? r.size : sizeof text - 1;
    memcpy(text, r.body, size);
    text[size] = 0;
    for (char* line = strtok(text, "\n"); line; line = strtok(nullptr, "\n")) {
        if (strncmp(line, "card ", 5) == 0) {
            snprintf(s.card, sizeof s.card, "%s", line + 5);
        } else if (strncmp(line, "inet ", 5) == 0) {
            s.address = addressOf(line + 5);
            const char* slash = strchr(line + 5, '/');
            const int prefix = slash ? atoi(slash + 1) : 32;
            s.mask = prefix <= 0 ? 0 : htonl(prefix >= 32 ? 0xffffffffu : ~(0xffffffffu >> prefix));
        } else if (strncmp(line, "gateway ", 8) == 0) {
            s.router = addressOf(line + 8);
        } else if (strncmp(line, "dns ", 4) == 0) {
            s.dns = addressOf(line + 4);
        }
    }
    const bool first = !s.known;
    s.known = true;
    if (!first) {
        return;
    }
    // A card that came: the sockets that listen on every card listen on it.
    for (Socket* h = handles; h; h = h->next) {
        if (h->listening && h->bound.sin_addr.s_addr == 0) {
            listenOn(*h, s);
        }
    }
}

static void stackGone(Stack* s) {
    for (Socket* h = handles; h; h = h->next) {
        for (Listener** at = &h->listeners; *at;) {
            Listener* l = *at;
            if (l->stack == s) {
                *at = l->next;
                unwatch(epoll, l->end);
                free(l);
            } else {
                at = &l->next;
            }
        }
    }
    for (Stack** at = &stacks; *at; at = &(*at)->next) {
        if (*at == s) {
            *at = s->next;
            break;
        }
    }
    free(s);
}

// Starts a card's stack: "stack" in its ctl.
static int attach(const char* card) {
    char path[PATH_MAX];
    snprintf(path, sizeof path, "/dev/net/%s/ctl", card);
    const int fd = open(path, O_WRONLY | O_CLOEXEC);
    if (fd < 0) {
        return errno;
    }
    const int e = write(fd, "stack\n", 6) == 6 ? 0 : errno;
    close(fd);
    return e;
}

static Endpoint* watched(const char* path, void* of) {
    const int dir = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    if (dir < 0) {
        return nullptr;
    }
    const int w = watchdir(dir, O_NONBLOCK | O_CLOEXEC);
    close(dir);
    if (w < 0) {
        return nullptr;
    }
    Endpoint e{w, false, true, false, of};
    e.raw = true;
    return watch(epoll, e);
}

// Connects to the stack's socket of a card's, once: a refused connection,
// before the stack listens, is tried again as iface/status comes.
static void opens(const char* card) {
    for (const Stack* s = stacks; s; s = s->next) {
        if (strcmp(s->card, card) == 0) {
            return;
        }
    }
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    snprintf(address.sun_path, sizeof address.sun_path, "/dev/net/%s/iface/stack", card);
    const int fd = socket(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0);
    if (fd < 0 || connect(fd, reinterpret_cast<const sockaddr*>(&address), sizeof address) != 0) {
        if (fd >= 0) {
            close(fd);
        }
        return;
    }
    auto* s = static_cast<Stack*>(calloc(1, sizeof(Stack)));
    if (!s) {
        fatal("sockmgr: out of memory");
    }
    s->fd = fd;
    snprintf(s->card, sizeof s->card, "%s", card);
    s->next = stacks;
    stacks = s;
    watch(epoll, {fd, false, true, true, s});
}

static Card* cardOf(const void* of, bool& iface) {
    for (Card* c = cards; c; c = c->next) {
        if (c == of || &c->ifaceTag == of) {
            iface = &c->ifaceTag == of;
            return c;
        }
    }
    return nullptr;
}

// A card that came: its directory watched, and its stack started when
// the pattern names it.
static void cardCame(const char* name) {
    for (Card* c = cards; c; c = c->next) {
        if (strcmp(c->name, name) == 0) {
            return;
        }
    }
    auto* c = static_cast<Card*>(calloc(1, sizeof(Card)));
    if (!c) {
        fatal("sockmgr: out of memory");
    }
    snprintf(c->name, sizeof c->name, "%s", name);
    char path[PATH_MAX];
    snprintf(path, sizeof path, "/dev/net/%s", name);
    c->dir = watched(path, c);
    if (!c->dir) {
        free(c);
        return;
    }
    c->next = cards;
    cards = c;
    if (fnmatch(pattern, name, 0) == 0) {
        attach(name);
    }
}

// The end of a watch of a card's: of its iface, which comes again with
// its stack; of its directory, the card gone.
static void cardWatchEnded(Card* c, bool iface) {
    if (iface) {
        c->iface = nullptr;
        return;
    }
    if (c->iface) {
        unwatch(epoll, c->iface);
    }
    for (Card** at = &cards; *at; at = &(*at)->next) {
        if (*at == c) {
            *at = c->next;
            break;
        }
    }
    free(c);
}

// The records of a watch: /dev's, for net; /dev/net's, the cards; a
// card's, for iface; its iface's, for the stack's file.
static void records(Endpoint& e) {
    unsigned char b[1024];
    for (ssize_t n; (n = read(e.fd, b, sizeof b)) != 0;) {
        if (n < 0 && errno == EAGAIN) {
            return;
        }
        if (n < 0) {
            break;
        }
        for (ssize_t at = 0; at < n;) {
            const unsigned kind = b[at];
            const char* name = reinterpret_cast<const char*>(b + at + 1);
            at += 2 + ssize_t(strlen(name));
            if (kind != WATCH_EXISTING && kind != WATCH_ADDED) {
                continue;
            }
            bool iface = false;
            Card* c = cardOf(e.of, iface);
            if (e.of == &devTag && strcmp(name, "net") == 0 && !netWatch) {
                netWatch = watched("/dev/net", &netTag);
            } else if (e.of == &netTag) {
                cardCame(name);
            } else if (c && !iface && strcmp(name, "iface") == 0 && !c->iface) {
                char path[PATH_MAX];
                snprintf(path, sizeof path, "/dev/net/%s/iface", c->name);
                c->iface = watched(path, &c->ifaceTag);
            } else if (c && iface && (strcmp(name, "stack") == 0 || strcmp(name, "status") == 0)) {
                opens(c->name);
            }
        }
    }
    // The end of a watch, or of its file system, EIO, as devfs ends with
    // the system: /dev/net went with its last card, and comes back with
    // the next.
    bool iface = false;
    Card* c = cardOf(e.of, iface);
    const bool net = e.of == &netTag;
    unwatch(epoll, &e);
    if (net) {
        netWatch = nullptr;
    } else if (c) {
        cardWatchEnded(c, iface);
    }
}

// ---- the files --------------------------------------------------------------

static size_t ctlText(char* out, size_t room) {
    size_t n = size_t(snprintf(out, room, "auto %s\n", pattern));
    for (const Stack* s = stacks; s && n < room; s = s->next) {
        char a[INET_ADDRSTRLEN], r[INET_ADDRSTRLEN];
        inet_ntop(AF_INET, &s->address, a, sizeof a);
        inet_ntop(AF_INET, &s->router, r, sizeof r);
        n += size_t(snprintf(out + n, room - n, "stack %s inet %s/%d gateway %s\n", s->card, a, __builtin_popcount(ntohl(s->mask)), r));
    }
    return n < room ? n : room;
}

static void ctlCall(Endpoint& e, const Request& r) {
    Io io;
    if (r.h.op == opRead && r.memfd() >= 0 && r.io(io)) {
        char text[2048];
        const size_t size = ctlText(text, sizeof text);
        const uint64_t off = offsetOf(e, io, false, size);
        const size_t part = off < size ? (r.h.arg < size - off ? size_t(r.h.arg) : size_t(size - off)) : 0;
        if (part > 0 && pwrite(r.memfd(), text + off, part, 0) != ssize_t(part)) {
            return fail(r.reply(), EIO);
        }
        if ((io.flags & ioAt) == 0) {
            e.pos = off + part;
        }
        const uint32_t count = uint32_t(part);
        return answer(r.reply(), opOk, 0, &count, sizeof count);
    }
    if (r.h.op == opWrite && r.memfd() >= 0 && r.io(io)) {
        char text[256];
        if (r.h.arg >= sizeof text || pread(r.memfd(), text, r.h.arg, 0) != ssize_t(r.h.arg)) {
            return fail(r.reply(), EINVAL);
        }
        text[r.h.arg] = 0;
        for (char* line = strtok(text, "\n"); line; line = strtok(nullptr, "\n")) {
            if (strncmp(line, "attach ", 7) == 0) {
                if (const int err = attach(line + 7); err != 0 && err != EBUSY) {
                    return fail(r.reply(), err);
                }
            } else if (strncmp(line, "auto ", 5) == 0) {
                snprintf(pattern, sizeof pattern, "%s", line + 5);
            } else {
                return fail(r.reply(), EINVAL);
            }
        }
        return answer(r.reply(), opOk, 0, &r.h.arg, sizeof r.h.arg);
    }
    if (r.h.op == opSeek) {
        return answerSeek(e, r, 0);
    }
    fail(r.reply(), unserved(r.h.op));
}

static Socket* handleOf(const void* of) {
    for (Socket* h = handles; h; h = h->next) {
        if (h == of) {
            return h;
        }
    }
    return nullptr;
}

static Socket* acceptorOf(const void* of) {
    for (Socket* h = handles; h; h = h->next) {
        if (&h->acceptTag == of) {
            return h;
        }
    }
    return nullptr;
}

static Listener* listenerOf(const void* of) {
    for (Socket* h = handles; h; h = h->next) {
        for (Listener* l = h->listeners; l; l = l->next) {
            if (l == of) {
                return l;
            }
        }
    }
    return nullptr;
}

static void opened(Entry& f, const Request& r) {
    switch (f.file) {
        case File::Socket: {
            auto* h = static_cast<Socket*>(calloc(1, sizeof(Socket)));
            if (!h) {
                fatal("sockmgr: out of memory");
            }
            h->end = answerOpen(epoll, r, {openedFile, 0, 0, 0, POLLOUT}, h);
            if (!h->end) {
                free(h);
                return;
            }
            h->next = handles;
            handles = h;
            return;
        }
        case File::Ctl:
            answerOpen(epoll, r, {openedFile, 0, 0, 0, POLLIN | POLLOUT}, &ctlTag);
            return;
    }
}

static void handle(Endpoint& e, Request& r) {
    if (e.channel) {
        return opened(*static_cast<Entry*>(e.of), r);
    }
    if (e.of == &ctlTag) {
        return ctlCall(e, r);
    }
    if (Stack* s = stackOf(e.of)) {
        if (r.h.op == opStack) {
            stated(*s, r);
        }
        return;
    }
    if (Listener* l = listenerOf(e.of)) {
        if (r.h.op == opReady) {
            l->ready = (r.h.arg & POLLIN) != 0;
            Socket& h = *l->of;
            if (h.accepting && accepted(h, h.accepting->fd, h.acceptFlags)) {
                unkeep(epoll, h.accepting);
                h.accepting = nullptr;
            }
            toldOf(h);
        }
        return;
    }
    if (Socket* h = acceptorOf(e.of)) {
        if (r.h.op == opCancel) {
            fail(h->accepting->fd, EINTR);
            unkeep(epoll, h->accepting);
            h->accepting = nullptr;
        }
        return;
    }
    if (Socket* h = handleOf(e.of)) {
        return socketCall(*h, r);
    }
    fail(r.reply(), EINVAL);
}

static void gone(Endpoint& e) {
    if (e.channel) {
        return stopServing();
    }
    if (Stack* s = stackOf(e.of)) {
        return stackGone(s);
    }
    if (Listener* l = listenerOf(e.of)) {
        // The stack let go of it: it goes with its stack's STACK again.
        Socket& h = *l->of;
        for (Listener** at = &h.listeners; *at; at = &(*at)->next) {
            if (*at == l) {
                *at = l->next;
                break;
            }
        }
        free(l);
        return;
    }
    if (Socket* h = acceptorOf(e.of)) {
        h->accepting = nullptr; // who asked went: serve closes it
        return;
    }
    if (Socket* h = handleOf(e.of)) {
        forgetHandle(h);
    }
}

int sockmgrMain(int argc, char** argv) {
    int first;
    const int ready = readyDescriptor(argc, argv, first);
    if (first != argc) {
        dprintf(STDERR_FILENO, "usage: sockmgr [-r fd]\n");
        return 2;
    }
    epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0) {
        fatal("sockmgr: epoll_create1: errno %d", errno);
    }

    const struct {
        const char* name;
        File file;
        uint32_t mode;
    } files[] = {{"sockmgr/socket", File::Socket, S_IFCHR | 0666}, {"sockmgr/ctl", File::Ctl, S_IFREG | 0644}};

    for (size_t i = 0; i < 2; ++i) {
        entries[i] = {files[i].file, enroll("sockmgr", files[i].name, files[i].mode)};
        watch(epoll, {entries[i].device.end, true, false, false, &entries[i]});
    }
    if (!watched("/dev", &devTag)) {
        fatal("sockmgr: watchdir /dev: errno %d", errno);
    }
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle, gone, nullptr, nullptr, records);
    return 0;
}
