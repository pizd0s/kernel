/*
 * The kernel's functions a program on lib/c/lite calls, each a call of
 * the kernel's import of the same name, __kernel_<name> here, and nothing
 * else, as lib/c's (src/kernel.c). Each is the kernel's Go: a program of
 * no libc.wasm has no other. The forwarders are weak, as lib/c's.
 */
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define WEAK __attribute__((__weak__))

#define KERNEL(ret, name, params, args) \
    ret __kernel_##name params __KERNEL(name); \
    WEAK ret name params { \
        return __kernel_##name args; \
    }

#define KERNEL_VOID(name, params, args) \
    void __kernel_##name params __KERNEL(name); \
    WEAK void name params { \
        __kernel_##name args; \
    }

#define KERNEL_NORETURN(name, params, args) \
    __NORETURN void __kernel_##name params __KERNEL(name); \
    WEAK __NORETURN void name params { \
        __kernel_##name args; \
    }

/* errno.h */
KERNEL(int*, __errno_location, (void), ())

/* stdlib.h */
KERNEL(void*, malloc, (size_t a), (a))
KERNEL_VOID(free, (void* a), (a))

/* string.h */
KERNEL(void*, memcpy, (void* __restrict a, const void* __restrict b, size_t c), (a, b, c))
KERNEL(void*, memmove, (void* a, const void* b, size_t c), (a, b, c))
KERNEL(void*, memset, (void* a, int b, size_t c), (a, b, c))

/* unistd.h */
KERNEL(ssize_t, read, (int a, void* b, size_t c), (a, b, c))
KERNEL(ssize_t, write, (int a, const void* b, size_t c), (a, b, c))
KERNEL_NORETURN(_exit, (int a), (a))
