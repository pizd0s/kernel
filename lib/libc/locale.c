#include <ctype.h>
#include <errno.h>
#include <langinfo.h>
#include <locale.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <wchar.h>
#include <wctype.h>

/*
 * The _l functions over the one locale lib/c has, C with UTF-8 multibyte
 * strings: each is the function without the locale. The locale objects,
 * newlocale's, with their categories' names, are the kernel's. strftime_l
 * hands the time's size on to the kernel's strftime, which it came with.
 */
size_t __kernel_strftime(char* restrict, size_t, const char* restrict, const struct tm* restrict, size_t) __KERNEL(strftime);

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
