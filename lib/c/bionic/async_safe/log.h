#pragma once

/*
 * Bionic's fatal errors: the message on the standard error, after what
 * failed, then abort.
 */
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

static inline __attribute__((__noreturn__)) void async_safe_fatal_va_list(const char* prefix, const char* fmt, va_list args) {
    if (prefix) {
        dprintf(STDERR_FILENO, "%s: ", prefix);
    }

    vdprintf(STDERR_FILENO, fmt, args);
    dprintf(STDERR_FILENO, "\n");
    abort();
}

static inline __attribute__((__noreturn__, __format__(__printf__, 1, 2))) void async_safe_fatal(const char* fmt, ...) {
    va_list args;

    va_start(args, fmt);
    async_safe_fatal_va_list(0, fmt, args);
}
