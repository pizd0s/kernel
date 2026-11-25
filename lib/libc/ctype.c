/*
 * The character classes of the C locale's bytes, ASCII's: a value of no
 * byte below 128, EOF among them, is of none, and tolower and toupper
 * leave it as it is.
 */
#include <ctype.h>

int __kernel_isascii(int c) {
    return (unsigned)c < 128;
}

int __kernel_isdigit(int c) {
    return (unsigned)c - '0' < 10;
}

int __kernel_islower(int c) {
    return (unsigned)c - 'a' < 26;
}

int __kernel_isupper(int c) {
    return (unsigned)c - 'A' < 26;
}

int __kernel_isalpha(int c) {
    return ((unsigned)c | 32) - 'a' < 26;
}

int __kernel_isalnum(int c) {
    return __kernel_isalpha(c) || __kernel_isdigit(c);
}

int __kernel_isxdigit(int c) {
    return __kernel_isdigit(c) || ((unsigned)c | 32) - 'a' < 6;
}

int __kernel_isblank(int c) {
    return c == ' ' || c == '\t';
}

int __kernel_isspace(int c) {
    return c == ' ' || (unsigned)c - '\t' < 5;
}

int __kernel_iscntrl(int c) {
    return (unsigned)c < 32 || c == 127;
}

int __kernel_isprint(int c) {
    return (unsigned)c - 32 < 95;
}

int __kernel_isgraph(int c) {
    return (unsigned)c - 33 < 94;
}

int __kernel_ispunct(int c) {
    return __kernel_isgraph(c) && !__kernel_isalnum(c);
}

int __kernel_tolower(int c) {
    return __kernel_isupper(c) ? c + 32 : c;
}

int __kernel_toupper(int c) {
    return __kernel_islower(c) ? c - 32 : c;
}
