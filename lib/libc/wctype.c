/*
 * Wide characters' classes and cases, Unicode's of Go's unicode package
 * (unicode.h), as the kernel's Go had them: alphabetic characters are its
 * letters, letter numbers and Other_Alphabetic, blanks the tab and its
 * space separators, spaces its White_Space, neither of the no-break
 * spaces; the printable are all but the controls, separators of lines and
 * paragraphs, surrogates and non-characters; a lower case one has an upper
 * case, or is ß, an upper case one a lower. wcwidth is 0 of a combining
 * mark, of a zero width or of a tag, 2 of a wide or full width character,
 * -1 of a control or a non-character.
 */
#include "unicode.h"

#include <string.h>
#include <wchar.h>
#include <wctype.h>

enum Class { none, alnum, alpha, blank, cntrl, digit, graph, lower, print, punct, space, upper, xdigit };

static const char* const classNames[] = {"alnum", "alpha", "blank", "cntrl", "digit", "graph", "lower", "print", "punct", "space", "upper", "xdigit"};

static int within(const struct Range* list, size_t count, uint32_t c) {
    size_t lo = 0, hi = count;

    while (lo < hi) {
        size_t m = lo + (hi - lo) / 2;

        if (list[m].hi < c) {
            lo = m + 1;
        } else {
            hi = m;
        }
    }

    return lo < count && list[lo].lo <= c;
}

#define WITHIN(list, c) within(list, sizeof list / sizeof *list, c)

// The character's case of Go's unicode.To: upper or lower.
static uint32_t cased(uint32_t c, int toLower) {
    if (c > 0x10ffff) {
        return c;
    }

    if (c < 128) {
        return toLower ? (c - 'A' < 26 ? c + 32 : c) : (c - 'a' < 26 ? c - 32 : c);
    }

    size_t lo = 0, hi = sizeof cases / sizeof *cases;

    while (lo < hi) {
        size_t m = lo + (hi - lo) / 2;
        const struct CaseRange* r = &cases[m];

        if (r->lo <= c && c <= r->hi) {
            int32_t delta = toLower ? r->lower : r->upper;

            if (delta > 0x10ffff) {
                return r->lo + (((c - r->lo) & ~1u) | (toLower ? 1u : 0u));
            }

            return c + delta;
        }

        if (c < r->lo) {
            hi = m;
        } else {
            lo = m + 1;
        }
    }

    return c;
}

static int printable(uint32_t c) {
    if (c < 0xff) {
        return ((c + 1) & 0x7f) >= 0x21;
    }

    if (c < 0x2028 || c - 0x202a < 0xd800 - 0x202a || c - 0xe000 < 0xfff9 - 0xe000) {
        return 1;
    }

    return !(c - 0xfffc > 0x10ffff - 0xfffc || (c & 0xfffe) == 0xfffe);
}

static int classed(enum Class class, uint32_t c) {
    int valid = c <= 0x10ffff;

    switch (class) {
        case alnum:
            return c - '0' < 10 || (valid && WITHIN(alphabetic, c));
        case alpha:
            return valid && WITHIN(alphabetic, c);
        case blank:
            return c == '\t' || (valid && WITHIN(blanks, c));
        case cntrl:
            return c < 32 || c - 0x7f < 33 || c - 0x2028 < 2 || c - 0xfff9 < 3;
        case digit:
            return c - '0' < 10;
        case graph:
            return printable(c) && !classed(space, c);
        case lower:
            return cased(c, 0) != c || c == 0xdf;
        case print:
            return printable(c);
        case punct:
            return printable(c) && !classed(space, c) && !classed(alnum, c);
        case space:
            return valid && WITHIN(spaces, c);
        case upper:
            return cased(c, 1) != c;
        case xdigit:
            return c - '0' < 10 || (c | 32) - 'a' < 6;
        case none:
            break;
    }

    return 0;
}

int __kernel_iswalnum(wint_t c) {
    return classed(alnum, c);
}

int __kernel_iswalpha(wint_t c) {
    return classed(alpha, c);
}

int __kernel_iswblank(wint_t c) {
    return classed(blank, c);
}

int __kernel_iswcntrl(wint_t c) {
    return classed(cntrl, c);
}

int __kernel_iswdigit(wint_t c) {
    return classed(digit, c);
}

int __kernel_iswgraph(wint_t c) {
    return classed(graph, c);
}

int __kernel_iswlower(wint_t c) {
    return classed(lower, c);
}

int __kernel_iswprint(wint_t c) {
    return classed(print, c);
}

int __kernel_iswpunct(wint_t c) {
    return classed(punct, c);
}

int __kernel_iswspace(wint_t c) {
    return classed(space, c);
}

int __kernel_iswupper(wint_t c) {
    return classed(upper, c);
}

int __kernel_iswxdigit(wint_t c) {
    return classed(xdigit, c);
}

wctype_t __kernel_wctype(const char* name) {
    for (size_t i = 0; i < sizeof classNames / sizeof *classNames; i++) {
        if (strcmp(classNames[i], name) == 0) {
            return i + 1;
        }
    }

    return 0;
}

int __kernel_iswctype(wint_t c, wctype_t class) {
    return class <= xdigit && classed((enum Class)class, c);
}

wint_t __kernel_towlower(wint_t c) {
    return cased(c, 1);
}

wint_t __kernel_towupper(wint_t c) {
    return cased(c, 0);
}

static const int toUpper = 1, toLower = 2;

wctrans_t __kernel_wctrans(const char* name) {
    return strcmp(name, "toupper") == 0 ? &toUpper : strcmp(name, "tolower") == 0 ? &toLower : NULL;
}

wint_t __kernel_towctrans(wint_t c, wctrans_t trans) {
    return trans == &toUpper ? cased(c, 0) : trans == &toLower ? cased(c, 1) : c;
}
