#pragma once

/*
 * A structure the kernel reads or writes comes with its size, which is its
 * version (lib/c's kernel.c): libc.wasm knows this lib/c's, and a process
 * that passes another is ended by SIGSYS, as the kernel's Go ends it, with
 * the same message on its standard error (start.c).
 */
#include <stddef.h>

_Noreturn void unsized(const char* call, size_t size, size_t known);

static inline void sized(const char* call, size_t size, size_t known) {
    if (size != known) {
        unsized(call, size, known);
    }
}
