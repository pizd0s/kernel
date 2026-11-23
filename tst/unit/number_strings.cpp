//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "number strings: ok\n"

// Numbers as strings and back: a64l and l64a's radix-64, least significant digit first, a64l of six
// digits at most and its 32 bits sign-extended; strtod_l and its kind in
// a C locale; wcstoimax and wcstoumax as strtoimax reads, ERANGE past
// intmax_t.

#include <assert.h>
#include <errno.h>
#include <inttypes.h>
#include <locale.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

int main() {
    assert(strcmp(l64a(0), "") == 0 && strcmp(l64a(1), "/") == 0 && strcmp(l64a(64), "./") == 0);
    assert(strcmp(l64a(63), "z") == 0 && strcmp(l64a(12), "A") == 0 && strcmp(l64a(2), "0") == 0);
    assert(a64l("./") == 64 && a64l("") == 0 && a64l("zzzzzz") == -1 && a64l("zzzzzzzz") == -1);
    const long values[] = {1, 7, 4095, 123456789, 0x7fffffff};
    for (long value : values) {
        char text[8];
        strcpy(text, l64a(value));
        assert(a64l(text) == value);
    }

    locale_t c = newlocale(LC_ALL_MASK, "C", (locale_t)0);
    assert(c);
    char* end;
    assert(strtod_l("1.5e3x", &end, c) == 1500 && *end == 'x');
    assert(strtof_l("-0.25", &end, c) == -0.25f && *end == 0);
    assert(strtold_l("2.5", &end, c) == 2.5L && *end == 0);
    freelocale(c);

    wchar_t* wend;
    assert(wcstoimax(L" -123abc", &wend, 10) == -123 && wcscmp(wend, L"abc") == 0);
    assert(wcstoumax(L"0x1f", &wend, 0) == 31 && *wend == 0);
    assert(wcstoimax(L"0777", &wend, 0) == 511);
    errno = 0;
    assert(wcstoimax(L"99999999999999999999", &wend, 10) == INTMAX_MAX && errno == ERANGE);
    errno = 0;
    assert(wcstoimax(L"-99999999999999999999", &wend, 10) == INTMAX_MIN && errno == ERANGE);
    errno = 0;
    assert(wcstoumax(L"99999999999999999999", &wend, 10) == UINTMAX_MAX && errno == ERANGE);
    assert(wcstoimax(L"xyz", &wend, 10) == 0 && wcscmp(wend, L"xyz") == 0);

    printf("number strings: ok\n");
}
