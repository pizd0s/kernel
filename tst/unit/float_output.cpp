//@ compile-flags: -g
//@ stdout: "float output: ok\n"

// What printf writes of a double, as C has it (7.21.6.1): %f, %e and %g
// round the exact binary value to the precision, a tie to even; %g takes
// %e's form below 1e-4 and from the precision's power of ten on, its
// trailing zeros dropped but with #; %a writes the hexadecimal digits, a
// normal number's first 1, rounded to even past the precision; infinity
// and NaN by their names, an infinity's sign too, a NaN's as the libcs
// differ ([-]nan: C says no NaN is negative), padded with spaces where 0
// asks for zeros. %n stores the count so far, past what snprintf keeps too, in
// the type its length names, by position as well; %m is strerror(errno).

#include <assert.h>
#include <errno.h>
#include <float.h>
#include <math.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

static void is(const char* want, const char* format, double v) {
    char got[512];
    int n = snprintf(got, sizeof got, format, v);

    if (n != int(strlen(want)) || strcmp(got, want) != 0) {
        fprintf(stderr, "%s of %a: \"%s\", not \"%s\"\n", format, v, got, want);
        assert(false);
    }
}

// Either of two, where C leaves the text to the implementation.
static void isEither(const char* one, const char* other, const char* format, double v) {
    char got[512];
    snprintf(got, sizeof got, format, v);

    if (strcmp(got, one) != 0 && strcmp(got, other) != 0) {
        fprintf(stderr, "%s of %a: \"%s\", not \"%s\" or \"%s\"\n", format, v, got, one, other);
        assert(false);
    }
}

int main() {
    is("0", "%.0f", 0.5);
    is("2", "%.0f", 1.5);
    is("2", "%.0f", 2.5);
    is("4", "%.0f", 3.5);
    is("-0", "%.0f", -0.5);
    is("0.12", "%.2f", 0.125);
    is("0.38", "%.2f", 0.375);
    is("0.1000000000000000055511151231257827", "%.34f", 0.1);
    is("0.000000", "%f", 1e-7);
    is("0.0000001000", "%.10f", 1e-7);
    is("179769313486231570814527423731704356798070567525844996598917476803157260780028538760589558632766878171540458953514"
       "382464234321326889464182768467546703537516986049910576551282076245490090389328944075868508455133942304583236903222"
       "948165808559332123348274797826204144723168738177180919299881250404026184124858368",
       "%.0f", DBL_MAX);
    is("1.000000e+00", "%e", 1.0);
    is("1e+300", "%.0e", 1e300);
    is("9.9999999999999992e+22", "%.16e", 1e23);
    is("0.000000e+00", "%e", 0.0);
    is("4.940656e-324", "%e", 0x1p-1074);
    is("5e-324", "%.0e", 0x1p-1074);
    is("1.797693e+308", "%e", DBL_MAX);
    is("1e+308", "%.0e", 1e308);
    is("1e-05", "%g", 1e-5);
    is("0.0001", "%g", 1e-4);
    is("100000", "%g", 1e5);
    is("1e+06", "%g", 1e6);
    is("1.5", "%g", 1.5);
    is("1.50000", "%#g", 1.5);
    is("0.000123457", "%g", 0.0001234567);
    is("0", "%g", 0.0);
    is("-0", "%g", -0.0);
    is("0.00000", "%#g", 0.0);
    is("0.10000000000000001", "%.17g", 0.1);
    is("1E-10", "%G", 1e-10);

    is("0x1p+0", "%a", 1.0);
    is("0x1.8p+1", "%a", 3.0);
    is("-0x1.999999999999ap-4", "%a", -0.1);
    is("0X1.8P+1", "%A", 3.0);
    is("0x0p+0", "%a", 0.0);
    is("0x1p-1074", "%a", 0x1p-1074);
    is("0x1.fffffffffffffp+1023", "%a", DBL_MAX);
    is("0x1.2p+0", "%.1a", 0x1.18p0);
    is("0x1.2p+0", "%.1a", 0x1.28p0);
    is("0x1.3p+0", "%.1a", 0x1.281p0);
    is("0x1.1p+0", "%.1a", 0x1.0801p0);
    // The digit before the point is the implementation's but for zero
    // (7.21.6.1): 0x1.8 rounded up is 0x2p+0 or 0x1p+1.
    isEither("0x2p+0", "0x1p+1", "%.0a", 0x1.8p0);
    is("0x1.000p+0", "%.3a", 1.0);

    is("inf", "%f", INFINITY);
    is("-inf", "%e", -INFINITY);
    is("INF", "%F", INFINITY);
    is("nan", "%g", NAN);
    char nan[8];
    snprintf(nan, sizeof nan, "%f", -NAN);
    assert(strcmp(nan, "nan") == 0 || strcmp(nan, "-nan") == 0);
    is("  inf", "%05f", INFINITY);
    is("inf  ", "%-5f", INFINITY);
    is("+inf", "%+f", INFINITY);

    int n = 0;
    signed char hh = 0;
    short h = 0;
    long l = 0;
    long long ll = 0;
    intmax_t j = 0;
    size_t z = 0;
    ptrdiff_t t = 0;
    char buf[64];
    assert(snprintf(buf, sizeof buf, "ab%ncd%hhne%hnf%lng%llnh%jni%znj%tnk", &n, &hh, &h, &l, &ll, &j, &z, &t) == 11);
    assert(strcmp(buf, "abcdefghijk") == 0);
    assert(n == 2 && hh == 4 && h == 5 && l == 6 && ll == 7 && j == 8 && z == 9 && t == 10);
    n = 0;
    assert(snprintf(buf, 3, "abcdef%n", &n) == 6 && n == 6 && strcmp(buf, "ab") == 0);
    n = 0;
    assert(snprintf(buf, sizeof buf, "%2$s%1$n!", &n, "xyz") == 4 && n == 3 && strcmp(buf, "xyz!") == 0);

    char message[128];
    errno = ENOENT;
    assert(snprintf(buf, sizeof buf, "%m") > 0);
    assert(snprintf(message, sizeof message, "%s", strerror(ENOENT)) > 0 && strcmp(buf, message) == 0);
    errno = ENOENT;
    assert(snprintf(buf, sizeof buf, "[%.5m]") == 7 && strncmp(buf + 1, message, 5) == 0);

    printf("float output: ok\n");
}
