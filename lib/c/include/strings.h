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
 * The kernel's functions the compiler may compile at a call of one, as
 * string.h's are: inline here, defined in mem.c.
 */
int __kernel_bcmp(const void*, const void*, size_t) __KERNEL(bcmp);
void __kernel_bcopy(const void*, void*, size_t) __KERNEL(bcopy);
void __kernel_bzero(void*, size_t) __KERNEL(bzero);
char* __kernel_index(const char*, int) __KERNEL(index);

__INLINE int bcmp(const void* __a, const void* __b, size_t __n) {
    return __kernel_bcmp(__a, __b, __n);
}

__INLINE void bcopy(const void* __s, void* __d, size_t __n) {
    __kernel_bcopy(__s, __d, __n);
}

__INLINE void bzero(void* __d, size_t __n) {
    __kernel_bzero(__d, __n);
}

__INLINE char* index(const char* __s, int __c) {
    return __kernel_index(__s, __c);
}

__END_DECLS
