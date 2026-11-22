#pragma once

/*
 * lib/c's complex.h for FreeBSD's sources, under the name FreeBSD's
 * guards it by, which its math_private.h looks for before its complex
 * parts.
 */
#include_next <complex.h>

#define _COMPLEX_H
