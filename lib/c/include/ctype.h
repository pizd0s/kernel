#pragma once

#include <__base.h>

/*
 * The classes of characters and their cases, of the C locale's, which is
 * every locale's here: only ASCII is in one, as in musl. They are lib/c's
 * own, computed where they are called: no function of the kernel.
 */

__BEGIN_DECLS

__INLINE int isdigit(int __c) {
    return (unsigned)__c - '0' < 10;
}

__INLINE int islower(int __c) {
    return (unsigned)__c - 'a' < 26;
}

__INLINE int isupper(int __c) {
    return (unsigned)__c - 'A' < 26;
}

__INLINE int isalpha(int __c) {
    return ((unsigned)__c | 32) - 'a' < 26;
}

__INLINE int isalnum(int __c) {
    return isalpha(__c) || isdigit(__c);
}

__INLINE int isascii(int __c) {
    return !(__c & ~0x7f);
}

__INLINE int isblank(int __c) {
    return __c == ' ' || __c == '\t';
}

__INLINE int iscntrl(int __c) {
    return (unsigned)__c < 0x20 || __c == 0x7f;
}

__INLINE int isgraph(int __c) {
    return (unsigned)__c - 0x21 < 0x5e;
}

__INLINE int isprint(int __c) {
    return (unsigned)__c - 0x20 < 0x5f;
}

__INLINE int ispunct(int __c) {
    return isgraph(__c) && !isalnum(__c);
}

__INLINE int isspace(int __c) {
    return __c == ' ' || (unsigned)__c - '\t' < 5;
}

__INLINE int isxdigit(int __c) {
    return isdigit(__c) || ((unsigned)__c | 32) - 'a' < 6;
}

__INLINE int tolower(int __c) {
    return isupper(__c) ? __c | 32 : __c;
}

__INLINE int toupper(int __c) {
    return islower(__c) ? __c & 0x5f : __c;
}

__INLINE int toascii(int __c) {
    return __c & 0x7f;
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
