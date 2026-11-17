//@ env: LC_ALL=C
//@ compile-flags: -g -D_GNU_SOURCE -fno-builtin
//@ stdout: "string functions: ok\n"

// The functions of string.h and strings.h the kernel computes, by
// lib/str's functions on the memory it checked: the comparisons of at
// most n bytes and without regard to case, the searches for a byte, the
// copies of at most n bytes, the duplicates and the tokens, each against
// a loop over the bytes.

#include <assert.h>
#include <ctype.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>

static uint32_t seed = 54321;

static uint32_t next() {
    seed = seed * 1664525 + 1013904223;
    return seed >> 8;
}

static void text(char* s, size_t n, unsigned letters, bool cases = false) {
    for (size_t i = 0; i < n; ++i) {
        s[i] = char((cases && next() % 2 ? 'A' : 'a') + next() % letters);
    }
    s[n] = 0;
}

static int signOf(int x) {
    return (x > 0) - (x < 0);
}

// order is how a and b compare by their first n bytes at most, as
// unsigned bytes, folded to lower case with fold.
static int order(const char* a, const char* b, size_t n, bool fold) {
    for (size_t i = 0; i < n; ++i) {
        unsigned char x = static_cast<unsigned char>(a[i]), y = static_cast<unsigned char>(b[i]);
        if (fold) {
            x = static_cast<unsigned char>(tolower(x));
            y = static_cast<unsigned char>(tolower(y));
        }
        if (x != y || x == 0) {
            return (x > y) - (x < y);
        }
    }
    return 0;
}

static bool among(char c, const char* set) {
    for (; *set; ++set) {
        if (*set == c) {
            return true;
        }
    }
    return false;
}

int main() {
    static char a[700], b[700], c[700], d[700];

    // The comparisons: equal strings, a difference at every place, one
    // string the other's start, n before, at and past the difference.
    for (int round = 0; round < 8000; ++round) {
        size_t al = next() % 300, bl = next() % 6 ? al : next() % 300;
        text(a, al, 2 + next() % 3, true);
        memcpy(b, a, al + 1);
        if (bl != al) {
            text(b + (bl < al ? bl : al) / 2, bl - (bl < al ? bl : al) / 2, 3, true);
        } else if (al && next() % 2) {
            b[next() % al] ^= next() % 2 ? 0x20 : 1;
        }
        if (next() % 8 == 0) {
            a[next() % (al + 1)] = char(0x80 + next() % 128);
        }
        size_t n = next() % 4 == 0 ? SIZE_MAX : next() % 320;
        assert(signOf(strncmp(a, b, n)) == order(a, b, n, false));
        assert(signOf(strncasecmp(a, b, n)) == order(a, b, n, true));
        assert(signOf(strcasecmp(a, b)) == order(a, b, SIZE_MAX, true));
        assert(signOf(strcoll(a, b)) == order(a, b, SIZE_MAX, false));
        assert(strncmp(a, a, n) == 0 && strncasecmp(a, a, n) == 0 && strncmp(a, b, 0) == 0);
    }

    // A byte in a string, from its start, and the string's end for none.
    for (int round = 0; round < 4000; ++round) {
        size_t n = next() % 400;
        text(a, n, 4 + next() % 20);
        char want = char('a' + next() % 26);
        size_t at = 0;
        while (at < n && a[at] != want) {
            ++at;
        }
        assert(strchr(a, want) == (at < n ? a + at : nullptr) && index(a, want) == strchr(a, want));
        assert(strchrnul(a, want) == a + at && strchr(a, 0) == a + n && strchrnul(a, 0) == a + n);
        assert(strchr(a, want + 256) == strchr(a, want));
        size_t last = n;
        while (last > 0 && a[last - 1] != want) {
            --last;
        }
        assert(strrchr(a, want) == (last ? a + last - 1 : nullptr) && rindex(a, want) == strrchr(a, want));
        assert(memrchr(a, want, n) == (last ? a + last - 1 : nullptr) && strrchr(a, 0) == a + n);
    }

    // The copies of at most n bytes, the NULs they pad with, and the
    // duplicates.
    for (int round = 0; round < 4000; ++round) {
        size_t sl = next() % 300, n = next() % 340;
        text(a, sl, 26);
        memset(b, '#', sizeof b);
        memset(c, '#', sizeof c);
        size_t copied = sl < n ? sl : n;
        assert(strncpy(b, a, n) == b && stpncpy(c, a, n) == c + copied && memcmp(b, c, sizeof b) == 0);
        assert(memcmp(b, a, copied) == 0 && b[n] == '#');
        for (size_t i = copied; i < n; ++i) {
            assert(b[i] == 0);
        }
        memset(d, '#', sizeof d);
        assert(stpcpy(d, a) == d + sl && strcmp(d, a) == 0 && d[sl + 1] == '#');
        char* twin = strdup(a);
        char* part = strndup(a, n);
        assert(twin && part && strcmp(twin, a) == 0 && strlen(part) == copied && memcmp(part, a, copied) == 0);
        free(twin);
        free(part);
        size_t room = next() % 340;
        assert(strxfrm(d, a, room) == sl && (room <= sl || strcmp(d, a) == 0));
        char stop = char('a' + next() % 26);
        size_t until = 0;
        while (until < n && until < sl + 1 && a[until] != stop) {
            ++until;
        }
        size_t most = n < sl + 1 ? n : sl + 1;
        memset(d, '#', sizeof d);
        void* past = memccpy(d, a, stop, most);
        if (until < most) {
            assert(past == d + until + 1 && memcmp(d, a, until + 1) == 0 && d[until + 1] == '#');
        } else {
            assert(past == nullptr && memcmp(d, a, most) == 0 && d[most] == '#');
        }
    }

    // The tokens: strtok_r, strtok and strsep over the same text give
    // what a loop over its bytes gives.
    for (int round = 0; round < 4000; ++round) {
        size_t n = next() % 120;
        text(a, n, 6);
        char set[4];
        text(set, next() % 4, 6);
        memcpy(b, a, n + 1);
        memcpy(c, a, n + 1);
        memcpy(d, a, n + 1);
        char* save = nullptr;
        char* field = d;
        bool first = true;
        size_t at = 0;
        while (true) {
            while (at < n && among(a[at], set)) {
                ++at;
            }
            char* token = strtok_r(first ? b : nullptr, set, &save);
            char* same = strtok(first ? c : nullptr, set);
            first = false;
            if (at == n) {
                assert(token == nullptr && same == nullptr);
                break;
            }
            size_t end = at;
            while (end < n && !among(a[end], set)) {
                ++end;
            }
            assert(token == b + at && same == c + at && strlen(token) == end - at && memcmp(token, a + at, end - at) == 0);
            assert(strcmp(token, same) == 0);
            at = end < n ? end + 1 : end;
        }
        assert(strtok_r(nullptr, set, &save) == nullptr && strtok(nullptr, set) == nullptr);
        // strsep gives the empty fields too
        at = 0;
        while (true) {
            size_t end = at;
            while (end < n && !among(a[end], set)) {
                ++end;
            }
            char* piece = strsep(&field, set);
            assert(piece == d + at && strlen(piece) == end - at);
            if (end == n) {
                assert(field == nullptr && strsep(&field, set) == nullptr);
                break;
            }
            assert(field == d + end + 1);
            at = end + 1;
        }
    }
    puts("string functions: ok");
}
