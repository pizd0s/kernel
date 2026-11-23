#pragma once

/*
 * FreeBSD's xlocale.h for its stdio and multibyte sources: the functions
 * of theirs that take a locale, as FreeBSD's xlocale/ declares them, and
 * those of others they call, which lib/c has without a locale, its one.
 */
#include <inttypes.h>
#include <string.h>
#include <locale.h>
#include <stdio.h>
#include <uchar.h>
#include <wchar.h>

#define _XLOCALE_H_
#define _LOCALE_T_DEFINED

#include <xlocale/_stdio.h>
#include <xlocale/_uchar.h>
#include <xlocale/_wchar.h>

#define strtoimax_l(s, end, base, locale) strtoimax(s, end, base)
#define strtoumax_l(s, end, base, locale) strtoumax(s, end, base)
#define wcstoimax_l(s, end, base, locale) wcstoimax(s, end, base)
#define wcstoumax_l(s, end, base, locale) wcstoumax(s, end, base)
#define __strerror_rl(e, buf, size, locale) strerror_r(e, buf, size)
