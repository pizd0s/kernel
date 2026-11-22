#pragma once

/*
 * What FreeBSD's stdio takes of its atomics: a store with release order,
 * a plain one in a process of one thread.
 */
#define atomic_set_rel_ptr(p, v) (*(p) = (__typeof__(*(p)))(v))
