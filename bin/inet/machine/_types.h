/* wasm32: arm64's, of a machine of 32-bit words. */
#ifndef _MACHINE__TYPES_H_
#define _MACHINE__TYPES_H_

#ifndef _SYS__TYPES_H_
#error do not include this header, use sys/_types.h
#endif

typedef __int32_t __clock_t;
typedef __int32_t __critical_t;
#ifndef _STANDALONE
typedef double __double_t;
typedef float __float_t;
#endif
typedef __int32_t __int_fast8_t;
typedef __int32_t __int_fast16_t;
typedef __int32_t __int_fast32_t;
typedef __int64_t __int_fast64_t;
typedef __int32_t __register_t;
typedef __int32_t __segsz_t;
typedef __int64_t __time_t;
typedef __uint32_t __uint_fast8_t;
typedef __uint32_t __uint_fast16_t;
typedef __uint32_t __uint_fast32_t;
typedef __uint64_t __uint_fast64_t;
typedef __uint32_t __u_register_t;
typedef __uint32_t __vm_paddr_t;
typedef int ___wchar_t;

#define __WCHAR_MIN __INT_MIN
#define __WCHAR_MAX __INT_MAX

#endif
