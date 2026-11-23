#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * Tables and trees of the program's own, searched by its functions. A
 * tree's node is posix_tnode, void as POSIX.1-2024 has it: a pointer to
 * one points at a pointer to its key.
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

// LLVM libc's hcreate_r and the others keep their table in __opaque
// (lib/libc).
struct hsearch_data {
    void* __opaque;
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

typedef void posix_tnode;

void* tdelete(const void* __restrict, posix_tnode** __restrict, int (*)(const void*, const void*));
posix_tnode* tfind(const void*, posix_tnode* const*, int (*)(const void*, const void*));
posix_tnode* tsearch(const void*, posix_tnode**, int (*)(const void*, const void*));
void twalk(const posix_tnode*, void (*)(const posix_tnode*, VISIT, int));
void tdestroy(void*, void (*)(void*));

__END_DECLS
