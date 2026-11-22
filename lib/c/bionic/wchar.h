#pragma once

/*
 * lib/c's wchar.h for Bionic's sources, with stdio.h, which Bionic's
 * includes, and the names Bionic's gives the values C has its multibyte
 * conversions return (C23 7.30.1), which its functions use.
 */
#include <stdio.h>

#include_next <wchar.h>

#define BIONIC_MULTIBYTE_RESULT_ILLEGAL_SEQUENCE ((size_t)-1)
#define BIONIC_MULTIBYTE_RESULT_INCOMPLETE_SEQUENCE ((size_t)-2)
#define BIONIC_MULTIBYTE_RESULT_NO_BYTES_CONSUMED ((size_t)-3)
