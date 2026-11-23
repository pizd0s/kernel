#pragma once

/*
 * lib/c's math.h for FreeBSD's sources, with what they take of FreeBSD's
 * own: isnanf, the BSD name of isnan for a float, a function as its
 * sources call it, and sinpi and cospi, C23's, which its tgamma calls.
 */
#include_next <math.h>

static inline int isnanf(float x) {
    return isnan(x);
}

double sinpi(double);
long double sinpil(long double);
double cospi(double);
long double cospil(long double);
