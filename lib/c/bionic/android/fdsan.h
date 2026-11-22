#pragma once

/*
 * fdsan, Android's check that a descriptor is closed by its owner, is not
 * lib/c's: an owner's tag is 0, and closing with one is close.
 */
#include <stdint.h>
#include <unistd.h>

enum android_fdsan_owner_type {
    ANDROID_FDSAN_OWNER_TYPE_FILE = 1,
};

static inline uint64_t android_fdsan_create_owner_tag(enum android_fdsan_owner_type type, uint64_t tag) {
    (void)type;
    (void)tag;

    return 0;
}

static inline void android_fdsan_exchange_owner_tag(int fd, uint64_t expected, uint64_t tag) {
    (void)fd;
    (void)expected;
    (void)tag;
}

static inline int android_fdsan_close_with_tag(int fd, uint64_t tag) {
    (void)tag;

    return close(fd);
}
