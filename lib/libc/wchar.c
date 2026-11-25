/*
 * The multibyte functions of a conversion state, FreeBSD's: the state
 * comes with its size, as the kernel's structures do, the one layout it
 * has had.
 */
#include "sized.h"

#include <uchar.h>
#include <wchar.h>

size_t __kernel_mbrlen(const char* restrict s, size_t n, mbstate_t* restrict state, size_t size) {
    sized("mbrlen", size, sizeof(mbstate_t));

    return mbrlen(s, n, state);
}

size_t __kernel_mbrtowc(wchar_t* restrict wc, const char* restrict s, size_t n, mbstate_t* restrict state, size_t size) {
    sized("mbrtowc", size, sizeof(mbstate_t));

    return mbrtowc(wc, s, n, state);
}

int __kernel_mbsinit(const mbstate_t* state, size_t size) {
    sized("mbsinit", size, sizeof(mbstate_t));

    return mbsinit(state);
}

size_t __kernel_mbsrtowcs(wchar_t* restrict to, const char** restrict from, size_t n, mbstate_t* restrict state, size_t size) {
    sized("mbsrtowcs", size, sizeof(mbstate_t));

    return mbsrtowcs(to, from, n, state);
}

size_t __kernel_mbsnrtowcs(wchar_t* restrict to, const char** restrict from, size_t bytes, size_t n, mbstate_t* restrict state,
                           size_t size) {
    sized("mbsnrtowcs", size, sizeof(mbstate_t));

    return mbsnrtowcs(to, from, bytes, n, state);
}

size_t __kernel_wcrtomb(char* restrict s, wchar_t wc, mbstate_t* restrict state, size_t size) {
    sized("wcrtomb", size, sizeof(mbstate_t));

    return wcrtomb(s, wc, state);
}

size_t __kernel_wcsrtombs(char* restrict to, const wchar_t** restrict from, size_t n, mbstate_t* restrict state, size_t size) {
    sized("wcsrtombs", size, sizeof(mbstate_t));

    return wcsrtombs(to, from, n, state);
}

size_t __kernel_wcsnrtombs(char* restrict to, const wchar_t** restrict from, size_t wide, size_t n, mbstate_t* restrict state,
                           size_t size) {
    sized("wcsnrtombs", size, sizeof(mbstate_t));

    return wcsnrtombs(to, from, wide, n, state);
}

size_t __kernel_mbrtoc16(char16_t* restrict c, const char* restrict s, size_t n, mbstate_t* restrict state, size_t size) {
    sized("mbrtoc16", size, sizeof(mbstate_t));

    return mbrtoc16(c, s, n, state);
}

size_t __kernel_mbrtoc32(char32_t* restrict c, const char* restrict s, size_t n, mbstate_t* restrict state, size_t size) {
    sized("mbrtoc32", size, sizeof(mbstate_t));

    return mbrtoc32(c, s, n, state);
}

size_t __kernel_c16rtomb(char* restrict s, char16_t c, mbstate_t* restrict state, size_t size) {
    sized("c16rtomb", size, sizeof(mbstate_t));

    return c16rtomb(s, c, state);
}

size_t __kernel_c32rtomb(char* restrict s, char32_t c, mbstate_t* restrict state, size_t size) {
    sized("c32rtomb", size, sizeof(mbstate_t));

    return c32rtomb(s, c, state);
}
