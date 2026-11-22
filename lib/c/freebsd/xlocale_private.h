#pragma once

/*
 * FreeBSD's xlocale for its stdio and multibyte functions, over lib/c's
 * one locale: C's, with UTF-8 multibyte characters. Every locale_t they
 * are given is that one (xlocale.c), whose ctype is FreeBSD's UTF-8 and
 * whose collation is none, so ranges are of characters' values.
 */
#include <locale.h>
#include <stdint.h>
#include <stdlib.h>
#include <sys/types.h>

#include "xlocale.h"

enum {
    XLC_COLLATE = 0,
    XLC_CTYPE,
    XLC_MONETARY,
    XLC_NUMERIC,
    XLC_TIME,
    XLC_MESSAGES,
    XLC_LAST
};

struct xlocale_refcounted {
    long retain_count;
    void (*destructor)(void*);
};

struct xlocale_component {
    struct xlocale_refcounted header;
    char locale[32];
    char version[12];
};

struct _xlocale {
    struct xlocale_refcounted header;
    struct xlocale_component* components[XLC_LAST];
};

extern struct _xlocale __xlocale_global_locale;

static inline locale_t __get_locale(void) {
    return &__xlocale_global_locale;
}

#define FIX_LOCALE(l) ((l) = &__xlocale_global_locale)
#define localeconv_l(l) localeconv()
