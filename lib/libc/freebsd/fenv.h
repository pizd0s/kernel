#pragma once

/*
 * lib/c's fenv.h for FreeBSD's sources, which test and clear exceptions
 * and change the rounding: wasm has no exception, so their flags are
 * none, and no test of one is true; and it rounds to nearest alone, so
 * the other modes are names fegetround never gives.
 */
#include_next <fenv.h>

#define FE_INVALID 0
#define FE_DIVBYZERO 0
#define FE_OVERFLOW 0
#define FE_UNDERFLOW 0
#define FE_INEXACT 0
#define FE_DOWNWARD 1
#define FE_UPWARD 2
#define FE_TOWARDZERO 3
