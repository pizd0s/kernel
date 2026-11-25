/*
 * a64l and l64a: a number of 32 bits as at most six radix-64 digits,
 * "./0-9A-Za-z", the least significant first; a64l stops at the first
 * character of no digit, and its sum keeps the low 32 bits.
 */
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

static const char digits[] = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";

long __kernel_a64l(const char* s) {
    uint32_t v = 0;

    for (int i = 0; i < 6 && s[i]; i++) {
        const char* d = strchr(digits, s[i]);

        if (!d) {
            break;
        }

        v |= (uint32_t)(d - digits) << (6 * i);
    }

    return (int32_t)v;
}

char* __kernel_l64a(long value) {
    static char text[7];
    uint32_t v = (uint32_t)value;
    int n = 0;

    for (; v && n < 6; v >>= 6) {
        text[n++] = digits[v & 63];
    }

    text[n] = 0;

    return text;
}
