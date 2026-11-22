//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "money formats: ok\n"

// strfmon and strfmon_l, POSIX.1-2024's, in the POSIX locale, which has
// no currency symbols, no grouping, no monetary radix character and no
// signs: the numeric radix character, the default right precision 2 and
// "-" before a negative amount, since the locale's sign positions are
// {CHAR_MAX}; width and left justification, the left precision filled
// with the =f character and the parts around the amount padded to the
// longer of a positive and a negative amount's; ( for parentheses, + an
// EINVAL as both signs are empty; the right precision rounding; %%; the
// terminating NUL even after plain characters; E2BIG when the result and
// its NUL do not fit. host: false: the host musl's strfmon ignores =f, (,
// + and !, and leaves no NUL after a format's plain characters.

#include <assert.h>
#include <errno.h>
#include <locale.h>
#include <monetary.h>
#include <stdio.h>
#include <string.h>

namespace {
    char out[128];

    const char* money(const char* format, double value) {
        memset(out, 'x', sizeof out);
        ssize_t n = strfmon(out, sizeof out, format, value);
        assert(n >= 0 && (size_t)n == strlen(out));
        return out;
    }

    void same(const char* format, double value, const char* expected) {
        const char* got = money(format, value);
        if (strcmp(got, expected) != 0) {
            printf("%s of %g: [%s], expected [%s]\n", format, value, got, expected);
            assert(false);
        }
    }
}

int main() {
    same("%n", 123.45, "123.45");
    same("%n", -123.45, "-123.45");
    same("%i", 123.45, "123.45");
    same("%i", -123.45, "-123.45");
    same("%n", 0, "0.00");
    same("%n", 1234567.891, "1234567.89");
    same("[%n]", 1.5, "[1.50]");
    same("%11n|", 123.45, "     123.45|");
    same("%-11n|", 123.45, "123.45     |");
    same("%-11n|", -123.45, "-123.45    |");
    same("%#5n", 123.45, "   123.45");
    same("%#5n", -123.45, "-  123.45");
    same("%=*#5n", 123.45, " **123.45");
    same("%=0#6n", -42.5, "-000042.50");
    same("%#2n", 12345.6, " 12345.60");
    same("%(n", -123.45, "(123.45)");
    same("%(n", 123.45, "123.45");
    same("%(#5n", 123.45, "   123.45 ");
    same("%(#5n", -123.45, "(  123.45)");
    same("%!n", 123.45, "123.45");
    same("%^n", 1234567.0, "1234567.00");
    same("%.1n", 123.45, "123.5");
    same("%.0n", 123.45, "123");
    same("%.3i", -0.5, "-0.500");
    same("%#3.0n", 5.5, "   6");
    same("%% %n %%", 2, "% 2.00 %");

    memset(out, 0, sizeof out);
    assert(strfmon(out, sizeof out, "%n and %i", 1.0, -2.0) == 14 && strcmp(out, "1.00 and -2.00") == 0);
    errno = 0;
    assert(strfmon(out, sizeof out, "%+n", 123.45) == -1 && errno == EINVAL);
    errno = 0;
    assert(strfmon(out, 6, "%n", 123.45) == -1 && errno == E2BIG);
    assert(strfmon(out, 7, "%n", 123.45) == 6 && strcmp(out, "123.45") == 0);
    errno = 0;
    assert(strfmon(out, 3, "abc", 0.0) == -1 && errno == E2BIG);
    assert(strfmon(out, 4, "abc", 0.0) == 3 && strcmp(out, "abc") == 0);

    locale_t c = newlocale(LC_ALL_MASK, "C", (locale_t)0);
    assert(c);
    assert(strfmon_l(out, sizeof out, c, "%(#4n|%-8i|", -7.125, 3.0) > 0 && strcmp(out, "(   7.12)|3.00    |") == 0);
    freelocale(c);

    printf("money formats: ok\n");
}
