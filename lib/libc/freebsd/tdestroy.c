/*
 * tdestroy, glibc's, which FreeBSD's tree functions have not: every node of
 * a tree tsearch made freed, the program's function given each key first.
 */
#define _SEARCH_PRIVATE

#include <search.h>
#include <stdlib.h>

void __kernel_tdestroy(void* root, void (*destroy)(void*)) {
    posix_tnode* node = root;

    if (!node) {
        return;
    }

    __kernel_tdestroy(node->llink, destroy);
    __kernel_tdestroy(node->rlink, destroy);
    destroy(node->key);
    free(node);
}
