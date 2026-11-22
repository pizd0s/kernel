#pragma once

#include <complex.h>
#include <stdint.h>

/*
 * What the FreeBSD msun sources here take from FreeBSD's math_private.h
 * and sys/cdefs.h, as they are there: the high word of a double and the
 * word of a float, read and set; Dekker's sums, a + b as a sum and its
 * error, _2sumF only for |a| >= |b|; a NaN of two arguments; the complex
 * constructors C99 lacks; and weak aliases, which give long double, here
 * double, its C names: each of the type its header declares it with, an
 * alias of the double function, which has its representation.
 */
#define __unused __attribute__((__unused__))
#define __weak_reference(sym, alias) extern __typeof(alias) alias __attribute__((__weak__, __alias__(#sym)))

typedef union {
    double value;
    uint64_t bits;
} __DoubleBits;

typedef union {
    float value;
    uint32_t bits;
} __FloatBits;

#define GET_HIGH_WORD(i, d) \
    do { \
        __DoubleBits __u = {.value = (d)}; \
        (i) = (uint32_t)(__u.bits >> 32); \
    } while (0)

#define SET_HIGH_WORD(d, v) \
    do { \
        __DoubleBits __u = {.value = (d)}; \
        __u.bits = (__u.bits & 0xffffffffu) | (uint64_t)(uint32_t)(v) << 32; \
        (d) = __u.value; \
    } while (0)

#define GET_FLOAT_WORD(i, f) \
    do { \
        __FloatBits __u = {.value = (f)}; \
        (i) = __u.bits; \
    } while (0)

#define SET_FLOAT_WORD(f, i) \
    do { \
        __FloatBits __u = {.bits = (uint32_t)(i)}; \
        (f) = __u.value; \
    } while (0)

#define _2sum(a, b) \
    do { \
        __typeof(a) __s, __w; \
        __w = (a) + (b); \
        __s = __w - (a); \
        (b) = ((a) - (__w - __s)) + ((b) - __s); \
        (a) = __w; \
    } while (0)

#define _2sumF(a, b) \
    do { \
        __typeof(a) __w; \
        __w = (a) + (b); \
        (b) = ((a) - __w) + (b); \
        (a) = __w; \
    } while (0)

#define nan_mix(x, y) (((x) + 0.0L) + ((y) + 0))

#ifndef CMPLX
static inline double complex CMPLX(double x, double y) {
    union {
        double complex z;
        double parts[2];
    } u = {.parts = {x, y}};

    return u.z;
}
#endif

#ifndef CMPLXF
static inline float complex CMPLXF(float x, float y) {
    union {
        float complex z;
        float parts[2];
    } u = {.parts = {x, y}};

    return u.z;
}
#endif
