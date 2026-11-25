// dns: the names of the network, as Plan 9's ndb/dns: the addresses of a
// name, asked of the name servers of the cards, each by a socket of that
// card's (SO_BINDTODEVICE), the way any program's sockets go: the kernel,
// sockmgr, the card's stack. getaddrinfo asks it of a name /etc/hosts
// does not have.
//
// Its files, which it registers at /dev/init/ctl:
//
//     dns/query     a write of "<name> a" asks, and waits until the name is
//                   found: ENOENT, no such name, or none of its addresses;
//                   ETIMEDOUT, no server answered; ENETUNREACH, no card
//                   has a name server. A read then gives the answer from
//                   the start, a record a line: "<name> a <address>",
//                   "<name> cname <name>". Each write asks anew.
//     dns/message   a write of a message of the DNS's, a question of 512
//                   bytes at most, asks the servers as query does, the
//                   message as it is under an id of dns's own, and waits
//                   for an answer: one of no error or no such name, else
//                   the last a server gave, else ETIMEDOUT; EINVAL for no
//                   question. A read then gives the answer, the server's
//                   message, the asker's id on it. Nothing is kept. The
//                   resolver of lib/c, res_send, is its.
//
// The name servers are the cards' (/dev/net/<card>/iface/status, its dns
// line), read at each question, in the order of the cards' names. The
// first is asked; the next when 2 s go by with no answer, or the server
// fails; around the list twice. A name the server has not is the answer.
// An answer is kept for as long as its records live (TTL).
//
//     dns [-r fd]

#include <bin/tools/tools.h>
#include <lib/protocol/service.h>

#include <arpa/inet.h>
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <netinet/in.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

namespace {
    constexpr int maxServers = 8;
    constexpr int askEach = 2000;       // ms a server has to answer
    constexpr uint32_t longest = 86400; // s an answer is kept at most

    // A card's name server, in network order.
    struct Server {
        char card[NAME_MAX + 1];
        in_addr_t address;
    };

    // The socket of a card's that asks its name server, a raw endpoint
    // of the loop's.
    struct Link {
        char card[NAME_MAX + 1];
        Endpoint* end;
        Link* next;
    };

    // An open of dns/query: its endpoint; its question, the name in lower
    // case, as a name's case is none of its and a server's zone may tell
    // by it; the WRITE that waits for the answer (where it is answered,
    // of waitTag); the servers asked and which of them now, under what id
    // and until when; the answer, a record a line.
    struct Query {
        Endpoint* end;
        bool raw; // of dns/message: message is the question
        unsigned char message[512];
        size_t messageSize;
        char name[256];
        Endpoint* waiting;
        char waitTag;
        uint32_t count;
        Server servers[maxServers];
        int serverCount;
        int attempt;
        uint16_t id;
        long long deadline;
        char text[4096];
        size_t size;
        Query* next;
    };

    // An answer kept: its name, its text, and until when.
    struct Remembered {
        char name[256];
        char text[4096];
        size_t size;
        long long until;
    };
}

static int epoll = -1;
static Device device;
static Device messages;
static char messagesTag; // what dns/message's channel is of
static Query* queries;
static Link* links;
static Remembered remembered[32];

static long long nowMs() {
    timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (long long)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
}

static void lowered(char* out, size_t room, const char* in) {
    size_t n = 0;
    for (; in[n] && n + 1 < room; ++n) {
        out[n] = in[n] >= 'A' && in[n] <= 'Z' ? char(in[n] - 'A' + 'a') : in[n];
    }
    out[n] = 0;
}

// A name a question can have: labels of 1 to 63 bytes of no spaces or
// controls, 253 bytes at most, a dot after the last allowed.
static bool nameOk(const char* name) {
    const size_t length = strlen(name);
    if (length == 0 || length > 254 || (length == 254 && name[253] != '.')) {
        return false;
    }
    size_t label = 0;
    for (size_t i = 0; i < length; ++i) {
        const unsigned char c = (unsigned char)name[i];
        if (c <= ' ' || c == 0x7f) {
            return false;
        }
        if (c != '.') {
            ++label;
            if (label > 63) {
                return false;
            }
            continue;
        }
        if (label == 0) {
            return false;
        }
        label = 0;
    }
    return true;
}

// ---- the servers -------------------------------------------------------------

static int byCard(const void* a, const void* b) {
    return strcmp(static_cast<const Server*>(a)->card, static_cast<const Server*>(b)->card);
}

// The name server of the card, from its iface/status: false for a card
// with none, its stack not up.
static bool serverOf(const char* card, Server& out) {
    char path[PATH_MAX];
    snprintf(path, sizeof path, "/dev/net/%s/iface/status", card);
    const int fd = open(path, O_RDONLY | O_CLOEXEC);
    if (fd < 0) {
        return false;
    }
    char text[512];
    const ssize_t got = read(fd, text, sizeof text - 1);
    close(fd);
    if (got <= 0) {
        return false;
    }
    text[got] = 0;
    for (char* line = text; line && *line;) {
        char* end = strchr(line, '\n');
        if (end) {
            *end = 0;
        }
        in_addr a;
        if (strncmp(line, "dns ", 4) == 0 && inet_pton(AF_INET, line + 4, &a) == 1 && a.s_addr != 0) {
            snprintf(out.card, sizeof out.card, "%s", card);
            out.address = a.s_addr;
            return true;
        }
        line = end ? end + 1 : nullptr;
    }
    return false;
}

// The name servers of the cards, in the order of the cards' names.
static int serversOf(Server* out, int room) {
    DIR* d = opendir("/dev/net");
    if (!d) {
        return 0;
    }
    int n = 0;
    for (dirent* e; n < room && (e = readdir(d));) {
        if (e->d_name[0] != '.' && serverOf(e->d_name, out[n])) {
            ++n;
        }
    }
    closedir(d);
    qsort(out, size_t(n), sizeof *out, byCard);
    return n;
}

// ---- the cards' sockets ------------------------------------------------------

static Link* linkOf(const void* of) {
    for (Link* l = links; l; l = l->next) {
        if (l == of) {
            return l;
        }
    }
    return nullptr;
}

// The socket of the card's, made at its first question.
static Link* linkNamed(const char* card) {
    for (Link* l = links; l; l = l->next) {
        if (strcmp(l->card, card) == 0) {
            return l;
        }
    }
    const int fd = socket(AF_INET, SOCK_DGRAM | SOCK_NONBLOCK | SOCK_CLOEXEC, 0);
    if (fd < 0) {
        return nullptr;
    }
    if (setsockopt(fd, SOL_SOCKET, SO_BINDTODEVICE, card, socklen_t(strlen(card))) != 0) {
        close(fd);
        return nullptr;
    }
    auto* l = static_cast<Link*>(calloc(1, sizeof(Link)));
    if (!l) {
        fatal("dns: out of memory");
    }
    snprintf(l->card, sizeof l->card, "%s", card);
    Endpoint e{fd, false, true, false, l};
    e.raw = true;
    l->end = watch(epoll, e);
    l->next = links;
    links = l;
    return l;
}

// A socket gone bad, its card's stack gone: the next question makes
// another.
static void dropLink(Link* l) {
    for (Link** at = &links; *at; at = &(*at)->next) {
        if (*at == l) {
            *at = l->next;
            break;
        }
    }
    unwatch(epoll, l->end);
    free(l);
}

// ---- the messages ------------------------------------------------------------

// The question of a name's addresses, A of IN, recursion desired, into
// out: its length.
static size_t question(unsigned char* out, size_t room, uint16_t id, const char* name) {
    memset(out, 0, 12);
    out[0] = uint8_t(id >> 8);
    out[1] = uint8_t(id);
    out[2] = 0x01;
    out[5] = 1;
    size_t at = 12;
    for (const char* label = name; *label;) {
        const char* dot = strchr(label, '.');
        const size_t length = dot ? size_t(dot - label) : strlen(label);
        if (at + 1 + length + 5 > room) {
            return 0;
        }
        out[at++] = uint8_t(length);
        memcpy(out + at, label, length);
        at += length;
        label += length + (dot ? 1 : 0);
    }
    out[at++] = 0;
    out[at++] = 0;
    out[at++] = 1;
    out[at++] = 0;
    out[at++] = 1;
    return at;
}

// The name at at of a message of size bytes, its pointers followed, into
// out as text: where it ends in the message, 0 for what is no name or one
// of spaces or controls.
static size_t nameAt(const unsigned char* m, size_t size, size_t at, char* out, size_t room) {
    size_t end = 0;
    size_t n = 0;
    for (int jumps = 0;;) {
        if (at >= size) {
            return 0;
        }
        const unsigned length = m[at];
        if (length == 0) {
            if (end == 0) {
                end = at + 1;
            }
            break;
        }
        if ((length & 0xc0) == 0xc0) {
            if (at + 1 >= size || ++jumps > 32) {
                return 0;
            }
            if (end == 0) {
                end = at + 2;
            }
            at = ((length & 0x3f) << 8) | m[at + 1];
            continue;
        }
        if ((length & 0xc0) != 0 || at + 1 + length > size || n + length + 2 > room) {
            return 0;
        }
        if (n > 0) {
            out[n++] = '.';
        }
        for (unsigned i = 0; i < length; ++i) {
            const unsigned char c = m[at + 1 + i];
            if (c <= ' ' || c == 0x7f) {
                return 0;
            }
            out[n++] = char(c);
        }
        at += 1 + length;
    }
    out[n] = 0;
    return end;
}

static uint16_t u16(const unsigned char* p) {
    return uint16_t(p[0] << 8 | p[1]);
}

static uint32_t u32(const unsigned char* p) {
    return uint32_t(p[0]) << 24 | uint32_t(p[1]) << 16 | uint32_t(p[2]) << 8 | p[3];
}

// ---- the questions -----------------------------------------------------------

static Query* queryOf(const void* of) {
    for (Query* q = queries; q; q = q->next) {
        if (q == of) {
            return q;
        }
    }
    return nullptr;
}

static Query* waiterOf(const void* of) {
    for (Query* q = queries; q; q = q->next) {
        if (&q->waitTag == of) {
            return q;
        }
    }
    return nullptr;
}

// Answers the WRITE that waits: e, or OK with its count.
static void finish(Query& q, int e) {
    q.deadline = -1;
    q.id = 0;
    if (e != 0) {
        q.size = 0;
    }
    if (!q.waiting) {
        return;
    }
    if (e != 0) {
        fail(q.waiting->fd, e);
    } else {
        answer(q.waiting->fd, opOk, 0, &q.count, sizeof q.count);
    }
    unkeep(epoll, q.waiting);
    q.waiting = nullptr;
}

static bool recalled(Query& q) {
    const long long now = nowMs();
    for (const Remembered& r : remembered) {
        if (r.until > now && strcmp(r.name, q.name) == 0) {
            memcpy(q.text, r.text, r.size);
            q.size = r.size;
            return true;
        }
    }
    return false;
}

static void remember(const Query& q, uint32_t ttl) {
    if (ttl == 0) {
        return;
    }
    Remembered* slot = &remembered[0];
    for (Remembered& r : remembered) {
        if (r.until < slot->until) {
            slot = &r;
        }
    }
    snprintf(slot->name, sizeof slot->name, "%s", q.name);
    memcpy(slot->text, q.text, q.size);
    slot->size = q.size;
    slot->until = nowMs() + (long long)(ttl < longest ? ttl : longest) * 1000;
}

// Asks the server of the attempt, or the next that can be asked: ETIMEDOUT
// past the last.
static void ask(Query& q) {
    for (; q.attempt < 2 * q.serverCount; ++q.attempt) {
        const Server& s = q.servers[q.attempt % q.serverCount];
        Link* l = linkNamed(s.card);
        if (!l) {
            continue;
        }
        if (getentropy(&q.id, sizeof q.id) != 0 || q.id == 0) {
            q.id = uint16_t(nowMs() | 1);
        }
        unsigned char m[512];
        size_t n = q.messageSize;
        if (q.raw) {
            memcpy(m, q.message, n);
            m[0] = uint8_t(q.id >> 8);
            m[1] = uint8_t(q.id);
        } else {
            n = question(m, sizeof m, q.id, q.name);
        }
        sockaddr_in to{};
        to.sin_family = AF_INET;
        to.sin_port = htons(53);
        to.sin_addr.s_addr = s.address;
        if (sendto(l->end->fd, m, n, 0, reinterpret_cast<const sockaddr*>(&to), sizeof to) == ssize_t(n)) {
            q.deadline = nowMs() + askEach;
            return;
        }
        if (errno != EAGAIN) {
            dropLink(l);
        }
    }
    finish(q, q.raw && q.size > 0 ? 0 : ETIMEDOUT);
}

// A record of the answer's, a line of the text: false when it has no
// room.
static bool line(Query& q, const char* owner, const char* type, const char* value) {
    const int n = snprintf(q.text + q.size, sizeof q.text - q.size, "%s %s %s\n", owner, type, value);
    if (n < 0 || size_t(n) >= sizeof q.text - q.size) {
        return false;
    }
    q.size += size_t(n);
    return true;
}

// A server's answer to a message, the asker's id back on it: one of
// another code than no error or no such name goes on to the next server,
// kept for the asker where no other comes.
static void answeredMessage(Query& q, const unsigned char* m, size_t size) {
    q.size = size < sizeof q.text ? size : sizeof q.text;
    memcpy(q.text, m, q.size);
    q.text[0] = char(q.message[0]);
    q.text[1] = char(q.message[1]);
    const unsigned rcode = m[3] & 0x0f;
    if (rcode != 0 && rcode != 3) {
        ++q.attempt;
        return ask(q);
    }
    finish(q, 0);
}

// The answer of a server's: the question's records, the end of the
// question, or the next server's turn.
static void answered(Query& q, const unsigned char* m, size_t size) {
    if (q.raw) {
        return answeredMessage(q, m, size);
    }
    const unsigned rcode = m[3] & 0x0f;
    if (rcode == 3) {
        return finish(q, ENOENT);
    }
    if (rcode != 0 || (m[2] & 0x02) != 0) {
        ++q.attempt;
        return ask(q);
    }
    const unsigned questions = u16(m + 4);
    const unsigned records = u16(m + 6);
    char name[256];
    size_t at = 12;
    for (unsigned i = 0; i < questions; ++i) {
        const size_t end = nameAt(m, size, at, name, sizeof name);
        if (end == 0 || end + 4 > size) {
            return;
        }
        at = end + 4;
    }
    q.size = 0;
    uint32_t ttl = longest;
    bool found = false;
    for (unsigned i = 0; i < records; ++i) {
        char owner[256];
        const size_t end = nameAt(m, size, at, owner, sizeof owner);
        if (end == 0 || end + 10 > size) {
            break;
        }
        const unsigned type = u16(m + end);
        const unsigned klass = u16(m + end + 2);
        const uint32_t lives = u32(m + end + 4);
        const size_t length = u16(m + end + 8);
        const size_t data = end + 10;
        if (data + length > size) {
            break;
        }
        at = data + length;
        if (klass != 1) {
            continue;
        }
        char value[256];
        if (type == 1 && length == 4) {
            inet_ntop(AF_INET, m + data, value, sizeof value);
            if (!line(q, owner, "a", value)) {
                break;
            }
            found = true;
        } else if (type == 5 && nameAt(m, size, data, value, sizeof value) != 0) {
            if (!line(q, owner, "cname", value)) {
                break;
            }
        } else {
            continue;
        }
        ttl = lives < ttl ? lives : ttl;
    }
    if (!found) {
        return finish(q, ENOENT);
    }
    remember(q, ttl);
    finish(q, 0);
}

// What came on a card's socket: an answer to one of the questions it
// asked, its id's and its server's; the socket gone bad, dropped.
static void received(Endpoint& e) {
    Link* l = linkOf(e.of);
    if (!l) {
        return;
    }
    for (;;) {
        unsigned char m[1500];
        sockaddr_in from{};
        socklen_t fromLength = sizeof from;
        const ssize_t n = recvfrom(e.fd, m, sizeof m, 0, reinterpret_cast<sockaddr*>(&from), &fromLength);
        if (n < 0 && errno == EAGAIN) {
            return;
        }
        if (n < 0) {
            return dropLink(l);
        }
        if (n < 12 || (m[2] & 0x80) == 0 || from.sin_port != htons(53)) {
            continue;
        }
        char asked[256];
        if (nameAt(m, size_t(n), 12, asked, sizeof asked) == 0) {
            continue;
        }
        for (Query* q = queries; q; q = q->next) {
            const Server& s = q->servers[q->serverCount > 0 ? q->attempt % q->serverCount : 0];
            if (q->waiting && q->id == u16(m) && strcmp(s.card, l->card) == 0 && s.address == from.sin_addr.s_addr &&
                strcasecmp(asked, q->name) == 0) {
                answered(*q, m, size_t(n));
                break;
            }
        }
    }
}

// The servers' time: the questions whose server did not answer go on to
// the next; how long until the next is due.
static int expire() {
    const long long now = nowMs();
    long long next = -1;
    for (Query* q = queries; q; q = q->next) {
        if (q->waiting && q->deadline >= 0 && q->deadline <= now) {
            ++q->attempt;
            ask(*q);
        }
        if (q->waiting && q->deadline >= 0 && (next < 0 || q->deadline < next)) {
            next = q->deadline;
        }
    }
    return next < 0 ? -1 : int(next > now ? next - now : 0);
}

// ---- the file ----------------------------------------------------------------

// A message written, a question: its first question's name is what an
// answer must be of.
static void writtenMessage(Query& q, Endpoint& e, Request& r) {
    Io io;
    if (r.memfd() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    if (r.h.arg > sizeof q.message) {
        return fail(r.reply(), EMSGSIZE);
    }
    if (pread(r.memfd(), q.message, r.h.arg, 0) != ssize_t(r.h.arg)) {
        return fail(r.reply(), EIO);
    }
    q.messageSize = r.h.arg;
    char name[256];
    if (q.messageSize < 12 || (q.message[2] & 0x80) != 0 || u16(q.message + 4) == 0 ||
        nameAt(q.message, q.messageSize, 12, name, sizeof name) == 0) {
        return fail(r.reply(), EINVAL);
    }
    lowered(q.name, sizeof q.name, name);
    q.size = 0;
    e.pos = 0;
    q.count = r.h.arg;
    q.serverCount = serversOf(q.servers, maxServers);
    if (q.serverCount == 0) {
        return fail(r.reply(), ENETUNREACH);
    }
    q.attempt = 0;
    q.waiting = keep(epoll, r, &q.waitTag);
    ask(q);
}

// A question written: "<name> a", a line.
static void written(Query& q, Endpoint& e, Request& r) {
    if (q.waiting) {
        return fail(r.reply(), EBUSY);
    }
    if (q.raw) {
        return writtenMessage(q, e, r);
    }
    Io io;
    char text[300];
    if (r.memfd() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    if (r.h.arg >= sizeof text) {
        return fail(r.reply(), ENOENT); // longer than a name can be
    }
    if (pread(r.memfd(), text, r.h.arg, 0) != ssize_t(r.h.arg)) {
        return fail(r.reply(), EIO);
    }
    text[r.h.arg] = 0;
    size_t length = r.h.arg;
    while (length > 0 && (text[length - 1] == '\n' || text[length - 1] == '\r' || text[length - 1] == ' ')) {
        text[--length] = 0;
    }
    char* type = strrchr(text, ' ');
    if (!type || strcmp(type + 1, "a") != 0) {
        return fail(r.reply(), EINVAL);
    }
    *type = 0;
    q.size = 0;
    e.pos = 0;
    if (!nameOk(text)) {
        return fail(r.reply(), ENOENT);
    }
    text[strlen(text) - (text[strlen(text) - 1] == '.' ? 1 : 0)] = 0;
    lowered(q.name, sizeof q.name, text);
    q.count = r.h.arg;
    if (recalled(q)) {
        return answer(r.reply(), opOk, 0, &q.count, sizeof q.count);
    }
    q.serverCount = serversOf(q.servers, maxServers);
    if (q.serverCount == 0) {
        return fail(r.reply(), ENETUNREACH);
    }
    q.attempt = 0;
    q.waiting = keep(epoll, r, &q.waitTag);
    ask(q);
}

// The answer read, from the open file's position or at off.
static void answerRead(const Query& q, Endpoint& e, const Request& r) {
    Io io;
    if (r.memfd() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    const uint64_t off = offsetOf(e, io, false, q.size);
    const size_t part = off < q.size ? (r.h.arg < q.size - off ? size_t(r.h.arg) : size_t(q.size - off)) : 0;
    if (part > 0 && pwrite(r.memfd(), q.text + off, part, 0) != ssize_t(part)) {
        return fail(r.reply(), EIO);
    }
    if ((io.flags & ioAt) == 0) {
        e.pos = off + part;
    }
    const uint32_t count = uint32_t(part);
    answer(r.reply(), opOk, 0, &count, sizeof count);
}

static void handle(Endpoint& e, Request& r) {
    if (e.channel) {
        auto* q = static_cast<Query*>(calloc(1, sizeof(Query)));
        if (!q) {
            fatal("dns: out of memory");
        }
        q->deadline = -1;
        q->raw = e.of == &messagesTag;
        const Device& d = q->raw ? messages : device;
        q->end = answerOpen(epoll, r, {openedFile, 0, d.st.dev, d.st.ino, POLLIN | POLLOUT}, q);
        if (!q->end) {
            free(q);
            return;
        }
        q->next = queries;
        queries = q;
        return;
    }
    if (Query* q = waiterOf(e.of)) {
        if (r.h.op == opCancel) {
            finish(*q, EINTR);
        }
        return;
    }
    Query* q = queryOf(e.of);
    if (!q) {
        return fail(r.reply(), EINVAL);
    }
    switch (r.h.op) {
        case opWrite:
            return written(*q, e, r);
        case opRead:
            return answerRead(*q, e, r);
        case opSeek:
            return answerSeek(e, r, q->size);
        case opFstat:
            return answer(r.reply(), opOk, 0, q->raw ? &messages.st : &device.st, sizeof device.st);
        default:
            return fail(r.reply(), unserved(r.h.op));
    }
}

// An endpoint closed: a WRITE's asker gone, the loop closing where it is
// answered; or an open of the file, which goes.
static void gone(Endpoint& e) {
    if (Query* q = waiterOf(e.of)) {
        q->waiting = nullptr;
        q->deadline = -1;
        q->id = 0;
        return;
    }
    Query* q = queryOf(e.of);
    if (!q) {
        return;
    }
    if (q->waiting) {
        unkeep(epoll, q->waiting);
    }
    for (Query** at = &queries; *at; at = &(*at)->next) {
        if (*at == q) {
            *at = q->next;
            break;
        }
    }
    free(q);
}

int dnsMain(int argc, char** argv) {
    int first;
    const int ready = readyDescriptor(argc, argv, first);
    if (first != argc) {
        dprintf(STDERR_FILENO, "usage: dns [-r fd]\n");
        return 2;
    }
    epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0) {
        fatal("dns: epoll_create1: errno %d", errno);
    }
    device = enroll("dns", "dns/query", S_IFREG | 0666);
    watch(epoll, {device.end, true, false, false, nullptr});
    messages = enroll("dns", "dns/message", S_IFREG | 0666);
    watch(epoll, {messages.end, true, false, false, &messagesTag});
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle, gone, nullptr, expire, received);
    return 0;
}
