/*
 * BIND's resolver, the part of it programs use: the messages of DNS and
 * the system's name servers, whose answers /dev/dns/message gives, a
 * question written to it and the answer read back.
 *
 * res_init's state: the servers are the cards' (interfaces.h), at most
 * MAXNS; the search list /etc/resolv.conf's domain or
 * search line, else the host name's domain, LOCALDOMAIN's where it is set,
 * and RES_OPTIONS' ndots over the file's options. A function of a state
 * not yet made makes it first. res_query's answer is the message, its
 * error in res_h_errno: NO_DATA where there is no record, HOST_NOT_FOUND
 * of NXDOMAIN, TRY_AGAIN of SERVFAIL or of no answer at all. res_search
 * asks for the name as it is first where it has ndots dots, then with
 * each domain, then as it is where it has not been.
 *
 * A name in a message is labels, each its length and bytes, ended by an
 * empty one or by a pointer, two bytes of 0xc0 and an offset, to the rest
 * of a name before it; dn_comp points to the longest end of a name of the
 * list dnptrs has, and adds its own there. As text a label's dots,
 * specials and bytes outside the printable ASCII are escaped, \. and
 * \ddd, and a name is its labels between dots, dn_expand's root "" and
 * ns_name_uncompress's ".". A message that leaves its bounds, or loops,
 * is EMSGSIZE.
 */
#include "interfaces.h"

#include <arpa/inet.h>
#include <arpa/nameser.h>
#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <netdb.h>
#include <resolv.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static struct __res_state global;

struct __res_state* __kernel___res_state(size_t size) {
    return &global;
}

// Names.

// The uncompressed wire form of a name's text, its labels and an empty
// one, and how many labels: 0, or -1 for a name of none.
static int wire(const char* name, unsigned char out[NS_MAXCDNAME], size_t* length) {
    size_t total = 1, at = 0;
    unsigned char label[NS_MAXCDNAME + 1];
    size_t n = 0;
    int count = 0;

    if (strcmp(name, ".") == 0) {
        out[0] = 0;
        *length = 1;

        return 0;
    }

    for (size_t i = 0;; i++) {
        unsigned char c = name[i];
        int end = c == 0 || c == '.';

        if (c == '\\' && isdigit((unsigned char)name[i + 1]) && isdigit((unsigned char)name[i + 2]) && isdigit((unsigned char)name[i + 3])) {
            int v = (name[i + 1] - '0') * 100 + (name[i + 2] - '0') * 10 + (name[i + 3] - '0');

            if (v > 255) {
                return -1;
            }

            c = v;
            i += 3;
        } else if (c == '\\') {
            if (!name[i + 1]) {
                return -1;
            }

            c = name[++i];
        } else if (end && (c == '.' || n > 0)) {
            if (n == 0 || n > NS_MAXLABEL || (total += n + 1) > NS_MAXCDNAME) {
                return -1;
            }

            out[at++] = n;
            memcpy(out + at, label, n);
            at += n;
            n = 0;
            count++;
        }

        if (!c && end) {
            break;
        }

        if (!end) {
            if (n > NS_MAXCDNAME) {
                return -1;
            }

            label[n++] = c;
        }
    }

    out[at++] = 0;
    *length = at;

    return count;
}

// The labels of the name at at, following pointers, their wire form in
// out, and how many bytes of the message the name takes there; -1 where
// it is none.
static int labels(const unsigned char* msg, size_t size, size_t at, unsigned char out[NS_MAXCDNAME], size_t* used) {
    size_t total = 1, start = at, length = 0;
    size_t jumps = 0;
    int count = 0;

    *used = 0;

    for (;;) {
        if (at >= size) {
            return -1;
        }

        unsigned n = msg[at];

        if (n == 0) {
            *used = *used ? *used : at + 1 - start;
            out[length] = 0;

            return count;
        }

        if ((n & NS_CMPRSFLGS) == NS_CMPRSFLGS) {
            if (at + 1 >= size || jumps > size) {
                return -1;
            }

            *used = *used ? *used : at + 2 - start;
            jumps++;
            at = (n & ~NS_CMPRSFLGS) << 8 | msg[at + 1];

            continue;
        }

        if (n & NS_CMPRSFLGS || at + 1 + n > size || (total += n + 1) > NS_MAXCDNAME) {
            return -1;
        }

        memcpy(out + length, msg + at, n + 1);
        length += n + 1;
        count++;
        at += 1 + n;
    }
}

// Where the name at at ends, or 0 where it does not in size.
static size_t skipped(const unsigned char* msg, size_t size, size_t at) {
    while (at < size) {
        unsigned n = msg[at];

        if (n == 0) {
            return at + 1;
        }

        if ((n & NS_CMPRSFLGS) == NS_CMPRSFLGS) {
            return at + 2 <= size ? at + 2 : 0;
        }

        if (n & NS_CMPRSFLGS) {
            return 0;
        }

        at += 1 + n;
    }

    return 0;
}

// Where count records from at end, the question's or the others', or 0.
static size_t records(const unsigned char* msg, size_t size, size_t at, int question, long count) {
    for (long i = 0; i < count; i++) {
        size_t end = skipped(msg, size, at);

        if (!end) {
            return 0;
        }

        at = end + NS_QFIXEDSZ;

        if (!question) {
            if ((at = end + NS_RRFIXEDSZ) > size) {
                return 0;
            }

            at += msg[at - 2] << 8 | msg[at - 1];
        }

        if (at > size) {
            return 0;
        }
    }

    return at;
}

// A name's text of its labels, in out of the size: its length, or -1
// where it does not fit.
static int text(const unsigned char* name, const char* root, char* out, size_t size) {
    size_t at = 0;
    char piece[8];

    if (!*name) {
        if (strlen(root) >= size) {
            return -1;
        }

        strcpy(out, root);

        return strlen(root);
    }

    for (int first = 1; *name; first = 0) {
        size_t n = *name++;

        for (size_t i = 0; i < n + !first; i++) {
            unsigned char c = i == 0 && !first ? '.' : name[i - !first];

            if (i == 0 && !first) {
                snprintf(piece, sizeof piece, ".");
            } else if (strchr(".;\\()\"@$", c) && c) {
                snprintf(piece, sizeof piece, "\\%c", c);
            } else if (c <= ' ' || c >= 0x7f) {
                snprintf(piece, sizeof piece, "\\%03d", c);
            } else {
                snprintf(piece, sizeof piece, "%c", c);
            }

            size_t length = strlen(piece);

            if (at + length >= size) {
                return -1;
            }

            memcpy(out + at, piece, length);
            at += length;
        }

        name += n;
    }

    out[at] = 0;

    return at;
}

static int sameLabel(const unsigned char* a, const unsigned char* b) {
    if (*a != *b) {
        return 0;
    }

    for (size_t i = 1; i <= *a; i++) {
        if (tolower(a[i]) != tolower(b[i])) {
            return 0;
        }
    }

    return 1;
}

static int sameName(const unsigned char* a, const unsigned char* b) {
    for (; *a && *b; a += *a + 1, b += *b + 1) {
        if (!sameLabel(a, b)) {
            return 0;
        }
    }

    return !*a && !*b;
}

// Where in the message a name of the list ends in the labels, or -1.
static long found(const unsigned char* msg, size_t size, const size_t* names, size_t count, const unsigned char* name) {
    for (size_t k = 0; k < count; k++) {
        size_t jumps = 0;

        for (size_t at = names[k]; at < size && jumps <= size;) {
            unsigned n = msg[at];

            if ((n & NS_CMPRSFLGS) == NS_CMPRSFLGS) {
                if (at + 1 >= size) {
                    break;
                }

                jumps++;
                at = (n & ~NS_CMPRSFLGS) << 8 | msg[at + 1];

                continue;
            }

            if (n == 0 || n & NS_CMPRSFLGS) {
                break;
            }

            unsigned char here[NS_MAXCDNAME];
            size_t used;

            if (labels(msg, size, at, here, &used) >= 0 && sameName(here, name)) {
                return at;
            }

            at += 1 + n;
        }
    }

    return -1;
}

int __kernel_dn_comp(const char* name, unsigned char* out, int length, unsigned char** dnptrs, unsigned char** lastdnptr) {
    unsigned char plain[NS_MAXCDNAME], made[NS_MAXCDNAME];
    size_t size, at = 0;
    int count = wire(name, plain, &size);

    if (count < 0) {
        errno = EMSGSIZE;

        return -1;
    }

    const unsigned char* base = dnptrs ? dnptrs[0] : NULL;
    size_t names[64];
    size_t known = 0;
    size_t message = base && out >= base ? out - base : 0;

    if (base && out >= base) {
        for (unsigned char** p = dnptrs + 1; p != lastdnptr && *p && known < 64; p++) {
            names[known++] = *p - base;
        }
    }

    int literal = 0;
    const unsigned char* label = plain;

    for (int i = 0; i < count; i++, label += *label + 1) {
        long off = found(base, message, names, known, label);

        if (base && off >= 0 && off < 0x4000) {
            made[at++] = NS_CMPRSFLGS | off >> 8;
            made[at++] = off;

            break;
        }

        memcpy(made + at, label, *label + 1);
        at += *label + 1;
        literal++;
    }

    if (literal == count) {
        made[at++] = 0;
    }

    if ((long)at > length) {
        errno = EMSGSIZE;

        return -1;
    }

    memcpy(out, made, at);

    if (base && out >= base && lastdnptr && literal > 0 && message < 0x4000) {
        unsigned char** slot = dnptrs + 1;

        while (slot != lastdnptr && *slot) {
            slot++;
        }

        if (slot + 1 < lastdnptr) {
            slot[0] = out;
            slot[1] = NULL;
        }
    }

    return at;
}

static int expanded(const unsigned char* msg, const unsigned char* eom, const unsigned char* src, char* dst, size_t size, const char* root) {
    unsigned char name[NS_MAXCDNAME];
    size_t used;

    if (eom <= msg || src < msg || src >= eom || labels(msg, eom - msg, src - msg, name, &used) < 0 || text(name, root, dst, size) < 0) {
        errno = EMSGSIZE;

        return -1;
    }

    return used;
}

int __kernel_dn_expand(const unsigned char* msg, const unsigned char* eom, const unsigned char* src, char* dst, int size) {
    return expanded(msg, eom, src, dst, size > 0 ? size : 0, "");
}

int __kernel_ns_name_uncompress(const unsigned char* msg, const unsigned char* eom, const unsigned char* src, char* dst, size_t size) {
    return expanded(msg, eom, src, dst, size, ".");
}

int __kernel_dn_skipname(const unsigned char* at, const unsigned char* eom) {
    size_t end = eom > at ? skipped(at, eom - at, 0) : 0;

    if (!end) {
        errno = EMSGSIZE;

        return -1;
    }

    return end;
}

int __kernel_ns_skiprr(const unsigned char* at, const unsigned char* eom, ns_sect section, int count) {
    size_t end = eom >= at ? records(at, eom - at, 0, section == 0, count) : 0;

    if (eom < at || (!end && count > 0)) {
        errno = EMSGSIZE;

        return -1;
    }

    return end;
}

// Messages.

static void sectionOf(ns_msg* handle, int section) {
    handle->_sect = section;
    handle->_rrnum = section == ns_s_max ? -1 : 0;
    handle->_msg_ptr = section == ns_s_max ? NULL : handle->_sections[section];
}

int __kernel_ns_initparse(const unsigned char* msg, int size, ns_msg* handle, size_t handleSize) {
    size_t at = NS_HFIXEDSZ;
    uint16_t counts[ns_s_max];
    const unsigned char* sections[ns_s_max] = {NULL};

    if (size < NS_HFIXEDSZ) {
        errno = EMSGSIZE;

        return -1;
    }

    for (int i = 0; i < ns_s_max; i++) {
        counts[i] = msg[4 + 2 * i] << 8 | msg[5 + 2 * i];

        if (!counts[i]) {
            continue;
        }

        sections[i] = msg + at;
        at = records(msg, size, at, i == 0, counts[i]);

        if (!at) {
            errno = EMSGSIZE;

            return -1;
        }
    }

    if (at != (size_t)size) {
        errno = EMSGSIZE;

        return -1;
    }

    handle->_msg = msg;
    handle->_eom = msg + size;
    handle->_id = msg[0] << 8 | msg[1];
    handle->_flags = msg[2] << 8 | msg[3];

    for (int i = 0; i < ns_s_max; i++) {
        handle->_counts[i] = counts[i];
        handle->_sections[i] = sections[i];
    }

    sectionOf(handle, ns_s_max);

    return 0;
}

int __kernel_ns_parserr(ns_msg* handle, size_t handleSize, ns_sect section, int number, ns_rr* rr, size_t rrSize) {
    if ((unsigned)section >= ns_s_max) {
        errno = ENODEV;

        return -1;
    }

    if (handle->_sect != section) {
        sectionOf(handle, section);
    }

    unsigned num = number == -1 ? (unsigned)handle->_rrnum : (unsigned)number;
    unsigned count = handle->_counts[section];

    if (num >= count) {
        errno = ENODEV;

        return -1;
    }

    if (num < (unsigned)handle->_rrnum) {
        sectionOf(handle, section);
    }

    const unsigned char* msg = handle->_msg;
    size_t size = handle->_eom - msg;
    size_t at = handle->_msg_ptr - msg;

    if (num > (unsigned)handle->_rrnum && !(at = records(msg, size, at, section == 0, num - handle->_rrnum))) {
        errno = EMSGSIZE;

        return -1;
    }

    unsigned char name[NS_MAXCDNAME];
    size_t used;

    if (labels(msg, size, at, name, &used) < 0 || text(name, "", rr->name, NS_MAXDNAME) < 0 || (at += used) + 4 > size) {
        errno = EMSGSIZE;

        return -1;
    }

    rr->type = msg[at] << 8 | msg[at + 1];
    rr->rr_class = msg[at + 2] << 8 | msg[at + 3];
    rr->ttl = 0;
    rr->rdlength = 0;
    rr->rdata = NULL;
    at += 4;

    if (section != 0) {
        if (at + 6 > size) {
            errno = EMSGSIZE;

            return -1;
        }

        rr->ttl = (uint32_t)msg[at] << 24 | msg[at + 1] << 16 | msg[at + 2] << 8 | msg[at + 3];
        rr->rdlength = msg[at + 4] << 8 | msg[at + 5];
        at += 6;

        if (at + rr->rdlength > size) {
            errno = EMSGSIZE;

            return -1;
        }

        rr->rdata = msg + at;
        at += rr->rdlength;
    }

    handle->_rrnum = num + 1;
    handle->_msg_ptr = msg + at;

    return 0;
}

// The resolver.

// A file's text, at most of the size, NUL-ended in buf, or an empty one.
static void small(const char* path, char* buf, size_t size) {
    int fd = open(path, O_RDONLY | O_CLOEXEC);
    size_t n = 0;

    for (ssize_t got; fd >= 0 && n + 1 < size && (got = read(fd, buf + n, size - 1 - n)) != 0;) {
        if (got < 0 && errno != EINTR) {
            break;
        }

        n += got > 0 ? got : 0;
    }

    if (fd >= 0) {
        close(fd);
    }

    buf[n] = 0;
}

// The cards' name servers, at most MAXNS, in their order.
static int servers(struct in_addr out[MAXNS]) {
    struct Interface* cards;
    int count = __interfaces(&cards), found = 0;

    for (int i = 0; i < count && found < MAXNS; i++) {
        if (cards[i].named) {
            out[found++] = cards[i].dns;
        }
    }

    free(cards);

    return found;
}

static void options(const char* words, int* ndots) {
    static const char blank[] = " \t\n\v\f\r";

    for (const char* w = words + strspn(words, blank); *w; w += strspn(w, blank)) {
        size_t n = strcspn(w, blank);
        char word[32];

        if (n > 6 && n < sizeof word && strncmp(w, "ndots:", 6) == 0) {
            char* end;

            memcpy(word, w + 6, n - 6);
            word[n - 6] = 0;

            long v = strtol(word, &end, 10);

            if (!*end && end != word && word[0] != ' ') {
                *ndots = v < 0 ? 0 : v > RES_MAXNDOTS ? RES_MAXNDOTS : v;
            }
        }

        w += n;
    }
}

static void init(res_state state) {
    static const char blank[] = " \t\n\v\f\r";
    char conf[8192], domains[256 * 4] = "", host[256];
    int ndots = 1, given = 0;

    small("/etc/resolv.conf", conf, sizeof conf);

    for (char *line = conf, *end; line; line = end ? end + 1 : NULL) {
        end = strchr(line, '\n');

        if (end) {
            *end = 0;
        }

        line[strcspn(line, "#;")] = 0;

        char* word = line + strspn(line, blank);
        size_t n = strcspn(word, blank);
        char* rest = word + n + strspn(word + n, blank);

        if (!*rest) {
            continue;
        }

        if (n == 6 && strncmp(word, "domain", 6) == 0) {
            snprintf(domains, sizeof domains, "%.*s", (int)strcspn(rest, blank), rest);
            given = 1;
        } else if (n == 6 && strncmp(word, "search", 6) == 0) {
            snprintf(domains, sizeof domains, "%s", rest);
            given = 1;
        } else if (n == 7 && strncmp(word, "options", 7) == 0) {
            options(rest, &ndots);
        }
    }

    const char* local = getenv("LOCALDOMAIN");
    const char* extra = getenv("RES_OPTIONS");

    if (local) {
        snprintf(domains, sizeof domains, "%s", local);
        given = 1;
    }

    if (extra) {
        options(extra, &ndots);
    }

    if ((!given || !domains[strspn(domains, blank)]) && gethostname(host, sizeof host) == 0) {
        char* dot = strchr(host, '.');

        snprintf(domains, sizeof domains, "%s", dot && dot[1] ? dot + 1 : "");
    }

    struct in_addr found[MAXNS];
    int count = servers(found);

    memset(state, 0, sizeof *state);
    state->retrans = RES_TIMEOUT;
    state->retry = RES_DFLRETRY;
    state->options = RES_DEFAULT | RES_INIT;
    state->nscount = count;
    getentropy(&state->id, sizeof state->id);
    state->ndots = ndots;

    for (int i = 0; i < count; i++) {
        state->nsaddr_list[i] = (struct sockaddr_in){.sin_family = AF_INET, .sin_port = htons(NS_DEFAULTPORT), .sin_addr = found[i]};
    }

    size_t at = 0;
    int i = 0;

    for (const char* d = domains + strspn(domains, blank); *d && i < MAXDNSRCH; d += strspn(d, blank), i++) {
        size_t n = strcspn(d, blank);

        if (at + n + 1 > sizeof state->defdname) {
            break;
        }

        memcpy(state->defdname + at, d, n);
        state->defdname[at + n] = 0;
        state->dnsrch[i] = state->defdname + at;
        at += n + 1;
        d += n;
    }
}

static void ready(res_state state) {
    if (!(state->options & RES_INIT)) {
        init(state);
    }
}

// A question of the name, its message in m: its length, or -1 and errno.
static int question(res_state state, int op, const char* name, int class, int type, unsigned char m[NS_HFIXEDSZ + NS_MAXCDNAME + NS_QFIXEDSZ]) {
    size_t n;

    if ((unsigned)op > 15 || (unsigned)class > 0xffff || (unsigned)type > 0xffff) {
        errno = EINVAL;

        return -1;
    }

    if (wire(name, m + NS_HFIXEDSZ, &n) < 0) {
        errno = EMSGSIZE;

        return -1;
    }

    memset(m, 0, NS_HFIXEDSZ);
    getentropy(m, 2);
    m[2] = op << 3 | (state->options & RES_RECURSE ? 1 : 0);
    m[5] = 1;

    unsigned char* tail = m + NS_HFIXEDSZ + n;

    tail[0] = type >> 8;
    tail[1] = type;
    tail[2] = class >> 8;
    tail[3] = class;

    return NS_HFIXEDSZ + n + NS_QFIXEDSZ;
}

// The answer to a message, on the heap, and its length; null and errno.
static unsigned char* sent(const unsigned char* query, int length, size_t* size) {
    if (length < NS_HFIXEDSZ) {
        errno = EINVAL;

        return NULL;
    }

    int fd = open("/dev/dns/message", O_RDWR | O_CLOEXEC);

    if (fd < 0) {
        return NULL;
    }

    unsigned char* answer = malloc(NS_MAXMSG + 4096);
    size_t n = 0;
    int e = 0;

    if (!answer) {
        e = ENOMEM;
    } else if (write(fd, query, length) < 0) {
        e = errno;
    }

    while (!e && n < NS_MAXMSG) {
        ssize_t got = read(fd, answer + n, 4096);

        if (got < 0) {
            e = errno;
        } else if (got == 0) {
            break;
        } else {
            n += got;
        }
    }

    close(fd);

    if (e) {
        free(answer);
        errno = e;

        return NULL;
    }

    *size = n;

    return answer;
}

static int copied(unsigned char* to, int room, const unsigned char* answer, size_t size) {
    size_t n = room > 0 ? (size_t)room : 0;

    n = n < size ? n : size;
    memcpy(to, answer, n);

    return n;
}

static int query(res_state state, const char* name, int class, int type, unsigned char* to, int room) {
    unsigned char m[NS_HFIXEDSZ + NS_MAXCDNAME + NS_QFIXEDSZ];
    int length = question(state, QUERY, name, class, type, m);
    size_t size;

    if (length < 0) {
        state->res_h_errno = NO_RECOVERY;

        return -1;
    }

    unsigned char* answer = sent(m, length, &size);

    if (!answer) {
        state->res_h_errno = TRY_AGAIN;

        return -1;
    }

    int n = copied(to, room, answer, size);
    int code = size < NS_HFIXEDSZ ? -1 : answer[3] & 0xf;
    int count = size < NS_HFIXEDSZ ? 0 : answer[6] << 8 | answer[7];

    free(answer);

    if (code == NOERROR && count > 0) {
        return n;
    }

    state->res_h_errno = code == NOERROR ? NO_DATA : code == NXDOMAIN ? HOST_NOT_FOUND : code == SERVFAIL ? TRY_AGAIN : NO_RECOVERY;

    return -1;
}

static int querydomain(res_state state, const char* name, const char* domain, int class, int type, unsigned char* to, int room) {
    char full[NS_MAXDNAME + 1];
    size_t n = strlen(name);

    if (!domain || !*domain) {
        n -= n > 0 && name[n - 1] == '.';

        if (n >= NS_MAXDNAME) {
            state->res_h_errno = NO_RECOVERY;
            errno = EMSGSIZE;

            return -1;
        }

        memcpy(full, name, n);
        full[n] = 0;
    } else if (n + 1 + strlen(domain) >= NS_MAXDNAME) {
        state->res_h_errno = NO_RECOVERY;
        errno = EMSGSIZE;

        return -1;
    } else {
        snprintf(full, sizeof full, "%s.%s", name, domain);
    }

    return query(state, full, class, type, to, room);
}

static int search(res_state state, const char* name, int class, int type, unsigned char* to, int room) {
    unsigned long options = state->options;
    int dots = 0, saved = -1, tried = 0, nodata = 0, servfail = 0;
    size_t length = strlen(name);

    for (const char* c = name; *c; c++) {
        dots += *c == '.';
    }

    if (length > 0 && name[length - 1] == '.') {
        return querydomain(state, name, "", class, type, to, room);
    }

    if (dots >= (int)state->ndots) {
        int n = querydomain(state, name, "", class, type, to, room);

        if (n >= 0) {
            return n;
        }

        saved = state->res_h_errno;
        tried = 1;
    }

    if ((dots == 0 && options & RES_DEFNAMES) || (dots != 0 && options & RES_DNSRCH)) {
        for (int i = 0; i < MAXDNSRCH && state->dnsrch[i]; i++) {
            int n = querydomain(state, name, state->dnsrch[i], class, type, to, room);

            if (n >= 0) {
                return n;
            }

            int code = state->res_h_errno;

            nodata |= code == NO_DATA;
            servfail |= code == TRY_AGAIN;

            if ((code != NO_DATA && code != HOST_NOT_FOUND && code != TRY_AGAIN) || !(options & RES_DNSRCH)) {
                break;
            }
        }
    }

    if (!tried) {
        int n = querydomain(state, name, "", class, type, to, room);

        if (n >= 0) {
            return n;
        }
    }

    if (saved >= 0) {
        state->res_h_errno = saved;
    } else if (nodata) {
        state->res_h_errno = NO_DATA;
    } else if (servfail) {
        state->res_h_errno = TRY_AGAIN;
    }

    return -1;
}

static int mkquery(res_state state, int op, const char* name, int class, int type, unsigned char* buf, int room) {
    unsigned char m[NS_HFIXEDSZ + NS_MAXCDNAME + NS_QFIXEDSZ];

    ready(state);

    int length = question(state, op, name, class, type, m);

    if (length >= 0 && length > room) {
        errno = EMSGSIZE;
        length = -1;
    }

    if (length >= 0) {
        memcpy(buf, m, length);
    }

    return length;
}

static int transmit(res_state state, const unsigned char* msg, int length, unsigned char* to, int room) {
    size_t size;

    ready(state);

    unsigned char* answer = sent(msg, length, &size);

    if (!answer) {
        return -1;
    }

    int n = copied(to, room, answer, size);

    free(answer);

    return n;
}

int __kernel_res_init(void) {
    init(&global);

    return 0;
}

int __kernel_res_ninit(res_state state, size_t size) {
    init(state);

    return 0;
}

void __kernel_res_nclose(res_state state, size_t size) {
}

int __kernel_res_query(const char* name, int class, int type, unsigned char* answer, int room) {
    ready(&global);

    return query(&global, name, class, type, answer, room);
}

int __kernel_res_nquery(res_state state, size_t size, const char* name, int class, int type, unsigned char* answer, int room) {
    ready(state);

    return query(state, name, class, type, answer, room);
}

int __kernel_res_querydomain(const char* name, const char* domain, int class, int type, unsigned char* answer, int room) {
    ready(&global);

    return querydomain(&global, name, domain, class, type, answer, room);
}

int __kernel_res_nquerydomain(res_state state, size_t size, const char* name, const char* domain, int class, int type, unsigned char* answer,
                              int room) {
    ready(state);

    return querydomain(state, name, domain, class, type, answer, room);
}

int __kernel_res_search(const char* name, int class, int type, unsigned char* answer, int room) {
    ready(&global);

    return search(&global, name, class, type, answer, room);
}

int __kernel_res_nsearch(res_state state, size_t size, const char* name, int class, int type, unsigned char* answer, int room) {
    ready(state);

    return search(state, name, class, type, answer, room);
}

int __kernel_res_mkquery(int op, const char* name, int class, int type, const unsigned char* data, int length, const unsigned char* newrr,
                         unsigned char* buf, int room) {
    return mkquery(&global, op, name, class, type, buf, room);
}

int __kernel_res_nmkquery(res_state state, size_t size, int op, const char* name, int class, int type, const unsigned char* data, int length,
                          const unsigned char* newrr, unsigned char* buf, int room) {
    return mkquery(state, op, name, class, type, buf, room);
}

int __kernel_res_send(const unsigned char* msg, int length, unsigned char* answer, int room) {
    return transmit(&global, msg, length, answer, room);
}

int __kernel_res_nsend(res_state state, size_t size, const unsigned char* msg, int length, unsigned char* answer, int room) {
    return transmit(state, msg, length, answer, room);
}
