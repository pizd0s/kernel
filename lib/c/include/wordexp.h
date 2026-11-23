#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * wordexp, the kernel's: words checked for the characters and quoting
 * POSIX refuses, then expanded by /bin/sh as the arguments of a command,
 * a word each field of their splitting and pathname expansion.
 */
typedef struct {
    size_t we_wordc;
    char** we_wordv;
    size_t we_offs;
} wordexp_t;

#define WRDE_DOOFFS 1
#define WRDE_APPEND 2
#define WRDE_NOCMD 4
#define WRDE_REUSE 8
#define WRDE_SHOWERR 16
#define WRDE_UNDEF 32

#define WRDE_NOSPACE 1
#define WRDE_BADCHAR 2
#define WRDE_BADVAL 3
#define WRDE_CMDSUB 4
#define WRDE_SYNTAX 5

int wordexp(const char* __restrict, wordexp_t* __restrict, int);
void wordfree(wordexp_t*);

__END_DECLS
