#include <err.h>
#include <errno.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/*
 * BSD's messages, on stderr: the program's name, the message, and for
 * warn and err ": " and what errno named at the call; err and errx then
 * exit. The name is the one crt.c keeps.
 */
void vwarn(const char* format, va_list ap) {
    int error = errno;

    fprintf(stderr, "%s: ", program_invocation_short_name);
    if (format) {
        vfprintf(stderr, format, ap);
        fputs(": ", stderr);
    }
    fprintf(stderr, "%s\n", strerror(error));
}

void vwarnx(const char* format, va_list ap) {
    fprintf(stderr, "%s: ", program_invocation_short_name);
    if (format) {
        vfprintf(stderr, format, ap);
    }
    putc('\n', stderr);
}

void verr(int status, const char* format, va_list ap) {
    vwarn(format, ap);
    exit(status);
}

void verrx(int status, const char* format, va_list ap) {
    vwarnx(format, ap);
    exit(status);
}

void warn(const char* format, ...) {
    va_list ap;
    va_start(ap, format);
    vwarn(format, ap);
    va_end(ap);
}

void warnx(const char* format, ...) {
    va_list ap;
    va_start(ap, format);
    vwarnx(format, ap);
    va_end(ap);
}

void err(int status, const char* format, ...) {
    va_list ap;
    va_start(ap, format);
    verr(status, format, ap);
}

void errx(int status, const char* format, ...) {
    va_list ap;
    va_start(ap, format);
    verrx(status, format, ap);
}
