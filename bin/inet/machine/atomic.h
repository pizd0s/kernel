/* wasm32: the stack runs on one thread, with no interrupts: FreeBSD's
 * atomic(9) are plain operations, its fences nothing, and the _acq and
 * _rel forms the plain ones. */
#ifndef _MACHINE_ATOMIC_H_
#define _MACHINE_ATOMIC_H_

#include <sys/atomic_common.h>

#define atomic_thread_fence_acq() ((void)0)
#define atomic_thread_fence_rel() ((void)0)
#define atomic_thread_fence_acq_rel() ((void)0)
#define atomic_thread_fence_seq_cst() ((void)0)
#define atomic_interrupt_fence() ((void)0)

/* The operations that have _acq and _rel forms, of a name w and a type t,
 * with an order's suffix o. */
#define ATOMIC_ORDERED(w, t, o)                                              \
    static __inline void atomic_add##o##_##w(volatile t* p, t v) {          \
        *p += v;                                                             \
    }                                                                        \
    static __inline void atomic_subtract##o##_##w(volatile t* p, t v) {     \
        *p -= v;                                                             \
    }                                                                        \
    static __inline void atomic_set##o##_##w(volatile t* p, t v) {          \
        *p |= v;                                                             \
    }                                                                        \
    static __inline void atomic_clear##o##_##w(volatile t* p, t v) {        \
        *p &= ~v;                                                            \
    }                                                                        \
    static __inline int atomic_cmpset##o##_##w(volatile t* p, t old, t new) { \
        if (*p != old)                                                       \
            return 0;                                                        \
        *p = new;                                                            \
        return 1;                                                            \
    }                                                                        \
    static __inline int atomic_fcmpset##o##_##w(volatile t* p, t* old, t new) { \
        if (*p != *old) {                                                    \
            *old = *p;                                                       \
            return 0;                                                        \
        }                                                                    \
        *p = new;                                                            \
        return 1;                                                            \
    }

/* Every operation of a name w and a type t of bits bits. */
#define ATOMIC_OPS(w, t, bits)                                               \
    ATOMIC_ORDERED(w, t, )                                                   \
    ATOMIC_ORDERED(w, t, _acq)                                               \
    ATOMIC_ORDERED(w, t, _rel)                                               \
    static __inline t atomic_fetchadd_##w(volatile t* p, t v) {             \
        t r = *p;                                                            \
        *p = r + v;                                                          \
        return r;                                                            \
    }                                                                        \
    static __inline t atomic_readandclear_##w(volatile t* p) {              \
        t r = *p;                                                            \
        *p = 0;                                                              \
        return r;                                                            \
    }                                                                        \
    static __inline t atomic_swap_##w(volatile t* p, t v) {                 \
        t r = *p;                                                            \
        *p = v;                                                              \
        return r;                                                            \
    }                                                                        \
    static __inline t atomic_load_acq_##w(const volatile t* p) {            \
        return *p;                                                           \
    }                                                                        \
    static __inline void atomic_store_rel_##w(volatile t* p, t v) {         \
        *p = v;                                                              \
    }                                                                        \
    static __inline int atomic_testandset_##w(volatile t* p, u_int bit) {   \
        t m = (t)1 << (bit % bits);                                          \
        int r = (*p & m) != 0;                                               \
        *p |= m;                                                             \
        return r;                                                            \
    }                                                                        \
    static __inline int atomic_testandset_acq_##w(volatile t* p, u_int bit) { \
        return atomic_testandset_##w(p, bit);                                \
    }                                                                        \
    static __inline int atomic_testandclear_##w(volatile t* p, u_int bit) { \
        t m = (t)1 << (bit % bits);                                          \
        int r = (*p & m) != 0;                                               \
        *p &= ~m;                                                            \
        return r;                                                            \
    }

ATOMIC_OPS(8, uint8_t, 8)
ATOMIC_OPS(16, uint16_t, 16)
ATOMIC_OPS(32, uint32_t, 32)
ATOMIC_OPS(64, uint64_t, 64)
ATOMIC_OPS(char, u_char, 8)
ATOMIC_OPS(short, u_short, 16)
ATOMIC_OPS(int, u_int, 32)
ATOMIC_OPS(long, u_long, 32)
ATOMIC_OPS(ptr, uintptr_t, 32)

static __inline uint64_t atomic_load_64(const volatile uint64_t* p) {
    return *p;
}

static __inline void atomic_store_64(volatile uint64_t* p, uint64_t v) {
    *p = v;
}

#endif
