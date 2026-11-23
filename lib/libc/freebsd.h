#pragma once

/*
 * What FreeBSD's sources in libc.wasm take of FreeBSD beside lib/c's
 * freebsd/ headers, included first in each: errno, as this directory's
 * sources reach it (libc.h); and regcomp asks the rune locale's encoding
 * before it takes a pattern's bytes for characters' substrings, and lib/c's
 * one locale's is UTF-8.
 */
#include "libc.h"

#define _CurrentRuneLocale (&(const struct { const char* __encoding; }){"UTF-8"})
