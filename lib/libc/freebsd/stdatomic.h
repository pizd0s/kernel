#pragma once

/*
 * What FreeBSD's sources take of C11's stdatomic.h, which is the
 * compiler's and not among lib/c's headers (FreeBSD's quick_exit): the
 * compiler's builtins of C11's atomics, _Atomic being its keyword.
 */
#define memory_order_acquire __ATOMIC_ACQUIRE
#define atomic_load_explicit(object, order) __c11_atomic_load(object, order)
#define atomic_compare_exchange_strong(object, expected, desired) \
    __c11_atomic_compare_exchange_strong(object, expected, desired, __ATOMIC_SEQ_CST, __ATOMIC_SEQ_CST)
