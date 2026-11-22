#pragma once

/*
 * lib/c's ctype.h for Bionic's sources, with Bionic's _tolower: an ASCII
 * letter of either case made lowercase, as its strtol takes it of any
 * letter isalpha finds, all ASCII in lib/c's locale.
 */
#include_next <ctype.h>

#ifndef _tolower
#define _tolower(c) ((c) | 0x20)
#endif
