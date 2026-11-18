#pragma once

#include <__base.h>

__BEGIN_DECLS

void* memcpy(void* __restrict, const void* __restrict, size_t);
void* memmove(void*, const void*, size_t);
void* memset(void*, int, size_t);
int memcmp(const void*, const void*, size_t);
void* memchr(const void*, int, size_t);
void* memrchr(const void*, int, size_t);
void* memmem(const void*, size_t, const void*, size_t);
void* mempcpy(void* __restrict, const void* __restrict, size_t);
void* memccpy(void* __restrict, const void* __restrict, int, size_t);
void* rawmemchr(const void*, int);

size_t strlen(const char*);
size_t strnlen(const char*, size_t);
char* strcpy(char* __restrict, const char* __restrict);
char* strncpy(char* __restrict, const char* __restrict, size_t);
char* stpcpy(char* __restrict, const char* __restrict);
char* stpncpy(char* __restrict, const char* __restrict, size_t);
char* strcat(char* __restrict, const char* __restrict);
char* strncat(char* __restrict, const char* __restrict, size_t);
size_t strlcpy(char*, const char*, size_t);
size_t strlcat(char*, const char*, size_t);

int strcmp(const char*, const char*);
int strncmp(const char*, const char*, size_t);
int strcoll(const char*, const char*);
size_t strxfrm(char* __restrict, const char* __restrict, size_t);
int strcoll_l(const char*, const char*, locale_t);
size_t strxfrm_l(char* __restrict, const char* __restrict, size_t, locale_t);
int strverscmp(const char*, const char*);

char* strchr(const char*, int);
char* strrchr(const char*, int);
char* strchrnul(const char*, int);
char* strstr(const char*, const char*);
char* strcasestr(const char*, const char*);
size_t strspn(const char*, const char*);
size_t strcspn(const char*, const char*);
char* strpbrk(const char*, const char*);
char* strtok(char* __restrict, const char* __restrict);
char* strtok_r(char* __restrict, const char* __restrict, char** __restrict);
char* strsep(char**, const char*);

char* strdup(const char*);
char* strndup(const char*, size_t);

char* strerror(int);
int strerror_r(int, char*, size_t);
char* strsignal(int);

void explicit_bzero(void*, size_t);

/*
 * These are the kernel's functions the compiler may compile at a call of
 * one, as the kernel would do them: the ranges and strings checked, a
 * trap past the memory's end, lib/str's functions called. Inline here, so
 * that a call is the import's call; mem.c defines them for a pointer and
 * a call without this header, and a pointer to the import itself calls
 * the kernel's.
 */
void* __kernel_memcpy(void* __restrict, const void* __restrict, size_t) __KERNEL(memcpy);
void* __kernel_memmove(void*, const void*, size_t) __KERNEL(memmove);
void* __kernel_memset(void*, int, size_t) __KERNEL(memset);
int __kernel_memcmp(const void*, const void*, size_t) __KERNEL(memcmp);
void* __kernel_memchr(const void*, int, size_t) __KERNEL(memchr);
void* __kernel_memmem(const void*, size_t, const void*, size_t) __KERNEL(memmem);
void* __kernel_mempcpy(void* __restrict, const void* __restrict, size_t) __KERNEL(mempcpy);
size_t __kernel_strlen(const char*) __KERNEL(strlen);
size_t __kernel_strnlen(const char*, size_t) __KERNEL(strnlen);
char* __kernel_strcpy(char* __restrict, const char* __restrict) __KERNEL(strcpy);
char* __kernel_strncpy(char* __restrict, const char* __restrict, size_t) __KERNEL(strncpy);
char* __kernel_stpcpy(char* __restrict, const char* __restrict) __KERNEL(stpcpy);
char* __kernel_stpncpy(char* __restrict, const char* __restrict, size_t) __KERNEL(stpncpy);
char* __kernel_strcat(char* __restrict, const char* __restrict) __KERNEL(strcat);
char* __kernel_strncat(char* __restrict, const char* __restrict, size_t) __KERNEL(strncat);
size_t __kernel_strlcpy(char*, const char*, size_t) __KERNEL(strlcpy);
size_t __kernel_strlcat(char*, const char*, size_t) __KERNEL(strlcat);
int __kernel_strcmp(const char*, const char*) __KERNEL(strcmp);
int __kernel_strncmp(const char*, const char*, size_t) __KERNEL(strncmp);
int __kernel_strcoll(const char*, const char*) __KERNEL(strcoll);
char* __kernel_strchr(const char*, int) __KERNEL(strchr);
char* __kernel_strchrnul(const char*, int) __KERNEL(strchrnul);
char* __kernel_strstr(const char*, const char*) __KERNEL(strstr);
size_t __kernel_strspn(const char*, const char*) __KERNEL(strspn);
size_t __kernel_strcspn(const char*, const char*) __KERNEL(strcspn);
char* __kernel_strpbrk(const char*, const char*) __KERNEL(strpbrk);
char* __kernel_strdup(const char*) __KERNEL(strdup);
void __kernel_explicit_bzero(void*, size_t) __KERNEL(explicit_bzero);

__INLINE void* memcpy(void* __restrict __d, const void* __restrict __s, size_t __n) {
    return __kernel_memcpy(__d, __s, __n);
}

__INLINE void* memmove(void* __d, const void* __s, size_t __n) {
    return __kernel_memmove(__d, __s, __n);
}

__INLINE void* memset(void* __d, int __c, size_t __n) {
    return __kernel_memset(__d, __c, __n);
}

__INLINE int memcmp(const void* __a, const void* __b, size_t __n) {
    return __kernel_memcmp(__a, __b, __n);
}

__INLINE void* memchr(const void* __s, int __c, size_t __n) {
    return __kernel_memchr(__s, __c, __n);
}

__INLINE void* memmem(const void* __h, size_t __hl, const void* __n, size_t __nl) {
    return __kernel_memmem(__h, __hl, __n, __nl);
}

__INLINE void* mempcpy(void* __restrict __d, const void* __restrict __s, size_t __n) {
    return __kernel_mempcpy(__d, __s, __n);
}

__INLINE size_t strlen(const char* __s) {
    return __kernel_strlen(__s);
}

__INLINE size_t strnlen(const char* __s, size_t __n) {
    return __kernel_strnlen(__s, __n);
}

__INLINE char* strcpy(char* __restrict __d, const char* __restrict __s) {
    return __kernel_strcpy(__d, __s);
}

__INLINE char* strncpy(char* __restrict __d, const char* __restrict __s, size_t __n) {
    return __kernel_strncpy(__d, __s, __n);
}

__INLINE char* stpcpy(char* __restrict __d, const char* __restrict __s) {
    return __kernel_stpcpy(__d, __s);
}

__INLINE char* stpncpy(char* __restrict __d, const char* __restrict __s, size_t __n) {
    return __kernel_stpncpy(__d, __s, __n);
}

__INLINE char* strcat(char* __restrict __d, const char* __restrict __s) {
    return __kernel_strcat(__d, __s);
}

__INLINE char* strncat(char* __restrict __d, const char* __restrict __s, size_t __n) {
    return __kernel_strncat(__d, __s, __n);
}

__INLINE size_t strlcpy(char* __d, const char* __s, size_t __n) {
    return __kernel_strlcpy(__d, __s, __n);
}

__INLINE size_t strlcat(char* __d, const char* __s, size_t __n) {
    return __kernel_strlcat(__d, __s, __n);
}

__INLINE int strcmp(const char* __a, const char* __b) {
    return __kernel_strcmp(__a, __b);
}

__INLINE int strncmp(const char* __a, const char* __b, size_t __n) {
    return __kernel_strncmp(__a, __b, __n);
}

__INLINE int strcoll(const char* __a, const char* __b) {
    return __kernel_strcoll(__a, __b);
}

__INLINE char* strchr(const char* __s, int __c) {
    return __kernel_strchr(__s, __c);
}

__INLINE char* strchrnul(const char* __s, int __c) {
    return __kernel_strchrnul(__s, __c);
}

__INLINE char* strstr(const char* __h, const char* __n) {
    return __kernel_strstr(__h, __n);
}

__INLINE size_t strspn(const char* __s, const char* __set) {
    return __kernel_strspn(__s, __set);
}

__INLINE size_t strcspn(const char* __s, const char* __set) {
    return __kernel_strcspn(__s, __set);
}

__INLINE char* strpbrk(const char* __s, const char* __set) {
    return __kernel_strpbrk(__s, __set);
}

__INLINE char* strdup(const char* __s) {
    return __kernel_strdup(__s);
}

__INLINE void explicit_bzero(void* __d, size_t __n) {
    __kernel_explicit_bzero(__d, __n);
}

__END_DECLS

#include <strings.h>
