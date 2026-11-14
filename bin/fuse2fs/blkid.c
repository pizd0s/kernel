/*
 * What fuse2fs's journal code asks of libblkid, for a journal on a device
 * of its own: the system has no such device to find, so the cache is none
 * and no name is found. A file system with an external journal is refused
 * as fuse2fs refuses one whose journal it cannot open.
 */
#include <blkid/blkid.h>

#include <errno.h>
#include <stddef.h>

int blkid_get_cache(blkid_cache* cache, const char* filename) {
    (void)filename;
    *cache = NULL;
    return -ENODEV;
}

void blkid_put_cache(blkid_cache cache) {
    (void)cache;
}

char* blkid_get_devname(blkid_cache cache, const char* token, const char* value) {
    (void)cache;
    (void)token;
    (void)value;
    return NULL;
}

char* blkid_devno_to_devname(dev_t devno) {
    (void)devno;
    return NULL;
}
