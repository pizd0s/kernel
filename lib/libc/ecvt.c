#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/*
 * ecvt and fcvt, SUSv3's (legacy): the digits of a double, rounded as
 * printf rounds, without its radix character, *decpt saying where that
 * goes and *sign whether the value is negative. A double gives 15 digits
 * at most, so a longer request gets those; 0 digits are an empty string;
 * fcvt's fraction digits are at least 0, and a value they round to zero
 * is digits of 0 with *decpt 1. An infinity or NaN gives printf's name
 * of it with *decpt 0. The string is static, the two functions' one.
 */
enum { DIGITS = 15 };

static char digits[DIGITS + 1];

static int special(double x, int* decpt, int* sign) {
    if (isfinite(x)) {
        return 0;
    }

    *sign = signbit(x) != 0;
    *decpt = 0;
    strcpy(digits, isnan(x) ? "nan" : "inf");

    return 1;
}

char* __kernel_ecvt(double x, int n, int* decpt, int* sign) {
    if (special(x, decpt, sign)) {
        return digits;
    }

    int wanted = n < 1 ? 1 : n > DIGITS ? DIGITS : n;
    char text[32];
    snprintf(text, sizeof text, "%.*e", wanted - 1, x);

    const char* c = text;
    int count = 0;

    *sign = *c == '-';
    c += *sign;

    for (; *c != 'e'; c++) {
        if (*c != '.') {
            digits[count++] = *c;
        }
    }

    digits[n < 1 ? 0 : count] = 0;
    *decpt = atoi(c + 1) + 1;

    return digits;
}

char* __kernel_fcvt(double x, int n, int* decpt, int* sign) {
    if (special(x, decpt, sign)) {
        return digits;
    }

    // A double's first digit is at most 324 places after the point.
    int places = n < 0 ? 0 : n > 340 ? 340 : n;
    char text[700];
    snprintf(text, sizeof text, "%.*f", places, fabs(x));

    char* point = strchr(text, '.');
    int integer = point ? (int)(point - text) : (int)strlen(text);
    const char* first = strpbrk(text, "123456789");

    *sign = signbit(x) != 0;

    if (!first) {
        int zeros = places + 1 > DIGITS ? DIGITS : places + 1;
        memset(digits, '0', zeros);
        digits[zeros] = 0;
        *decpt = 1;

        return digits;
    }

    int at = first < text + integer ? integer - (int)(first - text) : -(int)(first - point - 1);

    if (at + places > DIGITS) {
        return ecvt(x, DIGITS, decpt, sign);
    }

    int count = 0;

    for (const char* c = first; *c; c++) {
        if (*c != '.') {
            digits[count++] = *c;
        }
    }

    digits[count] = 0;
    *decpt = at;

    return digits;
}
