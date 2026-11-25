/*
 * scandir's orders of entries: alphasort's is strcoll's of their names,
 * versionsort's strverscmp's.
 */
#include "sized.h"

#include <dirent.h>
#include <string.h>

int __kernel_alphasort(const struct dirent** a, size_t aSize, const struct dirent** b, size_t bSize) {
    sized("alphasort", aSize, sizeof(struct dirent));
    sized("alphasort", bSize, sizeof(struct dirent));

    return strcoll((*a)->d_name, (*b)->d_name);
}

int __kernel_versionsort(const struct dirent** a, size_t aSize, const struct dirent** b, size_t bSize) {
    sized("versionsort", aSize, sizeof(struct dirent));
    sized("versionsort", bSize, sizeof(struct dirent));

    return strverscmp((*a)->d_name, (*b)->d_name);
}
