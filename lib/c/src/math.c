#include <complex.h>
#include <math.h>

/*
 * The math functions wasm has an instruction for, which neither LLVM
 * libc nor FreeBSD knows here: the square root, the absolute value, the
 * sign, rounding to an integer, in the one rounding mode wasm has, to
 * nearest, and a rounded value as an integer, which wasm's conversion
 * saturates, an unspecified value outside the type as C has it. long
 * double is a double here.
 */
double sqrt(double x) {
    return __builtin_sqrt(x);
}

float sqrtf(float x) {
    return __builtin_sqrtf(x);
}

long double sqrtl(long double x) {
    return __builtin_sqrtl(x);
}

double fabs(double x) {
    return __builtin_fabs(x);
}

float fabsf(float x) {
    return __builtin_fabsf(x);
}

long double fabsl(long double x) {
    return __builtin_fabsl(x);
}

double copysign(double x, double y) {
    return __builtin_copysign(x, y);
}

float copysignf(float x, float y) {
    return __builtin_copysignf(x, y);
}

long double copysignl(long double x, long double y) {
    return __builtin_copysignl(x, y);
}

double floor(double x) {
    return __builtin_floor(x);
}

float floorf(float x) {
    return __builtin_floorf(x);
}

long double floorl(long double x) {
    return __builtin_floorl(x);
}

double ceil(double x) {
    return __builtin_ceil(x);
}

float ceilf(float x) {
    return __builtin_ceilf(x);
}

long double ceill(long double x) {
    return __builtin_ceill(x);
}

double trunc(double x) {
    return __builtin_trunc(x);
}

float truncf(float x) {
    return __builtin_truncf(x);
}

long double truncl(long double x) {
    return __builtin_truncl(x);
}

double rint(double x) {
    return __builtin_rint(x);
}

float rintf(float x) {
    return __builtin_rintf(x);
}

long double rintl(long double x) {
    return __builtin_rintl(x);
}

// The inexact exception rint may raise and nearbyint may not is none here.
double nearbyint(double x) {
    return __builtin_rint(x);
}

float nearbyintf(float x) {
    return __builtin_rintf(x);
}

long double nearbyintl(long double x) {
    return __builtin_rintl(x);
}

long lrint(double x) {
    return (long)__builtin_rint(x);
}

long lrintf(float x) {
    return (long)__builtin_rintf(x);
}

long lrintl(long double x) {
    return (long)__builtin_rintl(x);
}

long long llrint(double x) {
    return (long long)__builtin_rint(x);
}

long long llrintf(float x) {
    return (long long)__builtin_rintf(x);
}

long long llrintl(long double x) {
    return (long long)__builtin_rintl(x);
}

// FreeBSD's lround has no long double name.
long lroundl(long double x) {
    return lround(x);
}

/*
 * What math.h declares for its classifying macros, musl's names, which
 * the macros themselves no longer call.
 */
int __fpclassify(double x) {
    return __builtin_fpclassify(FP_NAN, FP_INFINITE, FP_NORMAL, FP_SUBNORMAL, FP_ZERO, x);
}

int __fpclassifyf(float x) {
    return __builtin_fpclassify(FP_NAN, FP_INFINITE, FP_NORMAL, FP_SUBNORMAL, FP_ZERO, x);
}

int __fpclassifyl(long double x) {
    return __builtin_fpclassify(FP_NAN, FP_INFINITE, FP_NORMAL, FP_SUBNORMAL, FP_ZERO, x);
}

int __signbit(double x) {
    return __builtin_signbit(x);
}

int __signbitf(float x) {
    return __builtin_signbit(x);
}

int __signbitl(long double x) {
    return __builtin_signbit(x);
}

/*
 * Functions of what complex.h has as macros: their names in parentheses
 * are not the macros'.
 */
double(creal)(double complex z) {
    return __real__ z;
}

float(crealf)(float complex z) {
    return __real__ z;
}

long double(creall)(long double complex z) {
    return __real__ z;
}

double(cimag)(double complex z) {
    return __imag__ z;
}

float(cimagf)(float complex z) {
    return __imag__ z;
}

long double(cimagl)(long double complex z) {
    return __imag__ z;
}

/*
 * The long double complex functions FreeBSD has none of where long double
 * is a double: the double ones, exactly.
 */
long double complex ccoshl(long double complex z) {
    return ccosh(z);
}

long double complex csinhl(long double complex z) {
    return csinh(z);
}

long double complex ctanhl(long double complex z) {
    return ctanh(z);
}

long double complex ccosl(long double complex z) {
    return ccos(z);
}

long double complex csinl(long double complex z) {
    return csin(z);
}

long double complex ctanl(long double complex z) {
    return ctan(z);
}
