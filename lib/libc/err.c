#include <err.h>
#include <errno.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "kernel.h"

/*
 * BSD's messages, on stderr: the program's name, the message, and for
 * warn and err ": " and what errno named at the call; err and errx then
 * exit. The name is the kernel's (__kernel_name). A variadic one takes its
 * va_list, as a program's forwarder passes it.
 */
void __kernel_vwarn(const char* format, va_list ap) {
    int error = errno;

    fprintf(stderr, "%s: ", __kernel_name());
    if (format) {
        vfprintf(stderr, format, ap);
        fputs(": ", stderr);
    }
    fprintf(stderr, "%s\n", strerror(error));
}

void __kernel_vwarnx(const char* format, va_list ap) {
    fprintf(stderr, "%s: ", __kernel_name());
    if (format) {
        vfprintf(stderr, format, ap);
    }
    putc('\n', stderr);
}

void __kernel_verr(int status, const char* format, va_list ap) {
    __kernel_vwarn(format, ap);
    exit(status);
}

void __kernel_verrx(int status, const char* format, va_list ap) {
    __kernel_vwarnx(format, ap);
    exit(status);
}

void __kernel_warn(const char* format, va_list ap) {
    __kernel_vwarn(format, ap);
}

void __kernel_warnx(const char* format, va_list ap) {
    __kernel_vwarnx(format, ap);
}

void __kernel_err(int status, const char* format, va_list ap) {
    __kernel_verr(status, format, ap);
}

void __kernel_errx(int status, const char* format, va_list ap) {
    __kernel_verrx(status, format, ap);
}
