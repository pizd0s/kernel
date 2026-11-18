#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * The classes of characters and their cases: the kernel's functions, of
 * the C locale, which is every locale's here. Inline, so that a call is
 * the import's call, which the compiler may compile there; ctype.c
 * defines them for a pointer and a call without this header.
 */
int __kernel_isalnum(int) __KERNEL(isalnum);
int __kernel_isalpha(int) __KERNEL(isalpha);
int __kernel_isascii(int) __KERNEL(isascii);
int __kernel_isblank(int) __KERNEL(isblank);
int __kernel_iscntrl(int) __KERNEL(iscntrl);
int __kernel_isdigit(int) __KERNEL(isdigit);
int __kernel_isgraph(int) __KERNEL(isgraph);
int __kernel_islower(int) __KERNEL(islower);
int __kernel_isprint(int) __KERNEL(isprint);
int __kernel_ispunct(int) __KERNEL(ispunct);
int __kernel_isspace(int) __KERNEL(isspace);
int __kernel_isupper(int) __KERNEL(isupper);
int __kernel_isxdigit(int) __KERNEL(isxdigit);
int __kernel_tolower(int) __KERNEL(tolower);
int __kernel_toupper(int) __KERNEL(toupper);
int __kernel_toascii(int) __KERNEL(toascii);

__INLINE int isalnum(int __c) {
    return __kernel_isalnum(__c);
}

__INLINE int isalpha(int __c) {
    return __kernel_isalpha(__c);
}

__INLINE int isascii(int __c) {
    return __kernel_isascii(__c);
}

__INLINE int isblank(int __c) {
    return __kernel_isblank(__c);
}

__INLINE int iscntrl(int __c) {
    return __kernel_iscntrl(__c);
}

__INLINE int isdigit(int __c) {
    return __kernel_isdigit(__c);
}

__INLINE int isgraph(int __c) {
    return __kernel_isgraph(__c);
}

__INLINE int islower(int __c) {
    return __kernel_islower(__c);
}

__INLINE int isprint(int __c) {
    return __kernel_isprint(__c);
}

__INLINE int ispunct(int __c) {
    return __kernel_ispunct(__c);
}

__INLINE int isspace(int __c) {
    return __kernel_isspace(__c);
}

__INLINE int isupper(int __c) {
    return __kernel_isupper(__c);
}

__INLINE int isxdigit(int __c) {
    return __kernel_isxdigit(__c);
}

__INLINE int tolower(int __c) {
    return __kernel_tolower(__c);
}

__INLINE int toupper(int __c) {
    return __kernel_toupper(__c);
}

__INLINE int toascii(int __c) {
    return __kernel_toascii(__c);
}

int isalnum_l(int, locale_t);
int isalpha_l(int, locale_t);
int isblank_l(int, locale_t);
int iscntrl_l(int, locale_t);
int isdigit_l(int, locale_t);
int isgraph_l(int, locale_t);
int islower_l(int, locale_t);
int isprint_l(int, locale_t);
int ispunct_l(int, locale_t);
int isspace_l(int, locale_t);
int isupper_l(int, locale_t);
int isxdigit_l(int, locale_t);
int tolower_l(int, locale_t);
int toupper_l(int, locale_t);

__END_DECLS
