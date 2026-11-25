#pragma once

/*
 * What FreeBSD's sources take of its sys/cdefs.h, for the parts of its
 * libc and msun that libc.wasm builds (make.py), which includes it first in
 * each: their markers, which say nothing here, __DECONST, __min_size, and
 * weak and strong aliases, the long double names of the double functions
 * among them; and of its machine types, which its sys/types.h brings: the
 * types float and double expressions are evaluated in, as FLT_EVAL_METHOD
 * 0 has them.
 */
#define __FBSDID(s)
#define __SCCSID(s)
#define __RCSID(s)
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
#define __warn_references(sym, msg)
#define __noinline __attribute__((__noinline__))
#define __hidden __attribute__((__visibility__("hidden")))
#define __DECONST(type, var) ((type)(__UINTPTR_TYPE__)(const void*)(var))
#define __min_size(x) static(x)
#define __printflike(f, a) __attribute__((__format__(__printf__, f, a)))
#define __scanflike(f, a) __attribute__((__format__(__scanf__, f, a)))
#define __strong_reference(sym, alias) __strong_reference_(sym, alias)
#define __strong_reference_(sym, alias) extern __typeof(sym) alias __attribute__((__alias__(#sym)))

typedef float __float_t;
typedef double __double_t;

// FreeBSD's name of va_list, and its locale_t, a pointer to its struct
// _xlocale (xlocale_private.h), which lib/c's names __locale_struct.
typedef __builtin_va_list __va_list;
#define __locale_struct _xlocale
#define __mbstate_t mbstate_t

// What FreeBSD's getopt has beside POSIX's variables, none of lib/c's
// headers'.
extern int optreset;

// FreeBSD's functions of a locale that are none of lib/c's, made reserved
// names.
#define asprintf_l __freebsd_asprintf_l
#define btowc_l __freebsd_btowc_l
#define c16rtomb_l __freebsd_c16rtomb_l
#define c32rtomb_l __freebsd_c32rtomb_l
#define f_prealloc __freebsd_f_prealloc
#define fdclose __freebsd_fdclose
#define fgetwc_l __freebsd_fgetwc_l
#define fgetws_l __freebsd_fgetws_l
#define fprintf_l __freebsd_fprintf_l
#define fputwc_l __freebsd_fputwc_l
#define fputws_l __freebsd_fputws_l
#define fscanf_l __freebsd_fscanf_l
#define fwprintf_l __freebsd_fwprintf_l
#define fwscanf_l __freebsd_fwscanf_l
#define getwc_l __freebsd_getwc_l
#define getwchar_l __freebsd_getwchar_l
#define mblen_l __freebsd_mblen_l
#define mbrlen_l __freebsd_mbrlen_l
#define mbrtoc16_l __freebsd_mbrtoc16_l
#define mbrtoc32_l __freebsd_mbrtoc32_l
#define mbrtowc_l __freebsd_mbrtowc_l
#define mbsinit_l __freebsd_mbsinit_l
#define mbsnrtowcs_l __freebsd_mbsnrtowcs_l
#define mbsrtowcs_l __freebsd_mbsrtowcs_l
#define mbstowcs_l __freebsd_mbstowcs_l
#define mbtowc_l __freebsd_mbtowc_l
#define printf_l __freebsd_printf_l
#define putwc_l __freebsd_putwc_l
#define putwchar_l __freebsd_putwchar_l
#define scanf_l __freebsd_scanf_l
#define snprintf_l __freebsd_snprintf_l
#define sprintf_l __freebsd_sprintf_l
#define sscanf_l __freebsd_sscanf_l
#define swprintf_l __freebsd_swprintf_l
#define swscanf_l __freebsd_swscanf_l
#define ungetwc_l __freebsd_ungetwc_l
#define vasprintf_l __freebsd_vasprintf_l
#define vfprintf_l __freebsd_vfprintf_l
#define vfscanf_l __freebsd_vfscanf_l
#define vfwprintf_l __freebsd_vfwprintf_l
#define vfwscanf_l __freebsd_vfwscanf_l
#define vprintf_l __freebsd_vprintf_l
#define vscanf_l __freebsd_vscanf_l
#define vsnprintf_l __freebsd_vsnprintf_l
#define vsprintf_l __freebsd_vsprintf_l
#define vsscanf_l __freebsd_vsscanf_l
#define vswprintf_l __freebsd_vswprintf_l
#define vswscanf_l __freebsd_vswscanf_l
#define vwprintf_l __freebsd_vwprintf_l
#define vwscanf_l __freebsd_vwscanf_l
#define wcrtomb_l __freebsd_wcrtomb_l
#define wcsnrtombs_l __freebsd_wcsnrtombs_l
#define wcsrtombs_l __freebsd_wcsrtombs_l
#define wcstombs_l __freebsd_wcstombs_l
#define wctob_l __freebsd_wctob_l
#define wctomb_l __freebsd_wctomb_l
#define wprintf_l __freebsd_wprintf_l
#define wscanf_l __freebsd_wscanf_l
