#pragma once

/*
 * lib/c's stdlib.h for FreeBSD's sources, with what FreeBSD's adds and
 * they take: reallocf, a realloc that frees the old block where it fails.
 */
#include_next <stdlib.h>

static inline void* reallocf(void* p, size_t n) {
    void* q = realloc(p, n);

    if (!q && n) {
        free(p);
    }

    return q;
}
