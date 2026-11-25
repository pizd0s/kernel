/*
 * hsearch's tables, LLVM libc's: an entry comes by its place and a table
 * with its size, as the kernel's structures do, the one layout each has
 * had.
 */
#include "sized.h"

#include <search.h>

ENTRY* __kernel_hsearch(const ENTRY* item, size_t size, ACTION action) {
    sized("hsearch", size, sizeof(ENTRY));

    return hsearch(*item, action);
}

int __kernel_hsearch_r(const ENTRY* item, size_t size, ACTION action, ENTRY** found, struct hsearch_data* table, size_t tableSize) {
    sized("hsearch_r", size, sizeof(ENTRY));
    sized("hsearch_r", tableSize, sizeof(struct hsearch_data));

    return hsearch_r(*item, action, found, table);
}

int __kernel_hcreate_r(size_t count, struct hsearch_data* table, size_t size) {
    sized("hcreate_r", size, sizeof(struct hsearch_data));

    return hcreate_r(count, table);
}

void __kernel_hdestroy_r(struct hsearch_data* table, size_t size) {
    sized("hdestroy_r", size, sizeof(struct hsearch_data));

    hdestroy_r(table);
}
