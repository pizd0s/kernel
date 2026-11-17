#pragma once

#include <__base.h>

__BEGIN_DECLS

int bcmp(const void*, const void*, size_t);
void bcopy(const void*, void*, size_t);
void bzero(void*, size_t);
char* index(const char*, int);
char* rindex(const char*, int);
int ffs(int);
int ffsl(long);
int ffsll(long long);
int strcasecmp(const char*, const char*);
int strncasecmp(const char*, const char*, size_t);

/*
 * Other names of string.h's memcmp, memmove and memset: the same imports,
 * compiled at each call.
 */
int __kernel_memcmp(const void*, const void*, size_t) __KERNEL(__kernel_memcmp);
void* __kernel_memmove(void*, const void*, size_t) __KERNEL(__kernel_memmove);
void* __kernel_memset(void*, int, size_t) __KERNEL(__kernel_memset);

__INLINE int bcmp(const void* __a, const void* __b, size_t __n) {
    return __kernel_memcmp(__a, __b, __n);
}

__INLINE void bcopy(const void* __s, void* __d, size_t __n) {
    __kernel_memmove(__d, __s, __n);
}

__INLINE void bzero(void* __d, size_t __n) {
    __kernel_memset(__d, 0, __n);
}

__END_DECLS
