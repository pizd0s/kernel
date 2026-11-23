#pragma once

/*
 * What libc.wasm's sources include first. errno is the process's, whose
 * address the kernel learns at its start: they reach it through the
 * kernel's __errno_location. A kernel function that takes the size of a
 * structure of lib/c's, as lib/c's forwarder gives it, is called of
 * another size by a program of other headers: sized has the kernel end the
 * process then, as its own functions do.
 */
#include <__base.h>
#include <errno.h>

int* __kernel___errno_location(void) __KERNEL(__errno_location);
void __kernel___sized(const char* call, size_t have, size_t want) __KERNEL(__sized);

#define errno (*__kernel___errno_location())

static inline void sized(const char* call, size_t have, size_t want) {
    if (have != want) {
        __kernel___sized(call, have, want);
    }
}
