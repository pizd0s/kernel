#pragma once

/*
 * What Bionic's printf takes of gdtoa, which lib/c has not: the digits of
 * a double, decimal (__dtoa, by gdtoa's modes) and hexadecimal (__hdtoa),
 * where the decimal point is and the sign, in memory of malloc's. They
 * are the kernel's, strconv's, as strtod is; long double is a double.
 */
#include <stdlib.h>

__BEGIN_DECLS

char* __dtoa(double, int, int, int*, int*, char**);
char* __ldtoa(long double*, int, int, int*, int*, char**);
char* __hdtoa(double, const char*, int, int*, int*, char**);
char* __hldtoa(long double, const char*, int, int*, int*, char**);

static inline void __freedtoa(char* s) {
    free(s);
}

__END_DECLS
