#pragma once

/*
 * lib/c's pwd.h for FreeBSD's sources, with what FreeBSD's adds and they
 * take: _PASSWORD_EFMT1, the first character of an extended DES setting,
 * crypt-des.c's.
 */
#include_next <pwd.h>

#define _PASSWORD_EFMT1 '_'
