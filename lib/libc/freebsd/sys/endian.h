#pragma once

/*
 * FreeBSD's name of endian.h, which its sources include, with its
 * functions of a big-endian number's bytes, which sha2 and crypt take.
 */
#include <endian.h>
#include <stdint.h>

static inline uint32_t be32dec(const void* p) {
    const uint8_t* b = p;

    return (uint32_t)b[0] << 24 | (uint32_t)b[1] << 16 | (uint32_t)b[2] << 8 | b[3];
}

static inline uint64_t be64dec(const void* p) {
    return (uint64_t)be32dec(p) << 32 | be32dec((const uint8_t*)p + 4);
}

static inline void be32enc(void* p, uint32_t v) {
    uint8_t* b = p;

    b[0] = v >> 24;
    b[1] = v >> 16;
    b[2] = v >> 8;
    b[3] = v;
}

static inline void be64enc(void* p, uint64_t v) {
    be32enc(p, v >> 32);
    be32enc((uint8_t*)p + 4, (uint32_t)v);
}
