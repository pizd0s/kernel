//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "integer strings: ok\n"

// strtol and its kind, as C11 7.22.1.4 has them: white space and a sign
// first; base 0 takes 0x as hexadecimal and a leading 0 as octal, base 16
// takes 0x, which is no prefix with no hexadecimal digit after it; digits
// past 9 are letters of either case; with no digits nothing converts and
// the end is the string's start, which POSIX lets fail with EINVAL, as an
// unsupported base; a value past the type's range is its limit and ERANGE,
// a negated unsigned value the type's; the wide forms read alike; atoi,
// atol, atoll and atof are the conversions in base 10; errno stays where
// nothing fails.

#include <assert.h>
#include <errno.h>
#include <inttypes.h>
#include <limits.h>
#include <locale.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

static void is(const char* s, int base, long want, size_t used, int error = 0) {
    char* end = nullptr;
    errno = EBADF;
    long v = strtol(s, &end, base);
    bool kept = used == 0 ? errno == EBADF || errno == EINVAL : errno == (error ? error : EBADF);
    assert(v == want && size_t(end - s) == used && kept);
}

int main() {
    is("  -123abc", 10, -123, 6);
    is("+42", 10, 42, 3);
    is("0x1f", 0, 31, 4);
    is("0X1F", 16, 31, 4);
    is("0x1f", 10, 0, 1);
    is("0x", 16, 0, 1);
    is("0x", 0, 0, 1);
    is("0xg", 0, 0, 1);
    is("017", 0, 15, 3);
    is("08", 0, 0, 1);
    is("z", 36, 35, 1);
    is("Zz", 36, 35 * 36 + 35, 2);
    is("101", 2, 5, 3);
    is("", 10, 0, 0);
    is("   ", 10, 0, 0);
    is("+", 10, 0, 0);
    is(" -x", 10, 0, 0);
    is("12", 1, 0, 0);
    is("12", 37, 0, 0);

    char text[64];
    snprintf(text, sizeof text, "%ld", LONG_MAX);
    is(text, 10, LONG_MAX, strlen(text));
    snprintf(text, sizeof text, "%lu0", (unsigned long)LONG_MAX);
    is(text, 10, LONG_MAX, strlen(text), ERANGE);
    snprintf(text, sizeof text, "%ld", LONG_MIN);
    is(text, 10, LONG_MIN, strlen(text));
    snprintf(text, sizeof text, "-%lu0", (unsigned long)LONG_MAX);
    is(text, 10, LONG_MIN, strlen(text), ERANGE);

    char* end = nullptr;
    errno = 0;
    assert(strtoul("-1", &end, 10) == ULONG_MAX && *end == 0 && errno == 0);
    snprintf(text, sizeof text, "%lu0", ULONG_MAX);
    assert(strtoul(text, &end, 10) == ULONG_MAX && *end == 0 && errno == ERANGE);
    errno = 0;
    assert(strtoull("18446744073709551615", &end, 10) == ULLONG_MAX && errno == 0);
    assert(strtoull("18446744073709551616", &end, 10) == ULLONG_MAX && errno == ERANGE);
    errno = 0;
    assert(strtoll("-9223372036854775808", &end, 10) == LLONG_MIN && errno == 0);
    assert(strtoll("9223372036854775808", &end, 10) == LLONG_MAX && errno == ERANGE);
    errno = 0;
    assert(strtoll("-9223372036854775809", &end, 10) == LLONG_MIN && errno == ERANGE);
    errno = 0;
    assert(strtoimax("-0x7fffffffffffffff", &end, 16) == -INTMAX_MAX && errno == 0);
    assert(strtoumax("0xffffffffffffffff", &end, 0) == UINTMAX_MAX && errno == 0);
    assert(strtoull("-0x10", &end, 0) == ULLONG_MAX - 15 && *end == 0);

    wchar_t* wend = nullptr;
    const wchar_t* wide = L"\t-0x2A tail";
    assert(wcstol(wide, &wend, 0) == -42 && wend == wide + 6);
    assert(wcstoul(L"777", &wend, 8) == 511 && *wend == 0);
    assert(wcstoll(L"-12", &wend, 10) == -12 && wcstoull(L"12", &wend, 10) == 12);
    assert(wcstoimax(L"99", &wend, 10) == 99 && wcstoumax(L"99", &wend, 10) == 99);
    const wchar_t* none = L" x";
    assert(wcstol(none, &wend, 10) == 0 && wend == none);
    errno = EBADF;

    assert(atoi("  42xyz") == 42 && atol("-7") == -7 && atoll("123456789012") == 123456789012LL);
    assert(atof("1.5e1") == 15.0 && errno == EBADF && atoi("") == 0);

    locale_t c = newlocale(LC_ALL_MASK, "C", nullptr);
    assert(c);
    assert(strtod_l("2.5x", &end, c) == 2.5 && *end == 'x');
    assert(strtof_l("0.5", nullptr, c) == 0.5f && strtold_l("-4", nullptr, c) == -4.0L);
    freelocale(c);

    printf("integer strings: ok\n");
}
