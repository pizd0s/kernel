#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * The XSI interprocess communication of the kernel's: message queues and
 * semaphore sets of keys, each set of keys its own (sys/msg.h, sys/sem.h).
 * There is no shared memory between processes.
 */
struct ipc_perm {
    key_t __key;
    uid_t uid;
    gid_t gid;
    uid_t cuid;
    gid_t cgid;
    mode_t mode;
    int __seq;
};

#define IPC_CREAT 01000
#define IPC_EXCL 02000
#define IPC_NOWAIT 04000

#define IPC_PRIVATE ((key_t)0)

#define IPC_RMID 0
#define IPC_SET 1
#define IPC_STAT 2

key_t ftok(const char*, int);

__END_DECLS
