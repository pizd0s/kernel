#pragma once

/*
 * Bionic raises a signal by its system call, to keep a crash's stack
 * short; lib/c's raise is the kernel's.
 */
#include <signal.h>

static inline int inline_raise(int sig, void* value = nullptr) {
    (void)value;

    return raise(sig);
}
