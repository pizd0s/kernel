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
double __kernel_sqrt(double x) {
    return __builtin_sqrt(x);
}

float __kernel_sqrtf(float x) {
    return __builtin_sqrtf(x);
}

long double __kernel_sqrtl(long double x) {
    return __builtin_sqrtl(x);
}

double __kernel_fabs(double x) {
    return __builtin_fabs(x);
}

float __kernel_fabsf(float x) {
    return __builtin_fabsf(x);
}

long double __kernel_fabsl(long double x) {
    return __builtin_fabsl(x);
}

double __kernel_copysign(double x, double y) {
    return __builtin_copysign(x, y);
}

float __kernel_copysignf(float x, float y) {
    return __builtin_copysignf(x, y);
}

long double __kernel_copysignl(long double x, long double y) {
    return __builtin_copysignl(x, y);
}

double __kernel_floor(double x) {
    return __builtin_floor(x);
}

float __kernel_floorf(float x) {
    return __builtin_floorf(x);
}

long double __kernel_floorl(long double x) {
    return __builtin_floorl(x);
}

double __kernel_ceil(double x) {
    return __builtin_ceil(x);
}

float __kernel_ceilf(float x) {
    return __builtin_ceilf(x);
}

long double __kernel_ceill(long double x) {
    return __builtin_ceill(x);
}

double __kernel_trunc(double x) {
    return __builtin_trunc(x);
}

float __kernel_truncf(float x) {
    return __builtin_truncf(x);
}

long double __kernel_truncl(long double x) {
    return __builtin_truncl(x);
}

double __kernel_rint(double x) {
    return __builtin_rint(x);
}

float __kernel_rintf(float x) {
    return __builtin_rintf(x);
}

long double __kernel_rintl(long double x) {
    return __builtin_rintl(x);
}

// The inexact exception rint may raise and nearbyint may not is none here.
double __kernel_nearbyint(double x) {
    return __builtin_rint(x);
}

float __kernel_nearbyintf(float x) {
    return __builtin_rintf(x);
}

long double __kernel_nearbyintl(long double x) {
    return __builtin_rintl(x);
}

long __kernel_lrint(double x) {
    return (long)__builtin_rint(x);
}

long __kernel_lrintf(float x) {
    return (long)__builtin_rintf(x);
}

long __kernel_lrintl(long double x) {
    return (long)__builtin_rintl(x);
}

long long __kernel_llrint(double x) {
    return (long long)__builtin_rint(x);
}

long long __kernel_llrintf(float x) {
    return (long long)__builtin_rintf(x);
}

long long __kernel_llrintl(long double x) {
    return (long long)__builtin_rintl(x);
}

// lgamma's sign goes to signgam, the program's, whose place a call comes
// with.
double __kernel_lgamma(double x, int* sign) {
    return lgamma_r(x, sign);
}

float __kernel_lgammaf(float x, int* sign) {
    return lgammaf_r(x, sign);
}

long double __kernel_lgammal(long double x, int* sign) {
    return lgammal_r(x, sign);
}

// FreeBSD's lround has no long double name.
long __kernel_lroundl(long double x) {
    return lround(x);
}

/*
 * What math.h declares for its classifying macros, musl's names, which
 * the macros themselves no longer call.
 */
int __kernel___fpclassify(double x) {
    return __builtin_fpclassify(FP_NAN, FP_INFINITE, FP_NORMAL, FP_SUBNORMAL, FP_ZERO, x);
}

int __kernel___fpclassifyf(float x) {
    return __builtin_fpclassify(FP_NAN, FP_INFINITE, FP_NORMAL, FP_SUBNORMAL, FP_ZERO, x);
}

int __kernel___fpclassifyl(long double x) {
    return __builtin_fpclassify(FP_NAN, FP_INFINITE, FP_NORMAL, FP_SUBNORMAL, FP_ZERO, x);
}

int __kernel___signbit(double x) {
    return __builtin_signbit(x);
}

int __kernel___signbitf(float x) {
    return __builtin_signbit(x);
}

int __kernel___signbitl(long double x) {
    return __builtin_signbit(x);
}

// The functions of what complex.h has as macros too.
double __kernel_creal(double complex z) {
    return __real__ z;
}

float __kernel_crealf(float complex z) {
    return __real__ z;
}

long double __kernel_creall(long double complex z) {
    return __real__ z;
}

double __kernel_cimag(double complex z) {
    return __imag__ z;
}

float __kernel_cimagf(float complex z) {
    return __imag__ z;
}

long double __kernel_cimagl(long double complex z) {
    return __imag__ z;
}

/*
 * The long double complex functions FreeBSD has none of where long double
 * is a double: the double ones, exactly.
 */
long double complex __kernel_ccoshl(long double complex z) {
    return ccosh(z);
}

long double complex __kernel_csinhl(long double complex z) {
    return csinh(z);
}

long double complex __kernel_ctanhl(long double complex z) {
    return ctanh(z);
}

long double complex __kernel_ccosl(long double complex z) {
    return ccos(z);
}

long double complex __kernel_csinl(long double complex z) {
    return csin(z);
}

long double complex __kernel_ctanl(long double complex z) {
    return ctan(z);
}
