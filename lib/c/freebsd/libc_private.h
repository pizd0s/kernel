#pragma once

#include "namespace.h"

/*
 * What FreeBSD's libc sources take of its libc_private.h, for the parts
 * lib/c builds (make.py):
 * - a process of lib/c's has one thread, so no stream and no list of
 *   streams is locked, and a thread has no cleanup handlers to push;
 * - exit calls __cleanup, which stdio sets, to flush the streams.
 */
#define __isthreaded 0
extern void (*__cleanup)(void);
