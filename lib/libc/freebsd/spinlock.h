#pragma once

/* FreeBSD's spin locks of libc: none in a process of one thread. */
typedef struct {
    int unused;
} spinlock_t;

#define _SPINLOCK_INITIALIZER {0}
#define _SPINLOCK(lock) ((void)(lock))
#define _SPINUNLOCK(lock) ((void)(lock))
