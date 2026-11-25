#pragma once

#include "namespace.h"

/*
 * What FreeBSD's libc sources take of its libc_private.h, for the parts
 * libc.wasm builds (make.py):
 * - a process of lib/c's has one thread, so no stream and no list of
 *   streams is locked, and a thread has no cleanup handlers to push;
 * - exit calls __cleanup, which stdio sets, to flush the streams;
 * - _once is pthread_once;
 * - environ is the program's, which libc.wasm has no name for, but where
 *   lib/c's __program says it is (kernel.h).
 */
#include <pthread.h>

#include "../../c/src/kernel.h"

#define environ (*__program->environAt)

#define __isthreaded 0
#define _once(once, init) pthread_once(once, init)
extern void (*__cleanup)(void);
