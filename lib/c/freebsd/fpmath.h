#pragma once

/*
 * FreeBSD's fpmath.h, the fields of a float, a double and a long double,
 * which is a double here, laid out as on FreeBSD's little-endian ARM,
 * whose long double is one too (lib/libc/arm/_fpmath.h).
 */
union IEEEf2bits {
    float f;
    struct {
        unsigned int man : 23;
        unsigned int exp : 8;
        unsigned int sign : 1;
    } bits;
};

#define DBL_MANH_SIZE 20
#define DBL_MANL_SIZE 32

union IEEEd2bits {
    double d;
    struct {
        unsigned int manl : 32;
        unsigned int manh : 20;
        unsigned int exp : 11;
        unsigned int sign : 1;
    } bits;
};

union IEEEl2bits {
    long double e;
    struct {
        unsigned int manl : 32;
        unsigned int manh : 20;
        unsigned int exp : 11;
        unsigned int sign : 1;
    } bits;
};

#define LDBL_NBIT 0
#define LDBL_IMPLICIT_NBIT
#define mask_nbit_l(u) ((void)0)
#define LDBL_MANH_SIZE 20
#define LDBL_MANL_SIZE 32
#define LDBL_TO_ARRAY32(u, a) \
    do { \
        (a)[0] = (uint32_t)(u).bits.manl; \
        (a)[1] = (uint32_t)(u).bits.manh; \
    } while (0)
