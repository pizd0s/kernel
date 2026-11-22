//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "number strings: ok\n"

// Numbers as strings and back: ecvt, fcvt and gcvt (SUSv3's, legacy)
// digits rounded as printf rounds, the radix's place and the sign aside;
// a64l and l64a's radix-64, least significant digit first, a64l of six
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

namespace {
    void digits(char* s, const char* expected, int decpt, int expectedDecpt, int sign, int expectedSign) {
        if (strcmp(s, expected) != 0 || decpt != expectedDecpt || sign != expectedSign) {
            printf("[%s] %d %d, expected [%s] %d %d\n", s, decpt, sign, expected, expectedDecpt, expectedSign);
            assert(false);
        }
    }
}

int main() {
    int decpt;
    int sign;
    digits(ecvt(3.14159, 3, &decpt, &sign), "314", decpt, 1, sign, 0);
    digits(ecvt(-0.000123456, 4, &decpt, &sign), "1235", decpt, -3, sign, 1);
    digits(ecvt(1e20, 2, &decpt, &sign), "10", decpt, 21, sign, 0);
    digits(ecvt(9.96, 2, &decpt, &sign), "10", decpt, 2, sign, 0);
    digits(ecvt(0.5, 1, &decpt, &sign), "5", decpt, 0, sign, 0);
    char* zero = ecvt(0.0, 3, &decpt, &sign);
    assert(strcmp(zero, "000") == 0 && (decpt == 0 || decpt == 1) && sign == 0);
    char* many = ecvt(123.456, 40, &decpt, &sign);
    assert(strlen(many) >= 15 && strlen(many) <= 17 && strncmp(many, "123456", 6) == 0 && decpt == 3);

    digits(fcvt(3.14159, 2, &decpt, &sign), "314", decpt, 1, sign, 0);
    digits(fcvt(1234.5678, 1, &decpt, &sign), "12346", decpt, 4, sign, 0);
    digits(fcvt(0.000123, 5, &decpt, &sign), "12", decpt, -3, sign, 0);
    digits(fcvt(42.0, 0, &decpt, &sign), "42", decpt, 2, sign, 0);
    char* small = fcvt(0.0001, 2, &decpt, &sign);
    assert(strspn(small, "0") == strlen(small) && sign == 0);

    char buffer[64];
    assert(gcvt(3.14159, 3, buffer) == buffer && strcmp(buffer, "3.14") == 0);
    assert(strcmp(gcvt(1e20, 5, buffer), "1e+20") == 0);
    assert(strcmp(gcvt(0.0001234, 2, buffer), "0.00012") == 0);
    assert(strcmp(gcvt(-2.5, 2, buffer), "-2.5") == 0);
    assert(strncmp(gcvt(100.0, 5, buffer), "100", 3) == 0 && strspn(buffer + 3, ".") == strlen(buffer + 3));

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
