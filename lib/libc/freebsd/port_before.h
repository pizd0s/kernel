#pragma once

/*
 * What FreeBSD's inet sources, of BIND's, take of its port_before.h, and
 * the BSD types, of lib/c's sys/types.h, which FreeBSD's headers bring.
 */
#include <sys/types.h>

#define ISC_FORMAT_PRINTF(fmt, args) __attribute__((__format__(__printf__, fmt, args)))
#define UNUSED(x) (void)(x)
