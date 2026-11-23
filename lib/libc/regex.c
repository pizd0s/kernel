/*
 * regex: FreeBSD's, as its libregex builds it (make.py), behind the
 * kernel's functions as lib/c's forwarders call them, with the sizes of
 * regex_t and regmatch_t, which tell the program's lib/c. A program of the
 * lib/c before FreeBSD's regex.h has a regex_t of 32 bytes, re_nsub first,
 * in which FreeBSD's is kept at 8; a regmatch_t of two longs; and
 * REG_NEWLINE and REG_NOSUB of each other's values. Another size is
 * REG_INVARG.
 */
#include <regex.h>
#include <stdlib.h>

struct EarlierRegex {
    size_t re_nsub;
    void* unused;
    regex_t regex;
    size_t rest[2];
};

struct EarlierMatch {
    long rm_so;
    long rm_eo;
};

int __kernel_regcomp(regex_t* restrict r, size_t size, const char* restrict pattern, int flags);
int __kernel_regexec(const regex_t* restrict r, size_t size, const char* restrict s, size_t n, regmatch_t* restrict matches,
                     size_t matchSize, int flags);
void __kernel_regfree(regex_t* r, size_t size);
size_t __kernel_regerror(int code, const regex_t* restrict r, size_t size, char* restrict buffer, size_t room);

// regexOf is FreeBSD's regex_t of a program's of size bytes, or none.
static regex_t* regexOf(const regex_t* r, size_t size) {
    switch (size) {
        case sizeof(regex_t):
            return (regex_t*)r;
        case sizeof(struct EarlierRegex):
            return &((struct EarlierRegex*)r)->regex;
    }

    return NULL;
}

int __kernel_regcomp(regex_t* restrict r, size_t size, const char* restrict pattern, int flags) {
    regex_t* regex = regexOf(r, size);

    if (!regex) {
        return REG_INVARG;
    }

    if (regex == r) {
        return regcomp(r, pattern, flags);
    }

    int earlier = (flags & 1 ? REG_EXTENDED : 0) | (flags & 2 ? REG_ICASE : 0) | (flags & 4 ? REG_NEWLINE : 0) | (flags & 8 ? REG_NOSUB : 0);
    int code = regcomp(regex, pattern, earlier);

    if (code == 0) {
        ((struct EarlierRegex*)r)->re_nsub = regex->re_nsub;
    }

    return code;
}

int __kernel_regexec(const regex_t* restrict r, size_t size, const char* restrict s, size_t n, regmatch_t* restrict matches,
                     size_t matchSize, int flags) {
    const regex_t* regex = regexOf(r, size);

    if (!regex || (matchSize != sizeof(regmatch_t) && matchSize != sizeof(struct EarlierMatch))) {
        return REG_INVARG;
    }

    if (matchSize == sizeof(regmatch_t)) {
        return regexec(regex, s, n, matches, flags);
    }

    struct EarlierMatch* earlier = (struct EarlierMatch*)matches;
    size_t count = n > 0 ? n : (flags & REG_STARTEND) != 0;
    regmatch_t* current = count ? calloc(count, sizeof(regmatch_t)) : NULL;

    if (count && !current) {
        return REG_ESPACE;
    }

    if (flags & REG_STARTEND) {
        current[0].rm_so = earlier[0].rm_so;
        current[0].rm_eo = earlier[0].rm_eo;
    }

    int code = regexec(regex, s, n, current, flags);

    for (size_t i = 0; code == 0 && i < n; i++) {
        earlier[i].rm_so = current[i].rm_so;
        earlier[i].rm_eo = current[i].rm_eo;
    }

    free(current);

    return code;
}

void __kernel_regfree(regex_t* r, size_t size) {
    regex_t* regex = regexOf(r, size);

    if (regex) {
        regfree(regex);
    }
}

// regerror reads the regex_t of REG_ATOI alone, its re_endp, which only
// FreeBSD's has.
size_t __kernel_regerror(int code, const regex_t* restrict r, size_t size, char* restrict buffer, size_t room) {
    const regex_t none = {.re_endp = ""};

    return regerror(code, r && size == sizeof(regex_t) ? r : &none, buffer, room);
}
