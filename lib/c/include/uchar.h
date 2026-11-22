#pragma once

#include <__base.h>
#include <wchar.h>

__BEGIN_DECLS

/*
 * UTF-16 and UTF-32 code units to and from the multibyte characters of
 * the locale, UTF-8: lib/c's, musl's.
 */
#if !defined(__cplusplus) || __cplusplus < 201103L
typedef unsigned short char16_t;
typedef unsigned char32_t;
#endif

size_t c16rtomb(char* __restrict, char16_t, mbstate_t* __restrict);
size_t mbrtoc16(char16_t* __restrict, const char* __restrict, size_t, mbstate_t* __restrict);
size_t c32rtomb(char* __restrict, char32_t, mbstate_t* __restrict);
size_t mbrtoc32(char32_t* __restrict, const char* __restrict, size_t, mbstate_t* __restrict);

__END_DECLS
