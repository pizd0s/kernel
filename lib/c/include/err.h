#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * BSD's messages on stderr: the program's name, the message, and for
 * warn and err the error errno names; err and errx then exit.
 */
void warn(const char*, ...) __PRINTF(1, 2);
void vwarn(const char*, va_list) __PRINTF(1, 0);
void warnx(const char*, ...) __PRINTF(1, 2);
void vwarnx(const char*, va_list) __PRINTF(1, 0);
__NORETURN void err(int, const char*, ...) __PRINTF(2, 3);
__NORETURN void verr(int, const char*, va_list) __PRINTF(2, 0);
__NORETURN void errx(int, const char*, ...) __PRINTF(2, 3);
__NORETURN void verrx(int, const char*, va_list) __PRINTF(2, 0);

__END_DECLS
