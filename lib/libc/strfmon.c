#include <ctype.h>
#include <errno.h>
#include <limits.h>
#include <locale.h>
#include <math.h>
#include <monetary.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/*
 * strfmon, POSIX's: amounts of money as LC_MONETARY of localeconv writes
 * them. An amount is its digits rounded to the right precision (the
 * locale's, 2 where it has none) at the monetary radix character (the
 * numeric one where the locale has none), grouped unless ^, padded with
 * the fill character to the left precision; its sign and currency symbol
 * go around it as the locale's positions and spaces say, C's lconv rules,
 * in parentheses with ( or a position 0, with the sign strings with + or
 * a position {CHAR_MAX}, those placed first where the position is 0 or
 * {CHAR_MAX}, and "-" for a negative amount where both strings are empty
 * and no + asked for them. A left precision pads the parts around the
 * amount with spaces to the longer of a positive and a negative amount's.
 */
typedef struct {
    int fill;
    int group;
    int style;
    int symbol;
    int left;
    int width;
    int align;
    int places;
} Spec;

typedef struct {
    char* text;
    size_t used;
    size_t size;
} Text;

static int add(Text* t, const char* s, size_t n) {
    if (t->used + n + 1 > t->size) {
        size_t size = 2 * (t->used + n + 1);
        char* grown = realloc(t->text, size);

        if (!grown) {
            return -1;
        }

        t->text = grown;
        t->size = size;
    }

    memcpy(t->text + t->used, s, n);
    t->used += n;
    t->text[t->used] = 0;

    return 0;
}

static int addString(Text* t, const char* s) {
    return add(t, s, strlen(s));
}

// The amount without sign or symbol: fill, grouped digits, radix, fraction.
static int amount(Text* t, double x, const Spec* spec, const struct lconv* lc) {
    char* digits = 0;
    int n = asprintf(&digits, "%.*f", spec->places, fabs(x));

    if (n < 0) {
        return -1;
    }

    if (!isfinite(x)) {
        int r = add(t, digits, n);
        free(digits);

        return r;
    }

    char* point = strchr(digits, '.');
    int integer = point ? (int)(point - digits) : n;
    const char* grouping = spec->group ? lc->mon_grouping : "";
    const char* separator = lc->mon_thousands_sep;
    const char* radix = *lc->mon_decimal_point ? lc->mon_decimal_point : lc->decimal_point;
    int r = 0;

    for (int i = integer; i < spec->left && r == 0; i++) {
        r = add(t, (const char[]){(char)spec->fill}, 1);
    }

    // Where the separators go: after the digit with as many on its right
    // as the groups from the right hold.
    char marks[400] = {0};

    if (*separator && *grouping) {
        int at = integer;
        const char* g = grouping;
        int size = 0;

        while (at > 0) {
            if (*g && *g != CHAR_MAX) {
                size = *g++;
            } else if (*g == CHAR_MAX || size <= 0) {
                break;
            }

            at -= size;

            if (at > 0 && at < (int)sizeof marks) {
                marks[at] = 1;
            }
        }
    }

    for (int i = 0; i < integer && r == 0; i++) {
        if (marks[i]) {
            r = addString(t, separator);
        }

        if (r == 0) {
            r = add(t, digits + i, 1);
        }
    }

    if (r == 0 && spec->places > 0) {
        r = addString(t, radix);

        if (r == 0) {
            r = add(t, point + 1, strlen(point + 1));
        }
    }

    free(digits);

    return r;
}

/*
 * A space between two parts that are there. C's sep_by_space: 1 a space
 * between the symbol, or the sign and the symbol beside it, and the
 * amount, so none without a symbol; 2 one between the sign and what it
 * touches, the symbol or the amount.
 */
static int space(Text* t, const char* a, const char* b) {
    return *a && *b ? add(t, " ", 1) : 0;
}

// The parts around the amount, as the locale's positions say.
static int around(Text* before, Text* after, int negative, int intl, const Spec* spec, const struct lconv* lc) {
    const char* symbol = spec->symbol ? (intl ? lc->int_curr_symbol : lc->currency_symbol) : "";
    char precedes = negative ? (intl ? lc->int_n_cs_precedes : lc->n_cs_precedes)
                             : (intl ? lc->int_p_cs_precedes : lc->p_cs_precedes);
    char separated = negative ? (intl ? lc->int_n_sep_by_space : lc->n_sep_by_space)
                              : (intl ? lc->int_p_sep_by_space : lc->p_sep_by_space);
    char position = negative ? (intl ? lc->int_n_sign_posn : lc->n_sign_posn)
                             : (intl ? lc->int_p_sign_posn : lc->p_sign_posn);
    const char* sign = negative ? lc->negative_sign : lc->positive_sign;
    int parentheses = spec->style == '(' || (spec->style == 0 && position == 0);
    int undetermined = position == CHAR_MAX;

    if (spec->style == '+' && !*lc->positive_sign && !*lc->negative_sign) {
        errno = EINVAL;

        return -1;
    }

    if (parentheses) {
        sign = "";
        position = 1;
    } else if (position == 0 || position == CHAR_MAX) {
        position = 1;
    }

    if (!parentheses && spec->style == 0 && undetermined && !*lc->positive_sign && !*lc->negative_sign) {
        sign = negative ? "-" : "";
    }

    if (precedes == CHAR_MAX) {
        precedes = 1;
    }

    if (separated == CHAR_MAX) {
        separated = 0;
    }

    const char* number = "N";
    int r = 0;

    if (parentheses && negative) {
        r |= add(before, "(", 1);
    }

    if (precedes) {
        switch (position) {
            case 1:
            case 3:
                r |= addString(before, sign);
                r |= separated == 2 ? space(before, sign, symbol) : 0;
                r |= addString(before, symbol);
                r |= separated == 1 ? space(before, symbol, number) : 0;
                break;
            case 2:
                r |= addString(before, symbol);
                r |= separated == 1 ? space(before, symbol, number) : 0;
                r |= separated == 2 ? space(after, number, sign) : 0;
                r |= addString(after, sign);
                break;
            case 4:
                r |= addString(before, symbol);
                r |= separated == 2 ? space(before, symbol, sign) : 0;
                r |= addString(before, sign);
                r |= separated == 1 ? space(before, symbol, number) : 0;
                break;
        }
    } else {
        switch (position) {
            case 1:
                r |= addString(before, sign);
                r |= separated == 2 ? space(before, sign, number) : 0;
                r |= separated == 1 ? space(after, number, symbol) : 0;
                r |= addString(after, symbol);
                break;
            case 2:
            case 4:
                r |= separated == 1 ? space(after, number, symbol) : 0;
                r |= addString(after, symbol);
                r |= separated == 2 ? space(after, symbol, sign) : 0;
                r |= addString(after, sign);
                break;
            case 3:
                r |= separated == 1 ? space(after, number, symbol) : 0;
                r |= addString(after, sign);
                r |= separated == 2 ? space(after, sign, symbol) : 0;
                r |= addString(after, symbol);
                break;
        }
    }

    if (parentheses && negative) {
        r |= add(after, ")", 1);
    }

    return r ? -1 : 0;
}

static int pad(Text* t, int n) {
    int r = 0;

    for (int i = 0; i < n && r == 0; i++) {
        r = add(t, " ", 1);
    }

    return r;
}

// One conversion, i or n, of x, appended to out.
static int convert(Text* out, double x, int intl, Spec* spec) {
    const struct lconv* lc = localeconv();
    int negative = x < 0;
    int places = intl ? lc->int_frac_digits : lc->frac_digits;

    if (spec->places < 0) {
        spec->places = places == CHAR_MAX || places < 0 ? 2 : places;
    }

    Text number = {0, 0, 0};
    Text before = {0, 0, 0};
    Text after = {0, 0, 0};
    Text other[2] = {{0, 0, 0}, {0, 0, 0}};
    int r = amount(&number, x, spec, lc);

    r = r ? r : around(&before, &after, negative, intl, spec, lc);

    if (r == 0 && spec->left >= 0) {
        r = around(&other[0], &other[1], !negative, intl, spec, lc);

        if (r == 0) {
            Text padded = {0, 0, 0};
            size_t wide = other[0].used > before.used ? other[0].used : before.used;

            r = pad(&padded, (int)(wide - before.used));
            r = r ? r : add(&padded, before.text ? before.text : "", before.used);
            free(before.text);
            before = padded;

            size_t tail = other[1].used > after.used ? other[1].used : after.used;
            r = r ? r : pad(&after, (int)(tail - after.used));
        }
    }

    if (r == 0) {
        size_t length = before.used + number.used + after.used;
        int fill = spec->width > (int)length ? spec->width - (int)length : 0;

        r = spec->align ? 0 : pad(out, fill);
        r = r ? r : add(out, before.text ? before.text : "", before.used);
        r = r ? r : add(out, number.text ? number.text : "", number.used);
        r = r ? r : add(out, after.text ? after.text : "", after.used);
        r = r ? r : spec->align ? pad(out, fill) : 0;
    }

    free(number.text);
    free(before.text);
    free(after.text);
    free(other[0].text);
    free(other[1].text);

    return r;
}

static ssize_t format(char* s, size_t maxsize, const char* f, va_list ap) {
    Text out = {0, 0, 0};
    int r = add(&out, "", 0);

    while (r == 0 && *f) {
        if (*f != '%' || f[1] == '%') {
            r = add(&out, f, 1);
            f += *f == '%' ? 2 : 1;
            continue;
        }

        Spec spec = {' ', 1, 0, 1, -1, 0, 0, -1};

        for (f++;; f++) {
            if (*f == '=' && f[1]) {
                spec.fill = (unsigned char)*++f;
            } else if (*f == '^') {
                spec.group = 0;
            } else if (*f == '+' || *f == '(') {
                spec.style = *f;
            } else if (*f == '!') {
                spec.symbol = 0;
            } else if (*f == '-') {
                spec.align = 1;
            } else {
                break;
            }
        }

        for (; isdigit((unsigned char)*f); f++) {
            spec.width = 10 * spec.width + (*f - '0');
        }

        if (*f == '#') {
            for (spec.left = 0, f++; isdigit((unsigned char)*f); f++) {
                spec.left = 10 * spec.left + (*f - '0');
            }
        }

        if (*f == '.') {
            for (spec.places = 0, f++; isdigit((unsigned char)*f); f++) {
                spec.places = 10 * spec.places + (*f - '0');
            }
        }

        if (*f != 'i' && *f != 'n') {
            errno = EINVAL;
            r = -1;
            break;
        }

        r = convert(&out, va_arg(ap, double), *f++ == 'i', &spec);
    }

    ssize_t n = -1;

    if (r == 0) {
        if (out.used < maxsize) {
            memcpy(s, out.text, out.used + 1);
            n = (ssize_t)out.used;
        } else {
            errno = E2BIG;
        }
    }

    free(out.text);

    return n;
}

// The two take their va_list, as a program's forwarders pass it.
ssize_t __kernel_strfmon(char* restrict s, size_t maxsize, const char* restrict f, va_list ap) {
    return format(s, maxsize, f, ap);
}

ssize_t __kernel_strfmon_l(char* restrict s, size_t maxsize, locale_t locale, const char* restrict f, va_list ap) {
    locale_t previous = uselocale(locale);
    ssize_t n = format(s, maxsize, f, ap);
    uselocale(previous);

    return n;
}
