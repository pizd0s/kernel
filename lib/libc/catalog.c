/*
 * Message catalogs of the BSDs' format, as gencat writes them, big-endian:
 * a header of the magic, the count of sets, the size after the header,
 * and where after it the messages' table and their text begin; the sets'
 * table follows, a set's number, its count of messages and its first,
 * then the messages', a message's number, its length and its text's
 * offset in the text, each ended by a NUL. A catalog is its file, the
 * whole of it, read into the heap; catopen checks all of it, catgets and
 * catclose that it is one: a handle of none, one closed among them, is
 * EBADF. Which file catopen reads, nl_types.h says.
 */
#include <errno.h>
#include <fcntl.h>
#include <locale.h>
#include <nl_types.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define MAGIC 0xff88ff89u
#define HEADER 20
#define ENTRY 12
#define LARGEST (1 << 24)
#define DEFAULT_PATH "/usr/share/nls/%L/%N.cat:/usr/share/nls/%N/%L"

static uint32_t number(const unsigned char* b) {
    return (uint32_t)b[0] << 24 | (uint32_t)b[1] << 16 | (uint32_t)b[2] << 8 | b[3];
}

static int valid(const unsigned char* b, size_t size) {
    if (size < HEADER || number(b) != MAGIC || (uint64_t)number(b + 8) + HEADER != size) {
        return 0;
    }

    uint64_t sets = number(b + 4), messages = HEADER + (uint64_t)number(b + 12), text = HEADER + (uint64_t)number(b + 16);

    if (HEADER + sets * ENTRY > messages || messages > text || text > size) {
        return 0;
    }

    uint64_t count = (text - messages) / ENTRY;

    for (uint64_t i = 0; i < sets; i++) {
        const unsigned char* s = b + HEADER + ENTRY * i;

        if ((uint64_t)number(s + 8) + number(s + 4) > count) {
            return 0;
        }
    }

    for (uint64_t i = 0; i < count; i++) {
        uint64_t at = text + number(b + messages + ENTRY * i + 8);

        if (at >= size || !memchr(b + at, 0, size - at)) {
            return 0;
        }
    }

    return 1;
}

// The first of count entries from at whose number is key or more.
static size_t search(const unsigned char* b, size_t at, size_t count, int32_t key) {
    size_t low = 0, high = count;

    while (low < high) {
        size_t middle = low + (high - low) / 2;

        if ((int32_t)number(b + at + ENTRY * middle) < key) {
            low = middle + 1;
        } else {
            high = middle;
        }
    }

    return low;
}

static const char* find(const unsigned char* b, int32_t set, int32_t message) {
    size_t sets = number(b + 4), messages = HEADER + number(b + 12), text = HEADER + number(b + 16);
    size_t i = search(b, HEADER, sets, set);

    if (i == sets || (int32_t)number(b + HEADER + ENTRY * i) != set) {
        return NULL;
    }

    const unsigned char* s = b + HEADER + ENTRY * i;
    size_t count = number(s + 4), first = messages + ENTRY * (size_t)number(s + 8);
    size_t k = search(b, first, count, message);

    if (k == count || (int32_t)number(b + first + ENTRY * k) != message) {
        return NULL;
    }

    return (const char*)b + text + number(b + first + ENTRY * k + 8);
}

// The catalog of a file, what of it can be read, or null and errno:
// ENOENT for a file of none.
static unsigned char* readCatalog(const char* path) {
    int fd = open(path, O_RDONLY | O_CLOEXEC);

    if (fd < 0) {
        return NULL;
    }

    unsigned char* b = NULL;
    size_t size = 0, room = 0;
    int e = 0;

    while (size <= LARGEST) {
        if (size == room) {
            unsigned char* more = realloc(b, room = room ? 2 * room : 4096);

            if (!more) {
                e = ENOMEM;

                break;
            }

            b = more;
        }

        ssize_t n = read(fd, b + size, room - size);

        if (n < 0 && errno == EINTR) {
            continue;
        }

        if (n <= 0) {
            break;
        }

        size += n;
    }

    close(fd);

    if (!e && (size > LARGEST || !valid(b, size))) {
        e = ENOENT;
    }

    if (e) {
        free(b);
        errno = e;

        return NULL;
    }

    return b;
}

// A template of NLSPATH's with the name and locale in it, on the heap, or
// null for one of an unknown %.
static char* expand(const char* template, size_t length, const char* name, const char* locale) {
    size_t language = strcspn(locale, "_.@");
    const char* territory = strchr(locale, '_') ? strchr(locale, '_') + 1 : locale + strlen(locale);
    size_t territoryLength = strcspn(territory, ".@");
    size_t room = 1;

    for (size_t i = 0; i < length; i++) {
        room += template[i] == '%' ? strlen(name) + strlen(locale) + 8 : 1;
    }

    char* out = malloc(length ? room : strlen(name) + 1);
    char* at = out;

    if (!out) {
        return NULL;
    }

    if (!length) {
        return strcpy(out, name);
    }

    for (size_t i = 0; i < length; i++) {
        if (template[i] != '%') {
            *at++ = template[i];

            continue;
        }

        if (++i == length) {
            free(out);

            return NULL;
        }

        switch (template[i]) {
            case 'N':
                at = stpcpy(at, name);
                break;
            case 'L':
                at = stpcpy(at, locale);
                break;
            case 'l':
                at = stpncpy(at, locale, language);
                break;
            case 't':
                at = stpncpy(at, territory, territoryLength);
                break;
            case 'c':
                at = stpcpy(at, "UTF-8");
                break;
            case '%':
                *at++ = '%';
                break;
            default:
                free(out);

                return NULL;
        }
    }

    *at = 0;

    return out;
}

nl_catd __kernel_catopen(const char* name, int flags) {
    if (strchr(name, '/')) {
        unsigned char* b = readCatalog(name);

        return b ? (nl_catd)b : (nl_catd)-1;
    }

    const char* locale = flags == NL_CAT_LOCALE ? setlocale(LC_MESSAGES, NULL) : getenv("LANG");
    const char* path = getenv("NLSPATH");

    locale = locale ? locale : "";
    path = path ? path : DEFAULT_PATH;

    for (const char* t = path;;) {
        size_t n = strcspn(t, ":");

        if (n || t[n]) {
            char* candidate = expand(t, n, name, locale);
            unsigned char* b = candidate ? readCatalog(candidate) : NULL;

            free(candidate);

            if (b) {
                return (nl_catd)b;
            }
        }

        if (!t[n]) {
            break;
        }

        t += n + 1;
    }

    errno = ENOENT;

    return (nl_catd)-1;
}

// The catalog of a handle, or null where it is none: of memory there is
// not, or of no catalog's magic.
static const unsigned char* catalogOf(nl_catd catd) {
    uintptr_t at = (uintptr_t)catd;
    uintptr_t memory = __builtin_wasm_memory_size(0) * 65536;

    if (!at || at > memory || memory - at < HEADER || number((const unsigned char*)catd) != MAGIC ||
        memory - at < HEADER + (uint64_t)number((const unsigned char*)catd + 8)) {
        return NULL;
    }

    return catd;
}

char* __kernel_catgets(nl_catd catd, int set, int message, const char* s) {
    const unsigned char* b = catalogOf(catd);
    const char* found = b ? find(b, set, message) : NULL;

    if (!found) {
        errno = b ? ENOMSG : EBADF;

        return (char*)s;
    }

    return (char*)found;
}

int __kernel_catclose(nl_catd catd) {
    unsigned char* b = (unsigned char*)catalogOf(catd);

    if (!b) {
        errno = EBADF;

        return -1;
    }

    memset(b, 0, 4);
    free(b);

    return 0;
}
