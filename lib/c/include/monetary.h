#pragma once

#include <__base.h>

__BEGIN_DECLS

/* Amounts of money as the locale writes them: lib/c's, musl's. */
ssize_t strfmon(char* __restrict, size_t, const char* __restrict, ...);
ssize_t strfmon_l(char* __restrict, size_t, locale_t, const char* __restrict, ...);

__END_DECLS
