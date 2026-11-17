//@ compile-flags: -g -D_GNU_SOURCE -fno-builtin
//@ stdout: "string searches: ok\n"

// The functions of string.h and strings.h that are lib/str's by a call
// from where they are used, or made of such calls: strnlen, strstr,
// memmem, strpbrk, mempcpy, strncat, strlcpy, strlcat, explicit_bzero,
// bcmp, bcopy and bzero, each called and called through a pointer, which
// is lib/c's own function of it, against a loop over the bytes.

#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>

static uint32_t seed = 12345;

static uint32_t next() {
    seed = seed * 1664525 + 1013904223;
    return seed >> 8;
}

// text fills s with n letters of a small alphabet and a NUL: short
// alphabets make repeats, partial matches and matches at every place.
static void text(char* s, size_t n, unsigned letters) {
    for (size_t i = 0; i < n; ++i) {
        s[i] = char('a' + next() % letters);
    }
    s[n] = 0;
}

static size_t boundedLength(const char* s, size_t n) {
    size_t i = 0;
    while (i < n && s[i]) {
        ++i;
    }
    return i;
}

static const char* found(const char* h, size_t hl, const char* n, size_t nl) {
    for (size_t i = 0; i + nl <= hl; ++i) {
        size_t k = 0;
        while (k < nl && h[i + k] == n[k]) {
            ++k;
        }
        if (k == nl) {
            return h + i;
        }
    }
    return nullptr;
}

static const char* firstOf(const char* s, const char* set) {
    for (; *s; ++s) {
        for (const char* c = set; *c; ++c) {
            if (*s == *c) {
                return s;
            }
        }
    }
    return nullptr;
}

int main() {
    size_t (*volatile lengthOf)(const char*, size_t) = strnlen;
    char* (*volatile search)(const char*, const char*) = strstr;
    void* (*volatile searchBytes)(const void*, size_t, const void*, size_t) = memmem;
    char* (*volatile breakAt)(const char*, const char*) = strpbrk;
    void* (*volatile copyPast)(void*, const void*, size_t) = mempcpy;
    char* (*volatile appendSome)(char*, const char*, size_t) = strncat;
    size_t (*volatile copySized)(char*, const char*, size_t) = strlcpy;
    size_t (*volatile appendSized)(char*, const char*, size_t) = strlcat;
    void (*volatile wipe)(void*, size_t) = explicit_bzero;
    int (*volatile differ)(const void*, const void*, size_t) = bcmp;
    void (*volatile moveBytes)(const void*, void*, size_t) = bcopy;
    void (*volatile zero)(void*, size_t) = bzero;

    static char hay[4200], needle[300], a[600], b[600], c[600];

    // strnlen: the NUL before, at and after the bound; no bytes at all.
    for (int round = 0; round < 2000; ++round) {
        size_t n = next() % 200;
        text(hay, n, 26);
        size_t bound = next() % 260;
        size_t want = boundedLength(hay, bound);
        assert(strnlen(hay, bound) == want && lengthOf(hay, bound) == want);
        assert(strnlen(hay, SIZE_MAX) == n && lengthOf(hay, SIZE_MAX) == n);
        assert(strnlen(hay + n, 0) == 0 && strnlen(hay, 0) == 0);
    }

    // strstr and memmem: needles of every length there and not there, in
    // haystacks of few letters; the empty needle is found at the start.
    for (int round = 0; round < 6000; ++round) {
        unsigned letters = 1 + next() % 4;
        size_t hl = round % 7 == 0 ? next() % 4096 : next() % 80;
        size_t nl = round % 5 == 0 ? next() % 260 : next() % 9;
        text(hay, hl, letters);
        if (next() % 2 && nl <= hl) {
            memcpy(needle, hay + next() % (hl - nl + 1), nl);
            needle[nl] = 0;
        } else {
            text(needle, nl, letters + next() % 2);
        }
        const char* want = found(hay, hl, needle, nl);
        assert(strstr(hay, needle) == want && search(hay, needle) == want);
        assert(memmem(hay, hl, needle, nl) == want && searchBytes(hay, hl, needle, nl) == want);
        // bytes with a NUL among them are bytes to memmem
        if (hl > 2 && nl > 1) {
            hay[hl / 2] = 0;
            needle[nl / 2] = 0;
            want = found(hay, hl, needle, nl);
            assert(memmem(hay, hl, needle, nl) == want && searchBytes(hay, hl, needle, nl) == want);
        }
    }
    assert(strstr("", "") != nullptr && strstr("", "a") == nullptr && memmem("abc", 0, "b", 1) == nullptr);

    // strpbrk: any byte of the set, the high ones too.
    for (int round = 0; round < 3000; ++round) {
        text(hay, next() % 120, 26);
        text(needle, next() % 6, 26);
        const char* want = firstOf(hay, needle);
        assert(strpbrk(hay, needle) == want && breakAt(hay, needle) == want);
    }
    const char high[] = {'x', '\x80', 'y', '\xff', 0};
    assert(strpbrk(high, "\xff\x80") == high + 1 && strpbrk(high, "") == nullptr);

    // The copies, the fills and the comparison by their other names.
    for (int round = 0; round < 2000; ++round) {
        size_t n = next() % 500;
        text(a, n, 26);
        memset(b, '#', sizeof b);
        assert(mempcpy(b, a, n) == b + n && memcmp(b, a, n) == 0 && b[n] == '#');
        memset(c, '#', sizeof c);
        assert(copyPast(c, a, n) == c + n && bcmp(c, b, sizeof b) == 0 && differ(c, b, sizeof b) == 0);
        c[n / 2] ^= 1;
        assert(n == 0 || (bcmp(c, a, n) != 0 && differ(c, a, n) != 0));
        // an overlapping move forward by a few bytes
        size_t shift = next() % 9;
        memcpy(c, a, n);
        bcopy(c, c + shift, n);
        assert(memcmp(c + shift, a, n) == 0);
        moveBytes(c + shift, c, n);
        assert(memcmp(c, a, n) == 0);
        size_t from = next() % (n + 1);
        bzero(b + from, n - from);
        zero(c + from, n - from);
        wipe(a + from, n - from);
        explicit_bzero(a, from / 2);
        for (size_t i = 0; i < n; ++i) {
            assert(b[i] == (i < from ? b[i] : 0) && c[i] == (i < from ? c[i] : 0) && a[i] == (i < from / 2 || i >= from ? 0 : a[i]));
        }
        assert(b[n] == '#');
    }

    // strncat, strlcpy and strlcat: what fits and what does not.
    for (int round = 0; round < 4000; ++round) {
        size_t dl = next() % 100, sl = next() % 100, n = next() % 220;
        text(a, sl, 26);
        memset(b, '#', sizeof b);
        text(b, dl, 26);
        memcpy(c, b, sizeof b);
        size_t k = boundedLength(a, n);
        assert(strncat(b, a, n) == b && appendSome(c, a, n) == c);
        assert(strlen(b) == dl + k && memcmp(b + dl, a, k) == 0 && b[dl + k + 1] == '#' && memcmp(b, c, sizeof b) == 0);

        memset(b, '#', sizeof b);
        memset(c, '#', sizeof c);
        assert(strlcpy(b, a, n) == sl && copySized(c, a, n) == sl && memcmp(b, c, sizeof b) == 0);
        if (n == 0) {
            assert(b[0] == '#');
        } else {
            size_t copied = sl < n - 1 ? sl : n - 1;
            assert(memcmp(b, a, copied) == 0 && b[copied] == 0 && b[copied + 1] == '#');
        }

        memset(b, '#', sizeof b);
        text(b, dl, 26);
        memcpy(c, b, sizeof b);
        size_t had = boundedLength(b, n);
        size_t want = had == n ? n + sl : dl + sl;
        assert(strlcat(b, a, n) == want && appendSized(c, a, n) == want && memcmp(b, c, sizeof b) == 0);
        if (had < n) {
            size_t added = sl < n - dl - 1 ? sl : n - dl - 1;
            assert(memcmp(b + dl, a, added) == 0 && b[dl + added] == 0 && b[dl + added + 1] == '#');
        } else {
            assert(b[dl] == 0 && b[dl + 1] == '#');
        }
    }
    puts("string searches: ok");
}
