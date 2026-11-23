#pragma once

/*
 * What libc.wasm's sources include first. errno is the process's, whose
 * address the kernel learns at its start: they reach it through the
 * kernel's __errno_location.
 */
#include <__base.h>
#include <errno.h>

int* __kernel___errno_location(void) __KERNEL(__errno_location);

#define errno (*__kernel___errno_location())
