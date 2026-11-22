#include <complex.h>
#include <math.h>
#include <stdint.h>

/*
 * Compiler runtime: wasm32 has no 128-bit multiply, so LLVM calls __multi3
 * for __int128 products and for 64-bit overflow checks. Only 64-bit
 * arithmetic is used here; an __int128 operator would call back into us.
 */
typedef union {
    __int128 all;
    struct {
        uint64_t low;
        uint64_t high;
    } parts;
} Words;

static Words multiplyWide(uint64_t a, uint64_t b) {
    const uint64_t lowerMask = 0xffffffffULL;
    Words r;
    uint64_t t;

    r.parts.low = (a & lowerMask) * (b & lowerMask);
    t = r.parts.low >> 32;
    r.parts.low &= lowerMask;
    t += (a >> 32) * (b & lowerMask);
    r.parts.low += (t & lowerMask) << 32;
    r.parts.high = t >> 32;
    t = r.parts.low >> 32;
    r.parts.low &= lowerMask;
    t += (b >> 32) * (a & lowerMask);
    r.parts.low += (t & lowerMask) << 32;
    r.parts.high += t >> 32;
    r.parts.high += (a >> 32) * (b >> 32);

    return r;
}

__int128 __multi3(__int128 a, __int128 b) {
    Words x;
    Words y;
    Words r;

    x.all = a;
    y.all = b;
    r = multiplyWide(x.parts.low, y.parts.low);
    r.parts.high += x.parts.high * y.parts.low + x.parts.low * y.parts.high;

    return r.all;
}

/*
 * 128-bit shifts, which LLVM libc's math uses, of a count below 128: of
 * the halves, one moving into the other.
 */
__int128 __ashlti3(__int128 a, int b) {
    Words x;
    Words r;

    x.all = a;
    if (b == 0) {
        return a;
    }
    if (b >= 64) {
        r.parts.low = 0;
        r.parts.high = x.parts.low << (b - 64);
    } else {
        r.parts.low = x.parts.low << b;
        r.parts.high = x.parts.high << b | x.parts.low >> (64 - b);
    }

    return r.all;
}

__int128 __lshrti3(__int128 a, int b) {
    Words x;
    Words r;

    x.all = a;
    if (b == 0) {
        return a;
    }
    if (b >= 64) {
        r.parts.high = 0;
        r.parts.low = x.parts.high >> (b - 64);
    } else {
        r.parts.high = x.parts.high >> b;
        r.parts.low = x.parts.low >> b | x.parts.high << (64 - b);
    }

    return r.all;
}

__int128 __ashrti3(__int128 a, int b) {
    Words x;
    Words r;

    x.all = a;
    if (b == 0) {
        return a;
    }
    if (b >= 64) {
        r.parts.high = (uint64_t)((int64_t)x.parts.high >> 63);
        r.parts.low = (uint64_t)((int64_t)x.parts.high >> (b - 64));
    } else {
        r.parts.high = (uint64_t)((int64_t)x.parts.high >> b);
        r.parts.low = x.parts.low >> b | x.parts.high << (64 - b);
    }

    return r.all;
}

/*
 * 128-bit division, which libc++'s conversions use: a quotient and remainder
 * of 64-bit halves, by native division when both fit in 64 bits and by
 * shift and subtract from the divisor's highest bit otherwise.
 */
static int leadingZeros(Words x) {
    if (x.parts.high) {
        return __builtin_clzll(x.parts.high);
    }

    return x.parts.low ? 64 + __builtin_clzll(x.parts.low) : 128;
}

static Words shiftLeft(Words x, int n) {
    Words r;

    if (n == 0) {
        return x;
    }

    if (n >= 64) {
        r.parts.high = x.parts.low << (n - 64);
        r.parts.low = 0;
    } else {
        r.parts.high = x.parts.high << n | x.parts.low >> (64 - n);
        r.parts.low = x.parts.low << n;
    }

    return r;
}

static Words divideUnsigned(Words n, Words d, Words* remainder) {
    Words q;
    q.parts.high = 0;
    q.parts.low = 0;

    if (!n.parts.high && !d.parts.high) {
        q.parts.low = n.parts.low / d.parts.low;
        remainder->parts.high = 0;
        remainder->parts.low = n.parts.low % d.parts.low;

        return q;
    }

    int shift = leadingZeros(d) - leadingZeros(n);

    if (shift < 0) {
        *remainder = n;

        return q;
    }

    Words s = shiftLeft(d, shift);

    for (int i = 0; i <= shift; i++) {
        q.parts.high = q.parts.high << 1 | q.parts.low >> 63;
        q.parts.low <<= 1;

        if (n.parts.high > s.parts.high || (n.parts.high == s.parts.high && n.parts.low >= s.parts.low)) {
            uint64_t borrow = n.parts.low < s.parts.low;
            n.parts.low -= s.parts.low;
            n.parts.high -= s.parts.high + borrow;
            q.parts.low |= 1;
        }

        s.parts.low = s.parts.low >> 1 | s.parts.high << 63;
        s.parts.high >>= 1;
    }

    *remainder = n;

    return q;
}

static Words negate(Words x) {
    x.parts.low = ~x.parts.low + 1;
    x.parts.high = ~x.parts.high + (x.parts.low == 0);

    return x;
}

static Words magnitude(Words x) {
    return (int64_t)x.parts.high < 0 ? negate(x) : x;
}

unsigned __int128 __udivti3(unsigned __int128 a, unsigned __int128 b) {
    Words x;
    Words y;
    Words r;

    x.all = a;
    y.all = b;

    return divideUnsigned(x, y, &r).all;
}

unsigned __int128 __umodti3(unsigned __int128 a, unsigned __int128 b) {
    Words x;
    Words y;
    Words r;

    x.all = a;
    y.all = b;
    divideUnsigned(x, y, &r);

    return r.all;
}

__int128 __divti3(__int128 a, __int128 b) {
    Words x;
    Words y;
    Words r;

    x.all = a;
    y.all = b;
    Words q = divideUnsigned(magnitude(x), magnitude(y), &r);

    return ((int64_t)(x.parts.high ^ y.parts.high) < 0 ? negate(q) : q).all;
}

__int128 __modti3(__int128 a, __int128 b) {
    Words x;
    Words y;
    Words r;

    x.all = a;
    y.all = b;
    divideUnsigned(magnitude(x), magnitude(y), &r);

    return ((int64_t)x.parts.high < 0 ? negate(r) : r).all;
}

/*
 * Products and quotients of complex numbers, which clang leaves to the
 * runtime: C's Annex G (G.5.1), as compiler-rt has them. The plain
 * formula first; where it gives NaN in both parts and an operand is
 * infinite, the result is an infinity, a quotient by zero too.
 */
static double complex doubleComplex(double x, double y) {
    union {
        double complex z;
        double parts[2];
    } u = {.parts = {x, y}};

    return u.z;
}

static float complex floatComplex(float x, float y) {
    union {
        float complex z;
        float parts[2];
    } u = {.parts = {x, y}};

    return u.z;
}

double complex __muldc3(double a, double b, double c, double d) {
    double ac = a * c;
    double bd = b * d;
    double ad = a * d;
    double bc = b * c;
    double x = ac - bd;
    double y = ad + bc;

    if (isnan(x) && isnan(y)) {
        int again = 0;

        if (isinf(a) || isinf(b)) {
            a = copysign(isinf(a) ? 1 : 0, a);
            b = copysign(isinf(b) ? 1 : 0, b);
            c = isnan(c) ? copysign(0, c) : c;
            d = isnan(d) ? copysign(0, d) : d;
            again = 1;
        }

        if (isinf(c) || isinf(d)) {
            c = copysign(isinf(c) ? 1 : 0, c);
            d = copysign(isinf(d) ? 1 : 0, d);
            a = isnan(a) ? copysign(0, a) : a;
            b = isnan(b) ? copysign(0, b) : b;
            again = 1;
        }

        if (!again && (isinf(ac) || isinf(bd) || isinf(ad) || isinf(bc))) {
            a = isnan(a) ? copysign(0, a) : a;
            b = isnan(b) ? copysign(0, b) : b;
            c = isnan(c) ? copysign(0, c) : c;
            d = isnan(d) ? copysign(0, d) : d;
            again = 1;
        }

        if (again) {
            x = INFINITY * (a * c - b * d);
            y = INFINITY * (a * d + b * c);
        }
    }

    return doubleComplex(x, y);
}

float complex __mulsc3(float a, float b, float c, float d) {
    float ac = a * c;
    float bd = b * d;
    float ad = a * d;
    float bc = b * c;
    float x = ac - bd;
    float y = ad + bc;

    if (isnan(x) && isnan(y)) {
        int again = 0;

        if (isinf(a) || isinf(b)) {
            a = copysignf(isinf(a) ? 1 : 0, a);
            b = copysignf(isinf(b) ? 1 : 0, b);
            c = isnan(c) ? copysignf(0, c) : c;
            d = isnan(d) ? copysignf(0, d) : d;
            again = 1;
        }

        if (isinf(c) || isinf(d)) {
            c = copysignf(isinf(c) ? 1 : 0, c);
            d = copysignf(isinf(d) ? 1 : 0, d);
            a = isnan(a) ? copysignf(0, a) : a;
            b = isnan(b) ? copysignf(0, b) : b;
            again = 1;
        }

        if (!again && (isinf(ac) || isinf(bd) || isinf(ad) || isinf(bc))) {
            a = isnan(a) ? copysignf(0, a) : a;
            b = isnan(b) ? copysignf(0, b) : b;
            c = isnan(c) ? copysignf(0, c) : c;
            d = isnan(d) ? copysignf(0, d) : d;
            again = 1;
        }

        if (again) {
            x = INFINITY * (a * c - b * d);
            y = INFINITY * (a * d + b * c);
        }
    }

    return floatComplex(x, y);
}

double complex __divdc3(double a, double b, double c, double d) {
    int scale = 0;
    double w = logb(fmax(fabs(c), fabs(d)));

    if (isfinite(w)) {
        scale = (int)w;
        c = scalbn(c, -scale);
        d = scalbn(d, -scale);
    }

    double denominator = c * c + d * d;
    double x = scalbn((a * c + b * d) / denominator, -scale);
    double y = scalbn((b * c - a * d) / denominator, -scale);

    if (isnan(x) && isnan(y)) {
        if (denominator == 0 && (!isnan(a) || !isnan(b))) {
            x = copysign(INFINITY, c) * a;
            y = copysign(INFINITY, c) * b;
        } else if ((isinf(a) || isinf(b)) && isfinite(c) && isfinite(d)) {
            a = copysign(isinf(a) ? 1 : 0, a);
            b = copysign(isinf(b) ? 1 : 0, b);
            x = INFINITY * (a * c + b * d);
            y = INFINITY * (b * c - a * d);
        } else if (isinf(w) && w > 0 && isfinite(a) && isfinite(b)) {
            c = copysign(isinf(c) ? 1 : 0, c);
            d = copysign(isinf(d) ? 1 : 0, d);
            x = 0 * (a * c + b * d);
            y = 0 * (b * c - a * d);
        }
    }

    return doubleComplex(x, y);
}

float complex __divsc3(float a, float b, float c, float d) {
    int scale = 0;
    float w = logbf(fmaxf(fabsf(c), fabsf(d)));

    if (isfinite(w)) {
        scale = (int)w;
        c = scalbnf(c, -scale);
        d = scalbnf(d, -scale);
    }

    float denominator = c * c + d * d;
    float x = scalbnf((a * c + b * d) / denominator, -scale);
    float y = scalbnf((b * c - a * d) / denominator, -scale);

    if (isnan(x) && isnan(y)) {
        if (denominator == 0 && (!isnan(a) || !isnan(b))) {
            x = copysignf(INFINITY, c) * a;
            y = copysignf(INFINITY, c) * b;
        } else if ((isinf(a) || isinf(b)) && isfinite(c) && isfinite(d)) {
            a = copysignf(isinf(a) ? 1 : 0, a);
            b = copysignf(isinf(b) ? 1 : 0, b);
            x = INFINITY * (a * c + b * d);
            y = INFINITY * (b * c - a * d);
        } else if (isinf(w) && w > 0 && isfinite(a) && isfinite(b)) {
            c = copysignf(isinf(c) ? 1 : 0, c);
            d = copysignf(isinf(d) ? 1 : 0, d);
            x = 0 * (a * c + b * d);
            y = 0 * (b * c - a * d);
        }
    }

    return floatComplex(x, y);
}
