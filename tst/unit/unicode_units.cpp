//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "unicode units: ok\n"

// uchar.h, C11's (C17's for a pair, DR 488), in UTF-8: mbrtoc32 and
// c32rtomb as mbrtowc and wcrtomb are; mbrtoc16 a character beyond the
// basic plane as two calls, the second (size_t)-3 with the low
// surrogate; c16rtomb a pair as nothing for the high surrogate and the
// character's bytes for the low, a lone low one EILSEQ; an incomplete
// sequence (size_t)-2, a bad one EILSEQ, NUL 0.

#include <assert.h>
#include <errno.h>
#include <locale.h>
#include <string.h>
#include <stdio.h>
#include <uchar.h>
#include <wchar.h>

int main() {
    assert(setlocale(LC_ALL, "C.UTF-8") != nullptr);
    const char face[] = "\xF0\x9F\x98\x80";
    char out[8];
    mbstate_t state = {};

    char32_t c32 = 0;
    assert(mbrtoc32(&c32, face, 4, &state) == 4 && c32 == 0x1F600);
    assert(mbrtoc32(&c32, face, 2, &state) == (size_t)-2);
    assert(mbrtoc32(&c32, face + 2, 2, &state) == 2 && c32 == 0x1F600 && mbsinit(&state));
    assert(mbrtoc32(&c32, "", 1, &state) == 0 && c32 == 0);
    errno = 0;
    assert(mbrtoc32(&c32, "\xFF", 1, &state) == (size_t)-1 && errno == EILSEQ);
    state = {};
    assert(c32rtomb(out, 0x1F600, &state) == 4 && memcmp(out, face, 4) == 0);
    assert(c32rtomb(out, U'é', &state) == 2 && memcmp(out, "\xC3\xA9", 2) == 0);
    errno = 0;
    assert(c32rtomb(out, 0x110000, &state) == (size_t)-1 && errno == EILSEQ);
    state = {};
    errno = 0;
    assert(c32rtomb(out, 0xD800, &state) == (size_t)-1 && errno == EILSEQ);
    state = {};
    assert(c32rtomb(nullptr, 0x1F600, &state) == 1);

    char16_t c16 = 0;
    assert(mbrtoc16(&c16, face, 4, &state) == 4 && c16 == 0xD83D);
    assert(mbrtoc16(&c16, face + 4, 0, &state) == (size_t)-3 && c16 == 0xDE00 && mbsinit(&state));
    assert(mbrtoc16(&c16, "\xC3\xA9", 2, &state) == 2 && c16 == 0x00E9);
    assert(mbrtoc16(&c16, "A", 1, &state) == 1 && c16 == 'A');
    assert(mbrtoc16(&c16, "", 1, &state) == 0 && c16 == 0);

    assert(c16rtomb(out, 0xD83D, &state) == 0 && !mbsinit(&state));
    assert(c16rtomb(out, 0xDE00, &state) == 4 && memcmp(out, face, 4) == 0 && mbsinit(&state));
    assert(c16rtomb(out, 0x00E9, &state) == 2 && memcmp(out, "\xC3\xA9", 2) == 0);
    errno = 0;
    assert(c16rtomb(out, 0xDE00, &state) == (size_t)-1 && errno == EILSEQ);
    state = {};
    assert(c16rtomb(out, 0, &state) == 1 && out[0] == 0);

    printf("unicode units: ok\n");
}
