#pragma once

/*
 * What Bionic's sources take of its sys/cdefs.h, for the parts of it that
 * lib/c builds (make.py), which includes it first in each: C's linkage
 * from C++, its markers and attributes, the API level saying nothing here,
 * its casts, and strong aliases, C's names from C++ too, of one prototype
 * in wasm, where another is a trap.
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
// Public on Android's 32-bit ABI for its old macros, hidden on its 64-bit
// one; lib/c's ABI has no such names.
#define __LIBC32_LEGACY_PUBLIC__ __LIBC_HIDDEN__
#ifdef __cplusplus
#define __BIONIC_CAST(_k, _t, _v) (_k<_t>(_v))
#else
#define __BIONIC_CAST(_k, _t, _v) ((_t)(_v))
#endif
#define __always_inline __attribute__((__always_inline__))
#define __noreturn __attribute__((__noreturn__))
#define __printflike(x, y) __attribute__((__format__(printf, x, y)))
#define __scanflike(x, y) __attribute__((__format__(scanf, x, y)))
#define __nodiscard __attribute__((__warn_unused_result__))
#define __wur __nodiscard
// Bionic's name of va_list, which its stdarg.h has.
typedef __builtin_va_list __va_list;
#define __INTRODUCED_IN(api)
#define __predict_true(e) __builtin_expect((e), 1)
#define __predict_false(e) __builtin_expect((e), 0)
#define __unused __attribute__((__unused__))
#ifndef __strong_alias
#define __strong_alias(alias, sym) __strong_alias_(alias, sym)
#ifdef __cplusplus
#define __strong_alias_(alias, sym) extern "C" __typeof(sym) alias __attribute__((__alias__(#sym)))
#else
#define __strong_alias_(alias, sym) extern __typeof(sym) alias __attribute__((__alias__(#sym)))
#endif
#endif

// A call again while it fails with EINTR, as Bionic's unistd.h has it.
#define TEMP_FAILURE_RETRY(exp)                \
    ({                                         \
        __typeof__(exp) _rc;                   \
        do {                                   \
            _rc = (exp);                       \
        } while (_rc == -1 && errno == EINTR); \
        _rc;                                   \
    })

// Bionic's internal names that are a program's to use, made reserved ones:
// a static archive's names are all global.
#define parsefloat __bionic_parsefloat
#define wparsefloat __bionic_wparsefloat
#define recallocarray __bionic_recallocarray
#define helpers __bionic_helpers
