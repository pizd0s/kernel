#pragma once

/*
 * FreeBSD's rune locale, of which its multibyte code takes only the type:
 * lib/c's one locale has none (xlocale_private.h). regcomp asks the
 * current one's encoding before it takes a pattern's bytes for characters'
 * substrings: UTF-8.
 */
typedef struct _RuneLocale _RuneLocale;

#define _CurrentRuneLocale (&(const struct { const char* __encoding; }){"UTF-8"})
