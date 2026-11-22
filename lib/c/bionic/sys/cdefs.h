#pragma once

/*
 * What Bionic's sources take of its sys/cdefs.h, for the parts of it that
 * lib/c builds (make.py), which includes it first in each: C's linkage
 * from C++, its markers, which say nothing here, and strong aliases, of
 * one prototype in wasm, where another is a trap.
 */
#ifndef __BEGIN_DECLS
#ifdef __cplusplus
#define __BEGIN_DECLS extern "C" {
#define __END_DECLS }
#else
#define __BEGIN_DECLS
#define __END_DECLS
#endif
#endif

#define __LIBC_HIDDEN__ __attribute__((__visibility__("hidden")))
#define __INTRODUCED_IN(api)
#define __predict_true(e) __builtin_expect((e), 1)
#define __predict_false(e) __builtin_expect((e), 0)
#define __unused __attribute__((__unused__))
#ifndef __strong_alias
#define __strong_alias(alias, sym) __strong_alias_(alias, sym)
#define __strong_alias_(alias, sym) extern __typeof(sym) alias __attribute__((__alias__(#sym)))
#endif
