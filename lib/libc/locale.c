#include <ctype.h>
#include <errno.h>
#include <langinfo.h>
#include <limits.h>
#include <locale.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <time.h>
#include <wchar.h>
#include <wctype.h>

/*
 * Locales. lib/c has one, C with UTF-8 multibyte strings, under any name
 * of the UTF-8 codeset, or of none: a locale is its categories' names,
 * which nothing else tells apart. A category's name is the one given, or
 * where it is empty LC_ALL's, the category's or LANG's from the
 * environment, the first set; one that is too long, begins with a dot or
 * has a slash is C.UTF-8; C and POSIX are C, and so is C.UTF-8 but of
 * LC_CTYPE. A name of another codeset is refused, ENOENT of newlocale.
 * LC_ALL's name is the categories', or theirs joined by semicolons where
 * they differ, as setlocale takes it back. The global locale is
 * setlocale's; newlocale's objects are the heap's, and the thread's
 * current locale, uselocale's, is one of them or the global one.
 *
 * The data are C's for every name, nl_langinfo's of the global locale,
 * and the _l functions are each the function without the locale.
 * strftime_l hands the time's size on to strftime, which it came with.
 */
size_t __kernel_strftime(char* restrict, size_t, const char* restrict, const struct tm* restrict, size_t);

enum {
    nameMax = 23,
};

struct __locale_struct {
    struct __locale_struct* next;
    char names[LC_ALL][nameMax + 1];
};

static char global[LC_ALL][nameMax + 1];
static struct __locale_struct* objects;
static locale_t current;

static const char* const variables[LC_ALL] = {"LC_CTYPE", "LC_NUMERIC", "LC_TIME", "LC_COLLATE", "LC_MONETARY", "LC_MESSAGES"};

// The strings a locale's names and items are given as, each kept once.
struct Kept {
    struct Kept* next;
    char text[];
};

static const char* kept(const char* text) {
    static struct Kept* all;

    for (struct Kept* k = all; k; k = k->next) {
        if (strcmp(k->text, text) == 0) {
            return k->text;
        }
    }

    struct Kept* k = malloc(sizeof *k + strlen(text) + 1);

    if (!k) {
        return NULL;
    }

    strcpy(k->text, text);
    k->next = all;
    all = k;

    return k->text;
}

// A category's name of the n bytes from name, as it is kept: empty for C.
static void named(char out[nameMax + 1], int category, const char* name, size_t n) {
    if (n == 0) {
        const char* from[] = {getenv("LC_ALL"), getenv(variables[category]), getenv("LANG")};

        for (int i = 0; i < 3 && n == 0; i++) {
            if (from[i] && *from[i]) {
                name = from[i];
                n = strlen(name);
            }
        }
    }

    if (n == 0 || n > nameMax || name[0] == '.' || memchr(name, '/', n)) {
        name = "C.UTF-8";
        n = 7;
    }

    if ((n == 1 && name[0] == 'C') || (n == 5 && memcmp(name, "POSIX", 5) == 0) || (category != LC_CTYPE && n == 7 && memcmp(name, "C.UTF-8", 7) == 0)) {
        n = 0;
    }

    memcpy(out, name, n);
    out[n] = 0;
}

// Whether a name is of the UTF-8 codeset, or of none.
static int utf8(const char* name) {
    const char* dot = strchr(name, '.');

    if (!dot) {
        return 1;
    }

    char codeset[nameMax + 1];
    size_t n = 0;

    for (const char* c = dot + 1; *c && *c != '@'; c++) {
        if (*c != '-') {
            codeset[n++] = tolower((unsigned char)*c);
        }
    }

    return n == 4 && memcmp(codeset, "utf8", 4) == 0;
}

// The names a request gives the categories of the mask: the name, or of
// all of them a part of it each, by semicolons, the last for the rest; 0,
// or ENOENT.
static int request(char names[LC_ALL][nameMax + 1], int mask, const char* name) {
    char result[LC_ALL][nameMax + 1];
    const char* part = name;
    const int all = (mask & ((1 << LC_ALL) - 1)) == (1 << LC_ALL) - 1;

    memcpy(result, names, sizeof result);

    for (int i = 0; i < LC_ALL; i++) {
        size_t n = all ? strcspn(part, ";") : strlen(part);

        if (mask & 1 << i) {
            named(result[i], i, part, n);

            if (!utf8(result[i])) {
                return ENOENT;
            }
        }

        if (all && part[n]) {
            part += n + 1;
        }
    }

    memcpy(names, result, sizeof result);

    return 0;
}

static const char* shown(const char* name) {
    return *name ? name : "C";
}

// The name of a category, or of LC_ALL, of the names.
static const char* nameOf(char names[LC_ALL][nameMax + 1], int category) {
    if (category < LC_ALL) {
        return kept(shown(names[category]));
    }

    char all[LC_ALL * (nameMax + 1)] = "";
    int same = 1;

    for (int i = 0; i < LC_ALL; i++) {
        same &= strcmp(names[i], names[0]) == 0;
        strcat(all, i ? ";" : "");
        strcat(all, shown(names[i]));
    }

    return kept(same ? shown(names[0]) : all);
}

char* __kernel_setlocale(int category, const char* name) {
    if (category < 0 || category > LC_ALL) {
        return NULL;
    }

    if (name && request(global, category == LC_ALL ? LC_ALL_MASK : 1 << category, name) != 0) {
        return NULL;
    }

    return (char*)nameOf(global, category);
}

static struct __locale_struct* object(locale_t locale) {
    for (struct __locale_struct* o = objects; o; o = o->next) {
        if (o == locale) {
            return o;
        }
    }

    return NULL;
}

static locale_t made(char names[LC_ALL][nameMax + 1]) {
    struct __locale_struct* o = malloc(sizeof *o);

    if (!o) {
        errno = ENOMEM;

        return NULL;
    }

    memcpy(o->names, names, sizeof o->names);
    o->next = objects;
    objects = o;

    return o;
}

locale_t __kernel_newlocale(int mask, const char* name, locale_t base) {
    char names[LC_ALL][nameMax + 1] = {""};
    struct __locale_struct* o = base ? object(base) : NULL;

    if (!name || mask < 0 || (base && !o)) {
        errno = EINVAL;

        return NULL;
    }

    if (o) {
        memcpy(names, o->names, sizeof names);
    }

    int e = request(names, mask, name);

    if (e) {
        errno = e;

        return NULL;
    }

    if (o) {
        memcpy(o->names, names, sizeof names);

        return o;
    }

    return made(names);
}

locale_t __kernel_duplocale(locale_t locale) {
    struct __locale_struct* o = object(locale);

    if (locale != LC_GLOBAL_LOCALE && !o) {
        errno = EINVAL;

        return NULL;
    }

    return made(o ? o->names : global);
}

void __kernel_freelocale(locale_t locale) {
    for (struct __locale_struct** o = &objects; *o; o = &(*o)->next) {
        if (*o == locale) {
            *o = locale->next;
            free(locale);

            return;
        }
    }
}

locale_t __kernel_uselocale(locale_t locale) {
    locale_t previous = current ? current : LC_GLOBAL_LOCALE;

    if (locale == LC_GLOBAL_LOCALE) {
        current = NULL;
    } else if (locale && !object(locale)) {
        errno = EINVAL;

        return NULL;
    } else if (locale) {
        current = locale;
    }

    return previous;
}

const char* __kernel_getlocalename_l(int category, locale_t locale) {
    struct __locale_struct* o = object(locale);

    if ((locale != LC_GLOBAL_LOCALE && !o) || category < 0 || category > LC_ALL) {
        return NULL;
    }

    return nameOf(o ? o->names : global, category);
}

struct lconv* __kernel_localeconv(size_t size) {
    static struct lconv c;

    c = (struct lconv){".", "", "", "", "", "", "", "", "", ""};
    memset(&c.int_frac_digits, CHAR_MAX, (char*)&c.int_n_sign_posn - &c.int_frac_digits + 1);

    return &c;
}

static const char* const times[] = {
    "Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat",
    "Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday",
    "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
    "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December",
    "AM", "PM", "%a %b %e %T %Y", "%m/%d/%y", "%H:%M:%S", "%I:%M:%S %p",
    "", "", "%m/%d/%y", "0123456789", "%a %b %e %T %Y", "%H:%M:%S",
};

static const char* const numbers[] = {".", ""};
static const char* const messages[] = {"^[yY]", "^[nN]", "yes", "no"};

char* __kernel_nl_langinfo(nl_item item) {
    unsigned category = (unsigned)item >> 16, index = item & 0xffff;

    if (item == CODESET) {
        return "UTF-8";
    }

    if (index == 0xffff && category < LC_ALL) {
        return (char*)kept(shown(global[category]));
    }

    const char* const* items[LC_ALL] = {[LC_NUMERIC] = numbers, [LC_TIME] = times, [LC_MESSAGES] = messages};
    const size_t counts[LC_ALL] = {[LC_NUMERIC] = 2, [LC_TIME] = sizeof times / sizeof *times, [LC_MESSAGES] = 4};

    return category < LC_ALL && index < counts[category] ? (char*)items[category][index] : "";
}

// Collation is of the characters' values, as C's is.
int __kernel_strcoll(const char* a, const char* b) {
    return strcmp(a, b);
}

size_t __kernel_strxfrm(char* restrict d, const char* restrict s, size_t n) {
    size_t length = strlen(s);

    if (n > length) {
        memcpy(d, s, length + 1);
    }

    return length;
}

int __kernel_wcscoll(const wchar_t* a, const wchar_t* b) {
    return wcscmp(a, b);
}

size_t __kernel_wcsxfrm(wchar_t* restrict d, const wchar_t* restrict s, size_t n) {
    size_t length = wcslen(s);

    if (length < n) {
        wmemcpy(d, s, length + 1);
    } else if (n > 0) {
        wmemcpy(d, s, n - 1);
        d[n - 1] = 0;
    }

    return length;
}

int __kernel_isalnum_l(int c, locale_t locale) {
    return isalnum(c);
}

int __kernel_isalpha_l(int c, locale_t locale) {
    return isalpha(c);
}

int __kernel_isblank_l(int c, locale_t locale) {
    return isblank(c);
}

int __kernel_iscntrl_l(int c, locale_t locale) {
    return iscntrl(c);
}

int __kernel_isdigit_l(int c, locale_t locale) {
    return isdigit(c);
}

int __kernel_isgraph_l(int c, locale_t locale) {
    return isgraph(c);
}

int __kernel_islower_l(int c, locale_t locale) {
    return islower(c);
}

int __kernel_isprint_l(int c, locale_t locale) {
    return isprint(c);
}

int __kernel_ispunct_l(int c, locale_t locale) {
    return ispunct(c);
}

int __kernel_isspace_l(int c, locale_t locale) {
    return isspace(c);
}

int __kernel_isupper_l(int c, locale_t locale) {
    return isupper(c);
}

int __kernel_isxdigit_l(int c, locale_t locale) {
    return isxdigit(c);
}

int __kernel_tolower_l(int c, locale_t locale) {
    return tolower(c);
}

int __kernel_toupper_l(int c, locale_t locale) {
    return toupper(c);
}

int __kernel_iswalnum_l(wint_t c, locale_t locale) {
    return iswalnum(c);
}

int __kernel_iswalpha_l(wint_t c, locale_t locale) {
    return iswalpha(c);
}

int __kernel_iswblank_l(wint_t c, locale_t locale) {
    return iswblank(c);
}

int __kernel_iswcntrl_l(wint_t c, locale_t locale) {
    return iswcntrl(c);
}

int __kernel_iswdigit_l(wint_t c, locale_t locale) {
    return iswdigit(c);
}

int __kernel_iswgraph_l(wint_t c, locale_t locale) {
    return iswgraph(c);
}

int __kernel_iswlower_l(wint_t c, locale_t locale) {
    return iswlower(c);
}

int __kernel_iswprint_l(wint_t c, locale_t locale) {
    return iswprint(c);
}

int __kernel_iswpunct_l(wint_t c, locale_t locale) {
    return iswpunct(c);
}

int __kernel_iswspace_l(wint_t c, locale_t locale) {
    return iswspace(c);
}

int __kernel_iswupper_l(wint_t c, locale_t locale) {
    return iswupper(c);
}

int __kernel_iswxdigit_l(wint_t c, locale_t locale) {
    return iswxdigit(c);
}

int __kernel_iswctype_l(wint_t c, wctype_t type, locale_t locale) {
    return iswctype(c, type);
}

wctype_t __kernel_wctype_l(const char* name, locale_t locale) {
    return wctype(name);
}

wint_t __kernel_towlower_l(wint_t c, locale_t locale) {
    return towlower(c);
}

wint_t __kernel_towupper_l(wint_t c, locale_t locale) {
    return towupper(c);
}

int __kernel_strcoll_l(const char* a, const char* b, locale_t locale) {
    return strcoll(a, b);
}

size_t __kernel_strxfrm_l(char* restrict d, const char* restrict s, size_t n, locale_t locale) {
    return strxfrm(d, s, n);
}

int __kernel_wcscoll_l(const wchar_t* a, const wchar_t* b, locale_t locale) {
    return wcscoll(a, b);
}

size_t __kernel_wcsxfrm_l(wchar_t* restrict d, const wchar_t* restrict s, size_t n, locale_t locale) {
    return wcsxfrm(d, s, n);
}

size_t __kernel_strftime_l(char* restrict s, size_t n, const char* restrict format, const struct tm* restrict tm, size_t size, locale_t locale) {
    return __kernel_strftime(s, n, format, tm, size);
}

char* __kernel_nl_langinfo_l(nl_item item, locale_t locale) {
    return nl_langinfo(item);
}

int __kernel_strcasecmp_l(const char* a, const char* b, locale_t locale) {
    return strcasecmp(a, b);
}

int __kernel_strncasecmp_l(const char* a, const char* b, size_t n, locale_t locale) {
    return strncasecmp(a, b, n);
}

int __kernel_wcscasecmp_l(const wchar_t* a, const wchar_t* b, locale_t locale) {
    return wcscasecmp(a, b);
}

int __kernel_wcsncasecmp_l(const wchar_t* a, const wchar_t* b, size_t n, locale_t locale) {
    return wcsncasecmp(a, b, n);
}

char* __kernel_strerror_l(int e, locale_t locale) {
    return strerror(e);
}

float __kernel_strtof_l(const char* restrict s, char** restrict end, locale_t locale) {
    return strtof(s, end);
}

double __kernel_strtod_l(const char* restrict s, char** restrict end, locale_t locale) {
    return strtod(s, end);
}

long double __kernel_strtold_l(const char* restrict s, char** restrict end, locale_t locale) {
    return strtold(s, end);
}

wctrans_t __kernel_wctrans_l(const char* name, locale_t locale) {
    return wctrans(name);
}

wint_t __kernel_towctrans_l(wint_t c, wctrans_t trans, locale_t locale) {
    return towctrans(c, trans);
}
