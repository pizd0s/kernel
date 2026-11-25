#pragma once

/*
 * FreeBSD's xlocale.h for its stdio, multibyte and numbers' sources: the
 * functions of theirs that take a locale, as FreeBSD's xlocale/ declares
 * them, and those of others they call, which lib/c has without a locale,
 * its one.
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

intmax_t strtoimax_l(const char* __restrict, char** __restrict, int, locale_t);
uintmax_t strtoumax_l(const char* __restrict, char** __restrict, int, locale_t);
intmax_t wcstoimax_l(const wchar_t* __restrict, wchar_t** __restrict, int, locale_t);
uintmax_t wcstoumax_l(const wchar_t* __restrict, wchar_t** __restrict, int, locale_t);
long strtol_l(const char* __restrict, char** __restrict, int, locale_t);
long long strtoll_l(const char* __restrict, char** __restrict, int, locale_t);
unsigned long strtoul_l(const char* __restrict, char** __restrict, int, locale_t);
unsigned long long strtoull_l(const char* __restrict, char** __restrict, int, locale_t);
long wcstol_l(const wchar_t* __restrict, wchar_t** __restrict, int, locale_t);
long long wcstoll_l(const wchar_t* __restrict, wchar_t** __restrict, int, locale_t);
unsigned long wcstoul_l(const wchar_t* __restrict, wchar_t** __restrict, int, locale_t);
unsigned long long wcstoull_l(const wchar_t* __restrict, wchar_t** __restrict, int, locale_t);
double wcstod_l(const wchar_t* __restrict, wchar_t** __restrict, locale_t);
float wcstof_l(const wchar_t* __restrict, wchar_t** __restrict, locale_t);
long double wcstold_l(const wchar_t* __restrict, wchar_t** __restrict, locale_t);
int atoi_l(const char*, locale_t);
long atol_l(const char*, locale_t);
long long atoll_l(const char*, locale_t);
double atof_l(const char*, locale_t);
#define __strerror_rl(e, buf, size, locale) strerror_r(e, buf, size)
