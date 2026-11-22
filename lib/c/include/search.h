#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * Tables and trees of the program's own, searched by its functions:
 * lib/c's, musl's.
 */
typedef enum { FIND, ENTER } ACTION;
typedef enum { preorder, postorder, endorder, leaf } VISIT;

typedef struct entry {
    char* key;
    void* data;
} ENTRY;

int hcreate(size_t);
void hdestroy(void);
ENTRY* hsearch(ENTRY, ACTION);

// As LLVM libc has it, whose hcreate_r and the others lib/c's are: what
// they keep in __opaque.
struct hsearch_data {
    void* __opaque;
    unsigned int __reserved[2];
};

int hcreate_r(size_t, struct hsearch_data*);
void hdestroy_r(struct hsearch_data*);
int hsearch_r(ENTRY, ACTION, ENTRY**, struct hsearch_data*);

struct qelem {
    struct qelem *q_forw, *q_back;
    char q_data[1];
};

void insque(void*, void*);
void remque(void*);

void* lsearch(const void*, void*, size_t*, size_t, int (*)(const void*, const void*));
void* lfind(const void*, const void*, size_t*, size_t, int (*)(const void*, const void*));

void* tdelete(const void* __restrict, void** __restrict, int (*)(const void*, const void*));
void* tfind(const void*, void* const*, int (*)(const void*, const void*));
void* tsearch(const void*, void**, int (*)(const void*, const void*));
void twalk(const void*, void (*)(const void*, VISIT, int));
void tdestroy(void*, void (*)(void*));

__END_DECLS
