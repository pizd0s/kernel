#pragma once

/*
 * lib/c's stdlib.h for FreeBSD's sources, with what FreeBSD's adds and
 * they take: reallocf, a realloc that frees the old block where it fails,
 * and arc4random_uniform, of getentropy's bytes, which lib/c has, as it
 * has no arc4random.
 */
#include_next <stdlib.h>

#include <stdint.h>
#include <unistd.h>

static inline void* reallocf(void* p, size_t n) {
    void* q = realloc(p, n);

    if (!q && n) {
        free(p);
    }

    return q;
}

static inline uint32_t arc4random_uniform(uint32_t n) {
    uint32_t r = 0;

    getentropy(&r, sizeof r);

    return n ? r % n : 0;
}
