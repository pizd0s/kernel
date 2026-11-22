//@ compile-flags: -g
//@ stdout: "float strings: ok\n"
//@ host: false

// strtod, strtof, strtold and their wide forms as C11 has them (7.22.1.3,
// 7.29.4.1.1): white space skipped, a sign, then the longest prefix of a
// decimal or hexadecimal number, of INF or INFINITY, or of NAN or
// NAN(n-char-sequence), in any case; where that ends, and the start when
// nothing converts; the nearest value, of a float itself for strtof, ties
// to even; a sign negating a NaN too; ERANGE past the largest value, with
// HUGE_VAL, and where the result underflows, too small to be represented
// without extraordinary roundoff error (7.12.1): not of a subnormal value
// that is exact; errno kept otherwise, but where nothing converts, which
// POSIX lets fail with EINVAL. strtold and wcstold are checked where long
// double is a double, as lib/c's is.
//
// Not on the host: musl's strtod("-nan") is a NaN of no sign, and its
// hexadecimal overflow sets no ERANGE.

#include <assert.h>
#include <errno.h>
#include <float.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

namespace {
    bool errs(int err, size_t ends) {
        return errno == err || ends == 0 && errno == EINVAL;
    }

    template <typename T>
    bool same(T v, T want) {
        if (isnan(want)) {
            return isnan(v) && signbit(v) == signbit(want);
        }
        return v == want && signbit(v) == signbit(want);
    }

    void d(const char* s, double want, size_t ends, int err = 0) {
        char* end;
        errno = 0;
        const double v = strtod(s, &end);
        if (!same(v, want) || size_t(end - s) != ends || !errs(err, ends)) {
            printf("strtod(\"%s\") = %a, %zu, %d; want %a, %zu, %d\n", s, v, size_t(end - s), errno, want, ends, err);
            assert(false);
        }
        if (LDBL_MANT_DIG == DBL_MANT_DIG) {
            errno = 0;
            const long double l = strtold(s, &end);
            assert(same(l, (long double)want) && size_t(end - s) == ends && errs(err, ends));
        }
    }

    void f(const char* s, float want, size_t ends, int err = 0) {
        char* end;
        errno = 0;
        const float v = strtof(s, &end);
        if (!same(v, want) || size_t(end - s) != ends || !errs(err, ends)) {
            printf("strtof(\"%s\") = %a, %zu, %d; want %a, %zu, %d\n", s, v, size_t(end - s), errno, want, ends, err);
            assert(false);
        }
    }

    void w(const wchar_t* s, double want, size_t ends, int err = 0) {
        wchar_t* end;
        errno = 0;
        const double v = wcstod(s, &end);
        if (!same(v, want) || size_t(end - s) != ends || !errs(err, ends)) {
            printf("wcstod(\"%ls\") = %a, %zu, %d; want %a, %zu, %d\n", s, v, size_t(end - s), errno, want, ends, err);
            assert(false);
        }
        errno = 0;
        assert(same(wcstof(s, &end), float(want)) && size_t(end - s) == ends);
        if (LDBL_MANT_DIG == DBL_MANT_DIG) {
            assert(same(wcstold(s, &end), (long double)want) && size_t(end - s) == ends);
        }
    }
}

int main() {
    d("  \t\n1.5x", 1.5, 7);
    d("+1.5", 1.5, 4);
    d("-1.5e2", -150, 6);
    d("12abc", 12, 2);
    d("1e", 1, 1);
    d("1e+", 1, 1);
    d("1E+5", 1e5, 4);
    d(".5", 0.5, 2);
    d("5.", 5, 2);
    d("-0", -0.0, 2);
    d(".", 0, 0);
    d("-.", 0, 0);
    d("", 0, 0);
    d("  x", 0, 0);
    d("+", 0, 0);
    d("0x1.8", 1.5, 5);
    d("0x1.8p1", 3, 7);
    d("0X.8P-1", 0.25, 7);
    d("-0x10", -16, 5);
    d("0x", 0, 1);
    d("0xg", 0, 1);
    d("0x.", 0, 1);
    d("0x1p", 1, 3);
    d("0x1p+", 1, 3);
    d("0x1_0", 1, 3);
    d("1_0", 1, 1);
    d("inf", INFINITY, 3);
    d("INFINITY", INFINITY, 8);
    d("infinite", INFINITY, 3);
    d("-Inf", -INFINITY, 4);
    d("in", 0, 0);
    d("nan", NAN, 3);
    d("NaN(123abc_)", NAN, 12);
    d("nan(", NAN, 3);
    d("nan(1 2)", NAN, 3);
    d("nan()", NAN, 5);
    d("-nan", -NAN, 4);
    d("1e400", HUGE_VAL, 5, ERANGE);
    d("-1e400", -HUGE_VAL, 6, ERANGE);
    d("0x1p1024", HUGE_VAL, 8, ERANGE);
    d("1e-400", 0, 6, ERANGE);
    d("-1e-400", -0.0, 7, ERANGE);
    d("0x1p-1075", 0, 9, ERANGE);
    d("0x1p-1074", 0x1p-1074, 9);
    d("0x1.8p-1074", 0x1p-1073, 11, ERANGE);
    d("0x1p-1022", 0x1p-1022, 9);
    d("1e-310", 1e-310, 6, ERANGE);
    d("0.0e-999999", 0, 11);
    d("1e999999999999999999999", HUGE_VAL, 23, ERANGE);
    d("2.2250738585072011e-308", 0x0.fffffffffffffp-1022, 23, ERANGE);
    d("2.2250738585072014e-308", 0x1p-1022, 23);
    d("1.00000000000000011102230246251565404236316680908203125", 1.0, 55);
    d("1.00000000000000011102230246251565404236316680908203126", 0x1.0000000000001p0, 55);
    d("1.0000000000000003330669073875469621270895004272460937", 0x1.0000000000001p0, 54);
    d("9007199254740993", 9007199254740992.0, 16);
    d("179769313486231570814527423731704356798070567525844996598917476803157260780028538760589558632766878171540458953514382464234321326889464182768467546703537516986049910576551282076245490090389328944075868508455133942304583236903222948165808559332123348274797826204144723168738177180919299881250404026184124858368", DBL_MAX, 309);
    char longer[1104] = "0.";
    memset(longer + 2, '0', 1100);
    strcpy(longer + 1102, "1");
    d(longer, 0, 1103, ERANGE);

    f("1.5", 1.5f, 3);
    f("0x1.000001p0", 1.0f, 12);
    f("0x1.0000011p0", 0x1.000002p0f, 13);
    f("1.000000059604644775390625", 1.0f, 26);
    f("1.000000059604644775390626", 0x1.000002p0f, 26);
    f("3.4028236e38", HUGE_VALF, 12, ERANGE);
    f("3.4028234e38", FLT_MAX, 12);
    f("1e-46", 0.0f, 5, ERANGE);
    f("1e-40", 1e-40f, 5, ERANGE);
    f("0x1p-149", 0x1p-149f, 8);
    f("-inf", -INFINITY, 4);

    w(L"  1.5x", 1.5, 5);
    w(L"　2.5", 2.5, 4);
    w(L"0x1.8p1", 3, 7);
    w(L"nan(x)", NAN, 6);
    w(L"1.5١", 1.5, 3);
    w(L"١", 0, 0);
    w(L"", 0, 0);

    errno = 0;
    assert(strtod("2.5", nullptr) == 2.5 && errno == 0);
    errno = EINTR;
    assert(strtod("1", nullptr) == 1 && errno == EINTR);

    printf("float strings: ok\n");
}
