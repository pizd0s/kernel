// gencat: makes a message catalog, of the BSDs' format, which catopen and
// catgets read (lib/c's nl_types.h), from message files, POSIX's: a
// catalog there is takes what the files say on top of its messages.
//
// A message file is lines: "$set n", the set of the messages after it,
// NL_SETD before any; "$delset n", the set deleted; "$quote c", the
// character a message may be between, so that its spaces at the end
// show, none without c; "$" and a blank, a comment; "n text", message n
// of the set, the text after one blank, empty where there is none;
// "n" alone, message n deleted; an empty line. In a text \n, \t, \v, \b,
// \r, \f, \\, \ and up to three octal digits are what C makes of them,
// and a backslash at a line's end goes on to the next line.
//
//     gencat <catfile> <msgfile>...
//
// catfile or msgfile "-" is the standard output or input. catfile is
// written to catfile.tmp and renamed over it.

#include <bin/tools/tools.h>

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

namespace {
    constexpr uint32_t magic = 0xff88ff89;

    struct Message {
        int set;
        int number;
        char* text;
        size_t length;
    };

    Message* messages;
    size_t count;
    size_t room;

    [[noreturn]] void outOfMemory() {
        dprintf(STDERR_FILENO, "gencat: out of memory\n");
        exit(1);
    }

    void put(int set, int number, const char* text, size_t length) {
        Message* m = nullptr;
        for (size_t i = 0; i < count && !m; ++i) {
            if (messages[i].set == set && messages[i].number == number) {
                m = &messages[i];
                free(m->text);
            }
        }
        if (!m) {
            if (count == room) {
                room = room ? 2 * room : 64;
                messages = static_cast<Message*>(realloc(messages, room * sizeof(Message)));
                if (!messages) {
                    outOfMemory();
                }
            }
            m = &messages[count++];
        }
        m->set = set;
        m->number = number;
        m->text = static_cast<char*>(malloc(length + 1));
        if (!m->text) {
            outOfMemory();
        }
        memcpy(m->text, text, length);
        m->text[length] = 0;
        m->length = length;
    }

    // Deletes message number of set, or every message of it for -1.
    void drop(int set, int number) {
        size_t kept = 0;
        for (size_t i = 0; i < count; ++i) {
            if (messages[i].set == set && (number < 0 || messages[i].number == number)) {
                free(messages[i].text);
            } else {
                messages[kept++] = messages[i];
            }
        }
        count = kept;
    }

    // All of a descriptor's bytes, a NUL after them: null where it fails.
    char* readAll(int fd, size_t& size) {
        size_t have = 0;
        size_t capacity = 4096;
        char* data = static_cast<char*>(malloc(capacity + 1));
        for (;;) {
            if (!data) {
                outOfMemory();
            }
            const ssize_t n = read(fd, data + have, capacity - have);
            if (n < 0 && errno == EINTR) {
                continue;
            }
            if (n < 0) {
                free(data);
                return nullptr;
            }
            if (n == 0) {
                break;
            }
            have += size_t(n);
            if (have == capacity) {
                capacity *= 2;
                data = static_cast<char*>(realloc(data, capacity + 1));
            }
        }
        data[have] = 0;
        size = have;
        return data;
    }

    uint32_t be32(const unsigned char* p) {
        return uint32_t(p[0]) << 24 | uint32_t(p[1]) << 16 | uint32_t(p[2]) << 8 | p[3];
    }

    void putBe32(unsigned char* p, uint32_t v) {
        p[0] = uint8_t(v >> 24);
        p[1] = uint8_t(v >> 16);
        p[2] = uint8_t(v >> 8);
        p[3] = uint8_t(v);
    }

    // The messages of a catalog there is: false for one that is no
    // catalog.
    bool load(const char* path) {
        const int fd = open(path, O_RDONLY | O_CLOEXEC);
        if (fd < 0) {
            return errno == ENOENT;
        }
        size_t size;
        char* data = readAll(fd, size);
        close(fd);
        if (!data) {
            return false;
        }
        const auto* b = reinterpret_cast<const unsigned char*>(data);
        bool ok = size >= 20 && be32(b) == magic && 20 + size_t(be32(b + 8)) == size;
        const size_t sets = ok ? be32(b + 4) : 0;
        const size_t msgs = 20 + size_t(ok ? be32(b + 12) : 0);
        const size_t text = 20 + size_t(ok ? be32(b + 16) : 0);
        ok = ok && 20 + 12 * sets <= msgs && msgs <= text && text <= size;
        for (size_t i = 0; ok && i < sets; ++i) {
            const unsigned char* s = b + 20 + 12 * i;
            const size_t first = be32(s + 8);
            const size_t n = be32(s + 4);
            for (size_t k = first; ok && k < first + n; ++k) {
                const unsigned char* m = b + msgs + 12 * k;
                const size_t at = text + be32(m + 8);
                ok = m + 12 <= b + text && at < size && memchr(b + at, 0, size - at);
                if (ok) {
                    put(int(be32(s)), int(be32(m)), data + at, strlen(data + at));
                }
            }
        }
        free(data);
        return ok;
    }

    int bySetAndNumber(const void* a, const void* b) {
        const auto* x = static_cast<const Message*>(a);
        const auto* y = static_cast<const Message*>(b);
        if (x->set != y->set) {
            return x->set < y->set ? -1 : 1;
        }
        return x->number < y->number ? -1 : x->number > y->number;
    }

    // The catalog's bytes: the header, the sets, the messages' headers
    // and their texts, each with a NUL after it.
    unsigned char* catalog(size_t& size) {
        qsort(messages, count, sizeof(Message), bySetAndNumber);
        size_t sets = 0;
        size_t texts = 0;
        for (size_t i = 0; i < count; ++i) {
            sets += i == 0 || messages[i].set != messages[i - 1].set;
            texts += messages[i].length + 1;
        }
        size = 20 + 12 * sets + 12 * count + texts;
        auto* b = static_cast<unsigned char*>(calloc(1, size));
        if (!b) {
            outOfMemory();
        }
        putBe32(b, magic);
        putBe32(b + 4, uint32_t(sets));
        putBe32(b + 8, uint32_t(size - 20));
        putBe32(b + 12, uint32_t(12 * sets));
        putBe32(b + 16, uint32_t(12 * sets + 12 * count));
        unsigned char* set = b + 20 - 12;
        unsigned char* header = b + 20 + 12 * sets;
        unsigned char* text = header + 12 * count;
        size_t offset = 0;
        for (size_t i = 0; i < count; ++i) {
            const Message& m = messages[i];
            if (i == 0 || m.set != messages[i - 1].set) {
                set += 12;
                putBe32(set, uint32_t(m.set));
                putBe32(set + 8, uint32_t(i));
            }
            putBe32(set + 4, be32(set + 4) + 1);
            putBe32(header + 12 * i, uint32_t(m.number));
            putBe32(header + 12 * i + 4, uint32_t(m.length + 1));
            putBe32(header + 12 * i + 8, uint32_t(offset));
            memcpy(text + offset, m.text, m.length);
            offset += m.length + 1;
        }
        return b;
    }

    bool blank(char c) {
        return c == ' ' || c == '\t';
    }

    // A number at p, of 1 to max: 0 for none.
    long numberAt(const char*& p, long max) {
        if (*p < '0' || *p > '9') {
            return 0;
        }
        long n = 0;
        for (; *p >= '0' && *p <= '9'; ++p) {
            n = n * 10 + (*p - '0');
            if (n > max) {
                return 0;
            }
        }
        return n;
    }

    // The text of a message from p to the end of its line, its escapes
    // and quotes taken off, into out: where the next line starts.
    const char* textAt(const char* p, int quote, char* out, size_t& length) {
        length = 0;
        const bool quoted = quote >= 0 && *p == quote;
        if (quoted) {
            ++p;
        }
        for (; *p && *p != '\n'; ++p) {
            if (quoted && *p == quote) {
                while (*p && *p != '\n') {
                    ++p;
                }
                break;
            }
            if (*p != '\\') {
                out[length++] = *p;
                continue;
            }
            const char c = *++p;
            switch (c) {
                case 'n':
                    out[length++] = '\n';
                    break;
                case 't':
                    out[length++] = '\t';
                    break;
                case 'v':
                    out[length++] = '\v';
                    break;
                case 'b':
                    out[length++] = '\b';
                    break;
                case 'r':
                    out[length++] = '\r';
                    break;
                case 'f':
                    out[length++] = '\f';
                    break;
                case '\n':
                    break;
                case 0:
                    --p;
                    break;
                default:
                    if (c >= '0' && c <= '7') {
                        int v = 0;
                        for (int i = 0; i < 3 && *p >= '0' && *p <= '7'; ++i, ++p) {
                            v = v * 8 + (*p - '0');
                        }
                        --p;
                        out[length++] = char(v);
                    } else {
                        out[length++] = c;
                    }
            }
        }
        return *p ? p + 1 : p;
    }

    bool parse(const char* name, const char* data) {
        int set = 1;
        int quote = -1;
        int line = 1;
        char* text = static_cast<char*>(malloc(strlen(data) + 1));
        if (!text) {
            outOfMemory();
        }
        bool ok = true;
        for (const char* p = data; ok && *p;) {
            const char* start = p;
            while (blank(*p)) {
                ++p;
            }
            if (*p == '\n') {
                ++p;
            } else if (*p == '$') {
                ++p;
                if (strncmp(p, "set", 3) == 0 && blank(p[3])) {
                    p += 3;
                    while (blank(*p)) {
                        ++p;
                    }
                    ok = (set = int(numberAt(p, NL_SETMAX))) != 0;
                } else if (strncmp(p, "delset", 6) == 0 && blank(p[6])) {
                    p += 6;
                    while (blank(*p)) {
                        ++p;
                    }
                    const long n = numberAt(p, NL_SETMAX);
                    ok = n != 0;
                    drop(int(n), -1);
                } else if (strncmp(p, "quote", 5) == 0 && (blank(p[5]) || p[5] == '\n' || !p[5])) {
                    p += 5;
                    while (blank(*p)) {
                        ++p;
                    }
                    quote = *p && *p != '\n' ? (unsigned char)*p : -1;
                }
                while (*p && *p != '\n') {
                    ++p;
                }
                if (*p) {
                    ++p;
                }
            } else {
                const long n = numberAt(p, NL_MSGMAX);
                if (n == 0 || (*p && *p != '\n' && !blank(*p))) {
                    ok = false;
                } else if (!*p || *p == '\n') {
                    drop(set, int(n));
                    p += *p ? 1 : 0;
                } else {
                    size_t length;
                    const char* next = textAt(p + 1, quote, text, length);
                    for (const char* q = p; q < next; ++q) {
                        line += *q == '\n';
                    }
                    line -= *(next - 1) == '\n';
                    p = next;
                    put(set, int(n), text, length);
                }
            }
            if (!ok) {
                const char* end = strchr(start, '\n');
                dprintf(STDERR_FILENO, "gencat: %s:%d: %.*s: not a line of a message file\n", name, line,
                        int(end ? end - start : strlen(start)), start);
            }
            line += p > start && *(p - 1) == '\n';
        }
        free(text);
        return ok;
    }
}

int gencatMain(int argc, char** argv) {
    if (argc < 3) {
        dprintf(STDERR_FILENO, "usage: gencat <catfile> <msgfile>...\n");
        return 2;
    }
    const char* out = argv[1];
    if (strcmp(out, "-") != 0 && !load(out)) {
        dprintf(STDERR_FILENO, "gencat: %s: not a message catalog\n", out);
        return 1;
    }
    for (int i = 2; i < argc; ++i) {
        const bool standard = strcmp(argv[i], "-") == 0;
        const int fd = standard ? STDIN_FILENO : open(argv[i], O_RDONLY | O_CLOEXEC);
        size_t size;
        char* data = fd < 0 ? nullptr : readAll(fd, size);
        if (fd >= 0 && !standard) {
            close(fd);
        }
        if (!data) {
            dprintf(STDERR_FILENO, "gencat: %s: %s\n", argv[i], strerror(errno));
            return 1;
        }
        const bool ok = parse(argv[i], data);
        free(data);
        if (!ok) {
            return 1;
        }
    }
    size_t size;
    unsigned char* b = catalog(size);
    char tmp[PATH_MAX];
    const bool standard = strcmp(out, "-") == 0;
    if (!standard && snprintf(tmp, sizeof tmp, "%s.tmp", out) >= int(sizeof tmp)) {
        dprintf(STDERR_FILENO, "gencat: %s: %s\n", out, strerror(ENAMETOOLONG));
        return 1;
    }
    const int fd = standard ? STDOUT_FILENO : open(tmp, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC, 0666);
    bool ok = fd >= 0;
    for (size_t done = 0; ok && done < size;) {
        const ssize_t n = write(fd, b + done, size - done);
        if (n < 0 && errno == EINTR) {
            continue;
        }
        ok = n > 0;
        done += ok ? size_t(n) : 0;
    }
    if (!standard && fd >= 0) {
        ok = close(fd) == 0 && ok;
        ok = ok && rename(tmp, out) == 0;
        if (!ok) {
            unlink(tmp);
        }
    }
    free(b);
    if (!ok) {
        dprintf(STDERR_FILENO, "gencat: %s: %s\n", out, strerror(errno));
        return 1;
    }
    return 0;
}
