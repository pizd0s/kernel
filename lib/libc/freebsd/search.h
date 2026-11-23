#pragma once

/*
 * lib/c's search.h, but for FreeBSD's tree functions, which ask by
 * _SEARCH_PRIVATE for the node they build, as FreeBSD's search.h gives it
 * them: the same functions over it, the key first, then the two subtrees
 * and the balance of an AVL tree.
 */
#ifdef _SEARCH_PRIVATE

#include <stddef.h>

__BEGIN_DECLS

typedef enum { preorder, postorder, endorder, leaf } VISIT;

typedef struct __posix_tnode {
    void* key;
    struct __posix_tnode *llink, *rlink;
    signed char balance;
} posix_tnode;

void* tdelete(const void* __restrict, posix_tnode** __restrict, int (*)(const void*, const void*));
posix_tnode* tfind(const void*, posix_tnode* const*, int (*)(const void*, const void*));
posix_tnode* tsearch(const void*, posix_tnode**, int (*)(const void*, const void*));
void twalk(const posix_tnode*, void (*)(const posix_tnode*, VISIT, int));
void tdestroy(void*, void (*)(void*));

__END_DECLS

#else

#include_next <search.h>

#endif
