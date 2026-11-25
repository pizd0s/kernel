#pragma once

/*
 * lib/c's unistd.h for FreeBSD's sources, with what FreeBSD's adds and
 * they take: crypt_r's data, libcrypt's.
 */
#include_next <unistd.h>

struct crypt_data {
    int initialized;
    char __buf[256];
};

char* crypt_r(const char*, const char*, struct crypt_data*);
