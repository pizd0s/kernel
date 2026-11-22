#pragma once

/*
 * lib/c's limits.h for FreeBSD's sources, with what FreeBSD's sys/limits.h
 * adds and they take: size_t's and off_t's limits.
 */
#include <stdint.h>

#include_next <limits.h>

#define SIZE_T_MAX SIZE_MAX
#define OFF_MAX 0x7fffffffffffffffLL
#define OFF_MIN (-OFF_MAX - 1)
