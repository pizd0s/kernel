/*
 * The network databases, read whole at every call: /etc/hosts, of an
 * address and its names, the first the host's, the rest its aliases;
 * /etc/services, of a name, a port and its protocol, "80/tcp", and the
 * aliases; /etc/protocols, of a name, a number and the aliases;
 * /etc/networks, of a name, a network's number, at most four bytes of C's
 * bases between dots, and the aliases. A # begins a comment, fields are
 * between blanks, and a line of no number, or of fewer than two fields,
 * is passed over. Where /etc/services or /etc/protocols cannot be read,
 * the well-known ones are.
 *
 * getaddrinfo finds a host by its numbers, IPv4's as inet_aton reads them
 * or IPv6's with a scope, else in /etc/hosts, of a name in any case, else
 * by the system's name servers, whose answers /dev/dns/query gives; a
 * service by its number or in /etc/services; AI_ADDRCONFIG keeps the
 * families the system has sockets of. getnameinfo finds a name in
 * /etc/hosts, else gives the numbers. gethostbyname and its kind find a
 * host as getaddrinfo does, its addresses of the family, with no
 * aliases, its name the canonical one; gethostbyaddr finds it in
 * /etc/hosts. getservbyname and the rest give
 * the first entry that matches; get*ent go on from the last one they
 * gave, set and end start again. A result is of the heap, the one before
 * it freed: ENOMEM where there is no room; none found leaves errno and the
 * one before as they were. The _r functions write it into the buffer,
 * ERANGE where it is too small; of a host, they say why there is none.
 */
#include "sized.h"

#include <arpa/inet.h>
#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <net/if.h>
#include <netdb.h>
#include <netinet/in.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

#define LARGEST (16 << 20)

static const char blank[] = " \t\n\v\f\r";

static const char wellKnownServices[] =
    "echo 7/tcp\necho 7/udp\ndiscard 9/tcp\ndiscard 9/udp\ndaytime 13/tcp\nftp-data 20/tcp\nftp 21/tcp\nssh 22/tcp\n"
    "telnet 23/tcp\nsmtp 25/tcp\ntime 37/tcp\ndomain 53/tcp\ndomain 53/udp\ntftp 69/udp\nfinger 79/tcp\nhttp 80/tcp\n"
    "pop3 110/tcp\nsunrpc 111/tcp\nsunrpc 111/udp\nnntp 119/tcp\nntp 123/udp\nimap 143/tcp\nsnmp 161/udp\nldap 389/tcp\n"
    "https 443/tcp\nsyslog 514/udp\nsubmission 587/tcp\nimaps 993/tcp\npop3s 995/tcp\n";

static const char wellKnownProtocols[] =
    "ip 0\nicmp 1\nigmp 2\nggp 3\nipencap 4\nst 5\ntcp 6\negp 8\npup 12\nudp 17\nhmp 20\nxns-idp 22\nrdp 27\niso-tp4 29\n"
    "xtp 36\nddp 37\nidpr-cmtp 38\nipv6 41\nipv6-route 43\nipv6-frag 44\nidrp 45\nrsvp 46\ngre 47\nesp 50\nah 51\nskip 57\n"
    "ipv6-icmp 58\nipv6-nonxt 59\nipv6-opts 60\nrspf 73\nvmtp 81\nospf 89\nipip 94\nencap 98\npim 103\nraw 255\n";

// A file's text on the heap, NUL-ended, what of it can be read; null and
// errno where it cannot be opened, or with no room for it.
static char* readAll(const char* path) {
    int fd = open(path, O_RDONLY | O_CLOEXEC);

    if (fd < 0) {
        return NULL;
    }

    char* text = NULL;
    size_t size = 0, room = 0;
    int e = 0;

    while (size < LARGEST) {
        if (size + 1 >= room) {
            char* more = realloc(text, room = room ? 2 * room : 4096);

            if (!more) {
                e = ENOMEM;

                break;
            }

            text = more;
        }

        ssize_t n = read(fd, text + size, room - size - 1);

        if (n < 0 && errno == EINTR) {
            continue;
        }

        if (n <= 0) {
            break;
        }

        size += n;
    }

    close(fd);

    if (e) {
        free(text);
        errno = e;

        return NULL;
    }

    text[size] = 0;

    return text;
}

// A database's text: the file's, or the well-known one where there is
// one and the file cannot be read. Null where there is none, with
// *e ENOMEM where there was no room, and errno as it was.
static char* database(const char* path, const char* known, int* e) {
    int saved = errno;
    char* text = readAll(path);

    *e = !text && errno == ENOMEM ? ENOMEM : 0;

    if (!text && known && !*e) {
        text = strdup(known);
        *e = text ? 0 : ENOMEM;
    }

    errno = saved;

    return text;
}

// A line: its first two fields, ended in place, and the aliases after.
struct Line {
    char* name;
    char* value;
    char* rest;
};

static char* field(char** at) {
    char* s = *at + strspn(*at, blank);
    size_t n = strcspn(s, blank);

    if (n == 0) {
        return NULL;
    }

    *at = s + n + (s[n] != 0);
    s[n] = 0;

    return s;
}

// The next line of two fields at least of the text at *at, or none.
static int nextLine(char** at, struct Line* l) {
    while (*at && **at) {
        char* line = *at;
        char* end = strchr(line, '\n');

        *at = end ? end + 1 : line + strlen(line);

        if (end) {
            *end = 0;
        }

        line[strcspn(line, "#")] = 0;

        char* rest = line;

        l->name = field(&rest);
        l->value = l->name ? field(&rest) : NULL;
        l->rest = rest;

        if (l->value) {
            return 1;
        }
    }

    return 0;
}

// The next of the aliases at *at, its length, or 0 at their end.
static size_t alias(const char** at, const char** start) {
    *at += strspn(*at, blank);
    *start = *at;

    size_t n = strcspn(*at, blank);

    *at += n;

    return n;
}

static int aliased(const struct Line* l, const char* name) {
    size_t n = strlen(name);
    const char* at = l->rest;
    const char* a;

    for (size_t length; (length = alias(&at, &a));) {
        if (length == n && memcmp(a, name, n) == 0) {
            return 1;
        }
    }

    return 0;
}

// How many aliases there are and the room of their strings.
static size_t aliases(const char* rest, size_t* room) {
    const char* a;
    size_t count = 0;

    *room = 0;

    for (size_t length; (length = alias(&rest, &a)); count++) {
        *room += length + 1;
    }

    return count;
}

// The aliases' list at list and their strings from s, which it returns
// the end of.
static char* putAliases(char** list, char* s, const char* rest) {
    const char* a;
    size_t i = 0;

    for (size_t length; (length = alias(&rest, &a)); i++) {
        list[i] = s;
        memcpy(s, a, length);
        s[length] = 0;
        s += length + 1;
    }

    list[i] = NULL;

    return s;
}

static size_t padding(uintptr_t at) {
    return (4 - at % 4) % 4;
}

// A number as strconv's Atoi reads it, of an optional sign.
static int integer(const char* s, long long* value) {
    int negative = *s == '-';
    unsigned long long v = 0;

    s += *s == '-' || *s == '+';

    if (!*s) {
        return 0;
    }

    for (; *s; s++) {
        if (*s < '0' || *s > '9' || v > (unsigned long long)INT64_MAX / 10) {
            return 0;
        }

        v = v * 10 + (*s - '0');
    }

    if (v > (unsigned long long)INT64_MAX + negative) {
        return 0;
    }

    *value = negative ? (long long)(0 - v) : (long long)v;

    return 1;
}

// A network's number: at most four parts between dots, each a byte of C's
// bases, 0x hex, 0b binary, 0o or 0 octal.
static int network(const char* s, uint32_t* net) {
    uint32_t n = 0;
    int parts = 0;

    for (;; s++) {
        size_t length = strcspn(s, ".");
        unsigned base = 10, v = 0;
        size_t at = 0;

        if (length >= 2 && s[0] == '0' && strchr("xXbBoO", s[1])) {
            base = s[1] == 'x' || s[1] == 'X' ? 16 : s[1] == 'b' || s[1] == 'B' ? 2 : 8;
            at = 2;
        } else if (length >= 2 && s[0] == '0') {
            base = 8;
            at = 1;
        }

        if (length == 0 || at == length || ++parts > 4) {
            return 0;
        }

        for (; at < length; at++) {
            int d = isdigit((unsigned char)s[at]) ? s[at] - '0' : isxdigit((unsigned char)s[at]) ? (s[at] | 32) - 'a' + 10 : 99;

            if (d >= (int)base || (v = v * base + d) > 255) {
                return 0;
            }
        }

        n = n << 8 | v;
        s += length;

        if (!*s) {
            *net = n;

            return 1;
        }
    }
}

// The result of a get* function that is not _r: a block of the heap, the
// one before it freed, or null with ENOMEM.
static void* block(void** result, size_t size) {
    void* b = malloc(size);

    if (!b) {
        errno = ENOMEM;

        return NULL;
    }

    free(*result);
    *result = b;

    return b;
}

// Services.

struct Service {
    struct Line line;
    long long port;
    char* proto;
};

static int nextService(char** at, struct Service* s) {
    while (nextLine(at, &s->line)) {
        char* slash = strchr(s->line.value, '/');

        s->proto = slash ? slash + 1 : "";

        if (slash) {
            *slash = 0;
        }

        if (integer(s->line.value, &s->port)) {
            return 1;
        }
    }

    return 0;
}

static size_t serviceSize(const struct Service* s, uintptr_t align, size_t* count) {
    size_t room;

    *count = aliases(s->line.rest, &room);

    return padding(align) + sizeof(char*) * (*count + 1) + strlen(s->line.name) + strlen(s->proto) + 2 + room;
}

static void putService(struct servent* out, char* buf, const struct Service* s, size_t count) {
    char** list = (char**)(buf + padding((uintptr_t)buf));
    char* at = (char*)(list + count + 1);

    out->s_name = at;
    at = stpcpy(at, s->line.name) + 1;
    out->s_proto = at;
    at = stpcpy(at, s->proto) + 1;
    out->s_aliases = list;
    out->s_port = htons((uint16_t)s->port);
    putAliases(list, at, s->line.rest);
}

enum Match { byName, byNumber, next };

// The services' text and the service of the name, or of the port, of the
// protocol where one is named, or the one after those before; 0, ENOENT
// where there is none, or ENOMEM.
static int findService(enum Match match, const char* name, int port, const char* proto, size_t* index, char** text, struct Service* s) {
    int e;
    char* at = *text = database("/etc/services", wellKnownServices, &e);
    size_t i = 0;

    if (!at) {
        return e ? e : ENOENT;
    }

    while (nextService(&at, s)) {
        int same = !proto || !*proto || strcmp(s->proto, proto) == 0;

        if (match == byName ? same && (strcmp(s->line.name, name) == 0 || aliased(&s->line, name))
                            : match == byNumber ? same && s->port == port : i++ == *index) {
            if (match == next) {
                ++*index;
            }

            return 0;
        }
    }

    return ENOENT;
}

static struct servent* serviceResult(int e, char* text, struct Service* s) {
    static void* result;
    size_t count;
    struct servent* out = e ? NULL : block(&result, sizeof(struct servent) + serviceSize(s, sizeof(struct servent), &count));

    if (out) {
        putService(out, (char*)(out + 1), s, count);
    } else if (e == ENOMEM) {
        errno = ENOMEM;
    }

    free(text);

    return out;
}

static int serviceInto(int e, char* text, struct Service* s, struct servent* out, char* buf, size_t size, struct servent** result) {
    size_t count;
    int found = !e;

    *result = NULL;
    e = e == ENOENT ? 0 : e;

    if (found && serviceSize(s, (uintptr_t)buf, &count) > size) {
        e = ERANGE;
    } else if (found) {
        putService(out, buf, s, count);
        *result = out;
    }

    free(text);

    return e;
}

static size_t serviceIndex;

struct servent* __kernel_getservbyname(const char* name, const char* proto, size_t size) {
    sized("getservbyname", size, sizeof(struct servent));

    char* text;
    struct Service s;
    int e = findService(byName, name, 0, proto, NULL, &text, &s);

    return serviceResult(e, text, &s);
}

struct servent* __kernel_getservbyport(int port, const char* proto, size_t size) {
    sized("getservbyport", size, sizeof(struct servent));

    char* text;
    struct Service s;
    int e = findService(byNumber, NULL, ntohs((uint16_t)port), proto, NULL, &text, &s);

    return serviceResult(e, text, &s);
}

struct servent* __kernel_getservent(size_t size) {
    sized("getservent", size, sizeof(struct servent));

    char* text;
    struct Service s;
    int e = findService(next, NULL, 0, NULL, &serviceIndex, &text, &s);

    return serviceResult(e, text, &s);
}

void __kernel_setservent(int stay) {
    serviceIndex = 0;
}

void __kernel_endservent(void) {
    serviceIndex = 0;
}

int __kernel_getservbyname_r(const char* restrict name, const char* restrict proto, struct servent* restrict out, size_t outSize,
                             char* restrict buf, size_t size, struct servent** restrict result, size_t resultSize) {
    sized("getservbyname_r", outSize, sizeof(struct servent));
    sized("getservbyname_r", resultSize, sizeof(struct servent));

    char* text;
    struct Service s;
    int e = findService(byName, name, 0, proto, NULL, &text, &s);

    return serviceInto(e, text, &s, out, buf, size, result);
}

int __kernel_getservbyport_r(int port, const char* restrict proto, struct servent* restrict out, size_t outSize, char* restrict buf,
                             size_t size, struct servent** restrict result, size_t resultSize) {
    sized("getservbyport_r", outSize, sizeof(struct servent));
    sized("getservbyport_r", resultSize, sizeof(struct servent));

    char* text;
    struct Service s;
    int e = findService(byNumber, NULL, ntohs((uint16_t)port), proto, NULL, &text, &s);

    return serviceInto(e, text, &s, out, buf, size, result);
}

// Protocols and networks: a name, a number, the aliases.

struct Numbered {
    struct Line line;
    long long number;
};

static int nextProtocol(char** at, struct Numbered* p) {
    while (nextLine(at, &p->line)) {
        if (integer(p->line.value, &p->number)) {
            return 1;
        }
    }

    return 0;
}

static int nextNetwork(char** at, struct Numbered* n) {
    uint32_t net;

    while (nextLine(at, &n->line)) {
        if (network(n->line.value, &net)) {
            n->number = net;

            return 1;
        }
    }

    return 0;
}

static int findNumbered(int networks, enum Match match, const char* name, long long number, size_t* index, char** text, struct Numbered* n) {
    int e;
    char* at = *text = database(networks ? "/etc/networks" : "/etc/protocols", networks ? NULL : wellKnownProtocols, &e);
    size_t i = 0;

    if (!at) {
        return e ? e : ENOENT;
    }

    while (networks ? nextNetwork(&at, n) : nextProtocol(&at, n)) {
        if (match == byName ? strcmp(n->line.name, name) == 0 || aliased(&n->line, name) : match == byNumber ? n->number == number : i++ == *index) {
            if (match == next) {
                ++*index;
            }

            return 0;
        }
    }

    return ENOENT;
}

// A protocol's or network's entry, the name and the aliases' list after
// the structure of the size, whose first two fields they are.
static void* numberedResult(void** result, int e, char* text, const struct Numbered* n, size_t size) {
    size_t room;
    size_t count = aliases(n->line.rest, &room);
    char** out = e ? NULL : block(result, size + sizeof(char*) * (count + 1) + strlen(n->line.name) + 1 + room);

    if (out) {
        char** list = (char**)((char*)out + size);
        char* at = (char*)(list + count + 1);

        out[0] = at;
        out[1] = (char*)list;
        putAliases(list, stpcpy(at, n->line.name) + 1, n->line.rest);
    } else if (e == ENOMEM) {
        errno = ENOMEM;
    }

    free(text);

    return out;
}

static struct protoent* protocolResult(int e, char* text, const struct Numbered* n) {
    static void* result;
    struct protoent* p = numberedResult(&result, e, text, n, sizeof(struct protoent));

    if (p) {
        p->p_proto = (int)n->number;
    }

    return p;
}

static struct netent* networkResult(int e, char* text, const struct Numbered* n) {
    static void* result;
    struct netent* net = numberedResult(&result, e, text, n, sizeof(struct netent));

    if (net) {
        net->n_addrtype = AF_INET;
        net->n_net = (uint32_t)n->number;
    }

    return net;
}

static size_t protocolIndex, networkIndex;

struct protoent* __kernel_getprotobyname(const char* name, size_t size) {
    sized("getprotobyname", size, sizeof(struct protoent));

    char* text;
    struct Numbered n;
    int e = findNumbered(0, byName, name, 0, NULL, &text, &n);

    return protocolResult(e, text, &n);
}

struct protoent* __kernel_getprotobynumber(int number, size_t size) {
    sized("getprotobynumber", size, sizeof(struct protoent));

    char* text;
    struct Numbered n;
    int e = findNumbered(0, byNumber, NULL, number, NULL, &text, &n);

    return protocolResult(e, text, &n);
}

struct protoent* __kernel_getprotoent(size_t size) {
    sized("getprotoent", size, sizeof(struct protoent));

    char* text;
    struct Numbered n;
    int e = findNumbered(0, next, NULL, 0, &protocolIndex, &text, &n);

    return protocolResult(e, text, &n);
}

void __kernel_setprotoent(int stay) {
    protocolIndex = 0;
}

void __kernel_endprotoent(void) {
    protocolIndex = 0;
}

struct netent* __kernel_getnetbyname(const char* name, size_t size) {
    sized("getnetbyname", size, sizeof(struct netent));

    char* text;
    struct Numbered n;
    int e = findNumbered(1, byName, name, 0, NULL, &text, &n);

    return networkResult(e, text, &n);
}

struct netent* __kernel_getnetbyaddr(uint32_t net, int family, size_t size) {
    sized("getnetbyaddr", size, sizeof(struct netent));

    char* text = NULL;
    struct Numbered n;
    int e = family == AF_INET ? findNumbered(1, byNumber, NULL, net, NULL, &text, &n) : ENOENT;

    return networkResult(e, text, &n);
}

struct netent* __kernel_getnetent(size_t size) {
    sized("getnetent", size, sizeof(struct netent));

    char* text;
    struct Numbered n;
    int e = findNumbered(1, next, NULL, 0, &networkIndex, &text, &n);

    return networkResult(e, text, &n);
}

void __kernel_setnetent(int stay) {
    networkIndex = 0;
}

void __kernel_endnetent(void) {
    networkIndex = 0;
}

// Hosts.

// A host: its name and aliases, its family and addresses, each length
// bytes; the aliases are a line's rest.
struct Host {
    const char* name;
    const char* rest;
    int family;
    size_t count;
    const unsigned char* addresses;
};

// An address of /etc/hosts: IPv4's, or IPv6's with no zone.
static int hostAddress(const char* text, unsigned char address[16], int* family) {
    char copy[64];
    size_t n = strcspn(text, "%");

    if (inet_pton(AF_INET, text, address) == 1) {
        *family = AF_INET;

        return 1;
    }

    if (n >= sizeof copy || (text[n] && !text[n + 1])) {
        return 0;
    }

    memcpy(copy, text, n);
    copy[n] = 0;
    *family = AF_INET6;

    return inet_pton(AF_INET6, copy, address) == 1;
}

static size_t length(int family) {
    return family == AF_INET6 ? 16 : 4;
}

static size_t hostSize(const struct Host* h, uintptr_t align, size_t* count) {
    size_t room;

    *count = h->rest ? aliases(h->rest, &room) : (room = 0);

    return padding(align) + sizeof(char*) * (*count + 1) + sizeof(char*) * (h->count + 1) + length(h->family) * h->count + strlen(h->name) + 1 +
           room;
}

static void putHost(struct hostent* out, char* buf, const struct Host* h, size_t count) {
    char** list = (char**)(buf + padding((uintptr_t)buf));
    char** addresses = list + count + 1;
    char* data = (char*)(addresses + h->count + 1);
    size_t n = length(h->family);

    for (size_t i = 0; i < h->count; i++) {
        addresses[i] = memcpy(data + n * i, h->addresses + n * i, n);
    }

    addresses[h->count] = NULL;

    char* at = data + n * h->count;

    *out = (struct hostent){at, list, h->family, (int)n, addresses};

    if (h->rest) {
        putAliases(list, stpcpy(at, h->name) + 1, h->rest);
    } else {
        strcpy(at, h->name);
        list[0] = NULL;
    }
}

static struct hostent* hostResult(const struct Host* h) {
    static void* result;
    size_t count;
    struct hostent* out = block(&result, sizeof(struct hostent) + hostSize(h, sizeof(struct hostent), &count));

    if (out) {
        putHost(out, (char*)(out + 1), h, count);
    }

    return out;
}

// What a host's _r function returns: 0 with the host in the buffer, or
// with none and why in *error, EAGAIN where to try again, EBADMSG where
// it cannot be found, ENOMEM, or ERANGE.
static int hostInto(int code, const struct Host* h, struct hostent* out, char* buf, size_t size, struct hostent** result, int* error) {
    size_t count;

    *result = NULL;

    if (code) {
        if (error) {
            *error = code < 0 ? NO_RECOVERY : code;
        }

        return code == TRY_AGAIN ? EAGAIN : code == NO_RECOVERY ? EBADMSG : code < 0 ? ENOMEM : 0;
    }

    if (hostSize(h, (uintptr_t)buf, &count) > size) {
        return ERANGE;
    }

    putHost(out, buf, h, count);
    *result = out;

    return 0;
}

// A host by its name, as getaddrinfo finds it, its addresses of the
// family on the heap: 0, h_errno's code where there is none, or -1 where
// there is no room.
static int hostByName(const char* name, int family, struct Host* h) {
    if (family != AF_INET && family != AF_INET6) {
        return NO_RECOVERY;
    }

    struct addrinfo hints = {.ai_flags = AI_CANONNAME, .ai_family = family, .ai_socktype = SOCK_STREAM};
    struct addrinfo* found = NULL;
    int saved = errno;
    int code = getaddrinfo(name, NULL, &hints, &found);

    errno = saved;

    switch (code) {
        case 0:
            break;
        case EAI_AGAIN:
            return TRY_AGAIN;
        case EAI_FAIL:
            return NO_RECOVERY;
        case EAI_MEMORY:
            return -1;
        default:
            return HOST_NOT_FOUND;
    }

    size_t count = 0, n = length(family);

    for (struct addrinfo* a = found; a; a = a->ai_next) {
        count++;
    }

    unsigned char* addresses = malloc(n * count + strlen(found->ai_canonname ? found->ai_canonname : name) + 1);

    if (!addresses) {
        freeaddrinfo(found);

        return -1;
    }

    size_t i = 0;

    for (struct addrinfo* a = found; a; a = a->ai_next, i++) {
        const void* from = family == AF_INET ? (const void*)&((struct sockaddr_in*)a->ai_addr)->sin_addr
                                             : (const void*)&((struct sockaddr_in6*)a->ai_addr)->sin6_addr;

        memcpy(addresses + n * i, from, n);
    }

    *h = (struct Host){strcpy((char*)addresses + n * count, found->ai_canonname ? found->ai_canonname : name), NULL, family, count, addresses};
    freeaddrinfo(found);

    return 0;
}

static struct hostent* hostOf(int code, struct Host* h) {
    struct hostent* out = code ? NULL : hostResult(h);

    if (code < 0) {
        errno = ENOMEM;
    }

    if (!code) {
        free((void*)h->addresses);
    }

    return out;
}

struct hostent* __kernel_gethostbyname(const char* name, size_t size) {
    sized("gethostbyname", size, sizeof(struct hostent));

    struct Host h;

    return hostOf(hostByName(name, AF_INET, &h), &h);
}

struct hostent* __kernel_gethostbyname2(const char* name, int family, size_t size) {
    sized("gethostbyname2", size, sizeof(struct hostent));

    struct Host h;

    return hostOf(hostByName(name, family, &h), &h);
}

int __kernel_gethostbyname_r(const char* restrict name, struct hostent* restrict out, size_t outSize, char* restrict buf, size_t size,
                             struct hostent** restrict result, size_t resultSize, int* restrict error) {
    sized("gethostbyname_r", outSize, sizeof(struct hostent));
    sized("gethostbyname_r", resultSize, sizeof(struct hostent));

    struct Host h;
    int code = hostByName(name, AF_INET, &h);
    int e = hostInto(code, &h, out, buf, size, result, error);

    if (!code) {
        free((void*)h.addresses);
    }

    return e;
}

int __kernel_gethostbyname2_r(const char* restrict name, int family, struct hostent* restrict out, size_t outSize, char* restrict buf,
                              size_t size, struct hostent** restrict result, size_t resultSize, int* restrict error) {
    sized("gethostbyname2_r", outSize, sizeof(struct hostent));
    sized("gethostbyname2_r", resultSize, sizeof(struct hostent));

    struct Host h;
    int code = hostByName(name, family, &h);
    int e = hostInto(code, &h, out, buf, size, result, error);

    if (!code) {
        free((void*)h.addresses);
    }

    return e;
}

// The host of an entry of /etc/hosts, its first, of the address where
// one is given, or the one after those before; the text it is of, or
// null: 0, h_errno's code where there is none, or -1 where there is no
// room.
static int hostEntry(const unsigned char* address, int family, size_t* index, char** text, struct Host* h, unsigned char storage[16]) {
    int e;
    char* at = *text = database("/etc/hosts", NULL, &e);
    size_t i = 0;
    struct Line l;

    if (!at) {
        return e ? -1 : HOST_NOT_FOUND;
    }

    while (nextLine(&at, &l)) {
        int f;

        if (!hostAddress(l.name, storage, &f)) {
            continue;
        }

        if (address ? f == family && memcmp(storage, address, length(f)) == 0 : i++ == *index) {
            if (!address) {
                ++*index;
            }
            *h = (struct Host){l.value, address ? NULL : l.rest, f, 1, storage};

            return 0;
        }
    }

    return HOST_NOT_FOUND;
}

static int hostByAddress(const void* address, socklen_t n, int family, char** text, struct Host* h, unsigned char storage[16]) {
    *text = NULL;

    if (!(family == AF_INET && n == 4) && !(family == AF_INET6 && n == 16)) {
        return NO_RECOVERY;
    }

    return hostEntry(address, family, NULL, text, h, storage);
}

struct hostent* __kernel_gethostbyaddr(const void* address, socklen_t n, int family, size_t size) {
    sized("gethostbyaddr", size, sizeof(struct hostent));

    char* text;
    struct Host h;
    unsigned char storage[16];
    int code = hostByAddress(address, n, family, &text, &h, storage);
    struct hostent* out = code ? NULL : hostResult(&h);

    if (code < 0) {
        errno = ENOMEM;
    }

    free(text);

    return out;
}

int __kernel_gethostbyaddr_r(const void* restrict address, socklen_t n, int family, struct hostent* restrict out, size_t outSize,
                             char* restrict buf, size_t size, struct hostent** restrict result, size_t resultSize, int* restrict error) {
    sized("gethostbyaddr_r", outSize, sizeof(struct hostent));
    sized("gethostbyaddr_r", resultSize, sizeof(struct hostent));

    char* text;
    struct Host h;
    unsigned char storage[16];
    int e = hostInto(hostByAddress(address, n, family, &text, &h, storage), &h, out, buf, size, result, error);

    free(text);

    return e;
}

static size_t hostIndex;

struct hostent* __kernel_gethostent(size_t size) {
    sized("gethostent", size, sizeof(struct hostent));

    char* text;
    struct Host h;
    unsigned char storage[16];
    int code = hostEntry(NULL, 0, &hostIndex, &text, &h, storage);
    struct hostent* out = code ? NULL : hostResult(&h);

    if (code < 0) {
        errno = ENOMEM;
    }

    free(text);

    return out;
}

void __kernel_sethostent(int stay) {
    hostIndex = 0;
}

void __kernel_endhostent(void) {
    hostIndex = 0;
}

const char* __kernel_hstrerror(int code) {
    static const char* const messages[] = {"Host not found", "Try again", "Non-recoverable error", "Address not available"};

    return code >= 1 && code <= 4 ? messages[code - 1] : "Unknown error";
}

// getaddrinfo and getnameinfo.

// An address of a host: its family's bytes, and an IPv6 one's scope.
struct Address {
    int family;
    unsigned char bytes[16];
    uint32_t scope;
};

// A list of addresses and the name they are of, on the heap.
struct Found {
    struct Address* addresses;
    size_t count;
    size_t room;
    char* canonical;
};

static int added(struct Found* f, const struct Address* a) {
    if (f->count == f->room) {
        struct Address* more = realloc(f->addresses, (f->room = f->room ? 2 * f->room : 4) * sizeof *more);

        if (!more) {
            return 0;
        }

        f->addresses = more;
    }

    f->addresses[f->count++] = *a;

    return 1;
}

static int named(struct Found* f, const char* name) {
    return f->canonical || (f->canonical = strdup(name)) != NULL;
}

static void forget(struct Found* f) {
    free(f->addresses);
    free(f->canonical);
}

// An IPv4-mapped IPv6 address as the IPv4 one.
static void unmapped(struct Address* a) {
    static const unsigned char prefix[12] = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff};

    if (a->family == AF_INET6 && memcmp(a->bytes, prefix, 12) == 0) {
        memmove(a->bytes, a->bytes + 12, 4);
        a->family = AF_INET;
    }
}

// A host's address as numbers: IPv4's as inet_aton reads it, or IPv6's
// with a scope, a number or a card's name, after a %.
static int numeric(const char* name, struct Address* a) {
    struct in_addr v4;
    const char* percent = strchr(name, '%');
    size_t n = percent ? (size_t)(percent - name) : strlen(name);
    char host[64];

    *a = (struct Address){0};

    if (inet_aton(name, &v4)) {
        a->family = AF_INET;
        memcpy(a->bytes, &v4, 4);

        return 1;
    }

    if (n >= sizeof host) {
        return 0;
    }

    memcpy(host, name, n);
    host[n] = 0;

    if (inet_pton(AF_INET6, host, a->bytes) != 1) {
        return 0;
    }

    a->family = AF_INET6;

    if (percent && percent[1]) {
        const char* zone = percent + 1;
        uint64_t v = 0;
        size_t digits = strspn(zone, "0123456789");

        for (size_t i = 0; i < digits && v <= UINT32_MAX; i++) {
            v = v * 10 + (zone[i] - '0');
        }

        if (zone[digits] == 0 && v <= UINT32_MAX) {
            a->scope = v;
        } else if (!(a->scope = if_nametoindex(zone))) {
            return 0;
        }
    }

    return 1;
}

// The addresses of /etc/hosts' entries a name of which is the name, in
// any case, the first one's name its canonical one: 0, EAI_NONAME, or
// EAI_MEMORY.
static int hostsOf(const char* name, struct Found* f) {
    int e;
    char* text = database("/etc/hosts", NULL, &e);
    char* at = text;
    struct Line l;

    if (!text) {
        return e ? EAI_MEMORY : EAI_NONAME;
    }

    while (nextLine(&at, &l)) {
        struct Address a = {0};
        const char* other;
        const char* rest = l.rest;
        int same = strcasecmp(l.value, name) == 0;

        for (size_t n; !same && (n = alias(&rest, &other));) {
            same = n == strlen(name) && strncasecmp(other, name, n) == 0;
        }

        if (same && hostAddress(l.name, a.bytes, &a.family) && (!added(f, &a) || !named(f, l.value))) {
            free(text);

            return EAI_MEMORY;
        }
    }

    free(text);

    return f->count ? 0 : EAI_NONAME;
}

// The addresses the system's name servers give a name: /dev/dns/query
// answers "name a\n" with lines of the name, "a" and an address.
static int lookup(const char* name, struct Found* f) {
    size_t n = strlen(name);

    if (n == 0 || n - (name[n - 1] == '.') > 253) {
        return EAI_NONAME;
    }

    for (const char* c = name; *c; c++) {
        if ((unsigned char)*c <= ' ' || *c == 0x7f) {
            return EAI_NONAME;
        }
    }

    int fd = open("/dev/dns/query", O_RDWR | O_CLOEXEC);

    if (fd < 0) {
        return EAI_FAIL;
    }

    char* question = malloc(n + 3);

    if (!question) {
        close(fd);

        return EAI_MEMORY;
    }

    memcpy(question, name, n);
    memcpy(question + n, " a\n", 3);

    ssize_t wrote = write(fd, question, n + 3);
    int e = errno;

    free(question);

    if (wrote < 0) {
        close(fd);

        return e == ENOENT ? EAI_NONAME : e == ETIMEDOUT || e == ENETUNREACH || e == EINTR ? EAI_AGAIN : EAI_FAIL;
    }

    char* text = NULL;
    size_t size = 0, room = 0;
    int code = 0;

    while (size < LARGEST) {
        if (size + 1 >= room) {
            char* more = realloc(text, room = room ? 2 * room : 4096);

            if (!more) {
                code = EAI_MEMORY;

                break;
            }

            text = more;
        }

        ssize_t got = read(fd, text + size, room - size - 1);

        if (got < 0) {
            code = EAI_FAIL;
        }

        if (got <= 0) {
            break;
        }

        size += got;
    }

    close(fd);

    if (code) {
        free(text);

        return code;
    }

    text[size] = 0;

    char* at = text;
    struct Line l;

    while (nextLine(&at, &l)) {
        const char* rest = l.rest;
        const char* address;
        size_t length = alias(&rest, &address);
        const char* more;
        struct Address a = {0};
        char copy[64];

        if (strcmp(l.value, "a") != 0 || !length || length >= sizeof copy || alias(&rest, &more)) {
            continue;
        }

        memcpy(copy, address, length);
        copy[length] = 0;

        if (!hostAddress(copy, a.bytes, &a.family)) {
            continue;
        }

        unmapped(&a);

        if (!added(f, &a) || !named(f, l.name)) {
            free(text);

            return EAI_MEMORY;
        }
    }

    free(text);

    return f->count ? 0 : EAI_NONAME;
}

// Whether the system has sockets of the family, as AI_ADDRCONFIG asks.
static int configured(int family) {
    int s = socket(family, SOCK_DGRAM, 0);

    if (s >= 0) {
        close(s);
    }

    return s >= 0;
}

struct Kind {
    int socktype;
    int protocol;
};

static int number16(const char* s, int* port) {
    long v = 0;

    if (!*s) {
        return 0;
    }

    for (; *s; s++) {
        if (*s < '0' || *s > '9' || (v = v * 10 + (*s - '0')) > 65535) {
            return 0;
        }
    }

    *port = v;

    return 1;
}

static struct addrinfo* made(int flags, const struct Address* a, int port, const struct Kind* k, const char* canonical) {
    size_t length = a->family == AF_INET ? sizeof(struct sockaddr_in) : sizeof(struct sockaddr_in6);
    size_t name = canonical && *canonical ? strlen(canonical) + 1 : 0;
    struct addrinfo* ai = calloc(1, sizeof *ai + length + name);

    if (!ai) {
        return NULL;
    }

    char* addr = (char*)(ai + 1);

    *ai = (struct addrinfo){flags, a->family, k->socktype, k->protocol, length, (struct sockaddr*)addr, name ? addr + length : NULL, NULL};

    if (a->family == AF_INET) {
        struct sockaddr_in* in = (struct sockaddr_in*)addr;

        in->sin_family = AF_INET;
        in->sin_port = htons(port);
        memcpy(&in->sin_addr, a->bytes, 4);
    } else {
        struct sockaddr_in6* in6 = (struct sockaddr_in6*)addr;

        in6->sin6_family = AF_INET6;
        in6->sin6_port = htons(port);
        memcpy(&in6->sin6_addr, a->bytes, 16);
        in6->sin6_scope_id = a->scope;
    }

    if (name) {
        strcpy(addr + length, canonical);
    }

    return ai;
}

void __kernel_freeaddrinfo(struct addrinfo* ai, size_t size) {
    sized("freeaddrinfo", size, sizeof(struct addrinfo));

    while (ai) {
        struct addrinfo* next = ai->ai_next;

        free(ai);
        ai = next;
    }
}

static int addrinfo(const char* node, const char* service, const struct addrinfo* hints, struct addrinfo** res) {
    int flags = hints ? hints->ai_flags : 0, family = hints ? hints->ai_family : 0;
    int socktype = hints ? hints->ai_socktype : 0, protocol = hints ? hints->ai_protocol : 0;
    const int known = AI_PASSIVE | AI_CANONNAME | AI_NUMERICHOST | AI_V4MAPPED | AI_ALL | AI_ADDRCONFIG | AI_NUMERICSERV;

    if (!node && !service) {
        return EAI_NONAME;
    }

    if (flags & ~known) {
        return EAI_BADFLAGS;
    }

    if (family != 0 && family != AF_INET && family != AF_INET6) {
        return EAI_FAMILY;
    }

    if (socktype != 0 && socktype != SOCK_STREAM && socktype != SOCK_DGRAM && socktype != SOCK_RAW) {
        return EAI_SOCKTYPE;
    }

    const struct Kind all[] = {{SOCK_STREAM, IPPROTO_TCP}, {SOCK_DGRAM, IPPROTO_UDP}, {SOCK_RAW, protocol}};
    struct Kind kinds[3];
    int count = 0;

    for (int i = 0; i < (socktype || protocol ? 3 : 2); i++) {
        if ((socktype == 0 || all[i].socktype == socktype) && (protocol == 0 || all[i].protocol == protocol)) {
            kinds[count++] = all[i];
        }
    }

    if (!count) {
        return EAI_SOCKTYPE;
    }

    int port = 0;

    if (service && !number16(service, &port)) {
        int kept = 0;

        if (flags & AI_NUMERICSERV) {
            return EAI_NONAME;
        }

        for (int i = 0; i < count; i++) {
            char* text;
            struct Service s;
            int e = findService(byName, service, 0, kinds[i].socktype == SOCK_DGRAM ? "udp" : "tcp", NULL, &text, &s);

            if (e == ENOMEM) {
                return EAI_MEMORY;
            }

            if (!e && kinds[i].socktype != SOCK_RAW) {
                port = s.port;
                kinds[kept++] = kinds[i];
            }

            free(text);
        }

        if (!kept) {
            return EAI_SERVICE;
        }

        count = kept;
    }

    struct Found f = {0};
    struct Address a;
    int code = 0;

    if (!node) {
        struct Address any4 = {AF_INET, {0}, 0}, any6 = {AF_INET6, {0}, 0};

        if (!(flags & AI_PASSIVE)) {
            any4.bytes[0] = 127;
            any4.bytes[3] = 1;
            any6.bytes[15] = 1;
        }

        code = added(&f, &any4) && added(&f, &any6) ? 0 : EAI_MEMORY;
    } else if (numeric(node, &a)) {
        code = added(&f, &a) && named(&f, node) ? 0 : EAI_MEMORY;
    } else if (flags & AI_NUMERICHOST) {
        code = EAI_NONAME;
    } else if ((code = hostsOf(node, &f)) == EAI_NONAME) {
        code = lookup(node, &f);
    }

    if (code) {
        forget(&f);

        return code;
    }

    int have6 = 0, usable4 = 1, usable6 = 1;
    struct addrinfo *first = NULL, *last = NULL;

    for (size_t i = 0; i < f.count; i++) {
        have6 |= f.addresses[i].family == AF_INET6;
    }

    if (flags & AI_ADDRCONFIG) {
        usable4 = configured(AF_INET);
        usable6 = configured(AF_INET6);
    }

    for (size_t i = 0; i < f.count; i++) {
        struct Address kept = f.addresses[i];
        int v4 = kept.family == AF_INET;

        if (family == AF_INET && !v4) {
            continue;
        }

        if (family == AF_INET6 && v4) {
            if (!(flags & AI_V4MAPPED) || (have6 && !(flags & AI_ALL))) {
                continue;
            }

            memmove(kept.bytes + 12, kept.bytes, 4);
            memset(kept.bytes, 0, 10);
            kept.bytes[10] = kept.bytes[11] = 0xff;
            kept.family = AF_INET6;
            kept.scope = 0;
        } else if ((v4 && !usable4) || (!v4 && !usable6)) {
            continue;
        }

        for (int k = 0; k < count; k++) {
            struct addrinfo* ai = made(flags, &kept, port, &kinds[k], !first && flags & AI_CANONNAME ? f.canonical : NULL);

            if (!ai) {
                __kernel_freeaddrinfo(first, sizeof *first);
                forget(&f);

                return EAI_MEMORY;
            }

            if (last) {
                last->ai_next = ai;
            } else {
                first = ai;
            }

            last = ai;
        }
    }

    forget(&f);

    if (!first) {
        return EAI_NONAME;
    }

    *res = first;

    return 0;
}

int __kernel_getaddrinfo(const char* restrict node, const char* restrict service, const struct addrinfo* restrict hints, size_t hintsSize,
                         struct addrinfo** restrict res, size_t resSize) {
    sized("getaddrinfo", hintsSize, sizeof(struct addrinfo));
    sized("getaddrinfo", resSize, sizeof(struct addrinfo));

    int saved = errno;
    int code = addrinfo(node, service, hints, res);

    errno = saved;

    return code;
}

const char* __kernel_gai_strerror(int code) {
    static const char* const messages[] = {"Invalid flags",
                                           "Name does not resolve",
                                           "Try again",
                                           "Non-recoverable error",
                                           "Name has no usable address",
                                           "Unrecognized address family or invalid length",
                                           "Unrecognized socket type",
                                           "Unrecognized service",
                                           "Unknown error",
                                           "Out of memory",
                                           "System error",
                                           "Overflow"};

    return code <= -1 && code >= -12 ? messages[-code - 1] : "Unknown error";
}

static int nameinfo(const struct sockaddr* sa, socklen_t length, char* host, socklen_t hostSize, char* serv, socklen_t servSize, int flags) {
    struct Address a = {0};

    if (length < 2) {
        return EAI_FAMILY;
    }

    if (sa->sa_family == AF_INET && length >= sizeof(struct sockaddr_in)) {
        a.family = AF_INET;
        memcpy(a.bytes, &((const struct sockaddr_in*)sa)->sin_addr, 4);
    } else if (sa->sa_family == AF_INET6 && length >= sizeof(struct sockaddr_in6)) {
        a.family = AF_INET6;
        memcpy(a.bytes, &((const struct sockaddr_in6*)sa)->sin6_addr, 16);
        a.scope = ((const struct sockaddr_in6*)sa)->sin6_scope_id;
    } else {
        return EAI_FAMILY;
    }

    if (host && hostSize > 0) {
        char name[NI_MAXHOST + 16];
        char* text = NULL;
        struct Host h;
        unsigned char storage[16];
        int code = flags & NI_NUMERICHOST ? HOST_NOT_FOUND : hostEntry(a.bytes, a.family, NULL, &text, &h, storage);

        if (code < 0) {
            return EAI_MEMORY;
        }

        if (!code) {
            snprintf(name, sizeof name, "%s", h.name);

            if (flags & NI_NOFQDN) {
                name[strcspn(name, ".")] = 0;
            }
        }

        free(text);

        if (code) {
            if (flags & NI_NAMEREQD) {
                return EAI_NONAME;
            }

            inet_ntop(a.family, a.bytes, name, sizeof name);

            if (a.scope) {
                snprintf(name + strlen(name), sizeof name - strlen(name), "%%%u", (unsigned)a.scope);
            }
        }

        if (strlen(name) >= hostSize) {
            return EAI_OVERFLOW;
        }

        strcpy(host, name);
    }

    if (serv && servSize > 0) {
        int port = ntohs(((const struct sockaddr_in*)sa)->sin_port);
        char name[NI_MAXSERV + 16];
        char* text = NULL;
        struct Service s;

        snprintf(name, sizeof name, "%d", port);

        if (!(flags & NI_NUMERICSERV) && findService(byNumber, NULL, port, flags & NI_DGRAM ? "udp" : "tcp", NULL, &text, &s) == 0) {
            snprintf(name, sizeof name, "%s", s.line.name);
        }

        free(text);

        if (strlen(name) >= servSize) {
            return EAI_OVERFLOW;
        }

        strcpy(serv, name);
    }

    return 0;
}

int __kernel_getnameinfo(const struct sockaddr* restrict sa, socklen_t length, char* restrict host, socklen_t hostSize, char* restrict serv,
                         socklen_t servSize, int flags) {
    int saved = errno;
    int code = nameinfo(sa, length, host, hostSize, serv, servSize, flags);

    errno = saved;

    return code;
}
