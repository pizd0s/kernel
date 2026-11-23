/*
 * regex: FreeBSD's, as its libregex builds it, behind the kernel's
 * functions as lib/c's forwarders call them. A program's regex_t is the
 * count of an expression's groups and the place of FreeBSD's regex_t, made
 * at regcomp and gone at regfree; its regmatch_t is two ssize_t, where
 * FreeBSD's offsets are off_t, so the matches go through FreeBSD's own.
 *
 * This file builds with FreeBSD's regex.h, which lib/c's cannot stand
 * beside: lib/c's two types are here by their layout, and the sizes a call
 * comes with are checked against them. Another size is REG_INVARG, as a
 * flag lib/c's header has not; a regex_t freed, or not compiled, is
 * REG_BADPAT.
 */
#include <regex.h>
#include <stdlib.h>
#include <sys/types.h>

struct Regex {
    size_t re_nsub;
    regex_t* compiled;
};

struct Match {
    ssize_t rm_so;
    ssize_t rm_eo;
};

// The matches converted in the frame; more are on the heap.
enum {
    inFrame = 16,
};

int __kernel_regcomp(struct Regex* restrict r, size_t size, const char* restrict pattern, int flags) {
    if (size != sizeof(struct Regex) || flags & ~(REG_EXTENDED | REG_ICASE | REG_NOSUB | REG_NEWLINE)) {
        return REG_INVARG;
    }

    regex_t* compiled = malloc(sizeof *compiled);

    r->compiled = NULL;

    if (!compiled) {
        return REG_ESPACE;
    }

    int code = regcomp(compiled, pattern, flags);

    if (code != 0) {
        free(compiled);

        return code;
    }

    r->re_nsub = compiled->re_nsub;
    r->compiled = compiled;

    return 0;
}

int __kernel_regexec(const struct Regex* restrict r, size_t size, const char* restrict s, size_t n, struct Match* restrict matches,
                     size_t matchSize, int flags) {
    if (size != sizeof(struct Regex) || matchSize != sizeof(struct Match) || flags & ~(REG_NOTBOL | REG_NOTEOL | REG_STARTEND)) {
        return REG_INVARG;
    }

    if (!r->compiled) {
        return REG_BADPAT;
    }

    // REG_STARTEND reads the first match where there is room for none.
    size_t count = !matches ? 0 : n > 0 ? n : (flags & REG_STARTEND) != 0;
    regmatch_t few[inFrame];
    regmatch_t* converted = few;

    if (count > inFrame) {
        converted = malloc(count * sizeof *converted);

        if (!converted) {
            return REG_ESPACE;
        }
    }

    for (size_t i = 0; i < count; i++) {
        converted[i].rm_so = matches[i].rm_so;
        converted[i].rm_eo = matches[i].rm_eo;
    }

    int code = regexec(r->compiled, s, matches ? n : 0, converted, flags);

    for (size_t i = 0; code == 0 && matches && i < n; i++) {
        matches[i].rm_so = converted[i].rm_so;
        matches[i].rm_eo = converted[i].rm_eo;
    }

    if (converted != few) {
        free(converted);
    }

    return code;
}

void __kernel_regfree(struct Regex* r, size_t size) {
    if (size != sizeof(struct Regex) || !r->compiled) {
        return;
    }

    regfree(r->compiled);
    free(r->compiled);
    r->compiled = NULL;
}

// FreeBSD's regerror reads its regex_t for a code of its own alone, which
// lib/c's header has not.
size_t __kernel_regerror(int code, const struct Regex* restrict r, size_t size, char* restrict buffer, size_t room) {
    const regex_t none = {.re_endp = ""};

    return regerror(code, &none, buffer, room);
}
