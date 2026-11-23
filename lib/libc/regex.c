/*
 * regex: FreeBSD's, as its libregex builds it (make.py), behind the
 * kernel's functions as lib/c's forwarders call them, with the sizes of
 * regex_t and regmatch_t.
 */
#include "libc.h"

#include <regex.h>

int __kernel_regcomp(regex_t* restrict r, size_t size, const char* restrict pattern, int flags);
int __kernel_regexec(const regex_t* restrict r, size_t size, const char* restrict s, size_t n, regmatch_t* restrict matches,
                     size_t matchSize, int flags);
void __kernel_regfree(regex_t* r, size_t size);
size_t __kernel_regerror(int code, const regex_t* restrict r, size_t size, char* restrict buffer, size_t room);

int __kernel_regcomp(regex_t* restrict r, size_t size, const char* restrict pattern, int flags) {
    sized("regcomp", size, sizeof(regex_t));

    return regcomp(r, pattern, flags);
}

int __kernel_regexec(const regex_t* restrict r, size_t size, const char* restrict s, size_t n, regmatch_t* restrict matches,
                     size_t matchSize, int flags) {
    sized("regexec", size, sizeof(regex_t));
    sized("regexec", matchSize, sizeof(regmatch_t));

    return regexec(r, s, n, matches, flags);
}

void __kernel_regfree(regex_t* r, size_t size) {
    sized("regfree", size, sizeof(regex_t));
    regfree(r);
}

size_t __kernel_regerror(int code, const regex_t* restrict r, size_t size, char* restrict buffer, size_t room) {
    sized("regerror", size, sizeof(regex_t));

    return regerror(code, r, buffer, room);
}
