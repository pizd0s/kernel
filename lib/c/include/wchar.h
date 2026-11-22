#pragma once

#include <__base.h>
/* the classification functions, which XSI's wchar.h declares too */
#include <wctype.h>

__BEGIN_DECLS

typedef struct __sFILE FILE;

struct tm;

// As FreeBSD's, whose multibyte functions lib/c's are: room for the state
// of any of them, a state of char16_t's holding one of char32_t's.
typedef union {
    char __mbstate8[128];
    long long _mbstateL;
} mbstate_t;

#define WEOF 0xffffffffU
#define WCHAR_MIN (-1 - __WCHAR_MAX__)
#define WCHAR_MAX __WCHAR_MAX__

wint_t btowc(int);
int wctob(wint_t);
int mbsinit(const mbstate_t*);
size_t mbrlen(const char* __restrict, size_t, mbstate_t* __restrict);
size_t mbrtowc(wchar_t* __restrict, const char* __restrict, size_t, mbstate_t* __restrict);
size_t wcrtomb(char* __restrict, wchar_t, mbstate_t* __restrict);
size_t mbsrtowcs(wchar_t* __restrict, const char** __restrict, size_t, mbstate_t* __restrict);
size_t wcsrtombs(char* __restrict, const wchar_t** __restrict, size_t, mbstate_t* __restrict);
size_t mbsnrtowcs(wchar_t* __restrict, const char** __restrict, size_t, size_t, mbstate_t* __restrict);
size_t wcsnrtombs(char* __restrict, const wchar_t** __restrict, size_t, size_t, mbstate_t* __restrict);
long wcstol(const wchar_t* __restrict, wchar_t** __restrict, int);
unsigned long wcstoul(const wchar_t* __restrict, wchar_t** __restrict, int);
long long wcstoll(const wchar_t* __restrict, wchar_t** __restrict, int);
unsigned long long wcstoull(const wchar_t* __restrict, wchar_t** __restrict, int);
float wcstof(const wchar_t* __restrict, wchar_t** __restrict);
double wcstod(const wchar_t* __restrict, wchar_t** __restrict);
long double wcstold(const wchar_t* __restrict, wchar_t** __restrict);

size_t wcslen(const wchar_t*);
size_t wcsnlen(const wchar_t*, size_t);
wchar_t* wcscpy(wchar_t* __restrict, const wchar_t* __restrict);
wchar_t* wcsncpy(wchar_t* __restrict, const wchar_t* __restrict, size_t);
wchar_t* wcscat(wchar_t* __restrict, const wchar_t* __restrict);
wchar_t* wcsncat(wchar_t* __restrict, const wchar_t* __restrict, size_t);
wchar_t* wcpcpy(wchar_t* __restrict, const wchar_t* __restrict);
wchar_t* wcpncpy(wchar_t* __restrict, const wchar_t* __restrict, size_t);
int wcscasecmp(const wchar_t*, const wchar_t*);
int wcsncasecmp(const wchar_t*, const wchar_t*, size_t);
int wcscasecmp_l(const wchar_t*, const wchar_t*, locale_t);
int wcsncasecmp_l(const wchar_t*, const wchar_t*, size_t, locale_t);
wchar_t* wcswcs(const wchar_t*, const wchar_t*);
int wcscmp(const wchar_t*, const wchar_t*);
int wcsncmp(const wchar_t*, const wchar_t*, size_t);
int wcscoll(const wchar_t*, const wchar_t*);
wchar_t* wcschr(const wchar_t*, wchar_t);
wchar_t* wcsrchr(const wchar_t*, wchar_t);
wchar_t* wcsstr(const wchar_t* __restrict, const wchar_t* __restrict);
wchar_t* wcspbrk(const wchar_t*, const wchar_t*);
size_t wcsspn(const wchar_t*, const wchar_t*);
size_t wcscspn(const wchar_t*, const wchar_t*);
wchar_t* wcstok(wchar_t* __restrict, const wchar_t* __restrict, wchar_t** __restrict);
size_t wcsxfrm(wchar_t* __restrict, const wchar_t* __restrict, size_t);
size_t wcsftime(wchar_t* __restrict, size_t, const wchar_t* __restrict, const struct tm* __restrict);
size_t wcsftime_l(wchar_t* __restrict, size_t, const wchar_t* __restrict, const struct tm* __restrict, locale_t);
wchar_t* wcsdup(const wchar_t*);
wchar_t* wmemchr(const wchar_t*, wchar_t, size_t);
int wmemcmp(const wchar_t*, const wchar_t*, size_t);
wchar_t* wmemcpy(wchar_t* __restrict, const wchar_t* __restrict, size_t);
wchar_t* wmemmove(wchar_t*, const wchar_t*, size_t);
wchar_t* wmemset(wchar_t*, wchar_t, size_t);
int wcwidth(wchar_t);
int wcswidth(const wchar_t*, size_t);

int wcscoll_l(const wchar_t*, const wchar_t*, locale_t);
size_t wcsxfrm_l(wchar_t* __restrict, const wchar_t* __restrict, size_t, locale_t);

wint_t fgetwc(FILE*);
wint_t getwc(FILE*);
wint_t getwchar(void);
wint_t ungetwc(wint_t, FILE*);
wint_t fputwc(wchar_t, FILE*);
wint_t putwc(wchar_t, FILE*);
wint_t putwchar(wchar_t);
wchar_t* fgetws(wchar_t* __restrict, int, FILE* __restrict);
int fputws(const wchar_t* __restrict, FILE* __restrict);
wint_t fgetwc_unlocked(FILE*);
wint_t getwc_unlocked(FILE*);
wint_t getwchar_unlocked(void);
wint_t fputwc_unlocked(wchar_t, FILE*);
wint_t putwc_unlocked(wchar_t, FILE*);
wint_t putwchar_unlocked(wchar_t);
wchar_t* fgetws_unlocked(wchar_t* __restrict, int, FILE* __restrict);
int fputws_unlocked(const wchar_t* __restrict, FILE* __restrict);
FILE* open_wmemstream(wchar_t**, size_t*);
int fwide(FILE*, int);
int swprintf(wchar_t* __restrict, size_t, const wchar_t* __restrict, ...);
int vswprintf(wchar_t* __restrict, size_t, const wchar_t* __restrict, va_list);
int fwprintf(FILE* __restrict, const wchar_t* __restrict, ...);
int vfwprintf(FILE* __restrict, const wchar_t* __restrict, va_list);
int wprintf(const wchar_t* __restrict, ...);
int vwprintf(const wchar_t* __restrict, va_list);
int swscanf(const wchar_t* __restrict, const wchar_t* __restrict, ...);
int vswscanf(const wchar_t* __restrict, const wchar_t* __restrict, va_list);
int fwscanf(FILE* __restrict, const wchar_t* __restrict, ...);
int vfwscanf(FILE* __restrict, const wchar_t* __restrict, va_list);
int wscanf(const wchar_t* __restrict, ...);
int vwscanf(const wchar_t* __restrict, va_list);

__END_DECLS
