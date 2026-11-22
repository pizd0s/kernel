#pragma once

/*
 * What FreeBSD's sources take of its sys/cdefs.h, for the parts of its
 * libc and msun that lib/c builds (make.py), which includes it first in
 * each: their markers, which say nothing here, and weak and strong
 * aliases, the long double names of the double functions among them; and
 * of its machine types, which its sys/types.h brings: the types float and
 * double expressions are evaluated in, as FLT_EVAL_METHOD 0 has them.
 */
#define __FBSDID(s)
#define __CONCAT1(x, y) x##y
#define __CONCAT(x, y) __CONCAT1(x, y)
#define __STRING(x) #x
#define __XSTRING(x) __STRING(x)
#define __always_inline __inline __attribute__((__always_inline__))
#define __unused __attribute__((__unused__))
#define __used __attribute__((__used__))
#define __dead2 __attribute__((__noreturn__))
#define __pure2 __attribute__((__const__))
#define __predict_true(e) __builtin_expect((e), 1)
#define __predict_false(e) __builtin_expect((e), 0)
#define __weak_reference(sym, alias) __weak_reference_(sym, alias)
#define __weak_reference_(sym, alias) extern __typeof(alias) alias __attribute__((__weak__, __alias__(#sym)))
#define __strong_reference(sym, alias) __strong_reference_(sym, alias)
#define __strong_reference_(sym, alias) extern __typeof(sym) alias __attribute__((__alias__(#sym)))

typedef float __float_t;
typedef double __double_t;
