//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "character conversion: ok\n"

#include <assert.h>
#include <errno.h>
#include <iconv.h>
#include <stdio.h>
#include <string.h>

// convert runs a whole conversion and returns the bytes written, or -1 with errno.
static long convert(const char* to, const char* from, const char* input, size_t length, char* output, size_t room) {
    iconv_t cd = iconv_open(to, from);
    assert(cd != (iconv_t)-1);
    char* in = const_cast<char*>(input);
    char* out = output;
    size_t left = length, space = room;
    size_t n = iconv(cd, &in, &left, &out, &space);
    int error = errno;
    assert(iconv_close(cd) == 0);
    if (n == (size_t)-1) {
        errno = error;
        return -1;
    }
    assert(left == 0);
    return long(room - space);
}

int main() {
    char out[64];

    // UTF-8 to UTF-16LE and back: two letters of two bytes, one of four.
    const char text[] = "\xd0\xbc\xd1\x81 \xf0\x9f\x98\x80";
    long n = convert("UTF-16LE", "UTF-8", text, sizeof(text) - 1, out, sizeof(out));
    assert(n == 10 && memcmp(out, "\x3c\x04\x41\x04\x20\x00\x3d\xd8\x00\xde", 10) == 0);
    char back[64];
    assert(convert("UTF-8", "UTF-16LE", out, 10, back, sizeof(back)) == long(sizeof(text) - 1));
    assert(memcmp(back, text, sizeof(text) - 1) == 0);

    // The code pages a file manager meets: KOI8-R, CP1251, ISO-8859-1, CP866.
    assert(convert("UTF-8", "KOI8-R", "\xcd\xd3", 2, out, sizeof(out)) == 4 && memcmp(out, "\xd0\xbc\xd1\x81", 4) == 0);
    assert(convert("UTF-8", "CP1251", "\xec\xf1", 2, out, sizeof(out)) == 4 && memcmp(out, "\xd0\xbc\xd1\x81", 4) == 0);
    assert(convert("UTF-8", "CP866", "\xac\xe1", 2, out, sizeof(out)) == 4 && memcmp(out, "\xd0\xbc\xd1\x81", 4) == 0);
    assert(convert("ISO-8859-1", "UTF-8", "caf\xc3\xa9", 5, out, sizeof(out)) == 4 && memcmp(out, "caf\xe9", 4) == 0);
    assert(convert("KOI8-R", "UTF-8", "\xd0\xbc\xd1\x81", 4, out, sizeof(out)) == 2 && memcmp(out, "\xcd\xd3", 2) == 0);

    // Names are matched without case; an unknown one is EINVAL.
    assert(convert("utf-8", "koi8-r", "\xcd", 1, out, sizeof(out)) == 2);
    assert(iconv_open("UTF-8", "NO-SUCH-CHARSET") == (iconv_t)-1 && errno == EINVAL);
    assert(iconv_open("NO-SUCH-CHARSET", "UTF-8") == (iconv_t)-1 && errno == EINVAL);

    // A byte that is no character is EILSEQ, a character cut short EINVAL,
    // no room for the next one E2BIG: each leaves the input where it stopped.
    iconv_t cd = iconv_open("UTF-16LE", "UTF-8");
    assert(cd != (iconv_t)-1);
    char bad[] = "a\xff" "b";
    char* in = bad;
    char* to = out;
    size_t left = 3, space = sizeof(out);
    assert(iconv(cd, &in, &left, &to, &space) == (size_t)-1 && errno == EILSEQ && in == bad + 1 && left == 2);
    char cut[] = "a\xd0";
    in = cut, to = out, left = 2, space = sizeof(out);
    assert(iconv(cd, &in, &left, &to, &space) == (size_t)-1 && errno == EINVAL && in == cut + 1 && left == 1);
    char two[] = "ab";
    in = two, to = out, left = 2, space = 3;
    assert(iconv(cd, &in, &left, &to, &space) == (size_t)-1 && errno == E2BIG && in == two + 1 && left == 1 && space == 1);
    assert(iconv_close(cd) == 0);

    // A character the target has none for becomes an asterisk, and the
    // call counts it: a conversion that cannot be reversed.
    cd = iconv_open("ISO-8859-1", "UTF-8");
    assert(cd != (iconv_t)-1);
    char letter[] = "a\xd0\xbc";
    in = letter, to = out, left = 3, space = sizeof(out);
    assert(iconv(cd, &in, &left, &to, &space) == 1 && left == 0 && to == out + 2 && memcmp(out, "a*", 2) == 0);
    assert(iconv_close(cd) == 0);

    puts("character conversion: ok");
}
